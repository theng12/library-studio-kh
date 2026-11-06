package companioninstall

import (
	"bytes"
	"context"
	"debug/macho"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type bundleInfo struct {
	Identifier string
	Version    string
	Executable string
	Path       string
}

// Detect reports what is already installed for id under appDir. It performs no
// writes and never opens the app.
func (i *Installer) Detect(ctx context.Context, id AppID, appDir string) (DetectResult, error) {
	spec, err := i.spec(id)
	if err != nil {
		return DetectResult{}, err
	}
	dir, err := cleanDestination(appDir)
	if err != nil {
		return DetectResult{}, err
	}
	return i.detect(ctx, spec, dir)
}

func (i *Installer) detect(ctx context.Context, spec appSpec, appDir string) (DetectResult, error) {
	result := DetectResult{AppID: spec.app.ID, State: DetectMissing}
	info, err := os.Stat(appDir)
	if errors.Is(err, fs.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if !info.IsDir() {
		return result, fmt.Errorf("companioninstall: destination %q is not a folder", appDir)
	}
	bundles, err := listBundles(appDir)
	if err != nil {
		return result, err
	}

	var matches []bundleInfo
	var keep *DetectResult
	for _, path := range bundles {
		found, err := i.readBundle(ctx, path)
		if err != nil {
			if sameBundleName(path, spec.bundleName) {
				keep = &DetectResult{Path: path, State: DetectKeep, BundleID: found.Identifier, Version: found.Version}
			}
			continue
		}
		if found.Identifier != spec.app.BundleID {
			if sameBundleName(path, spec.bundleName) {
				keep = &DetectResult{Path: path, State: DetectKeep, BundleID: found.Identifier, Version: found.Version}
			}
			continue
		}
		if err := checkArchitecture(found.Executable, spec); err != nil {
			keep = &DetectResult{Path: path, State: DetectKeep, BundleID: found.Identifier, Version: found.Version}
			continue
		}
		matches = append(matches, found)
	}

	switch {
	case len(matches) == 1 && keep == nil:
		return DetectResult{
			AppID:    spec.app.ID,
			Path:     matches[0].Path,
			Version:  matches[0].Version,
			BundleID: matches[0].Identifier,
			State:    DetectAdopt,
		}, nil
	case len(matches) > 0 || keep != nil:
		if keep == nil {
			keep = &DetectResult{
				Path:     matches[0].Path,
				Version:  matches[0].Version,
				BundleID: matches[0].Identifier,
			}
		}
		keep.AppID = spec.app.ID
		keep.State = DetectKeep
		return *keep, nil
	default:
		return result, nil
	}
}

// Install verifies and copies pkg into appDir. Existing bundles are adopted or
// kept, never overwritten, downgraded or replaced by a different app.
func (i *Installer) Install(ctx context.Context, pkg Package, appDir string) (InstallResult, error) {
	spec, err := i.spec(pkg.AppID)
	if err != nil {
		return InstallResult{}, err
	}
	dir, err := cleanDestination(appDir)
	if err != nil {
		return InstallResult{}, err
	}
	if err := validatePackage(pkg, spec); err != nil {
		return InstallResult{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return InstallResult{}, err
	}

	existing, err := i.detect(ctx, spec, dir)
	if err != nil {
		return InstallResult{}, err
	}
	switch existing.State {
	case DetectAdopt:
		return InstallResult{
			AppID:    pkg.AppID,
			Path:     existing.Path,
			Version:  existing.Version,
			BundleID: existing.BundleID,
			Existing: true,
			State:    Adopted,
		}, nil
	case DetectKeep:
		return InstallResult{
			AppID:    pkg.AppID,
			Path:     existing.Path,
			Version:  existing.Version,
			BundleID: existing.BundleID,
			Existing: true,
			State:    Kept,
		}, fmt.Errorf("%w: %s", ErrBundleConflict, existing.Path)
	}

	stage, err := os.MkdirTemp(dir, ".library-studio-stage-"+string(pkg.AppID)+"-")
	if err != nil {
		return InstallResult{}, err
	}
	installed := false
	defer func() {
		if installed {
			os.RemoveAll(stage)
			return
		}
		i.moveStageToTrash(dir, pkg.AppID, stage)
	}()

	asset, err := i.download(ctx, pkg, stage)
	if err != nil {
		return InstallResult{}, err
	}
	var bundlePath string
	switch spec.kind {
	case qbittorrentSource:
		bundlePath, err = i.extractDMG(ctx, asset, stage)
	default:
		bundlePath, err = extractZipBundle(asset, stage, zipExpansionCap(pkg.SizeCap))
	}
	if err != nil {
		return InstallResult{}, err
	}

	found, err := i.readBundle(ctx, bundlePath)
	if err != nil {
		return InstallResult{}, err
	}
	if found.Identifier != pkg.BundleID {
		return InstallResult{}, fmt.Errorf("%w: downloaded bundle identifies as %q, expected %q", ErrBundleConflict, found.Identifier, pkg.BundleID)
	}
	if err := checkArchitecture(found.Executable, spec); err != nil {
		return InstallResult{}, err
	}

	dest := filepath.Join(dir, filepath.Base(bundlePath))
	if _, err := os.Lstat(dest); err == nil {
		return InstallResult{AppID: pkg.AppID, Path: dest, Existing: true, State: Kept}, fmt.Errorf("%w: %s", ErrBundleConflict, dest)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return InstallResult{}, err
	}
	// shortcut: a concurrent process creating dest between Lstat and Rename
	// could be replaced; this is a single-user installer. Upgrade to a
	// no-replace rename syscall if concurrent installers ever become possible.
	if err := os.Rename(bundlePath, dest); err != nil {
		return InstallResult{}, err
	}
	installed = true

	result := InstallResult{
		AppID:               pkg.AppID,
		Path:                dest,
		Version:             found.Version,
		BundleID:            found.Identifier,
		Existing:            false,
		State:               Installed,
		OwnerApprovalNeeded: true,
	}
	receiptPath, err := i.writeReceipt(dir, pkg, dest, found)
	result.ReceiptPath = receiptPath
	if err != nil {
		return result, fmt.Errorf("companioninstall: installed %s but could not write its receipt: %w", dest, err)
	}
	return result, nil
}

func listBundles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		if strings.HasSuffix(strings.ToLower(entry.Name()), ".app") {
			out = append(out, filepath.Join(dir, entry.Name()))
		}
	}
	return out, nil
}

func sameBundleName(path, expected string) bool {
	return strings.EqualFold(filepath.Base(path), expected)
}

func (i *Installer) readBundle(ctx context.Context, path string) (bundleInfo, error) {
	info := bundleInfo{Path: path}
	st, err := os.Stat(path)
	if err != nil {
		return info, err
	}
	if !st.IsDir() {
		return info, fmt.Errorf("%q is not an app bundle folder", path)
	}
	plist := filepath.Join(path, "Contents", "Info.plist")
	pst, err := os.Stat(plist)
	if err != nil {
		return info, fmt.Errorf("%q has no readable Contents/Info.plist: %w", path, err)
	}
	if !pst.Mode().IsRegular() {
		return info, fmt.Errorf("%q has no regular Contents/Info.plist", path)
	}
	out, err := i.executeCommand(ctx, i.plutil, "-convert", "json", "-o", "-", plist)
	if err != nil {
		return info, fmt.Errorf("read %q: %w", plist, err)
	}
	var raw struct {
		Identifier string `json:"CFBundleIdentifier"`
		Executable string `json:"CFBundleExecutable"`
		Short      string `json:"CFBundleShortVersionString"`
		Bundle     string `json:"CFBundleVersion"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return info, fmt.Errorf("read %q: invalid property list JSON: %w", plist, err)
	}
	info.Identifier = strings.TrimSpace(raw.Identifier)
	if info.Identifier == "" {
		return info, fmt.Errorf("%q has no CFBundleIdentifier", path)
	}
	exe := strings.TrimSpace(raw.Executable)
	if exe == "" || exe == "." || exe == ".." || strings.ContainsAny(exe, `/\`) {
		return info, fmt.Errorf("%q has no safe CFBundleExecutable", path)
	}
	exePath := filepath.Join(path, "Contents", "MacOS", exe)
	resolved, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		return info, fmt.Errorf("%q: executable %q is missing: %w", path, exe, err)
	}
	bundleRoot, err := filepath.EvalSymlinks(path)
	if err != nil {
		return info, err
	}
	rel, err := filepath.Rel(bundleRoot, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return info, fmt.Errorf("%q: executable resolves outside the bundle", path)
	}
	est, err := os.Stat(resolved)
	if err != nil {
		return info, err
	}
	if !est.Mode().IsRegular() || est.Mode().Perm()&0o111 == 0 {
		return info, fmt.Errorf("%q: executable %q is not an executable file", path, exe)
	}
	info.Executable = resolved
	info.Version = strings.TrimSpace(raw.Short)
	if info.Version == "" {
		info.Version = strings.TrimSpace(raw.Bundle)
	}
	if info.Version == "" {
		return info, fmt.Errorf("%q has no bundle version", path)
	}
	return info, nil
}

func checkArchitecture(executable string, spec appSpec) error {
	var cpu macho.Cpu
	switch defaultArm64Label {
	case "arm64":
		cpu = macho.CpuArm64
	default:
		return fmt.Errorf("companioninstall: unsupported architecture %q", defaultArm64Label)
	}
	if file, err := macho.Open(executable); err == nil {
		defer file.Close()
		if file.Cpu != cpu {
			return fmt.Errorf("companioninstall: %s executable is %s, expected %s",
				spec.app.Name, cpuName(file.Cpu), defaultArm64Label)
		}
		return nil
	}
	if fat, err := macho.OpenFat(executable); err == nil {
		defer fat.Close()
		for _, arch := range fat.Arches {
			if arch.Cpu == cpu {
				return nil
			}
		}
		return fmt.Errorf("companioninstall: %s executable does not contain %s", spec.app.Name, defaultArm64Label)
	}
	// Not a Mach-O binary; the architecture expectation does not apply.
	return nil
}

func cpuName(cpu macho.Cpu) string {
	switch cpu {
	case macho.CpuArm64:
		return "arm64"
	case macho.CpuAmd64:
		return "x86_64"
	default:
		return fmt.Sprintf("cpu-%d", cpu)
	}
}

func (i *Installer) writeReceipt(appDir string, pkg Package, installedPath string, info bundleInfo) (string, error) {
	dir := filepath.Join(appDir, receiptDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, string(pkg.AppID)+".json")
	receipt := Receipt{
		Package:             pkg,
		InstalledPath:       installedPath,
		InstalledVersion:    info.Version,
		InstalledBundleID:   info.Identifier,
		InstalledAt:         i.now().UTC(),
		Installer:           installerName,
		OwnerApprovalNeeded: true,
		QuarantinePreserved: true,
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(receipt); err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o444)
	if errors.Is(err, fs.ErrExist) {
		// Receipts are immutable; keep the first one.
		return path, nil
	}
	if err != nil {
		return "", err
	}
	_, writeErr := f.Write(buf.Bytes())
	closeErr := f.Close()
	if writeErr != nil {
		return path, writeErr
	}
	return path, closeErr
}

func (i *Installer) moveStageToTrash(appDir string, id AppID, stage string) {
	dir := filepath.Join(appDir, trashDirName, string(id))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	name := fmt.Sprintf("%s-%d", filepath.Base(stage), i.now().UTC().UnixNano())
	_ = os.Rename(stage, filepath.Join(dir, name))
}

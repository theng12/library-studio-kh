package companioninstall

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// zipExpansionCap allows a vendor app zip to expand beyond its compressed
// download size, while still bounding a zip bomb. Published Servarr zips are
// roughly 2.5x larger unpacked.
func zipExpansionCap(downloadCap int64) int64 {
	const factor = 4
	if downloadCap > (1<<62)/factor {
		return 1 << 62
	}
	return downloadCap * factor
}

// extractZipBundle unpacks a vendor app zip into stage and returns its single
// top-level .app bundle.
func extractZipBundle(asset, stage string, sizeCap int64) (string, error) {
	root := filepath.Join(stage, "extract")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	if err := unzipTo(asset, root, sizeCap); err != nil {
		return "", err
	}
	bundles, err := topLevelBundles(root)
	if err != nil {
		return "", err
	}
	if len(bundles) != 1 {
		return "", fmt.Errorf("companioninstall: official zip contains %d app bundles, expected exactly one", len(bundles))
	}
	return bundles[0], nil
}

// unzipTo writes only regular files and folders inside root. Absolute paths,
// parent traversal, backslash names, symlinks and special files are rejected.
func unzipTo(asset, root string, sizeCap int64) error {
	reader, err := zip.OpenReader(asset)
	if err != nil {
		return fmt.Errorf("companioninstall: open official zip: %w", err)
	}
	defer reader.Close()

	var total int64
	for _, entry := range reader.File {
		name := entry.Name
		if strings.ContainsRune(name, 0) || strings.Contains(name, `\`) {
			return fmt.Errorf("companioninstall: unsafe zip entry %q", name)
		}
		dest, err := safeJoin(root, name)
		if err != nil {
			return err
		}
		mode := entry.Mode()
		switch {
		case mode&os.ModeSymlink != 0:
			return fmt.Errorf("companioninstall: refusing symlink in official zip: %q", name)
		case entry.FileInfo().IsDir() || strings.HasSuffix(name, "/"):
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
			continue
		case !mode.IsRegular():
			return fmt.Errorf("companioninstall: refusing special file in official zip: %q", name)
		}

		size := int64(entry.UncompressedSize64)
		if size < 0 || total > sizeCap-size {
			return fmt.Errorf("companioninstall: official zip expands beyond the %d byte cap", sizeCap)
		}
		total += size
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		rc, err := entry.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			rc.Close()
			return err
		}
		written, copyErr := io.Copy(out, io.LimitReader(rc, size+1))
		rc.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if written != size {
			return fmt.Errorf("companioninstall: zip entry %q declared %d bytes but produced %d", name, size, written)
		}
		perm := mode.Perm()
		if perm == 0 {
			perm = 0o644
		}
		if err := os.Chmod(dest, perm); err != nil {
			return err
		}
	}
	return nil
}

func safeJoin(root, name string) (string, error) {
	if name == "" {
		return "", errors.New("companioninstall: empty archive entry name")
	}
	if path.IsAbs(name) {
		return "", fmt.Errorf("companioninstall: absolute archive entry %q", name)
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("companioninstall: archive entry escapes the destination: %q", name)
	}
	dest := filepath.Join(root, filepath.FromSlash(clean))
	rel, err := filepath.Rel(root, dest)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("companioninstall: archive entry escapes the destination: %q", name)
	}
	return dest, nil
}

func topLevelBundles(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".app") {
			continue
		}
		full := filepath.Join(root, entry.Name())
		info, err := os.Stat(full)
		if err != nil || !info.IsDir() {
			continue
		}
		out = append(out, full)
	}
	return out, nil
}

// extractBinaryArchive unpacks the vendor's xz tar with the system bsdtar and
// returns the single top-level regular file named after the spec. bsdtar
// fails closed on ".." entries, strips absolute paths and refuses writes
// through symlinks; the walk below re-checks what actually landed: no
// symlinks, no special files, one executable of the expected name, bounded
// total size. shortcut: the tar is read straight to disk, so a huge
// vendor archive fills space before the size walk; the vendor digest gates
// that and the cap still fails closed afterwards.
func (i *Installer) extractBinaryArchive(ctx context.Context, asset, stage string, spec appSpec, downloadCap int64) (string, error) {
	root := filepath.Join(stage, "extract")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	if _, err := i.executeCommand(ctx, i.tar, "-xJf", asset, "-C", root); err != nil {
		return "", fmt.Errorf("companioninstall: extract official archive: %w", err)
	}
	target := filepath.Join(root, spec.bundleName)
	sizeCap := zipExpansionCap(downloadCap)
	var total int64
	found := false
	walkErr := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("companioninstall: archive entry escapes the staging folder: %q", rel)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("companioninstall: refusing symlink in official archive: %q", rel)
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("companioninstall: refusing special file in official archive: %q", rel)
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		if info.Size() > sizeCap-total {
			return fmt.Errorf("companioninstall: official archive expands beyond the %d byte cap", sizeCap)
		}
		total += info.Size()
		if p == target {
			found = true
			if info.Mode().Perm()&0o111 == 0 {
				return fmt.Errorf("companioninstall: official archive's %q is not executable", spec.bundleName)
			}
		}
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	if !found {
		return "", fmt.Errorf("companioninstall: official archive has no top-level executable %q", spec.bundleName)
	}
	return target, nil
}

// extractDMG mounts the official disk image read-only, copies the single app
// bundle with ditto, and always attempts to detach the image. It never clears
// quarantine or any other extended attribute.
func (i *Installer) extractDMG(ctx context.Context, asset, stage string) (string, error) {
	mount := filepath.Join(stage, "mnt")
	if err := os.MkdirAll(mount, 0o700); err != nil {
		return "", err
	}
	if _, err := i.executeCommand(ctx, i.hdiutil, "attach", "-nobrowse", "-readonly", "-mountpoint", mount, asset); err != nil {
		return "", fmt.Errorf("companioninstall: mount official DMG: %w", err)
	}
	detach := func() error {
		out, err := i.executeCommand(context.WithoutCancel(ctx), i.hdiutil, "detach", mount)
		if err != nil {
			return fmt.Errorf("companioninstall: detach %s: %w (%s)", mount, err, strings.TrimSpace(string(out)))
		}
		return nil
	}

	source, findErr := singleBundle(mount)
	if findErr != nil {
		_ = detach()
		return "", findErr
	}
	dest := filepath.Join(stage, "bundle", filepath.Base(source))
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		_ = detach()
		return "", err
	}
	if _, err := i.executeCommand(ctx, i.ditto, source, dest); err != nil {
		_ = detach()
		return "", fmt.Errorf("companioninstall: copy %s from the DMG: %w", filepath.Base(source), err)
	}
	if err := detach(); err != nil {
		return "", err
	}
	return dest, nil
}

func singleBundle(root string) (string, error) {
	bundles, err := topLevelBundles(root)
	if err != nil {
		return "", err
	}
	if len(bundles) != 1 {
		return "", fmt.Errorf("companioninstall: official disk image contains %d app bundles, expected exactly one", len(bundles))
	}
	return bundles[0], nil
}

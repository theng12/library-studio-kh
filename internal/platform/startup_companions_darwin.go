//go:build darwin

package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const openCommand = "/usr/bin/open"

func companionBlocked(spec CompanionServiceSpec, detail string) CompanionServiceStatus {
	return CompanionServiceStatus{
		Supported: true,
		State:     HostServiceBlocked,
		ID:        spec.ID,
		Label:     CompanionServiceLabel(spec.ID),
		Detail:    detail,
	}
}

func companionNotInstalled(spec CompanionServiceSpec, detail string) CompanionServiceStatus {
	return CompanionServiceStatus{
		Supported: true,
		State:     HostServiceNotInstalled,
		ID:        spec.ID,
		Label:     CompanionServiceLabel(spec.ID),
		Detail:    detail,
	}
}

func (s *CompanionService) plistPath(label string) (string, error) {
	dir, err := launchAgentFolder(s.agentDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, label+".plist"), nil
}

func (s *CompanionService) isLoaded(label string) bool {
	_, err := s.runner.Run("launchctl", "print", serviceDomain()+"/"+label)
	return err == nil
}

// Status only reports Library's own job. An external stock-app login item is
// never inspected, adopted or changed.
func (s *CompanionService) Status(spec CompanionServiceSpec) CompanionServiceStatus {
	if _, ok := companionApps[spec.ID]; !ok {
		return companionBlocked(spec, "Unknown companion; only Library's five stock apps are supported.")
	}
	if !filepath.IsAbs(spec.Home) {
		return companionBlocked(spec, "The Library configuration folder must be absolute.")
	}
	label := CompanionServiceLabel(spec.ID)
	path, err := s.plistPath(label)
	if err != nil {
		return companionBlocked(spec, "The login items folder could not be found.")
	}
	ours, exists, err := inspectLabeledPlist(path, label)
	if err != nil {
		return companionBlocked(spec, err.Error())
	}
	if !exists {
		return companionNotInstalled(spec, "At-login startup is off.")
	}
	if !ours {
		return companionBlocked(spec, "Another login job uses this Library companion name; refusing to change it.")
	}
	if err := checkJobHome(path, spec.Home); err != nil {
		return companionBlocked(spec, err.Error())
	}
	st := CompanionServiceStatus{
		Supported: true,
		State:     HostServiceInstalled,
		ID:        spec.ID,
		Label:     label,
		Installed: true,
	}
	if !s.isLoaded(label) {
		st.Detail = "At-login startup is installed but not loaded."
		return st
	}
	st.Loaded = true
	st.State = HostServiceLoaded
	if spec.AppPath != "" && spec.ProfileDir != "" {
		app := companionApps[spec.ID]
		clean := spec
		clean.Home = filepath.Clean(clean.Home)
		current, err := os.ReadFile(path)
		if err != nil || string(current) != companionPlist(clean, app, label) {
			st.State = HostServiceNeedsRestart
			st.Detail = "The saved at-login job does not match these settings. Reinstall at-login startup."
			return st
		}
	}
	if !loadedConfigMatches(path) {
		st.State = HostServiceNeedsRestart
		st.Detail = "The loaded at-login job does not match the saved settings. Reinstall at-login startup."
		return st
	}
	if err := s.validateDependency(spec); err != nil {
		st.State = HostServiceBlocked
		st.Detail = "At-login startup cannot be verified: " + err.Error()
		return st
	}
	st.Detail = "At-login startup is installed and loaded."
	return st
}

func containsControl(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// ownedDirInfo rejects symlinks and folders owned by another user.
func ownedDirInfo(path string) (os.FileInfo, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, errors.New("the Library companion folder is not ready yet")
	}
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("the Library companion folder must be a real directory, not a symlink")
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Getuid()) {
		return nil, errors.New("the Library companion folder must belong to this user")
	}
	return fi, nil
}

func requirePrivateDir(path string) error {
	fi, err := ownedDirInfo(path)
	if err != nil {
		return err
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return errors.New("the Library companion profile must be private (chmod 700)")
	}
	return nil
}

// companionProfileReady proves the owner completed the app's first run and the
// profile is one Library created, not a stock profile being adopted.
func companionProfileReady(id CompanionID, dir string) error {
	path := filepath.Join(dir, "config.xml")
	if id == CompanionQBittorrent {
		path = filepath.Join(dir, "qBittorrent", "config", "qBittorrent.ini")
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return errors.New("complete the app's first run and connect it before enabling at-login startup")
	}
	if !fi.Mode().IsRegular() || fi.Size() == 0 {
		return errors.New("the managed companion configuration is incomplete; finish its first run first")
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return errors.New("the managed companion configuration must be private (chmod 600)")
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); !ok || st.Uid != uint32(os.Getuid()) {
		return errors.New("the managed companion configuration must belong to this user")
	}
	return nil
}

func bundleString(runner CommandRunner, infoPath, key string) (string, error) {
	out, err := runner.Run("plutil", "-extract", key, "raw", "-o", "-", infoPath)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(out))
	if value == "" {
		return "", errors.New("missing value")
	}
	return value, nil
}

func validateCompanionApp(runner CommandRunner, id CompanionID, app companionApp, path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("the app path must be absolute")
	}
	path = filepath.Clean(path)
	if filepath.Base(path) != app.bundle {
		return fmt.Errorf("choose the %s app bundle", app.bundle)
	}
	fi, err := os.Lstat(path)
	if err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("the %s app must be an existing .app bundle", app.bundle)
	}
	info := filepath.Join(path, "Contents", "Info.plist")
	fi, err = os.Lstat(info)
	if err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("the %s app bundle is incomplete", app.bundle)
	}
	want := companionBundleID(id)
	if want == "" {
		return fmt.Errorf("the %s app has no registered identity", app.bundle)
	}
	got, err := bundleString(runner, info, "CFBundleIdentifier")
	if err != nil || got != want {
		return fmt.Errorf("the %s app is not the expected bundle", app.bundle)
	}
	exe, err := bundleString(runner, info, "CFBundleExecutable")
	if err != nil || strings.ContainsAny(exe, `/\`) || exe == "." || exe == ".." {
		return fmt.Errorf("the %s app has no usable executable", app.bundle)
	}
	exeInfo, err := os.Lstat(filepath.Join(path, "Contents", "MacOS", exe))
	if err != nil || !exeInfo.Mode().IsRegular() || exeInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("the %s app executable is missing", app.bundle)
	}
	return nil
}

func (s *CompanionService) validateDependency(spec CompanionServiceSpec) error {
	app, ok := companionApps[spec.ID]
	if !ok {
		return errors.New("unknown companion")
	}
	if !filepath.IsAbs(spec.Home) || spec.AppPath == "" || spec.ProfileDir == "" {
		return errors.New("the companion app and managed profile are needed to verify at-login startup")
	}
	home := filepath.Clean(spec.Home)
	if _, err := ownedDirInfo(home); err != nil {
		return err
	}
	if _, err := ownedDirInfo(filepath.Join(home, "companions")); err != nil {
		return err
	}
	want := CompanionServiceProfileDir(home, spec.ID)
	if filepath.Clean(spec.ProfileDir) != want {
		return fmt.Errorf("the companion profile must be the Library-managed folder %s", want)
	}
	if err := requirePrivateDir(want); err != nil {
		return err
	}
	if err := companionProfileReady(spec.ID, want); err != nil {
		return err
	}
	return validateCompanionApp(s.runner, spec.ID, app, spec.AppPath)
}

func (s *CompanionService) validateInstall(spec CompanionServiceSpec) (companionApp, string, error) {
	app, ok := companionApps[spec.ID]
	if !ok {
		return companionApp{}, "", errors.New("unknown companion; only Library's five stock apps are supported")
	}
	if !spec.AtLogin {
		return companionApp{}, "", errors.New("at-login startup was not selected")
	}
	if !filepath.IsAbs(spec.Home) {
		return companionApp{}, "", errors.New("the Library configuration folder must be absolute")
	}
	for _, value := range []string{spec.Home, spec.AppPath, spec.ProfileDir} {
		if containsControl(value) {
			return companionApp{}, "", errors.New("paths cannot contain control characters")
		}
	}
	if err := s.validateDependency(spec); err != nil {
		return companionApp{}, "", err
	}
	return app, CompanionServiceLabel(spec.ID), nil
}

func companionPlist(spec CompanionServiceSpec, app companionApp, label string) string {
	args := []string{openCommand, "-a", filepath.Clean(spec.AppPath), "--args"}
	args = append(args, app.args(filepath.Clean(spec.ProfileDir))...)
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>`)
	b.WriteString(xmlEscape(label))
	b.WriteString(`</string>
	<key>ProgramArguments</key>
	<array>
`)
	for _, arg := range args {
		b.WriteString("\t\t<string>")
		b.WriteString(xmlEscape(arg))
		b.WriteString("</string>\n")
	}
	b.WriteString(`	</array>
	<key>EnvironmentVariables</key>
	<dict><key>LIBRARY_STUDIO_HOME</key><string>`)
	b.WriteString(xmlEscape(spec.Home))
	b.WriteString(`</string></dict>
	<key>RunAtLoad</key>
	<true/>
	<key>StandardOutPath</key>
	<string>`)
	b.WriteString(xmlEscape(filepath.Join(spec.Home, "logs", label+".log")))
	b.WriteString(`</string>
	<key>StandardErrorPath</key>
	<string>`)
	b.WriteString(xmlEscape(filepath.Join(spec.Home, "logs", label+".err.log")))
	b.WriteString(`</string>
</dict>
</plist>
`)
	return b.String()
}

func (s *CompanionService) writeFiles(spec CompanionServiceSpec, label, plist string) error {
	agentDir, err := launchAgentFolder(s.agentDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(spec.Home, 0o700); err != nil {
		return err
	}
	logs := filepath.Join(spec.Home, "logs")
	if err := os.MkdirAll(logs, 0o700); err != nil {
		return err
	}
	for _, name := range []string{label + ".log", label + ".err.log"} {
		path := filepath.Join(logs, name)
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				return err
			}
		}
	}
	path, err := s.plistPath(label)
	if err != nil {
		return err
	}
	return writePrivate(path, []byte(plist))
}

func companionRetiredPath(home, label string) (string, error) {
	dir := filepath.Join(home, "retired-launchjobs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := label + "." + time.Now().UTC().Format("20060102T150405.000000000") + ".plist"
	return filepath.Join(dir, name), nil
}

func (s *CompanionService) Install(spec CompanionServiceSpec) (CompanionServiceStatus, error) {
	app, label, err := s.validateInstall(spec)
	if err != nil {
		return companionBlocked(spec, err.Error()), err
	}
	spec.Home = filepath.Clean(spec.Home)
	spec.AppPath = filepath.Clean(spec.AppPath)
	spec.ProfileDir = filepath.Clean(spec.ProfileDir)
	path, err := s.plistPath(label)
	if err != nil {
		return companionBlocked(spec, "The login items folder could not be found."), err
	}
	ours, exists, err := inspectLabeledPlist(path, label)
	if err != nil {
		return companionBlocked(spec, err.Error()), err
	}
	if exists && !ours {
		err := errors.New("another login job uses this Library companion name; not changing it")
		return companionBlocked(spec, err.Error()), err
	}
	if exists {
		if err := checkJobHome(path, spec.Home); err != nil {
			return companionBlocked(spec, err.Error()), err
		}
	}
	plist := companionPlist(spec, app, label)
	changed := true
	var old []byte
	if exists {
		old, err = os.ReadFile(path)
		if err != nil {
			return companionBlocked(spec, "The previous login job could not be read."), err
		}
		changed = string(old) != plist
	}
	loaded := s.isLoaded(label)
	if !changed && loaded && loadedConfigMatches(path) {
		return s.Status(spec), nil
	}
	// Stop the old job before changing or reloading its definition. If bootout
	// fails, the old plist and old job stay together and Status stays truthful.
	if loaded {
		if err := launchctlRun(s.runner, "bootout", serviceDomain()+"/"+label); err != nil {
			return companionBlocked(spec, "The previous at-login job could not be stopped; saved settings were not changed."), err
		}
	}
	if changed && exists {
		retired, err := companionRetiredPath(spec.Home, label)
		if err != nil {
			return companionBlocked(spec, "The previous login job could not be retired."), err
		}
		if err := writePrivate(retired, old); err != nil {
			return companionBlocked(spec, "The previous login job could not be retired."), err
		}
	}
	if err := s.writeFiles(spec, label, plist); err != nil {
		return companionBlocked(spec, "The at-login job could not be saved."), err
	}
	if err := launchctlRun(s.runner, "bootstrap", serviceDomain(), path); err != nil {
		return companionBlocked(spec, "The at-login job could not be loaded. Check its log in the Library configuration folder."), err
	}
	if err := markLoaded(path); err != nil {
		return companionBlocked(spec, "At-login startup started but its loaded settings could not be recorded; reinstall it."), err
	}
	return s.Status(spec), nil
}

func (s *CompanionService) Remove(spec CompanionServiceSpec) (CompanionServiceStatus, error) {
	if _, ok := companionApps[spec.ID]; !ok {
		return companionBlocked(spec, "Unknown companion; only Library's five stock apps are supported."), errors.New("unknown companion")
	}
	if !filepath.IsAbs(spec.Home) {
		return companionBlocked(spec, "The Library configuration folder must be absolute."), errors.New("the Library configuration folder must be absolute")
	}
	spec.Home = filepath.Clean(spec.Home)
	label := CompanionServiceLabel(spec.ID)
	path, err := s.plistPath(label)
	if err != nil {
		return companionBlocked(spec, "The login items folder could not be found."), err
	}
	ours, exists, err := inspectLabeledPlist(path, label)
	if err != nil {
		return companionBlocked(spec, err.Error()), err
	}
	if !exists {
		return companionNotInstalled(spec, "At-login startup is already off."), nil
	}
	if !ours {
		err := errors.New("refusing to remove another login job")
		return companionBlocked(spec, err.Error()), err
	}
	if err := checkJobHome(path, spec.Home); err != nil {
		return companionBlocked(spec, err.Error()), err
	}
	if s.isLoaded(label) {
		if err := launchctlRun(s.runner, "bootout", serviceDomain()+"/"+label); err != nil {
			return companionBlocked(spec, "The at-login job could not be stopped."), err
		}
	}
	retired, err := companionRetiredPath(spec.Home, label)
	if err != nil {
		return companionBlocked(spec, "The login job could not be retired."), err
	}
	if err := os.Rename(path, retired); err != nil {
		return companionBlocked(spec, "The login job could not be retired."), err
	}
	return companionNotInstalled(spec, "At-login startup was removed. The login job was retired, not deleted."), nil
}

//go:build darwin

package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const hostSettleTime = 3 * time.Second

func (s *HostService) agentFolder() (string, error) {
	return launchAgentFolder(s.agentDir)
}

func launchAgentFolder(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if dir := os.Getenv("LIBRARY_STUDIO_LAUNCHAGENT_DIR"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents"), nil
}

func (s *HostService) plistPath() (string, error) {
	dir, err := s.agentFolder()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, HostServiceLabel+".plist"), nil
}

func serviceDomain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

func (s *HostService) isLoaded() bool {
	_, err := s.runner.Run("launchctl", "print", serviceDomain()+"/"+HostServiceLabel)
	return err == nil
}

func (s *HostService) isRunning(home string) bool {
	return s.verify != nil && s.verify(home) == nil
}

func blockedStatus(detail string) HostServiceStatus {
	return HostServiceStatus{
		Supported: true,
		State:     HostServiceBlocked,
		Label:     HostServiceLabel,
		Detail:    detail,
	}
}

func needsRestartStatus(detail string) HostServiceStatus {
	return HostServiceStatus{
		Supported:    true,
		State:        HostServiceNeedsRestart,
		Label:        HostServiceLabel,
		Installed:    true,
		NeedsRestart: true,
		Detail:       detail,
	}
}

// Status inspects only Library's own job and the desktop descriptor. It never
// prints or returns the descriptor token or its address.
func (s *HostService) Status(spec HostServiceSpec) HostServiceStatus {
	if spec.Home == "" {
		return blockedStatus("The background host needs a configuration folder.")
	}
	path, err := s.plistPath()
	if err != nil {
		return blockedStatus("The background host folder could not be found.")
	}
	ours, exists, err := inspectExisting(path)
	if err != nil {
		return blockedStatus(err.Error())
	}
	if !exists {
		return HostServiceStatus{
			Supported: true,
			State:     HostServiceNotInstalled,
			Label:     HostServiceLabel,
			Detail:    "The background host is not installed; the desktop app runs its own server.",
		}
	}
	if !ours {
		return blockedStatus("Another login job uses Library Studio KH's name; refusing to change it.")
	}
	if err := checkJobHome(path, spec.Home); err != nil {
		return blockedStatus(err.Error())
	}
	st := HostServiceStatus{
		Supported: true,
		State:     HostServiceInstalled,
		Label:     HostServiceLabel,
		Installed: true,
	}
	if !s.isLoaded() {
		st.Detail = "The background host is installed but not loaded."
		return st
	}
	st.Loaded = true
	st.State = HostServiceLoaded
	// When the caller supplies a full desired spec, the saved job must match it.
	// A zero Port or empty Exe means "start what is installed", used by the
	// desktop helper, and is verified by the loaded marker instead.
	if spec.Exe != "" && spec.Port != 0 {
		current, err := os.ReadFile(path)
		if err != nil || string(current) != hostPlist(spec) {
			st.State = HostServiceNeedsRestart
			st.NeedsRestart = true
			st.Detail = "The saved login job does not match these settings. Run install to save them, then start."
			return st
		}
	}
	if !loadedConfigMatches(path) {
		st.State = HostServiceNeedsRestart
		st.NeedsRestart = true
		st.Detail = "The loaded login job does not match the saved settings. Run start or reinstall before relying on it."
		return st
	}
	if s.verify == nil {
		st.Detail = "The background host job is loaded."
		return st
	}
	if s.verify(spec.Home) == nil {
		st.Running = true
		st.State = HostServiceRunning
		st.Detail = "The background host is running."
		return st
	}
	st.State = HostServiceNeedsRestart
	st.NeedsRestart = true
	st.Detail = "The background host job is loaded but not answering. Quit Library Studio KH and reopen it, or run start."
	return st
}

func inspectExisting(path string) (ours, exists bool, err error) {
	return inspectLabeledPlist(path, HostServiceLabel)
}

type plistNode struct {
	XMLName xml.Name
	Content string      `xml:",chardata"`
	Nodes   []plistNode `xml:",any"`
}

// plistLabel reads the single top-level Label value. A label mentioned only in
// ProgramArguments, a nested dictionary or a second time is not accepted.
func plistLabel(data []byte) (string, bool, error) {
	var doc plistNode
	if err := xml.NewDecoder(bytes.NewReader(data)).Decode(&doc); err != nil || doc.XMLName.Local != "plist" {
		return "", false, errors.New("the launch job file is not a valid property list")
	}
	var dicts []plistNode
	for _, node := range doc.Nodes {
		if node.XMLName.Local == "dict" {
			dicts = append(dicts, node)
		}
	}
	if len(dicts) != 1 {
		return "", false, errors.New("the launch job file has no single job dictionary")
	}
	label := ""
	found := false
	nodes := dicts[0].Nodes
	for i, node := range nodes {
		if node.XMLName.Local != "key" || node.Content != "Label" {
			continue
		}
		if found {
			return "", false, errors.New("the launch job file has more than one Label")
		}
		found = true
		if i+1 >= len(nodes) || len(node.Nodes) != 0 {
			return "", false, errors.New("the launch job Label has no value")
		}
		value := nodes[i+1]
		if value.XMLName.Local != "string" || len(value.Nodes) != 0 {
			return "", false, errors.New("the launch job Label is not a plain string")
		}
		label = value.Content
	}
	return label, found, nil
}

func inspectLabeledPlist(path, label string) (ours, exists bool, err error) {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return false, true, fmt.Errorf("the launch job file could not be read")
	}
	if !fi.Mode().IsRegular() {
		return false, true, fmt.Errorf("the launch job file is not a regular file; refusing to use it")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false, true, fmt.Errorf("the launch job file could not be read")
	}
	got, found, err := plistLabel(b)
	if err != nil {
		return false, true, err
	}
	if !found {
		return false, true, nil
	}
	return got == label, true, nil
}

func validateInstallSpec(spec HostServiceSpec) error {
	if spec.Exe == "" || !filepath.IsAbs(spec.Exe) {
		return errors.New("the server path must be absolute")
	}
	if !filepath.IsAbs(spec.Home) {
		return errors.New("the configuration folder must be absolute")
	}
	for _, value := range []string{spec.Exe, spec.Home} {
		for _, r := range value {
			if r < 0x20 || r == 0x7f {
				return errors.New("paths cannot contain control characters")
			}
		}
	}
	selected := helperExecutable(spec.Exe, spec.Home)
	if helper := packagedHelper(spec.Exe); helper != "" {
		selected = helper
	}
	fi, err := os.Stat(selected)
	if err != nil {
		return fmt.Errorf("the selected server cannot be used: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return errors.New("the selected server is not a file")
	}
	if fi.Mode().Perm()&0o111 == 0 {
		return errors.New("the selected server is not executable")
	}
	if spec.Port < 1024 || spec.Port > 65535 {
		return errors.New("the port must be a whole number from 1024 to 65535")
	}
	return nil
}

func xmlEscape(value string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(value)
}

// packagedHelper returns the helper app's executable inside a packaged Library
// Studio KH.app, or "" for development builds.
func packagedHelper(exe string) string {
	macos := filepath.Dir(exe)
	contents := filepath.Dir(macos)
	if filepath.Base(macos) != "MacOS" || filepath.Base(contents) != "Contents" ||
		!strings.HasSuffix(filepath.Base(filepath.Dir(contents)), ".app") {
		return ""
	}
	helper := filepath.Join(contents, "Resources", "helper", "Library Studio KH Helper.app", "Contents", "MacOS", "Library Studio KH Helper")
	if fi, err := os.Stat(helper); err == nil && fi.Mode().IsRegular() {
		return helper
	}
	return ""
}

// helperInstallPath is where the packaged helper is copied: outside the main
// app bundle so macOS lists the background item as the helper, not the app.
func helperInstallPath(home string) string {
	return filepath.Join(home, "Helper", "Library Studio KH Helper.app", "Contents", "MacOS", "Library Studio KH Helper")
}

// savedJob returns the program and port saved in an installed Library job.
func savedJob(path string) (string, int, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", 0, false
	}
	var doc plistNode
	if xml.Unmarshal(b, &doc) != nil || doc.XMLName.Local != "plist" || len(doc.Nodes) != 1 {
		return "", 0, false
	}
	args, err := jobProperty(doc.Nodes[0], "ProgramArguments")
	if err != nil || args.XMLName.Local != "array" {
		return "", 0, false
	}
	var values []string
	for _, node := range args.Nodes {
		if node.XMLName.Local != "string" || len(node.Nodes) != 0 {
			return "", 0, false
		}
		values = append(values, node.Content)
	}
	for i := 2; i+1 < len(values); i++ {
		if values[i] != "-port" {
			continue
		}
		port, err := strconv.Atoi(values[i+1])
		if err != nil || port < 1024 || port > 65535 {
			return "", 0, false
		}
		return values[0], port, true
	}
	return "", 0, false
}

// helperExecutable is the path the login job runs: the installed helper when
// the server is packaged, otherwise the given executable.
func helperExecutable(exe, home string) string {
	if packagedHelper(exe) == "" {
		return exe
	}
	return helperInstallPath(home)
}

// installHelper copies the packaged helper into the configuration home and
// reports whether the installed copy changed. The replaced copy is kept as
// Helper.previous so a failed update can be rolled back.
func installHelper(exe, home string) (bool, error) {
	packaged := packagedHelper(exe)
	if packaged == "" {
		return false, nil
	}
	installed := helperInstallPath(home)
	if sameFileBytes(packaged, installed) {
		return false, nil
	}
	src := filepath.Dir(filepath.Dir(filepath.Dir(packaged)))
	dst := filepath.Dir(filepath.Dir(filepath.Dir(installed)))
	staging := dst + ".new"
	if err := os.RemoveAll(staging); err != nil {
		return false, err
	}
	if err := copyTree(src, staging); err != nil {
		return false, err
	}
	previous := dst + ".previous"
	if _, err := os.Stat(dst); err == nil {
		if err := os.RemoveAll(previous); err != nil {
			return false, err
		}
		if err := os.Rename(dst, previous); err != nil {
			return false, err
		}
	}
	if err := os.Rename(staging, dst); err != nil {
		return false, err
	}
	return true, nil
}

func sameFileBytes(a, b string) bool {
	one, err := os.ReadFile(a)
	if err != nil {
		return false
	}
	two, err := os.ReadFile(b)
	if err != nil {
		return false
	}
	return bytes.Equal(one, two)
}

func copyTree(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		from := filepath.Join(src, entry.Name())
		to := filepath.Join(dst, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if err := copyTree(from, to); err != nil {
				return err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("the helper bundle contains an unsupported file: %s", from)
		}
		data, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		if err := os.WriteFile(to, data, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

func hostPlist(spec HostServiceSpec) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + xmlEscape(HostServiceLabel) + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + xmlEscape(helperExecutable(spec.Exe, spec.Home)) + `</string>
		<string>-service</string>
		<string>-port</string>
		<string>` + strconv.Itoa(spec.Port) + `</string>
	</array>
	<key>EnvironmentVariables</key>
	<dict>
		<key>LIBRARY_STUDIO_HOME</key>
		<string>` + xmlEscape(spec.Home) + `</string>
	</dict>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>ThrottleInterval</key>
	<integer>10</integer>
	<key>ProcessType</key>
	<string>Background</string>
	<key>StandardOutPath</key>
	<string>` + xmlEscape(filepath.Join(spec.Home, "host.log")) + `</string>
	<key>StandardErrorPath</key>
	<string>` + xmlEscape(filepath.Join(spec.Home, "host.err.log")) + `</string>
</dict>
</plist>
`
}

func writePrivate(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadedMarkerPath(plistPath string) string { return plistPath + ".loaded" }

func plistDigest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// markLoaded records the exact plist launchd accepted. Status compares this
// marker so a failed or skipped reload can never read as the desired job.
func markLoaded(plistPath string) error {
	b, err := os.ReadFile(plistPath)
	if err != nil {
		return err
	}
	return writePrivate(loadedMarkerPath(plistPath), []byte(plistDigest(b)))
}

func loadedConfigMatches(plistPath string) bool {
	markerPath := loadedMarkerPath(plistPath)
	fi, err := os.Lstat(markerPath)
	if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o077 != 0 || fi.Size() > 128 {
		return false
	}
	marker, err := os.ReadFile(markerPath)
	if err != nil {
		return false
	}
	b, err := os.ReadFile(plistPath)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(marker)) == plistDigest(b)
}

// writeFiles keeps the login job private, returns a recoverable backup of a
// previous Library job, and never overwrites a file that is not ours.
func (s *HostService) writeFiles(path string, spec HostServiceSpec, plist string, backup bool) error {
	dir, err := s.agentFolder()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(spec.Home, 0o700); err != nil {
		return err
	}
	for _, name := range []string{"host.log", "host.err.log"} {
		logPath := filepath.Join(spec.Home, name)
		if _, err := os.Lstat(logPath); errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(logPath, nil, 0o600); err != nil {
				return err
			}
		}
	}
	if backup {
		if old, err := os.ReadFile(path); err == nil {
			if err := writePrivate(path+".bak", old); err != nil {
				return err
			}
		}
	}
	return writePrivate(path, []byte(plist))
}

func (s *HostService) launchctl(args ...string) error {
	return launchctlRun(s.runner, args...)
}

func launchctlRun(runner CommandRunner, args ...string) error {
	out, err := runner.Run("launchctl", args...)
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("launchctl %s failed: %s", args[0], detail)
	}
	return nil
}

func (s *HostService) bootstrap(path string) error {
	return s.launchctl("bootstrap", serviceDomain(), path)
}

// configHomeBusy reports whether another server process holds the home lock.
// The running background host itself holds it, so callers check that first.
func configHomeBusy(home string) bool {
	if fi, err := os.Stat(home); err != nil || !fi.IsDir() {
		return false
	}
	release, err := LockConfigHome(home)
	if err != nil {
		return true
	}
	release()
	return false
}

// finish briefly waits for a newly started job to prove itself, then reports
// the honest state. A slow host is "loaded", never assumed healthy.
func (s *HostService) finish(spec HostServiceSpec) HostServiceStatus {
	if s.verify != nil {
		deadline := time.Now().Add(hostSettleTime)
		for time.Now().Before(deadline) {
			if s.verify(spec.Home) == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	return s.Status(spec)
}

func (s *HostService) Install(spec HostServiceSpec) (HostServiceStatus, error) {
	if err := validateInstallSpec(spec); err != nil {
		return blockedStatus(err.Error()), err
	}
	path, err := s.plistPath()
	if err != nil {
		return blockedStatus("The background host folder could not be found."), err
	}
	ours, exists, err := inspectExisting(path)
	if err != nil {
		return blockedStatus(err.Error()), err
	}
	if exists && !ours {
		err := errors.New("another login job uses Library Studio KH's name; not changing it")
		return blockedStatus(err.Error()), err
	}
	if exists {
		if err := checkJobHome(path, spec.Home); err != nil {
			return blockedStatus(err.Error()), err
		}
	}
	helperChanged, err := installHelper(spec.Exe, spec.Home)
	if err != nil {
		return blockedStatus("The background helper could not be installed."), err
	}
	plist := hostPlist(spec)
	changed := helperChanged
	if exists {
		old, err := os.ReadFile(path)
		if err != nil {
			return blockedStatus("The background host file could not be read."), err
		}
		changed = string(old) != plist
	}
	loaded := s.isLoaded()
	if !changed && loaded && loadedConfigMatches(path) {
		return s.Status(spec), nil
	}
	// Stop the old job before changing or reloading its definition. If bootout
	// fails, the old plist and old job stay together and Status stays truthful.
	if loaded {
		if err := s.launchctl("bootout", serviceDomain()+"/"+HostServiceLabel); err != nil {
			return blockedStatus("The background host could not be stopped; saved settings were not changed."), err
		}
	}
	if err := s.writeFiles(path, spec, plist, changed && exists); err != nil {
		return blockedStatus("The login job could not be saved."), err
	}
	// When a desktop server owns this configuration home, starting a second
	// server would fail its lock. Save the job and ask for an explicit restart.
	if configHomeBusy(spec.Home) {
		return needsRestartStatus("The background host is installed. Quit and reopen Library Studio KH to start it."), nil
	}
	if err := s.bootstrap(path); err != nil {
		return blockedStatus("The background host could not be started. Check host.err.log in the configuration folder."), err
	}
	if err := markLoaded(path); err != nil {
		return blockedStatus("The background host started but its loaded settings could not be recorded; start it again."), err
	}
	return s.finish(spec), nil
}

func (s *HostService) Start(spec HostServiceSpec) (HostServiceStatus, error) {
	path, err := s.plistPath()
	if err != nil {
		return blockedStatus("The background host folder could not be found."), err
	}
	ours, exists, err := inspectExisting(path)
	if err != nil {
		return blockedStatus(err.Error()), err
	}
	if !exists || !ours {
		err := errors.New("the background host is not installed; run install first")
		return blockedStatus(err.Error()), err
	}
	if err := checkJobHome(path, spec.Home); err != nil {
		return blockedStatus(err.Error()), err
	}
	// Migrate a job saved by an older build to the named helper this app ships.
	// The saved job's port is kept, so the running host is simply replaced.
	if spec.Exe != "" {
		if program, port, ok := savedJob(path); ok && program != helperExecutable(spec.Exe, spec.Home) {
			return s.Install(HostServiceSpec{Exe: spec.Exe, Home: spec.Home, Port: port})
		}
	}
	helperChanged, err := installHelper(spec.Exe, spec.Home)
	if err != nil {
		return blockedStatus("The background helper could not be installed."), err
	}
	loaded := s.isLoaded()
	if loaded && loadedConfigMatches(path) && !helperChanged && (s.verify == nil || s.isRunning(spec.Home)) {
		return s.Status(spec), nil
	}
	if loaded {
		// Reload from disk so changed settings take effect; a kickstart would
		// keep the old job definition. Only our own label is touched.
		if err := s.launchctl("bootout", serviceDomain()+"/"+HostServiceLabel); err != nil {
			return blockedStatus("The old background host could not be stopped; the loaded job was not changed."), err
		}
	}
	if configHomeBusy(spec.Home) {
		return needsRestartStatus("The background host is installed, but Library Studio KH is already running its own server. Quit and reopen it to hand over."), nil
	}
	if err := s.bootstrap(path); err != nil {
		return blockedStatus("The background host could not be started. Check host.err.log in the configuration folder."), err
	}
	if err := markLoaded(path); err != nil {
		return blockedStatus("The background host started but its loaded settings could not be recorded; start it again."), err
	}
	return s.finish(spec), nil
}

func (s *HostService) Stop() (HostServiceStatus, error) {
	path, err := s.plistPath()
	if err != nil {
		return blockedStatus("The background host folder could not be found."), err
	}
	ours, exists, err := inspectExisting(path)
	if err != nil {
		return blockedStatus(err.Error()), err
	}
	if !exists {
		return HostServiceStatus{
			Supported: true,
			State:     HostServiceNotInstalled,
			Label:     HostServiceLabel,
			Detail:    "The background host is not installed; nothing to stop.",
		}, nil
	}
	if !ours {
		err := errors.New("refusing to stop another login job")
		return blockedStatus(err.Error()), err
	}
	loaded := s.isLoaded()
	if loaded {
		if err := s.launchctl("bootout", serviceDomain()+"/"+HostServiceLabel); err != nil {
			return blockedStatus("The background host could not be stopped."), err
		}
	}
	return HostServiceStatus{
		Supported: true,
		State:     HostServiceInstalled,
		Label:     HostServiceLabel,
		Installed: true,
		Detail:    "The background host was stopped. Its login job is kept.",
	}, nil
}

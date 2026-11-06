//go:build darwin

package platform

import (
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
	if s.agentDir != "" {
		return s.agentDir, nil
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
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return false, true, fmt.Errorf("the background host file could not be read")
	}
	if !fi.Mode().IsRegular() {
		return false, true, fmt.Errorf("the background host file is not a regular file; refusing to use it")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false, true, fmt.Errorf("the background host file could not be read")
	}
	return strings.Contains(string(b), "<key>Label</key>") &&
		strings.Contains(string(b), "<string>"+HostServiceLabel+"</string>"), true, nil
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
	fi, err := os.Stat(spec.Exe)
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

func hostPlist(spec HostServiceSpec) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + xmlEscape(HostServiceLabel) + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + xmlEscape(spec.Exe) + `</string>
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
	out, err := s.runner.Run("launchctl", args...)
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
	plist := hostPlist(spec)
	changed := true
	if exists {
		old, err := os.ReadFile(path)
		if err != nil {
			return blockedStatus("The background host file could not be read."), err
		}
		changed = string(old) != plist
	}
	if s.isRunning(spec.Home) {
		if !changed {
			return s.Status(spec), nil
		}
		if err := s.writeFiles(path, spec, plist, exists); err != nil {
			return blockedStatus("The login job could not be saved."), err
		}
		if err := s.launchctl("bootout", serviceDomain()+"/"+HostServiceLabel); err != nil {
			return blockedStatus("The running background host could not be restarted."), err
		}
		if err := s.bootstrap(path); err != nil {
			return blockedStatus("The background host could not be started."), err
		}
		return s.finish(spec), nil
	}
	if !changed && s.isLoaded() {
		return s.Status(spec), nil
	}
	if err := s.writeFiles(path, spec, plist, changed && exists); err != nil {
		return blockedStatus("The login job could not be saved."), err
	}
	// When a desktop server owns this configuration home, starting a second
	// server would fail its lock. Save the job and ask for an explicit restart.
	if configHomeBusy(spec.Home) {
		return needsRestartStatus("The background host is installed. Quit and reopen Library Studio KH to start it."), nil
	}
	if s.isLoaded() {
		if err := s.launchctl("bootout", serviceDomain()+"/"+HostServiceLabel); err != nil {
			return blockedStatus("The old background host could not be stopped."), err
		}
	}
	if err := s.bootstrap(path); err != nil {
		return blockedStatus("The background host could not be started. Check host.err.log in the configuration folder."), err
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
	if s.isRunning(spec.Home) {
		return s.Status(spec), nil
	}
	if s.isLoaded() {
		// Reload from disk so changed settings take effect; launchctl kickstart
		// would keep the old job definition. Only our own label is touched.
		if err := s.launchctl("bootout", serviceDomain()+"/"+HostServiceLabel); err != nil {
			return blockedStatus("The old background host could not be stopped."), err
		}
	}
	if configHomeBusy(spec.Home) {
		return needsRestartStatus("The background host is installed, but Library Studio KH is already running its own server. Quit and reopen it to hand over."), nil
	}
	if err := s.bootstrap(path); err != nil {
		return blockedStatus("The background host could not be started. Check host.err.log in the configuration folder."), err
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

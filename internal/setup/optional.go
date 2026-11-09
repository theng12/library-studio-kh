package setup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"librarystudio/internal/companioninstall"
	"librarystudio/internal/config"
)

// Optional helpers are configured only inside their vendors' own default
// locations, so the apps find the files without extra flags. Every file
// Library writes starts with optionalFileMarker; Setup may update a file it
// wrote and never edits one it did not write.

const optionalFileMarker = "# Written by Library Studio KH's guided setup."

// unpackerrConfigPath is Unpackerr's own default on macOS (its
// configFileLocactions darwin case): ~/.unpackerr/unpackerr.conf.
func unpackerrConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.New("the current user's home folder is unavailable")
	}
	return filepath.Join(home, ".unpackerr", "unpackerr.conf"), nil
}

// recyclarrConfigPath is Recyclarr's own default on macOS, the first place it
// looks when it runs without a -c flag.
func recyclarrConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", errors.New("the current user's application support folder is unavailable")
	}
	return filepath.Join(dir, "recyclarr", "recyclarr.yml"), nil
}

type optionalInstance struct {
	section string
	app     config.App
}

// optionalInstances lists the connected instances of the media apps selected
// in this setup. Credentials come from Library's saved connections only.
func optionalInstances(c config.Config, req Request, ids ...companioninstall.AppID) []optionalInstance {
	var out []optionalInstance
	for _, id := range ids {
		if !selected(req, id) {
			continue
		}
		a := getArr(c, id)
		if a.URL == "" || a.APIKey == "" {
			continue
		}
		out = append(out, optionalInstance{section: strings.ToLower(string(id)), app: a})
	}
	return out
}

// unpackerrConfigBody builds Unpackerr's TOML. Safety is explicit, never left
// to vendor defaults: delete_orig is off, delete_delay carries Unpackerr's
// "never delete" sentinel (negative), no watched folders and no web server
// are configured.
func unpackerrConfigBody(c config.Config, req Request) string {
	var b strings.Builder
	b.WriteString(optionalFileMarker + "\n")
	b.WriteString("# Setup refreshes this file when you press \"Check connections\". It deletes\n")
	b.WriteString("# nothing: delete_orig is off, delete_delay is Unpackerr's \"never delete\"\n")
	b.WriteString("# value, and no web server is started. Unpackerr is never launched for you.\n")
	b.WriteString("# Documentation: https://unpackerr.zip\n\n")
	b.WriteString("interval = \"2m\"\n")
	// delete_orig only exists per Starr section; the global delete_delay
	// covers anything a later section might inherit.
	b.WriteString("delete_delay = \"-1s\"\n")
	for _, inst := range optionalInstances(c, req, companioninstall.Sonarr, companioninstall.Radarr, companioninstall.Lidarr) {
		fmt.Fprintf(&b, "\n[%s.0]\n", inst.section)
		fmt.Fprintf(&b, "url = %s\n", strconv.Quote(inst.app.URL))
		fmt.Fprintf(&b, "api_key = %s\n", strconv.Quote(inst.app.APIKey))
		if path := strings.TrimSpace(req.DownloadPath); path != "" {
			fmt.Fprintf(&b, "paths = [%s]\n", strconv.Quote(path))
		}
		b.WriteString("delete_orig = false\n")
		b.WriteString("delete_delay = \"-1s\"\n")
	}
	return b.String()
}

// recyclarrConfigBody builds Recyclarr's YAML with the connected instance
// addresses only. Profile and custom-format choices are the owner's to make;
// Library writes none and never runs a sync by itself.
func recyclarrConfigBody(c config.Config, req Request) string {
	var b strings.Builder
	b.WriteString(optionalFileMarker + "\n")
	b.WriteString("# Setup refreshes this file when you press \"Check connections\". Recyclarr is\n")
	b.WriteString("# only ever run when you press Preview (a dry run) or Apply in Library; it is\n")
	b.WriteString("# never synced automatically. Add the profiles you want before applying.\n")
	b.WriteString("# Reference: https://recyclarr.dev/reference/configuration/\n")
	b.WriteString("# yaml-language-server: $schema=https://schemas.recyclarr.dev/latest/config-schema.json\n")
	for _, inst := range optionalInstances(c, req, companioninstall.Sonarr, companioninstall.Radarr) {
		heading := "series"
		if inst.section == "radarr" {
			heading = "movies"
		}
		fmt.Fprintf(&b, "\n%s:\n  %s:\n", inst.section, heading)
		fmt.Fprintf(&b, "    base_url: %s\n", strconv.Quote(inst.app.URL))
		fmt.Fprintf(&b, "    api_key: %s\n", strconv.Quote(inst.app.APIKey))
	}
	return b.String()
}

type configWriteResult string

const (
	configCreated configWriteResult = "created"
	configUpdated configWriteResult = "updated"
	configKept    configWriteResult = "kept"
)

// writeOptionalFile writes body privately (0600) to a path that either does
// not exist yet or already carries Setup's own exact marker AND identical
// content. An existing file whose content differs from body is kept
// untouched regardless of allowUpdate: the owner may have added profiles or
// other choices after Setup's last write, and overwriting would destroy
// them. allowUpdate only permits refreshing a file whose content matches
// body exactly (idempotent re-write after a verify failure).
func writeOptionalFile(path, body string, allowUpdate bool) (configWriteResult, error) {
	existed := false
	if data, err := os.ReadFile(path); err == nil {
		existed = true
		if !strings.HasPrefix(string(data), optionalFileMarker) {
			return configKept, nil
		}
		if string(data) != body {
			// Owner-modified or newer vendor schema: never overwrite.
			return configKept, nil
		}
		if !allowUpdate {
			return configKept, nil
		}
		// Content matches exactly; fall through to idempotent re-write.
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("the existing configuration could not be read: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("the configuration folder could not be created: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".library-studio-config-*")
	if err != nil {
		return "", err
	}
	failed := func(err error) (configWriteResult, error) { return "", err }
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		return failed(err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return failed(err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return failed(err)
	}
	if err := tmp.Close(); err != nil {
		return failed(err)
	}
	// Failed writes keep their temp file for repair rather than deleting evidence.
	if err := os.Rename(tmp.Name(), path); err != nil {
		return failed(fmt.Errorf("the configuration file could not be saved: %w", err))
	}
	if existed {
		return configUpdated, nil
	}
	return configCreated, nil
}

// verifyOptionalFile reads the file back and proves the write landed: exact
// content, regular file, private permissions.
func verifyOptionalFile(path, expected string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("the configuration file could not be read back: %w", err)
	}
	if string(data) != expected {
		return errors.New("the configuration file on disk differs from what Setup wrote")
	}
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return errors.New("the configuration path is not a regular file")
	}
	if st.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("the configuration file must be private, found mode %o", st.Mode().Perm())
	}
	return nil
}

// unpackerrInspection reads an Unpackerr file Library did not write: it
// reports whether the file deletes original downloads and how many Starr
// instance sections it watches. Only uncommented lines count.
func unpackerrInspection(data []byte) (deletes bool, instances int) {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "[sonarr."), strings.HasPrefix(line, "[radarr."), strings.HasPrefix(line, "[lidarr."):
			instances++
		case line == "delete_orig = true" || strings.HasPrefix(line, "delete_orig") && strings.Contains(line, "true"):
			deletes = true
		}
	}
	return deletes, instances
}

// configureOptional writes and verifies the optional helpers' own
// configuration and records exactly what happened as connection checks. It
// never launches an app and never runs Recyclarr. It returns false when it
// blocked the setup (the job is already marked blocked).
func (m *Manager) configureOptional(c config.Config, req Request) bool {
	for index, app := range m.job.View.Apps {
		if !companioninstall.Optional(app.ID) {
			continue
		}
		var (
			path     string
			expected string
			err      error
		)
		switch app.ID {
		case companioninstall.Unpackerr:
			path, err = unpackerrConfigPath()
			expected = unpackerrConfigBody(c, req)
		case companioninstall.Recyclarr:
			if len(optionalInstances(c, req, companioninstall.Sonarr, companioninstall.Radarr)) == 0 {
				m.block(app.Name, "Configuration file", errors.New("Recyclarr needs a connected Sonarr or Radarr instance from this setup. Connect it in Settings, then check again."), c)
				return false
			}
			path, err = recyclarrConfigPath()
			expected = recyclarrConfigBody(c, req)
		default:
			continue
		}
		if err != nil {
			m.block(app.Name, "Configuration file", err, c)
			return false
		}

		detail := ""
		result, werr := writeOptionalFile(path, expected, true)
		switch {
		case werr != nil:
			m.block(app.Name, "Configuration file", werr, c)
			return false
		case result == configKept:
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				m.block(app.Name, "Configuration file", fmt.Errorf("the existing configuration could not be read back: %w", rerr), c)
				return false
			}
			if app.ID == companioninstall.Unpackerr {
				deletes, instances := unpackerrInspection(data)
				if deletes {
					m.block(app.Name, "Configuration file", fmt.Errorf("the existing Unpackerr configuration deletes original downloads (delete_orig = true). Turn that off in Unpackerr or in %s before continuing; Library never edits a configuration it did not write", path), c)
					return false
				}
				// Config-only inspection: delete_orig is off in the file we
				// read, but runtime behaviour was not verified.
				detail = fmt.Sprintf("Existing configuration kept untouched at %s; delete_orig is off in the file and %d instance section(s) found. Runtime safety was not checked.", path, instances)
			} else {
				detail = fmt.Sprintf("Existing Recyclarr configuration kept untouched at %s; Library never edits a file it did not write. Profiles and choices are the owner's.", path)
			}
		case result == configCreated:
			detail = fmt.Sprintf("Configuration written to %s and read back: no deletion, no web server, %d instance(s). The app itself was not started.", path, len(optionalInstances(c, req, instanceIDs(app.ID)...)))
		case result == configUpdated:
			detail = fmt.Sprintf("Configuration refreshed at %s and read back: %d instance(s), same safety settings as before.", path, len(optionalInstances(c, req, instanceIDs(app.ID)...)))
		}
		if result != configKept {
			if verr := verifyOptionalFile(path, expected); verr != nil {
				m.block(app.Name, "Configuration file", verr, c)
				return false
			}
		}
		if m.addCheck(app.Name, "Configuration file", "passed", detail) != nil {
			return false
		}
		app.State = "configured"
		app.Detail = detail
		if m.update("configuring", "Writing and verifying the optional helper configurations…", index, &app) != nil {
			return false
		}
	}
	return true
}

func instanceIDs(id companioninstall.AppID) []companioninstall.AppID {
	if id == companioninstall.Recyclarr {
		return []companioninstall.AppID{companioninstall.Sonarr, companioninstall.Radarr}
	}
	return []companioninstall.AppID{companioninstall.Sonarr, companioninstall.Radarr, companioninstall.Lidarr}
}

// prepareOptionalInstallConfigs runs inside the install phase, before any
// owner can open a freshly installed app: a first launch of Unpackerr must
// never race Setup into an empty vendor default config. Existing files are
// left untouched; Recyclarr has no first launch, so its file waits for
// configureOptional. Returns false after marking the job failed.
func (m *Manager) prepareOptionalInstallConfigs() bool {
	if !selected(m.job.View.Request, companioninstall.Unpackerr) {
		return true
	}
	c := m.connectionConfig()
	path, err := unpackerrConfigPath()
	if err != nil {
		m.fail("Unpackerr's configuration folder is unavailable: " + err.Error())
		return false
	}
	if _, err := writeOptionalFile(path, unpackerrConfigBody(c, m.job.View.Request), false); err != nil {
		m.fail("Unpackerr's configuration file could not be written: " + safeSetupError(err, c))
		return false
	}
	return true
}

// OptionalAction runs Recyclarr only for an explicit owner request:
// "preview" is the vendor's own dry run that changes nothing, and "apply"
// (which changes native Sonarr/Radarr profiles) is refused unless the same
// request confirms it. Nothing else in the setup flow ever invokes the tool.
func (m *Manager) OptionalAction(ctx context.Context, jobID string, appID companioninstall.AppID, action string, confirm bool) (Job, error) {
	if appID != companioninstall.Recyclarr {
		return Job{}, errors.New("only Recyclarr runs from setup; check other helpers in their own apps")
	}
	if action != "preview" && action != "apply" {
		return Job{}, errors.New("unknown Recyclarr action")
	}
	cfg, err := m.recyclarrCfgPath()
	if err != nil {
		return Job{}, err
	}
	if _, err := os.Lstat(cfg); err != nil {
		return Job{}, errors.New("Recyclarr is not configured yet; finish setup first")
	}
	m.mu.Lock()
	if m.job == nil || m.job.View.ID != jobID {
		m.mu.Unlock()
		return Job{}, errors.New("setup plan is no longer current")
	}
	if m.busy {
		m.mu.Unlock()
		return Job{}, errors.New("setup is already working; wait for the current step")
	}
	var binary string
	var name string
	found := false
	for _, step := range m.job.View.Apps {
		if step.ID == companioninstall.Recyclarr {
			binary, name, found = step.Path, step.Name, true
			break
		}
	}
	if !found {
		m.mu.Unlock()
		return Job{}, errors.New("Recyclarr is not part of this setup plan")
	}
	if binary == "" {
		m.mu.Unlock()
		return Job{}, errors.New("Recyclarr is not installed yet; install the selected apps first")
	}
	if action == "apply" && !confirm {
		m.mu.Unlock()
		return Job{}, errors.New("applying changes your Sonarr and Radarr profiles; confirm the apply when you really want it")
	}
	// Reserve busy atomically with the plan/binary validation so a
	// concurrent Plan or OptionalAction cannot slip in between.
	m.busy = true
	m.mu.Unlock()

	if _, err := os.Lstat(binary); err != nil {
		m.finishWork()
		return Job{}, errors.New("the installed Recyclarr tool could not be found; install it again from a new plan")
	}
	defer m.finishWork()

	args := []string{"sync", "-c", cfg}
	if action == "preview" {
		args = append(args, "--preview")
	}
	runCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	out, runErr := m.runTool(runCtx, binary, args...)
	checkName := "Recyclarr preview"
	if action == "apply" {
		checkName = "Recyclarr apply"
	}
	detail := optionalOutput(out, runErr, m.store.Get())
	state := "passed"
	if runErr != nil {
		state = "failed"
	}
	m.mu.Lock()
	m.job.View.Checks = append(m.job.View.Checks, Check{App: name, Name: checkName, State: state, Detail: detail})
	saveErr := m.saveLocked()
	m.mu.Unlock()
	if saveErr != nil {
		return Job{}, saveErr
	}
	return m.Status()
}

// optionalOutput turns a vendor run's output into a check detail: exit
// failures and the tail of the log (the outcome sits at the end), with
// saved secrets redacted.
func optionalOutput(out []byte, err error, c config.Config) string {
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text != "" {
			text += "\n"
		}
		text += err.Error()
	}
	text = safeSetupError(errors.New(text), c)
	if strings.TrimSpace(text) == "" {
		return "Recyclarr returned no output."
	}
	const limit = 3000
	if len(text) > limit {
		text = "…(trimmed)\n" + text[len(text)-limit:]
	}
	return text
}

// defaultRunTool runs the vendor binary and keeps its combined output even
// when the command fails, so the check can show the real reason.
func defaultRunTool(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s: %w", filepath.Base(name), err)
	}
	return out, nil
}

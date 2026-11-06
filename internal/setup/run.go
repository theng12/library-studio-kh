package setup

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"librarystudio/internal/companioninstall"
	"librarystudio/internal/config"
	"librarystudio/internal/stock"
)

func (m *Manager) Run(id string) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.job == nil || m.job.View.ID != id {
		return Job{}, errors.New("setup plan is no longer current")
	}
	if m.busy {
		return Job{}, errors.New("setup is already working")
	}
	if m.job.View.State != "planned" && m.job.View.State != "failed" && m.job.View.State != "interrupted" {
		return Job{}, errors.New("review a setup plan before installing")
	}
	m.job.View.State = "installing"
	m.job.View.Detail = "Installing selected apps; existing bundles will be preserved."
	if err := m.saveLocked(); err != nil {
		return Job{}, err
	}
	m.busy = true
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	go m.install(ctx)
	return cloneJob(m.job.View), nil
}

func (m *Manager) install(ctx context.Context) {
	defer m.finishWork()
	for index, app := range m.job.View.Apps {
		if err := m.update("installing", "Checking and installing "+app.Name+"…", index, &app); err != nil {
			return
		}
		detected, err := m.installer.Detect(ctx, app.ID, appDirectory(app, m.job.View.Request.AppDir))
		if err != nil || detected.State == companioninstall.DetectKeep {
			m.fail(app.Name + " could not be adopted safely. Its installation was left untouched.")
			return
		}
		if detected.State == companioninstall.DetectMissing {
			pkg, ok := m.job.Packages[app.ID]
			if !ok {
				m.fail("The reviewed package for " + app.Name + " is missing. Review a new plan.")
				return
			}
			result, err := m.installer.Install(ctx, pkg, m.job.View.Request.AppDir)
			if err != nil {
				m.fail(app.Name + " installation failed. Existing apps and recoverable staging files were kept. Review the official package and retry.")
				return
			}
			app.Path = result.Path
			app.Version = result.Version
			app.Managed = !result.Existing
		} else {
			app.Path = detected.Path
			app.Version = detected.Version
		}
		if app.Managed {
			if err := m.prepareConnection(app); err != nil {
				m.fail(app.Name + " profile is not ready: " + err.Error())
				return
			}
			app.State = "needs_open"
			app.Detail = "Open this app through macOS and complete any first-launch approval, then check connections."
		} else {
			app.State = "needs_connection"
			app.Detail = "Existing app preserved. Connect its existing native account in Settings, then check connections."
		}
		if err := m.update("installing", "Preparing the next selected app…", index, &app); err != nil {
			return
		}
	}
	_ = m.update("needs_approval", "Apps are installed. Open each new app, complete its macOS prompts, then check connections.", -1, nil)
}

func (m *Manager) prepareConnection(app AppStep) error {
	port := m.job.View.Request.Ports[string(app.ID)]
	m.mu.Lock()
	defer m.mu.Unlock()
	if app.ID == companioninstall.QBittorrent {
		p, err := PrepareQBitProfile(m.home, port)
		if err != nil {
			return err
		}
		m.job.Candidate.QBit = config.QBit{URL: p.URL, Username: p.Username, Password: p.Password}
	} else {
		p, err := PrepareArrProfile(m.home, string(app.ID), port)
		if err != nil {
			return err
		}
		setArr(&m.job.Candidate, app.ID, config.App{URL: p.URL, APIKey: p.APIKey})
	}
	return m.saveLocked()
}

func setArr(c *config.Config, id companioninstall.AppID, a config.App) {
	switch id {
	case companioninstall.Sonarr:
		c.Sonarr = a
	case companioninstall.Radarr:
		c.Radarr = a
	case companioninstall.Lidarr:
		c.Lidarr = a
	case companioninstall.Prowlarr:
		c.Prowlarr = a
	}
}
func getArr(c config.Config, id companioninstall.AppID) config.App {
	switch id {
	case companioninstall.Sonarr:
		return c.Sonarr
	case companioninstall.Radarr:
		return c.Radarr
	case companioninstall.Lidarr:
		return c.Lidarr
	case companioninstall.Prowlarr:
		return c.Prowlarr
	}
	return config.App{}
}

// Open is deliberately separate from installation so first launch is an owner action.
func (m *Manager) Open(ctx context.Context, id string, appID companioninstall.AppID) (Job, error) {
	m.mu.Lock()
	if m.job == nil || m.job.View.ID != id || m.busy {
		m.mu.Unlock()
		return Job{}, errors.New("wait for the current setup step")
	}
	index := -1
	var app AppStep
	for i, a := range m.job.View.Apps {
		if a.ID == appID {
			index = i
			app = a
			break
		}
	}
	if index < 0 || !app.Managed || (app.State != "needs_open" && app.State != "opened" && app.State != "blocked") {
		m.mu.Unlock()
		return Job{}, errors.New("only a newly managed app awaiting approval may be opened here")
	}
	m.busy = true
	m.mu.Unlock()
	defer m.finishWork()
	actual, err := m.installer.Detect(ctx, app.ID, appDirectory(app, m.job.View.Request.AppDir))
	if err != nil || actual.State != companioninstall.DetectAdopt || actual.Path != app.Path {
		return Job{}, errors.New("installed app identity changed; it was not opened")
	}
	args := []string{"-data=" + filepath.Join(m.home, "companions", string(app.ID)), "-nobrowser"}
	if app.ID == companioninstall.QBittorrent {
		args = []string{"--profile=" + filepath.Join(m.home, "companions", string(app.ID))}
	}
	if err := m.openApp(ctx, app.Path, args); err != nil {
		return Job{}, errors.New("macOS did not open the app. Review its first-launch prompt and try again")
	}
	app.State = "opened"
	app.Detail = "Open requested through macOS; native readiness is not checked yet."
	if err := m.update("needs_approval", "Complete any app prompts, then check the native connections.", index, &app); err != nil {
		return Job{}, err
	}
	return m.Status()
}

func openNativeApp(ctx context.Context, path string, args []string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("native app opening is not supported on this platform")
	}
	argv := append([]string{"-g", "-a", path, "--args"}, args...)
	return exec.CommandContext(ctx, "/usr/bin/open", argv...).Run()
}

func (m *Manager) Continue(id string) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.job == nil || m.job.View.ID != id || m.busy {
		return Job{}, errors.New("wait for the current setup step")
	}
	if m.job.View.State != "needs_approval" && m.job.View.State != "needs_sources" && m.job.View.State != "blocked" && m.job.View.State != "ready" {
		return Job{}, errors.New("finish installation and first-launch approval before checking connections")
	}
	m.job.View.State = "checking"
	m.job.View.Detail = "Testing native connections and reading the actual saved settings…"
	m.job.View.Checks = []Check{}
	if err := m.saveLocked(); err != nil {
		return Job{}, err
	}
	m.busy = true
	go m.configure()
	return cloneJob(m.job.View), nil
}

func (m *Manager) connectionConfig() config.Config {
	c := m.store.Get()
	for _, app := range m.job.View.Apps {
		if app.Managed || app.ConnectionDetected {
			if app.ID == companioninstall.QBittorrent {
				c.QBit = m.job.Candidate.QBit
			} else {
				setArr(&c, app.ID, getArr(m.job.Candidate, app.ID))
			}
		}
	}
	return c
}

func (m *Manager) addCheck(app, name, state, detail string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.job.View.Checks = append(m.job.View.Checks, Check{App: app, Name: name, State: state, Detail: detail})
	return m.saveLocked()
}

func nativePing(c config.Config, app AppStep) error {
	if app.ID == companioninstall.QBittorrent {
		if c.QBit.URL == "" {
			return errors.New("connect the existing qBittorrent account in Settings")
		}
		_, err := stock.NewQBit(c.QBit.URL, c.QBit.Username, c.QBit.Password).Version()
		return err
	}
	a := getArr(c, app.ID)
	if a.URL == "" || a.APIKey == "" {
		return fmt.Errorf("connect the existing %s account in Settings", app.Name)
	}
	return stock.NewArr(app.Name, a.URL, a.APIKey).Ping()
}

func selected(req Request, id companioninstall.AppID) bool {
	for _, a := range req.Apps {
		if a == id {
			return true
		}
	}
	return false
}
func categoryFor(req Request, id companioninstall.AppID) string {
	return strings.TrimSpace(req.Categories[string(id)])
}

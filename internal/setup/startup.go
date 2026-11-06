package setup

import (
	"errors"
	"os"
	"path/filepath"

	"librarystudio/internal/platform"
)

func (m *Manager) SetHostPort(port int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.busy && port >= 1024 && port <= 65535 {
		m.hostPort = port
	}
}

func (m *Manager) installStartup() (bool, error) {
	if m.hostPort < 1024 || m.hostPort > 65535 {
		return false, errors.New("Library's actual bound host port is unavailable; reopen the desktop app before setting up startup")
	}
	for _, app := range m.job.View.Apps {
		if !app.Managed {
			continue
		}
		status, err := m.installCompanion(platform.CompanionServiceSpec{ID: platform.CompanionID(app.ID), AppPath: app.Path, ProfileDir: filepath.Join(m.home, "companions", string(app.ID)), Home: m.home, AtLogin: true})
		if err != nil {
			return false, err
		}
		if !status.Supported || !status.Installed || !status.Loaded || status.State == platform.HostServiceNeedsRestart || status.State == platform.HostServiceBlocked {
			return false, errors.New("the selected companion's at-login definition is not loaded; review its saved startup state")
		}
		if err := m.addCheck(app.Name, "At-login configuration", "passed", "The selected managed app's login definition is saved and loaded. A new login has not been tested."); err != nil {
			return false, err
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	status, err := m.installHost(platform.HostServiceSpec{Exe: exe, Home: m.home, Port: m.hostPort})
	if err != nil {
		return false, err
	}
	if status.NeedsRestart || status.State == platform.HostServiceNeedsRestart {
		if err := m.addCheck("Library", "At-login configuration", "not_checked", "Saved. Quit and reopen Library to hand off the desktop server, then check setup again."); err != nil {
			return false, err
		}
		return false, m.update("needs_restart", "Startup is saved. Quit and reopen Library, then check setup to verify the background host handoff.", -1, nil)
	}
	if !status.Supported || !status.Installed || !status.Loaded {
		return false, errors.New("Library's selected background login definition is not loaded")
	}
	if err := m.addCheck("Library", "At-login configuration", "passed", "The selected login definition is saved and loaded; this request reached the active Library server. A new login has not been tested."); err != nil {
		return false, err
	}
	return true, nil
}

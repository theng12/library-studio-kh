package setup

import (
	"context"
	"os"
	"path/filepath"

	"librarystudio/internal/companioninstall"
	"librarystudio/internal/config"
)

func (m *Manager) detectExisting(ctx context.Context, id companioninstall.AppID, preferred string) (companioninstall.DetectResult, error) {
	first, err := m.installer.Detect(ctx, id, preferred)
	if err != nil || first.State != companioninstall.DetectMissing {
		return first, err
	}
	for _, dir := range m.applicationDirs {
		if filepath.Clean(dir) == filepath.Clean(preferred) {
			continue
		}
		found, err := m.installer.Detect(ctx, id, dir)
		if err != nil {
			return found, err
		}
		if found.State == companioninstall.DetectAdopt {
			return found, nil
		}
	}
	return first, nil
}

// Reading an existing profile never creates directories or changes its binding.
func (m *Manager) adoptConnection(app *AppStep) {
	if app.ID == companioninstall.QBittorrent {
		return
	}
	current := getArr(m.job.Candidate, app.ID)
	if current.URL != "" && current.APIKey != "" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dirs := []string{filepath.Join(m.home, "companions", string(app.ID)), filepath.Join(home, "Library", "Application Support", app.Name), filepath.Join(home, ".config", app.Name)}
	for _, dir := range dirs {
		p, err := ReadArrProfile(string(app.ID), dir)
		if err != nil {
			continue
		}
		setArr(&m.job.Candidate, app.ID, config.App{URL: p.URL, APIKey: p.APIKey})
		app.ConnectionDetected = true
		app.Detail = "Keep the installed app and check its existing loopback profile."
		return
	}
}

func appDirectory(app AppStep, fallback string) string {
	if app.Path != "" {
		return filepath.Dir(app.Path)
	}
	return fallback
}

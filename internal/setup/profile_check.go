package setup

import (
	"errors"
	"os"
	"path/filepath"

	"librarystudio/internal/companioninstall"
	"librarystudio/internal/config"
)

// Check the native file after first launch, before treating its API as private.
func (m *Manager) checkManagedProfile(app AppStep, c *config.Config) error {
	if !app.Managed {
		return nil
	}
	if app.ID == companioninstall.QBittorrent {
		dir := filepath.Join(m.home, "companions", "qbittorrent")
		for _, path := range []string{filepath.Join(dir, "qBittorrent", "config", "qBittorrent.ini"), filepath.Join(dir, "webui-credentials.json")} {
			if _, err := os.Lstat(path); err != nil {
				return errors.New("the managed qBittorrent profile is missing; it was not recreated while the app may be running")
			}
		}
		p, err := PrepareQBitProfile(m.home, m.job.View.Request.Ports["qbittorrent"])
		if err != nil {
			return err
		}
		if p.URL != c.QBit.URL {
			return errors.New("qBittorrent's native port differs from the reviewed connection; check its settings before continuing")
		}
		c.QBit = config.QBit{URL: p.URL, Username: p.Username, Password: p.Password}
		return nil
	}
	p, err := ReadArrProfile(string(app.ID), filepath.Join(m.home, "companions", string(app.ID)))
	if err != nil {
		return err
	}
	if p.URL != getArr(*c, app.ID).URL {
		return errors.New("the native port differs from the reviewed connection; check its settings before continuing")
	}
	setArr(c, app.ID, config.App{URL: p.URL, APIKey: p.APIKey})
	return nil
}

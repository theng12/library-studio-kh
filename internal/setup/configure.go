package setup

import (
	"errors"
	"fmt"
	"strings"

	"librarystudio/internal/companioninstall"
	"librarystudio/internal/config"
	"librarystudio/internal/library"
	"librarystudio/internal/stock"
)

func (m *Manager) configure() {
	defer m.finishWork()
	before := m.store.Get()
	req := m.job.View.Request
	if selected(req, companioninstall.Whisparr) && before.WhisparrOn != m.job.Baseline.WhisparrOn {
		m.block("Whisparr", "Save connection", errors.New("Whisparr's enable setting changed after planning; it was preserved"), before)
		return
	}
	c := m.connectionConfig()
	blocked := false
	for index, app := range m.job.View.Apps {
		if helperApp(app.ID) {
			// Optional helpers have no native account here; they are written
			// and verified after every selected connection exists.
			continue
		}
		err := m.checkManagedProfile(app, &c)
		if err == nil {
			err = nativePing(c, app)
		}
		state := "passed"
		detail := "Native API answered with the saved account."
		if err != nil {
			state = "failed"
			detail = safeSetupError(err, c)
			blocked = true
			app.State = "blocked"
			app.Detail = detail
		} else {
			app.State = "connected"
			app.Detail = detail
		}
		if m.addCheck(app.Name, "Native connection", state, detail) != nil {
			return
		}
		if m.update("checking", "Checking selected native accounts…", index, &app) != nil {
			return
		}
	}
	if blocked {
		_ = m.update("blocked", "Some native connections are unavailable. Complete the app's prompts or connect its existing account in Settings, then check again.", -1, nil)
		return
	}
	if ready, err := m.musicPreferencesReady(c); err != nil {
		m.block("Lidarr", "Music profiles", err, c)
		return
	} else if !ready {
		return
	}
	if m.update("configuring", "Configuring selected folders, categories and native links…", -1, nil) != nil {
		return
	}
	if selected(req, companioninstall.QBittorrent) {
		if err := ensureSetupFolder(req.DownloadPath); err != nil {
			m.block("qBittorrent", "Download folder", err, c)
			return
		}
		for _, app := range m.job.View.Apps {
			if app.ID == companioninstall.QBittorrent && app.Managed {
				q := stock.NewQBit(c.QBit.URL, c.QBit.Username, c.QBit.Password)
				if err := q.SetPrefs(map[string]any{"save_path": req.DownloadPath}); err != nil {
					m.block(app.Name, "Download folder", err, c)
					return
				}
				prefs, err := q.Prefs()
				if err != nil {
					m.block(app.Name, "Download folder", err, c)
					return
				}
				if path, ok := prefs["save_path"].(string); !ok || cleanFolder(path) != cleanFolder(req.DownloadPath) {
					m.block(app.Name, "Download folder", errors.New("qBittorrent did not read back the chosen download folder"), c)
					return
				}
				if m.addCheck(app.Name, "Download folder", "passed", "Read back from qBittorrent; seeding limits were not changed.") != nil {
					return
				}
			}
		}
	}
	for _, app := range m.job.View.Apps {
		if app.ID == companioninstall.QBittorrent || app.ID == companioninstall.Prowlarr || helperApp(app.ID) {
			continue
		}
		if c.QBit.URL == "" {
			m.block(app.Name, "Download client", errors.New("connect qBittorrent before configuring its native clients"), c)
			return
		}
		root := req.Roots[string(app.ID)]
		if err := ensureSetupFolder(root); err != nil {
			m.block(app.Name, "Library folder", err, c)
			return
		}
		aConfig := getArr(c, app.ID)
		a := stock.NewArr(app.Name, aConfig.URL, aConfig.APIKey)
		folder, err := a.EnsureRootFolder(stock.RootFolderInput{Path: root, QualityProfileID: req.MusicQualityProfileID, MetadataProfileID: req.MusicMetadataProfileID})
		if err != nil {
			m.block(app.Name, "Library folder", err, c)
			return
		}
		if folder.Readback != stock.CheckPassed || folder.Accessible == nil || !*folder.Accessible {
			m.block(app.Name, "Library folder", errors.New("the native app has not confirmed this folder is accessible; review its macOS folder access"), c)
			return
		}
		if m.addCheck(app.Name, "Library folder", "passed", "The native app reports the selected root is accessible.") != nil {
			return
		}
		category := categoryFor(req, app.ID)
		q := stock.NewQBit(c.QBit.URL, c.QBit.Username, c.QBit.Password)
		cat, err := q.EnsureCategory(stock.CategoryInput{Name: category, SavePath: req.DownloadPath})
		if err != nil {
			m.block(app.Name, "Download category", err, c)
			return
		}
		if cat.Readback != stock.CheckPassed {
			m.block(app.Name, "Download category", errors.New("qBittorrent did not verify the selected category"), c)
			return
		}
		if m.addCheck(app.Name, "Download category", "passed", "Read back from qBittorrent; no unsupported category test was claimed.") != nil {
			return
		}
		client, err := a.EnsureQBitClient(stock.QBitClientInput{URL: c.QBit.URL, Username: c.QBit.Username, Password: c.QBit.Password, Category: category})
		if err != nil {
			m.block(app.Name, "Download client", err, c)
			return
		}
		if client.Test != stock.CheckPassed || client.Readback != stock.CheckPassed || client.SavedSecretCheck != stock.CheckPassed {
			m.block(app.Name, "Download client", errors.New("the saved native download-client login is not verified; test or repair it in the native app before continuing"), c)
			return
		}
		if m.addCheck(app.Name, "Download client", "passed", "Native connectivity and the saved login were verified.") != nil {
			return
		}
		if app.Managed {
			on, off := true, false
			patch := library.CompanionPatch{App: app.Name, EnableCompletedDownloadHandling: &on, RemoveCompletedDownloads: &off, ClientID: &client.ClientID}
			if app.ID == companioninstall.Sonarr {
				patch.RenameEpisodes = &req.RenameTV
			}
			if _, err := library.ApplyCompanionSettings(c, patch); err != nil {
				m.block(app.Name, "Import and naming settings", err, c)
				return
			}
			if m.addCheck(app.Name, "Import and naming settings", "passed", "Applied and read back for the newly managed profile; other naming choices were preserved.") != nil {
				return
			}
		}
		if c.Prowlarr.URL != "" && c.Prowlarr.APIKey != "" {
			p := stock.NewArr("Prowlarr", c.Prowlarr.URL, c.Prowlarr.APIKey)
			link, err := p.EnsureProwlarrApplication(stock.ApplicationInput{Target: app.Name, BaseURL: aConfig.URL, APIKey: aConfig.APIKey, ProwlarrURL: c.Prowlarr.URL})
			if err != nil {
				m.block(app.Name, "Prowlarr link", err, c)
				return
			}
			if link.Test != stock.CheckPassed || link.Readback != stock.CheckPassed || link.SavedSecretCheck != stock.CheckPassed {
				m.block(app.Name, "Prowlarr link", errors.New("the saved Prowlarr application login is not verified; test or repair it in Prowlarr before continuing"), c)
				return
			}
			if m.addCheck(app.Name, "Prowlarr link", "passed", "Prowlarr tested the native application and its saved login.") != nil {
				return
			}
		}
	}
	// Only new managed connections are merged; edits made during checking win.
	next := before
	if selected(req, companioninstall.Whisparr) {
		next.WhisparrOn = true
	}
	for _, app := range m.job.View.Apps {
		if app.Managed || app.ConnectionDetected {
			if app.ID == companioninstall.QBittorrent {
				if before.QBit != m.job.Baseline.QBit && before.QBit != c.QBit {
					m.block(app.Name, "Save connection", errors.New("Library's qBittorrent account changed after planning; it was preserved"), c)
					return
				}
				next.QBit = c.QBit
			} else {
				old := getArr(before, app.ID)
				candidate := getArr(c, app.ID)
				if old != getArr(m.job.Baseline, app.ID) && old != candidate {
					m.block(app.Name, "Save connection", errors.New("Library's native account changed after planning; it was preserved"), c)
					return
				}
				setArr(&next, app.ID, candidate)
			}
		}
	}
	if next.DownloadPath == "" {
		next.DownloadPath = req.DownloadPath
	}
	if next.LibraryPath == "" {
		if root := req.Roots["radarr"]; root != "" {
			next.LibraryPath = root
		} else {
			next.LibraryPath = req.Roots["sonarr"]
		}
	}
	if err := m.store.SaveIfUnchanged(before, next); err != nil {
		m.block("Library", "Save connections", err, c)
		return
	}
	if selected(req, companioninstall.Whisparr) {
		// Persist our own enable change so later owner OFF wins on retries and resume.
		m.mu.Lock()
		m.job.Baseline.WhisparrOn = next.WhisparrOn
		err := m.saveLocked()
		m.mu.Unlock()
		if err != nil {
			m.block("Whisparr", "Save setup state", err, c)
			return
		}
	}
	// Only after the connections are safely saved do the optional helpers get
	// their own configuration files, written and read back as real checks.
	if !m.configureOptional(c, req) {
		return
	}
	if err := m.verifySources(c); err != nil {
		if m.addCheck("Prowlarr", "Search sources", "not_checked", safeSetupError(err, c)) != nil {
			return
		}
		_ = m.update("needs_sources", "Native apps and folders are connected. Add and test a search source in Sources, then check setup again.", -1, nil)
		return
	}
	if req.StartAtLogin {
		if ready, err := m.installStartup(); err != nil {
			m.block("Library", "At-login configuration", err, c)
			return
		} else if !ready {
			return
		}
	}
	_ = m.update("ready", "Selected apps, folders, native links and search sources passed their checks. Optional helper configurations were written or kept; their runtime was not started or verified. Automatic cleanup remains at your saved setting.", -1, nil)
}

func (m *Manager) verifySources(c config.Config) error {
	if c.Prowlarr.URL == "" || c.Prowlarr.APIKey == "" {
		return errors.New("connect Prowlarr and add a supported source")
	}
	p := stock.NewArr("Prowlarr", c.Prowlarr.URL, c.Prowlarr.APIKey)
	items, err := p.Indexers()
	if err != nil {
		return err
	}
	checked := 0
	for _, item := range items {
		if item.Enabled != nil && *item.Enabled {
			if err := p.TestIndexer(stock.SourceInput{ID: item.ID}); err != nil {
				return fmt.Errorf("a configured source failed its native test: %w", err)
			}
			checked++
		}
	}
	if checked == 0 {
		return errors.New("no enabled source is configured")
	}
	return m.addCheck("Prowlarr", "Search sources", "passed", "Enabled sources passed their native connection tests; no media search or download was started.")
}

func (m *Manager) block(app, name string, err error, c config.Config) {
	if m.addCheck(app, name, "failed", safeSetupError(err, c)) != nil {
		return
	}
	_ = m.update("blocked", app+": "+safeSetupError(err, c), -1, nil)
}

func safeSetupError(err error, c config.Config) string {
	text := err.Error()
	for _, secret := range []string{c.QBit.Password, c.Sonarr.APIKey, c.Radarr.APIKey, c.Lidarr.APIKey, c.Prowlarr.APIKey, c.Bazarr.APIKey, c.Whisparr.APIKey} {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[private]")
		}
	}
	return text
}

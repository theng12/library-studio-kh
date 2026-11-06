package setup

import (
	"errors"

	"librarystudio/internal/companioninstall"
	"librarystudio/internal/config"
	"librarystudio/internal/stock"
)

func profileOffered(options []stock.Option, id int) bool {
	for _, option := range options {
		if option.ID == id && id > 0 {
			return true
		}
	}
	return false
}

func (m *Manager) musicPreferencesReady(c config.Config) (bool, error) {
	req := m.job.View.Request
	if !selected(req, companioninstall.Lidarr) {
		return true, nil
	}
	a := stock.NewArr("Lidarr", c.Lidarr.URL, c.Lidarr.APIKey)
	options, err := a.Options()
	if err != nil {
		return false, err
	}
	for _, root := range options.RootFolders {
		if cleanFolder(root.Path) == cleanFolder(req.Roots["lidarr"]) {
			return true, nil
		}
	}
	if profileOffered(options.QualityProfiles, req.MusicQualityProfileID) && profileOffered(options.MetadataProfiles, req.MusicMetadataProfileID) {
		return true, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.job.View.MusicProfiles = &options
	m.job.View.State = "needs_preferences"
	m.job.View.Detail = "Choose the music quality and metadata profiles Lidarr currently offers before creating its new library folder."
	return false, m.saveLocked()
}

func (m *Manager) ChooseMusicProfiles(id string, quality, metadata int) (Job, error) {
	m.mu.Lock()
	if m.job == nil || m.job.View.ID != id || m.busy || m.job.View.State != "needs_preferences" || m.job.View.MusicProfiles == nil {
		m.mu.Unlock()
		return Job{}, errors.New("wait until setup asks for the music profiles")
	}
	choices := m.job.View.MusicProfiles
	if !profileOffered(choices.QualityProfiles, quality) || !profileOffered(choices.MetadataProfiles, metadata) {
		m.mu.Unlock()
		return Job{}, errors.New("choose the quality and metadata profiles Lidarr currently lists")
	}
	m.job.View.Request.MusicQualityProfileID = quality
	m.job.View.Request.MusicMetadataProfileID = metadata
	m.job.View.State = "blocked"
	if err := m.saveLocked(); err != nil {
		m.mu.Unlock()
		return Job{}, err
	}
	m.mu.Unlock()
	return m.Continue(id)
}

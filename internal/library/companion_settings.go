package library

import (
	"fmt"

	"librarystudio/internal/config"
	"librarystudio/internal/stock"
)

// CompanionPolicy explains which side owns each setting. The UI shows it next
// to every row so "controlled by Sonarr" never reads as "Library policy".
const CompanionPolicy = "Library manages local copies. Remove-after-import stays off in each library app; qBittorrent per-torrent limits are separate and are not changed here."

type NumberRange struct {
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	Step float64 `json:"step"`
}

// CompanionSetting is one allowlisted native setting with its actual value.
// Control is toggle, number or stop. ManagedBy is "app" or "library".
type CompanionSetting struct {
	Key       string       `json:"key"`
	Label     string       `json:"label"`
	Control   string       `json:"control"`
	Value     any          `json:"value"`
	Unit      string       `json:"unit,omitempty"`
	Range     *NumberRange `json:"range,omitempty"`
	Editable  bool         `json:"editable"`
	ManagedBy string       `json:"managedBy"`
	Note      string       `json:"note,omitempty"`
}

type CompanionClient struct {
	ID                       int    `json:"id"`
	Name                     string `json:"name"`
	Implementation           string `json:"implementation"`
	RemoveCompletedDownloads bool   `json:"removeCompletedDownloads"`
	CanDisable               bool   `json:"canDisable"`
	Reason                   string `json:"reason,omitempty"`
}

type CompanionApp struct {
	App        string             `json:"app"`
	Configured bool               `json:"configured"`
	Connected  bool               `json:"connected"`
	Error      string             `json:"error,omitempty"`
	Settings   []CompanionSetting `json:"settings"`
	Clients    []CompanionClient  `json:"clients,omitempty"`
}

type CompanionState struct {
	Apps   []CompanionApp `json:"apps"`
	QBit   CompanionApp   `json:"qbittorrent"`
	Policy string         `json:"policy"`
}

// CompanionPatch is the typed, allowlisted write surface. A nil field is left
// alone; the server rejects unknown JSON fields.
type CompanionPatch struct {
	App                             string   `json:"app"`
	ClientID                        *int     `json:"clientId,omitempty"`
	EnableCompletedDownloadHandling *bool    `json:"enableCompletedDownloadHandling,omitempty"`
	RemoveCompletedDownloads        *bool    `json:"removeCompletedDownloads,omitempty"`
	RenameEpisodes                  *bool    `json:"renameEpisodes,omitempty"`
	RenameMovies                    *bool    `json:"renameMovies,omitempty"`
	MaxRatioEnabled                 *bool    `json:"max_ratio_enabled,omitempty"`
	MaxRatio                        *float64 `json:"max_ratio,omitempty"`
	MaxSeedingTimeEnabled           *bool    `json:"max_seeding_time_enabled,omitempty"`
	MaxSeedingTime                  *int     `json:"max_seeding_time,omitempty"`
	MaxRatioAct                     *int     `json:"max_ratio_act,omitempty"`
}

// ValidationError is a request the owner must fix; HTTP maps it to 400 rather
// than reporting an upstream failure.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// PartialApplyError means the write surface was valid but an upstream write
// failed. Applied names the fields that really changed; the caller must return
// fresh state, not a blank failure.
type PartialApplyError struct {
	Applied []string
	Err     error
}

func (e *PartialApplyError) Error() string { return e.Err.Error() }
func (e *PartialApplyError) Unwrap() error { return e.Err }

type arrTarget struct {
	name string
	cfg  config.App
}

func companionArrs(c config.Config) []arrTarget {
	return []arrTarget{
		{"Sonarr", c.Sonarr},
		{"Radarr", c.Radarr},
		{"Lidarr", c.Lidarr},
	}
}

// CompanionSettings reads the actual supported settings from each connected app.
func CompanionSettings(c config.Config) CompanionState {
	st := CompanionState{Apps: []CompanionApp{}, Policy: CompanionPolicy}
	for _, t := range companionArrs(c) {
		st.Apps = append(st.Apps, readArrCompanion(t))
	}
	st.QBit = readQBitCompanion(c)
	return st
}

func readArrCompanion(t arrTarget) CompanionApp {
	app := CompanionApp{App: t.name, Settings: []CompanionSetting{}, Clients: []CompanionClient{}}
	if t.cfg.URL == "" || t.cfg.APIKey == "" {
		app.Error = "Not connected. Add its address and API key in Settings."
		return app
	}
	app.Configured = true
	a := stock.NewArr(t.name, t.cfg.URL, t.cfg.APIKey)
	s, err := a.CompanionSettings()
	if err != nil {
		app.Error = a.SafeError(err).Error()
		return app
	}
	app.Connected = true
	app.Settings = append(app.Settings, CompanionSetting{
		Key: "enableCompletedDownloadHandling", Label: "Completed download handling", Control: "toggle",
		Value: s.Handling, Editable: true, ManagedBy: "app",
		Note: "Lets " + t.name + " import downloads that finish. Library can only check imports while this is on.",
	})
	switch t.name {
	case "Sonarr":
		app.Settings = append(app.Settings, renameSetting("renameEpisodes", "Rename episodes", s.RenameEpisodes, s.NamingError, t.name))
	case "Radarr":
		app.Settings = append(app.Settings, renameSetting("renameMovies", "Rename films", s.RenameMovies, s.NamingError, t.name))
	}
	for _, cl := range s.Clients {
		c := CompanionClient{ID: cl.ID, Name: cl.Name, Implementation: cl.Implementation, RemoveCompletedDownloads: cl.RemoveCompletedDownloads}
		if cl.RemoveCompletedDownloads {
			if cl.Implementation == "QBittorrent" {
				c.CanDisable = true
				c.Reason = "Turns file removal off for this client. Library keeps local copies until its staged policy says otherwise."
			} else {
				c.Reason = "Library changes this only for qBittorrent clients. Open " + t.name + " and turn off Remove Completed Downloads for this " + cl.Implementation + " client."
			}
		}
		app.Clients = append(app.Clients, c)
	}
	return app
}

func renameSetting(key, label string, value *bool, readErr, appName string) CompanionSetting {
	s := CompanionSetting{Key: key, Label: label, Control: "toggle", ManagedBy: "app",
		Note: "Tells " + appName + " to rename imported files. It changes file names, never what is imported."}
	if readErr != "" {
		s.Note = "Couldn't read this from " + appName + ": " + readErr
		return s
	}
	if value == nil {
		s.Note = appName + " did not report this setting."
		return s
	}
	s.Value, s.Editable = *value, true
	return s
}

func readQBitCompanion(c config.Config) CompanionApp {
	app := CompanionApp{App: "qBittorrent", Settings: []CompanionSetting{}}
	if c.QBit.URL == "" {
		app.Error = "Not connected. Add its address in Settings."
		return app
	}
	app.Configured = true
	q := stock.NewQBit(c.QBit.URL, c.QBit.Username, c.QBit.Password)
	s, err := q.Seeding()
	if err != nil {
		app.Error = err.Error()
		return app
	}
	app.Connected = true
	app.Settings = append(app.Settings,
		toggleSeeding("max_ratio_enabled", "Limit share ratio", s.MaxRatioEnabled,
			"Stops a torrent when its share ratio reaches the limit."),
		numberSeeding("max_ratio", "Share ratio limit", s.MaxRatio, "ratio", &NumberRange{Min: 0, Max: 1000, Step: 0.1},
			"This is qBittorrent's global default. Per-torrent limits set in qBittorrent are separate."),
		toggleSeeding("max_seeding_time_enabled", "Limit seeding time", s.MaxSeedingTimeEnabled,
			"Stops a torrent when its seeding time reaches the limit."),
		numberSeeding("max_seeding_time", "Seeding time limit", intAsFloat(s.MaxSeedingTime), "minutes", &NumberRange{Min: 0, Max: 525600, Step: 1},
			"2880 minutes is 2 days. This is the global default; per-torrent limits are separate."),
	)
	act := CompanionSetting{Key: "max_ratio_act", Label: "When a limit is reached", Control: "stop",
		ManagedBy: "library", Note: "Library only ever sets Stop torrent. It never removes a torrent or its files."}
	if s.MaxRatioAct == nil {
		act.Note = "qBittorrent did not report this setting."
	} else {
		act.Value = *s.MaxRatioAct
		act.Editable = true
		if *s.MaxRatioAct != 0 {
			act.Note = fmt.Sprintf("qBittorrent is set to a removal action (%d). Apply Stop to change it; Library never chooses removal.", *s.MaxRatioAct)
		}
	}
	app.Settings = append(app.Settings, act)
	return app
}

func toggleSeeding(key, label string, value *bool, note string) CompanionSetting {
	s := CompanionSetting{Key: key, Label: label, Control: "toggle", ManagedBy: "app", Note: note}
	if value == nil {
		s.Note = "qBittorrent did not report this setting."
		return s
	}
	s.Value, s.Editable = *value, true
	return s
}

func numberSeeding(key, label string, value *float64, unit string, r *NumberRange, note string) CompanionSetting {
	s := CompanionSetting{Key: key, Label: label, Control: "number", Unit: unit, Range: r, ManagedBy: "app", Note: note}
	if value == nil {
		s.Note = "qBittorrent did not report this setting."
		return s
	}
	s.Value, s.Editable = *value, true
	return s
}

func intAsFloat(v *int) *float64 {
	if v == nil {
		return nil
	}
	f := float64(*v)
	return &f
}

// ApplyCompanionSettings writes only the requested typed fields, then reads the
// actual values back. It never applies anything else and never fixes settings
// on its own.
func ApplyCompanionSettings(c config.Config, p CompanionPatch) (CompanionApp, error) {
	switch p.App {
	case "Sonarr", "Radarr", "Lidarr":
		return applyArrCompanion(c, p)
	case "qBittorrent":
		return applyQBitCompanion(c, p)
	}
	return CompanionApp{}, invalid("Choose Sonarr, Radarr, Lidarr or qBittorrent.")
}

func applyArrCompanion(c config.Config, p CompanionPatch) (CompanionApp, error) {
	var target *arrTarget
	for _, t := range companionArrs(c) {
		if t.name == p.App {
			t := t
			target = &t
		}
	}
	if target == nil {
		return CompanionApp{}, invalid("Choose Sonarr, Radarr, Lidarr or qBittorrent.")
	}
	if target.cfg.URL == "" || target.cfg.APIKey == "" {
		return CompanionApp{}, invalid("%s isn't connected yet. Add its address and API key in Settings.", p.App)
	}
	if p.MaxRatioEnabled != nil || p.MaxRatio != nil || p.MaxSeedingTimeEnabled != nil || p.MaxSeedingTime != nil || p.MaxRatioAct != nil {
		return CompanionApp{}, invalid("Seeding limits belong to qBittorrent.")
	}
	if p.RenameEpisodes != nil && p.App != "Sonarr" {
		return CompanionApp{}, invalid("Only Sonarr renames episodes.")
	}
	if p.RenameMovies != nil && p.App != "Radarr" {
		return CompanionApp{}, invalid("Only Radarr renames films.")
	}
	if p.RemoveCompletedDownloads != nil && *p.RemoveCompletedDownloads {
		return CompanionApp{}, invalid("Library never turns immediate removal on. It can only turn it off.")
	}
	if p.ClientID != nil {
		if p.RemoveCompletedDownloads == nil {
			return CompanionApp{}, invalid("A client can only be chosen when turning remove-after-import off.")
		}
		if *p.ClientID <= 0 {
			return CompanionApp{}, invalid("Choose the download client to change.")
		}
	}
	if p.EnableCompletedDownloadHandling == nil && p.RemoveCompletedDownloads == nil && p.RenameEpisodes == nil && p.RenameMovies == nil {
		return CompanionApp{}, invalid("Choose at least one setting to apply.")
	}

	a := stock.NewArr(target.name, target.cfg.URL, target.cfg.APIKey)
	var applied []string
	clientID := 0
	if p.ClientID != nil {
		clientID = *p.ClientID
	}
	fail := func(err error) (CompanionApp, error) {
		app := readArrCompanion(*target)
		if !app.Connected && app.Error != "" {
			app.Error = "Saved changes couldn't be read back: " + app.Error
		}
		return app, &PartialApplyError{Applied: applied, Err: a.SafeError(err)}
	}
	if p.EnableCompletedDownloadHandling != nil {
		if err := a.SetCompletedDownloadHandling(*p.EnableCompletedDownloadHandling); err != nil {
			return fail(err)
		}
		applied = append(applied, "enableCompletedDownloadHandling")
	}
	if p.RemoveCompletedDownloads != nil {
		if err := a.DisableRemoveCompletedDownloads(clientID); err != nil {
			return fail(err)
		}
		applied = append(applied, "removeCompletedDownloads")
	}
	if p.RenameEpisodes != nil {
		if err := a.SetRename("renameEpisodes", *p.RenameEpisodes); err != nil {
			return fail(err)
		}
		applied = append(applied, "renameEpisodes")
	}
	if p.RenameMovies != nil {
		if err := a.SetRename("renameMovies", *p.RenameMovies); err != nil {
			return fail(err)
		}
		applied = append(applied, "renameMovies")
	}
	app := readArrCompanion(*target)
	if !app.Connected && app.Error != "" {
		app.Error = "Saved, but the new value couldn't be read back: " + app.Error
	}
	return app, nil
}

func applyQBitCompanion(c config.Config, p CompanionPatch) (CompanionApp, error) {
	if c.QBit.URL == "" {
		return CompanionApp{}, invalid("qBittorrent isn't connected yet. Add its address in Settings.")
	}
	if p.EnableCompletedDownloadHandling != nil || p.RemoveCompletedDownloads != nil || p.RenameEpisodes != nil || p.RenameMovies != nil {
		return CompanionApp{}, invalid("Completed-download handling and renaming belong to the library apps.")
	}
	if p.ClientID != nil {
		return CompanionApp{}, invalid("Download client ids belong to the library apps.")
	}
	if p.MaxRatioEnabled == nil && p.MaxRatio == nil && p.MaxSeedingTimeEnabled == nil && p.MaxSeedingTime == nil && p.MaxRatioAct == nil {
		return CompanionApp{}, invalid("Choose at least one seeding setting to apply.")
	}
	if p.MaxRatio != nil && (*p.MaxRatio < 0 || *p.MaxRatio > 1000) {
		return CompanionApp{}, invalid("Share ratio must be between 0 and 1000.")
	}
	if p.MaxSeedingTime != nil && (*p.MaxSeedingTime < 0 || *p.MaxSeedingTime > 525600) {
		return CompanionApp{}, invalid("Seeding time must be between 0 and 525600 minutes (365 days).")
	}
	if p.MaxRatioAct != nil && *p.MaxRatioAct != 0 {
		return CompanionApp{}, invalid("Library only sets Stop; it never lets qBittorrent remove torrents or files.")
	}

	q := stock.NewQBit(c.QBit.URL, c.QBit.Username, c.QBit.Password)
	err := q.ApplySeedingPatch(stock.SeedingPatch{
		MaxRatioEnabled:       p.MaxRatioEnabled,
		MaxRatio:              p.MaxRatio,
		MaxSeedingTimeEnabled: p.MaxSeedingTimeEnabled,
		MaxSeedingTime:        p.MaxSeedingTime,
		MaxRatioAct:           p.MaxRatioAct,
	})
	if err != nil {
		app := readQBitCompanion(c)
		if !app.Connected && app.Error != "" {
			app.Error = "Nothing changed; the current state couldn't be read back: " + app.Error
		}
		return app, &PartialApplyError{Err: err}
	}
	app := readQBitCompanion(c)
	if !app.Connected && app.Error != "" {
		app.Error = "Saved, but the new value couldn't be read back: " + app.Error
	}
	return app, nil
}

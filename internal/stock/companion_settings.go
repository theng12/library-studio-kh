package stock

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ---- Sonarr / Radarr / Lidarr: completed download handling ----

// DownloadClient is the allowlisted part of one app's download client entry.
type DownloadClient struct {
	ID                       int
	Name                     string
	Implementation           string
	RemoveCompletedDownloads bool
}

// ArrSettings is the supported, editable part of one library app's settings.
// Only fields listed here ever leave this package or accept a write.
type ArrSettings struct {
	Handling       bool
	HandlingID     int
	Clients        []DownloadClient
	RenameEpisodes *bool // Sonarr
	RenameMovies   *bool // Radarr
	NamingID       int
	NamingError    string
}

func (a *Arr) CompanionSettings() (ArrSettings, error) {
	out := ArrSettings{Clients: []DownloadClient{}}
	var cfg map[string]any
	if err := a.do("GET", "/config/downloadclient", nil, &cfg); err != nil {
		return out, err
	}
	out.Handling, _ = cfg["enableCompletedDownloadHandling"].(bool)
	out.HandlingID = asInt(cfg["id"])
	var clients []map[string]any
	if err := a.do("GET", "/downloadclient", nil, &clients); err != nil {
		return out, err
	}
	for _, c := range clients {
		out.Clients = append(out.Clients, DownloadClient{
			ID:                       asInt(c["id"]),
			Name:                     a.sanitizeText(asStr(c["name"])),
			Implementation:           a.sanitizeText(asStr(c["implementation"])),
			RemoveCompletedDownloads: c["removeCompletedDownloads"] == true,
		})
	}
	if a.Name == "Sonarr" || a.Name == "Radarr" {
		cfg, err := a.namingConfig()
		if err != nil {
			out.NamingError = a.SafeError(err).Error()
			return out, nil
		}
		out.NamingID = asInt(cfg["id"])
		if v, ok := cfg["renameEpisodes"].(bool); ok {
			out.RenameEpisodes = &v
		}
		if v, ok := cfg["renameMovies"].(bool); ok {
			out.RenameMovies = &v
		}
	}
	return out, nil
}

// SetCompletedDownloadHandling merges one field into the full app settings
// object, so every unrelated field and credential-shaped value is preserved.
func (a *Arr) SetCompletedDownloadHandling(on bool) error {
	var cfg map[string]any
	if err := a.do("GET", "/config/downloadclient", nil, &cfg); err != nil {
		return err
	}
	id := asInt(cfg["id"])
	if id == 0 {
		return fmt.Errorf("%s did not report its download client settings id", a.Name)
	}
	cfg["enableCompletedDownloadHandling"] = on
	return a.do("PUT", fmt.Sprintf("/config/downloadclient/%d", id), cfg, nil)
}

// DisableRemoveCompletedDownloads turns the immediate-removal flag off on one
// qBittorrent client when id is given, or on all of them when id is 0. It never
// turns it on and never edits a client of another implementation.
func (a *Arr) DisableRemoveCompletedDownloads(id int) error {
	var clients []map[string]any
	if err := a.do("GET", "/downloadclient", nil, &clients); err != nil {
		return err
	}
	found := false
	for _, c := range clients {
		if c["implementation"] != "QBittorrent" || c["removeCompletedDownloads"] != true {
			continue
		}
		if id > 0 && asInt(c["id"]) != id {
			continue
		}
		found = true
		clientID := asInt(c["id"])
		if clientID == 0 {
			return fmt.Errorf("%s did not report a download client id", a.Name)
		}
		c["removeCompletedDownloads"] = false
		if err := a.do("PUT", fmt.Sprintf("/downloadclient/%d", clientID), c, nil); err != nil {
			return err
		}
	}
	if id > 0 && !found {
		return fmt.Errorf("%s has no qBittorrent client with removal switched on under id %d", a.Name, id)
	}
	return nil
}

// SetRename changes only the requested naming flag, keeping the rest of the
// naming settings object intact.
func (a *Arr) SetRename(field string, on bool) error {
	cfg, err := a.namingConfig()
	if err != nil {
		return err
	}
	id := asInt(cfg["id"])
	if id == 0 {
		return fmt.Errorf("%s did not report its naming settings id", a.Name)
	}
	cfg[field] = on
	return a.do("PUT", fmt.Sprintf("/config/naming/%d", id), cfg, nil)
}

// namingConfig accepts both shapes the v3 API has used: one object or a list.
func (a *Arr) namingConfig() (map[string]any, error) {
	var raw json.RawMessage
	if err := a.do("GET", "/config/naming", nil, &raw); err != nil {
		return nil, err
	}
	var one map[string]any
	if json.Unmarshal(raw, &one) == nil && len(one) > 0 {
		return one, nil
	}
	var list []map[string]any
	if err := json.Unmarshal(raw, &list); err != nil || len(list) == 0 {
		return nil, fmt.Errorf("%s returned naming settings that could not be read", a.Name)
	}
	return list[0], nil
}

// ---- qBittorrent: global seeding limits ----

// QBitSeeding is the supported, editable part of qBittorrent's global preferences.
type QBitSeeding struct {
	MaxRatioEnabled       *bool
	MaxRatio              *float64
	MaxSeedingTimeEnabled *bool
	MaxSeedingTime        *int // minutes
	MaxRatioAct           *int
}

func (q *QBit) Seeding() (QBitSeeding, error) {
	p, err := q.Prefs()
	if err != nil {
		return QBitSeeding{}, err
	}
	return QBitSeeding{
		MaxRatioEnabled:       boolValue(p, "max_ratio_enabled"),
		MaxRatio:              floatValue(p, "max_ratio"),
		MaxSeedingTimeEnabled: boolValue(p, "max_seeding_time_enabled"),
		MaxSeedingTime:        intValue(p, "max_seeding_time"),
		MaxRatioAct:           intValue(p, "max_ratio_act"),
	}, nil
}

// SeedingPatch is an explicit, typed set of changes. Nil means "leave it alone".
type SeedingPatch struct {
	MaxRatioEnabled       *bool
	MaxRatio              *float64
	MaxSeedingTimeEnabled *bool
	MaxSeedingTime        *int
	MaxRatioAct           *int
}

// ApplySeedingPatch reads the whole preferences object, replaces only the
// fields the owner chose, and writes the merged object back. Unrelated settings,
// including any download speed caps, are passed through untouched.
func (q *QBit) ApplySeedingPatch(p SeedingPatch) error {
	v, err := q.Version()
	if err != nil {
		return err
	}
	if versionMajor(v) < 4 {
		return fmt.Errorf("qBittorrent %s is too old; this build needs 4.1 or newer", v)
	}
	prefs, err := q.Prefs()
	if err != nil {
		return err
	}
	if p.MaxRatioEnabled != nil {
		prefs["max_ratio_enabled"] = *p.MaxRatioEnabled
	}
	if p.MaxRatio != nil {
		prefs["max_ratio"] = *p.MaxRatio
	}
	if p.MaxSeedingTimeEnabled != nil {
		prefs["max_seeding_time_enabled"] = *p.MaxSeedingTimeEnabled
	}
	if p.MaxSeedingTime != nil {
		prefs["max_seeding_time"] = *p.MaxSeedingTime
	}
	if p.MaxRatioAct != nil {
		prefs["max_ratio_act"] = *p.MaxRatioAct
	}
	return q.SetPrefs(prefs)
}

// Version is qBittorrent's own application version, e.g. v5.2.3. Writes require
// the v2 WebUI API, which arrives with 4.1.
func (q *QBit) Version() (string, error) {
	if err := q.login(); err != nil {
		return "", err
	}
	resp, err := q.c.Get(q.base + "/api/v2/app/version")
	if err != nil {
		return "", fmt.Errorf("qBittorrent is not reachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("qBittorrent answered %s for /api/v2/app/version", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "", fmt.Errorf("qBittorrent did not report its version")
	}
	return v, nil
}

func versionMajor(v string) int {
	v = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(v)), "v")
	var n int
	fmt.Sscanf(v, "%d", &n)
	return n
}

func boolValue(m map[string]any, key string) *bool {
	v, ok := m[key].(bool)
	if !ok {
		return nil
	}
	return &v
}

func floatValue(m map[string]any, key string) *float64 {
	v, ok := m[key].(float64)
	if !ok {
		return nil
	}
	return &v
}

func intValue(m map[string]any, key string) *int {
	v, ok := m[key].(float64)
	if !ok {
		return nil
	}
	n := int(v)
	return &n
}

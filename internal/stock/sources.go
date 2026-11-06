package stock

import (
	"errors"
	"os"
	"regexp"
	"strings"
	"time"
)

// Source shapes only carry allowlisted fields; upstream config, credentials and
// base URLs are never decoded into them.

type SourceIndexer struct {
	ID            int        `json:"id"`
	Name          string     `json:"name"`
	Enabled       *bool      `json:"enabled"`
	Protocol      string     `json:"protocol,omitempty"`
	Priority      *int       `json:"priority,omitempty"`
	DisabledUntil *time.Time `json:"disabledUntil,omitempty"`
}

type SourceLink struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	SyncLevel string `json:"syncLevel,omitempty"`
}

type SourceHealth struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// Indexers lists Prowlarr's configured indexers with their observed enable flag.
func (a *Arr) Indexers() ([]SourceIndexer, error) {
	var raw []map[string]any
	if err := a.do("GET", "/indexer", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]SourceIndexer, 0, len(raw))
	for _, r := range raw {
		s := SourceIndexer{ID: asInt(r["id"]), Name: a.sanitizeText(asStr(r["name"])), Protocol: a.sanitizeText(asStr(r["protocol"]))}
		if v, ok := r["enable"].(bool); ok {
			s.Enabled = &v
		}
		if v, ok := r["priority"].(float64); ok {
			n := int(v)
			s.Priority = &n
		}
		out = append(out, s)
	}
	return out, nil
}

// IndexerStatuses maps indexer id to the until-when Prowlarr has disabled it
// (upstream field: disabledTill). Any read failure is an error so the caller
// reports the unknown state instead of letting it read as healthy.
func (a *Arr) IndexerStatuses() (map[int]time.Time, error) {
	var raw []map[string]any
	if err := a.do("GET", "/indexerstatus", nil, &raw); err != nil {
		return nil, err
	}
	out := map[int]time.Time{}
	for _, r := range raw {
		if t, ok := r["disabledTill"].(string); ok && t != "" {
			if when, err := time.Parse(time.RFC3339, t); err == nil && !when.IsZero() {
				out[asInt(r["indexerId"])] = when
			}
		}
	}
	return out, nil
}

// Applications lists Prowlarr's links to the library apps (name and syncLevel only).
func (a *Arr) Applications() ([]SourceLink, error) {
	var raw []map[string]any
	if err := a.do("GET", "/applications", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]SourceLink, 0, len(raw))
	for _, r := range raw {
		out = append(out, SourceLink{ID: asInt(r["id"]), Name: a.sanitizeText(asStr(r["name"])), SyncLevel: a.sanitizeText(asStr(r["syncLevel"]))})
	}
	return out, nil
}

// Health reads Prowlarr's reported health messages, sanitized before they leave.
func (a *Arr) Health() ([]SourceHealth, error) {
	var raw []map[string]any
	if err := a.do("GET", "/health", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]SourceHealth, 0, len(raw))
	for _, r := range raw {
		msg := a.sanitizeText(asStr(r["message"]))
		if msg == "" {
			continue
		}
		typ := asStr(r["type"])
		if !reSafe.MatchString(typ) {
			typ = "Message"
		}
		out = append(out, SourceHealth{Type: typ, Message: msg})
	}
	return out, nil
}

// LibraryIndexers lists one library app's configured indexers from its /indexer API.
func (a *Arr) LibraryIndexers() ([]SourceIndexer, error) {
	var raw []map[string]any
	if err := a.do("GET", "/indexer", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]SourceIndexer, 0, len(raw))
	for _, r := range raw {
		s := SourceIndexer{ID: asInt(r["id"]), Name: a.sanitizeText(asStr(r["name"])), Protocol: a.sanitizeText(asStr(r["protocol"]))}
		if v, ok := r["enable"].(bool); ok {
			s.Enabled = &v
		} else {
			known := 0
			for _, field := range []string{"enableRss", "enableAutomaticSearch", "enableInteractiveSearch"} {
				if enabled, ok := r[field].(bool); ok {
					known++
					if enabled {
						value := true
						s.Enabled = &value
						break
					}
				}
			}
			if s.Enabled == nil && known == 3 {
				value := false
				s.Enabled = &value
			}
		}
		out = append(out, s)
	}
	return out, nil
}

var (
	reURL  = regexp.MustCompile(`(?i)\b(?:https?|magnet):\S+|www\.[^\s]+`)
	reKey  = regexp.MustCompile(`(?i)\b(?:api[ _-]?key|apikey|passkey|password|passwd|token|secret|url)\b\s*[:=]?\s*\S+`)
	reSafe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// sanitizeText strips URLs, key-shaped values and this app's own secrets so no
// config, password, API key, base URL, passkey or download link can leave.
func (a *Arr) sanitizeText(s string) string {
	pairs := []string{}
	for _, v := range []string{a.key, os.Getenv("SONARR__APIKEY"), os.Getenv("RADARR__APIKEY"),
		os.Getenv("LIDARR__APIKEY"), os.Getenv("PROWLARR__APIKEY"), a.base} {
		if v != "" {
			pairs = append(pairs, v, "REDACTED")
		}
	}
	if len(pairs) > 0 {
		s = strings.NewReplacer(pairs...).Replace(s)
	}
	s = reURL.ReplaceAllString(s, "REDACTED")
	s = reKey.ReplaceAllString(s, "REDACTED")
	return strings.TrimSpace(s)
}

// SafeError keeps useful upstream reasons without returning source URLs or credentials.
func (a *Arr) SafeError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(a.sanitizeText(err.Error()))
}

func asStr(v any) string {
	s, _ := v.(string)
	return s
}

func asInt(v any) int {
	if n, ok := v.(float64); ok {
		return int(n)
	}
	return 0
}

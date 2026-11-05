// Package librarystudio holds the app's version and What's New, both read from CHANGELOG.md.
package librarystudio

import (
	_ "embed"
	"strings"
)

//go:embed CHANGELOG.md
var changelog string

type Release struct {
	Version string   `json:"version"`
	Date    string   `json:"date"`
	Notes   []string `json:"notes"`
}

// Releases lists CHANGELOG.md versions, newest first.
func Releases() []Release {
	var out []Release
	for _, line := range strings.Split(changelog, "\n") {
		line = strings.TrimSpace(line)
		if h, ok := strings.CutPrefix(line, "## "); ok {
			v, d, _ := strings.Cut(h, " — ")
			out = append(out, Release{Version: strings.TrimSpace(v), Date: strings.TrimSpace(d)})
		} else if n, ok := strings.CutPrefix(line, "- "); ok && len(out) > 0 {
			out[len(out)-1].Notes = append(out[len(out)-1].Notes, n)
		}
	}
	return out
}

// Version is the newest CHANGELOG.md heading, the single source of the app version.
func Version() string {
	if r := Releases(); len(r) > 0 {
		return r[0].Version
	}
	return "unknown"
}

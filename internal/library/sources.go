package library

import (
	"strings"

	"librarystudio/internal/config"
	"librarystudio/internal/stock"
)

// SourceIndexer, SourceLink and SourceHealth are the allowlisted shapes returned
// to the dashboard; see stock/sources.go.
type (
	SourceIndexer = stock.SourceIndexer
	SourceLink    = stock.SourceLink
	SourceHealth  = stock.SourceHealth
)

type SourceLibrary struct {
	App        string          `json:"app"`
	Configured bool            `json:"configured"`
	Indexers   []SourceIndexer `json:"indexers"`
	Error      string          `json:"error,omitempty"`
}

type SourceReport struct {
	Configured bool            `json:"configured"`
	Indexers   []SourceIndexer `json:"indexers"`
	Links      []SourceLink    `json:"links"`
	Health     []SourceHealth  `json:"health"`
	Libraries  []SourceLibrary `json:"libraries"`
	Problems   []string        `json:"problems"`
}

var sourceApps = []string{"Sonarr", "Radarr", "Lidarr"}

// Sources reads Prowlarr's observed source configuration and each library app's
// configured indexers. Every failure is reported as a problem: nothing partial
// may read as all healthy, and no error text may carry URLs or credentials.
func Sources(c config.Config) SourceReport {
	out := SourceReport{
		Indexers: []SourceIndexer{}, Links: []SourceLink{},
		Health: []SourceHealth{}, Libraries: []SourceLibrary{}, Problems: []string{},
	}
	if c.Prowlarr.URL == "" || c.Prowlarr.APIKey == "" {
		out.Problems = append(out.Problems, "Prowlarr isn't connected. Add it in Settings to view its sources.")
	} else {
		out.Configured = true
		p := stock.NewArr("Prowlarr", c.Prowlarr.URL, c.Prowlarr.APIKey)
		fillProwlarr(&out, p)
	}
	for _, name := range sourceApps {
		out.Libraries = append(out.Libraries, readLibrary(c, name))
	}
	return out
}

func fillProwlarr(out *SourceReport, p *stock.Arr) {
	// Indexer status is fetched first: without it, disabledUntil is unknown and
	// an unknown state must not be presented as healthy.
	st, stErr := p.IndexerStatuses()
	idx, idxErr := p.Indexers()
	if stErr != nil || idxErr != nil {
		out.Problems = append(out.Problems, "Source availability could not be fully read, so disabled periods are unknown for every source.")
	}
	if idxErr != nil {
		out.Problems = append(out.Problems, sourceError("Prowlarr's source list", idxErr))
	} else {
		for _, x := range idx {
			if until, ok := st[x.ID]; ok {
				t := until
				x.DisabledUntil = &t
			}
			out.Indexers = append(out.Indexers, x)
		}
	}
	if links, err := p.Applications(); err != nil {
		out.Problems = append(out.Problems, sourceError("Prowlarr's application links", err))
	} else {
		out.Links = links
	}
	if health, err := p.Health(); err != nil {
		out.Problems = append(out.Problems, sourceError("Prowlarr's health messages", err))
	} else {
		out.Health = health
	}
}

func readLibrary(c config.Config, name string) SourceLibrary {
	lib := SourceLibrary{App: name, Indexers: []SourceIndexer{}}
	cfg := appConfig(c, name)
	if cfg.URL == "" || cfg.APIKey == "" {
		return lib // configured=false, shown as "Not connected"
	}
	lib.Configured = true
	idx, err := stock.NewArr(name, cfg.URL, cfg.APIKey).LibraryIndexers()
	if err != nil {
		lib.Error = sourceError(name+" source list", err)
		return lib
	}
	lib.Indexers = idx
	return lib
}

func appConfig(c config.Config, name string) config.App {
	switch name {
	case "Sonarr":
		return c.Sonarr
	case "Radarr":
		return c.Radarr
	default:
		return c.Lidarr
	}
}

// sourceError turns an upstream error into safe text: the app's own name only,
// never the reason, because reasons can embed URLs, keys and config.
func sourceError(what string, err error) string {
	if err != nil && (isRefused(err) || isUnreachable(err)) {
		return what + " could not be read: check the connection in Settings."
	}
	return what + " could not be read."
}

// Arr errors are shaped by stock: "%s refused the API key" / "%s is not reachable: ...".
func isRefused(err error) bool     { return strings.Contains(err.Error(), "refused the API key") }
func isUnreachable(err error) bool { return strings.Contains(err.Error(), "is not reachable") }

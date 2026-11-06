package library

import (
	"errors"
	"strings"

	"librarystudio/internal/config"
	"librarystudio/internal/stock"
)

// Download is one qBittorrent torrent with the library app that owns it and
// that app's parent media IDs (series/movie/artist) from its download queue.
type Download struct {
	stock.Torrent
	App      string `json:"app"`
	MediaIDs []int  `json:"mediaIds"`
}

// owner is one app's claim on a torrent hash, taken from its download queue
// (with parent IDs) or its import history (hash only).
type owner struct {
	app string
	ids []int
}

// DownloadQueue lists qBittorrent's torrents and labels each with the library
// app that owns it. Ownership comes only from each app's official queue and
// import history, matched case-insensitively by download ID — never by title
// or category. It fails only when qBittorrent itself is unreadable; an app
// that cannot be read is reported in problems and its torrents stay visible.
func DownloadQueue(c config.Config) ([]Download, []string, error) {
	qb, err := qbit(c)
	if err != nil {
		return nil, nil, err
	}
	ts, err := qb.Torrents()
	if err != nil {
		return nil, nil, errors.New(sourceError("qBittorrent", err))
	}
	problems := []string{}
	owners := map[string][]owner{}
	failed := false
	for _, a := range arrs(c) {
		entries, err := a.QueueEntries()
		if err != nil {
			problems = append(problems, sourceError(a.Name+" queue", err))
			failed = true
		} else {
			for _, e := range entries {
				owners[e.DownloadID] = claim(owners[e.DownloadID], owner{app: a.Name, ids: []int{e.MediaID}})
			}
		}
		imports, err := a.Imports()
		if err != nil {
			problems = append(problems, sourceError(a.Name+" import history", err))
			failed = true
		} else {
			for h := range imports {
				owners[h] = claim(owners[h], owner{app: a.Name})
			}
		}
	}
	out := make([]Download, 0, len(ts))
	for _, t := range ts {
		d := Download{Torrent: t, App: "Unmatched", MediaIDs: []int{}}
		if failed {
			d.App = "Unknown"
		}
		claims := owners[strings.ToUpper(t.Hash)]
		if len(claims) == 0 {
			out = append(out, d)
			continue
		}
		apps, ids := []string{}, []int{}
		for _, cl := range claims {
			if !contains(apps, cl.app) {
				apps = append(apps, cl.app)
			}
			for _, id := range cl.ids {
				if id > 0 && !containsInt(ids, id) {
					ids = append(ids, id)
				}
			}
		}
		switch {
		case len(apps) > 1: // one hash in several apps: never guess between them
			d.App = "Multiple apps"
		default:
			d.App, d.MediaIDs = apps[0], ids
		}
		out = append(out, d)
	}
	return out, problems, nil
}

// claim records one app's view of a hash; a repeat of the same app only adds
// parent IDs (a season pack lists the same series once per episode).
func claim(list []owner, o owner) []owner {
	for i := range list {
		if list[i].app != o.app {
			continue
		}
		for _, id := range o.ids {
			if id > 0 && !containsInt(list[i].ids, id) {
				list[i].ids = append(list[i].ids, id)
			}
		}
		return list
	}
	return append(list, o)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func containsInt(list []int, n int) bool {
	for _, v := range list {
		if v == n {
			return true
		}
	}
	return false
}

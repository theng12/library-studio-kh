package library

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"librarystudio/internal/config"
	"librarystudio/internal/metadata"
	"librarystudio/internal/stock"
)

// Download is one qBittorrent torrent with the library app that owns it and
// that app's parent media IDs (series/movie/artist) from its download queue.
// Proof fields come only from the saved cleanup record for the same hash;
// without one they stay empty, never guessed.
type Download struct {
	stock.Torrent
	App          string `json:"app"`
	MediaIDs     []int  `json:"mediaIds"`
	Status       string `json:"status"`     // same mapping as the dashboard (itemStatus)
	StatusAt     string `json:"statusAt"`   // RFC 3339 completion time, "" if unknown
	Verified     string `json:"verified"`   // hash | size | "" (no saved proof)
	VerifiedAt   string `json:"verifiedAt"` // RFC 3339, "" without proof
	TargetVolume string `json:"targetVolume"`
	TargetPath   string `json:"targetPath"`
	SourcePath   string `json:"sourcePath"`
	LocalCopy    bool   `json:"localCopy"`  // a saved cleanup record tracks a copy to verify
	PosterPath   string `json:"posterPath"` // cached poster file name, "" if none
	MediaTitle   string `json:"mediaTitle"`
	SourceGone   bool   `json:"sourceGone"`
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
//
// home locates the saved cleanup records (read-only); meta supplies cached
// posters only and may be nil. Nothing on this path touches the network
// beyond qBittorrent and the library apps.
func DownloadQueue(c config.Config, home string, meta *metadata.Client) ([]Download, []string, error) {
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
	imported, queued := map[string]time.Time{}, map[string]bool{}
	libApps := arrs(c)
	failed := false
	for _, a := range libApps {
		entries, err := a.QueueEntries()
		if err != nil {
			problems = append(problems, sourceError(a.Name+" queue", err))
			failed = true
		} else {
			for _, e := range entries {
				owners[e.DownloadID] = claim(owners[e.DownloadID], owner{app: a.Name, ids: []int{e.MediaID}})
				queued[e.DownloadID] = true
			}
		}
		imports, err := a.Imports()
		if err != nil {
			problems = append(problems, sourceError(a.Name+" import history", err))
			failed = true
		} else {
			for h, at := range imports {
				owners[h] = claim(owners[h], owner{app: a.Name})
				if at.After(imported[h]) {
					imported[h] = at
				}
			}
		}
	}
	proofs := map[string]*cleanupRecord{}
	if home != "" {
		recs, err := (&StagedCleanup{Home: home}).load()
		if err != nil {
			problems = append(problems, "Saved cleanup proof could not be read, so no download shows as verified: "+err.Error())
		}
		for _, r := range recs {
			proofs[r.Hash] = r // load upper-cases the hash
			if r.ImportedAt.After(imported[r.Hash]) {
				imported[r.Hash] = r.ImportedAt
			}
		}
	}
	out := make([]Download, 0, len(ts))
	for _, t := range ts {
		d := Download{Torrent: t, App: "Unmatched", MediaIDs: []int{}, MediaTitle: t.Name}
		if failed {
			d.App = "Unknown"
		}
		h := strings.ToUpper(t.Hash)
		kind, _, _ := Decide(t, imported, queued, time.Now(), c.CleanupDays)
		d.Status = itemStatus(kind, t.State)
		// Same proof join as the dashboard (applyRecord), so both read alike.
		var it Item
		if t.CompletionOn > 0 {
			it.StatusAt = time.Unix(t.CompletionOn, 0)
		}
		rec := proofs[h]
		if rec != nil {
			applyRecord(&it, rec)
			d.LocalCopy = true
			d.Verified, d.VerifiedAt = it.Verified, stamp(it.VerifiedAt)
			d.TargetPath, d.TargetVolume, d.SourcePath = it.TargetPath, it.TargetVolume, it.SourcePath
		}
		d.StatusAt = stamp(it.StatusAt)
		d.SourceGone = t.State == "missingFiles" || (rec == nil && t.Progress >= 1 && sourceMissing(t))
		claims := owners[h]
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
	nameMedia(out, libApps, meta)
	return out, problems, nil
}

// nameMedia reads each app's library once (only apps that own a parent ID)
// and labels downloads with the matched title and its cached poster. A library
// that cannot be read leaves the torrent name; ownership is unaffected.
func nameMedia(out []Download, apps []*stock.Arr, meta *metadata.Client) {
	need := map[string]bool{}
	for _, d := range out {
		if len(d.MediaIDs) > 0 {
			need[d.App] = true
		}
	}
	byApp := map[string]map[int]stock.Media{}
	for _, a := range apps {
		if !need[a.Name] {
			continue
		}
		items, err := a.Library()
		if err != nil {
			continue
		}
		byApp[a.Name] = map[int]stock.Media{}
		for _, m := range items {
			byApp[a.Name][m.ID] = m
		}
	}
	for i := range out {
		d := &out[i]
		for _, id := range d.MediaIDs {
			m, ok := byApp[d.App][id]
			if !ok || m.Title == "" {
				continue
			}
			d.MediaTitle = m.Title
			if info, ok := meta.Cached(metaKinds[d.App], m.Title, m.Year); ok && info.PosterPath != "" { // nil meta always misses
				d.PosterPath = filepath.Base(info.PosterPath)
			}
			break
		}
	}
}

// stamp is RFC 3339 in UTC, or "" for an unknown time (never year 1).
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// sourceMissing is true only when qBittorrent reported an absolute save path
// and the torrent's content there is definitely absent; an unreported path or
// an unreadable one is not evidence that the source is gone.
func sourceMissing(t stock.Torrent) bool {
	if !filepath.IsAbs(t.SavePath) {
		return false
	}
	_, err := os.Lstat(filepath.Join(t.SavePath, t.Name))
	return errors.Is(err, os.ErrNotExist)
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

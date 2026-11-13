package library

import (
	"context"
	"encoding/hex"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"librarystudio/internal/config"
	"librarystudio/internal/metadata"
	"librarystudio/internal/stock"
)

// metaKinds maps each library app to the metadata kind of its items. Lidarr has
// no keyless provider, so an artist is never given a film's facts or poster.
var metaKinds = map[string]string{"Sonarr": "series", "Radarr": "movie", "Lidarr": ""}

// prefetchMax bounds network lookups per Prefetch run so one poll never queues the whole library.
const prefetchMax = 40

// savedRecords reads the staged cleanup ledger, read-only, keyed by uppercase
// hash. A missing or damaged ledger yields no records, so no proof is shown.
func savedRecords(home string) map[string]*cleanupRecord {
	out := map[string]*cleanupRecord{}
	recs, err := (&StagedCleanup{Home: home}).load()
	if err != nil {
		return out
	}
	for _, r := range recs {
		out[r.Hash] = r
	}
	return out
}

// cleanupVerified is the proof strength saved for one record: a SHA-256 per
// file, or the weaker size-and-readback check.
func cleanupVerified(rec *cleanupRecord) string {
	for _, f := range rec.Files {
		if b, err := hex.DecodeString(f.SHA256); err != nil || len(b) != 32 {
			return "size"
		}
	}
	return "hash"
}

// applyRecord copies the saved verification and destination onto an item. The
// time shown is the import instant the proof and the cleanup waits both count
// from, so every page names the same moment for the same copy.
func applyRecord(it *Item, rec *cleanupRecord) {
	it.Verified = cleanupVerified(rec)
	if len(rec.Files) > 0 {
		it.ProofHash = rec.Files[0].SHA256
	}
	it.VerifiedAt = rec.ImportedAt
	if it.VerifiedAt.IsZero() {
		it.VerifiedAt = rec.VerifiedAt
	}
	it.TargetPath, it.TargetVolume, it.SourcePath = rec.ImportedPath, volumeName(rec.ImportedPath), rec.SavePath
	if it.StatusAt.IsZero() {
		it.StatusAt = it.VerifiedAt
	}
}

// volumeName names the disk a path lives on from the path alone (no OS calls):
// the drive on Windows, the /Volumes/<name> entry on macOS, otherwise "/".
func volumeName(p string) string {
	if strings.TrimSpace(p) == "" {
		return ""
	}
	if v := filepath.VolumeName(p); v != "" {
		return v
	}
	parts := strings.Split(filepath.ToSlash(filepath.Clean(p)), "/")
	if len(parts) > 2 && parts[0] == "" && parts[1] == "Volumes" {
		return parts[2]
	}
	return "/"
}

func proofKey(app string, id int) string { return app + "|" + strconv.Itoa(id) }

// Inventory lists every show, film and artist the library apps look after,
// joined to the newest import proof for that media and the saved cleanup
// record for its download. Metadata comes from the disk cache only.
func Inventory(c config.Config, home string, meta *metadata.Client) ([]Item, []string) {
	media, problems := Browse(c)
	recs := savedRecords(home)
	proofs := map[string]stock.ImportProof{}
	read := map[string]bool{} // apps whose import history was fully read
	for _, a := range arrs(c) {
		ps, err := a.WholeImports()
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		read[a.Name] = true
		for _, p := range ps {
			if p.MediaID <= 0 {
				continue
			}
			k := proofKey(p.App, p.MediaID)
			if old, ok := proofs[k]; !ok || p.ImportedAt.After(old.ImportedAt) {
				proofs[k] = p
			}
		}
	}
	// An app still handling a download is the current truth for that download.
	// The download ID matters for a proven item: a new episode queued for the
	// same show must never turn its already verified copy into "Importing".
	queuedMedia := map[string]bool{}
	queuedHash := map[string]bool{}
	for _, a := range arrs(c) {
		entries, err := a.QueueEntries()
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		for _, e := range entries {
			if e.MediaID > 0 {
				queuedMedia[proofKey(a.Name, e.MediaID)] = true
			}
			if e.DownloadID != "" {
				queuedHash[strings.ToUpper(e.DownloadID)] = true
			}
		}
	}
	items := make([]Item, 0, len(media))
	for _, m := range media {
		it := Item{ID: strconv.Itoa(m.ID), MediaID: m.ID, App: m.App, Title: m.Title, Year: m.Year, Genres: m.Genres, Overview: m.Overview, Kind: "library"}
		p, proven := proofs[proofKey(m.App, m.ID)]
		var rec *cleanupRecord
		if proven {
			rec = recs[p.DownloadID]
			if rec != nil {
				applyRecord(&it, rec)
			} else {
				it.TargetPath, it.TargetVolume, it.StatusAt = p.ImportedPath, volumeName(p.ImportedPath), p.ImportedAt
			}
		}
		switch {
		case proven && queuedHash[strings.ToUpper(p.DownloadID)]:
			it.Status = "importing" // the app is still handling the download that imported it
		case !proven && queuedMedia[proofKey(m.App, m.ID)] && m.HaveFiles:
			it.Status = "importing"
		case !proven && queuedMedia[proofKey(m.App, m.ID)]:
			it.Status = "downloading"
		case m.HaveFiles && proven && rec != nil && rec.State == phaseRemoved:
			it.Status = "moved"
		case m.HaveFiles && proven:
			it.Status = "copied"
		case m.HaveFiles:
			it.Status = "on-disk"
		case proven:
			it.Status = "missing" // the app imported it, but now reports no files
		case read[m.App]:
			it.Status = "queued"
		}
		// Unread history leaves Status empty: "queued" and "missing" can't be told apart.
		fillCached(&it, meta)
		items = append(items, it)
	}
	return items, problems
}

// fillCached adds cached facts. The app's own fields win; the cache only fills gaps.
func fillCached(it *Item, meta *metadata.Client) {
	info, ok := meta.Cached(metaKinds[it.App], it.Title, it.Year)
	if !ok {
		return
	}
	if info.PosterPath != "" {
		it.PosterPath = filepath.Base(info.PosterPath) // bare name for /api/poster?path=
	}
	if it.Overview == "" {
		it.Overview = info.Overview
	}
	if len(it.Genres) == 0 && len(info.Genres) > 0 {
		it.Genres = info.Genres
	}
	if it.Year == 0 {
		it.Year = info.Year
	}
	it.RuntimeMinutes, it.Rating = info.RuntimeMinutes, info.Rating
}

// Prefetch fills the metadata cache for library items the cache doesn't know
// yet, one lookup at a time, de-duplicated, at most prefetchMax per run. Run it
// off the request path; requests read only the cache.
func Prefetch(items []Item, meta *metadata.Client) {
	seen := map[string]bool{}
	looked := 0
	for _, it := range items {
		kind := metaKinds[it.App]
		if meta == nil || it.Kind != "library" || kind == "" {
			continue
		}
		k := kind + "|" + strings.ToLower(it.Title) + "|" + strconv.Itoa(it.Year)
		if seen[k] {
			continue
		}
		seen[k] = true
		if _, ok := meta.Cached(kind, it.Title, it.Year); ok {
			continue
		}
		if looked == prefetchMax {
			return
		}
		looked++
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		meta.Lookup(ctx, kind, it.Title, it.Year)
		cancel()
	}
}

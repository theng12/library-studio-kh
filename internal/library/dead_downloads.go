package library

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"librarystudio/internal/config"
	"librarystudio/internal/stock"
)

// Dead entries: a download whose source files are gone and which has no import
// proof. They are never touched until the owner's grace has passed, and then
// only qBittorrent's bookkeeping goes — the files on disk are never touched.

const deadStateFile = "dead_downloads.json"

// DeadCandidate is one dead row as the settings preview and the Downloads row
// show it, with the time its grace ends.
type DeadCandidate struct {
	Hash       string    `json:"hash"`
	Name       string    `json:"name"`
	SavePath   string    `json:"savePath"`
	Reason     string    `json:"reason,omitempty"` // missing | stalled
	FirstSeen  time.Time `json:"firstSeen,omitempty"`
	EligibleAt time.Time `json:"eligibleAt,omitempty"`
}

// deadRow says why a row is dead, or "" when it is not. A paused row and one
// being checked or moved is never dead. Two reasons count:
//
//   - missing: qBittorrent reports the files gone (already imported elsewhere).
//   - stalled: no metadata, or no peer traffic with the file still incomplete.
//     Nothing will move it, so the release is blocklisted and a different one
//     is searched for.
//
// `error` is deliberately not dead: that is usually this Mac (a permission, a
// path, a full disk), not a bad release, and blocklisting a good release would
// be wrong. It stays visible and is fixed by hand.
func deadRow(t stock.Torrent) string {
	state := strings.ToLower(t.State)
	if strings.HasPrefix(state, "paused") || strings.HasPrefix(state, "stopped") {
		return ""
	}
	if strings.Contains(state, "check") || state == "moving" || state == "allocating" {
		return ""
	}
	if state == "missingfiles" {
		return "missing"
	}
	if state == "metadl" || state == "stalleddl" {
		if t.Progress < 1 {
			return "stalled"
		}
	}
	return ""
}

func loadDeadState(home string) map[string]time.Time {
	out := map[string]time.Time{}
	if b, err := os.ReadFile(filepath.Join(home, deadStateFile)); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func saveDeadState(home string, state map[string]time.Time) error {
	if len(state) > 500 {
		for k := range state { // a bounded state file; oldest entries go first
			if len(state) <= 500 {
				break
			}
			delete(state, k)
		}
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(home, deadStateFile), b, 0o600)
}

// provenHashes is every download hash with an import proof: the app's history
// or a saved cleanup record. These rows are never treated as dead.
func provenHashes(c config.Config, home string) map[string]bool {
	out := map[string]bool{}
	for _, a := range arrs(c) {
		if im, err := a.Imports(); err == nil {
			for h := range im {
				out[strings.ToUpper(h)] = true
			}
		}
	}
	for h := range savedRecords(home) {
		out[strings.ToUpper(h)] = true
	}
	return out
}

// deadDownloads finds the dead, unproven rows and records the first time each
// was seen dead, so the grace counts from then and not from the torrent's age.
func deadDownloads(c config.Config, home string, now time.Time) ([]DeadCandidate, map[string]time.Time, []string, error) {
	qb, err := qbit(c)
	if err != nil {
		return nil, nil, nil, err
	}
	ts, err := qb.Torrents()
	if err != nil {
		return nil, nil, nil, err
	}
	proofs := provenHashes(c, home)
	state := loadDeadState(home)
	out := []DeadCandidate{}
	problems := []string{}
	for _, t := range ts {
		reason := deadRow(t)
		if reason == "" {
			continue
		}
		h := strings.ToUpper(t.Hash)
		if proofs[h] {
			continue
		}
		first, seen := state[h]
		if !seen {
			first = now
			state[h] = now
		}
		out = append(out, DeadCandidate{
			Hash: h, Name: t.Name, SavePath: t.SavePath, Reason: reason, FirstSeen: first,
			EligibleAt: first.Add(time.Duration(c.DeadCleanupHours) * time.Hour),
		})
	}
	// A hash that stops being dead (files returned, or an import proof arrived)
	// is forgotten, so the grace restarts if it goes missing again.
	for h := range state {
		stillDead := false
		for _, cand := range out {
			if cand.Hash == h {
				stillDead = true
				break
			}
		}
		if !stillDead {
			delete(state, h)
		}
	}
	return out, state, problems, nil
}

// DeadDownloads lists the dead rows for display. It records first sightings so
// the grace is honest from the first time the owner's Library saw them.
func DeadDownloads(c config.Config, home string, now time.Time) ([]DeadCandidate, []string, error) {
	cands, state, problems, err := deadDownloads(c, home, now)
	if err != nil {
		return nil, problems, err
	}
	if err := saveDeadState(home, state); err != nil {
		problems = append(problems, "Dead-download state could not be saved: "+err.Error())
	}
	return cands, problems, nil
}

// SweepDead removes qBittorrent's entry for dead, unproven downloads whose
// grace has passed. The switch must be on; files are never touched and one
// Activity event records each removal.
func SweepDead(c config.Config, home string, now time.Time, execute bool) ([]DeadCandidate, []string, error) {
	if !c.DeadCleanupOn {
		return nil, nil, nil
	}
	cands, state, problems, err := deadDownloads(c, home, now)
	if err != nil {
		return nil, problems, err
	}
	grace := time.Duration(c.DeadCleanupHours) * time.Hour
	removed := []DeadCandidate{}
	for _, cand := range cands {
		first := state[cand.Hash]
		if now.Sub(first) < grace {
			continue
		}
		removed = append(removed, cand)
		if !execute {
			continue
		}
		qb, err := qbit(c)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		if err := qb.RemoveMetadata(cand.Hash); err != nil {
			problems = append(problems, "qBittorrent could not remove "+cand.Name+": "+err.Error())
			continue
		}
		delete(state, cand.Hash)
		RecordEvent(home, Item{ID: cand.Hash, Kind: "download-dead-removed", Title: cand.Name, SourcePath: cand.SavePath, When: now})
	}
	if err := saveDeadState(home, state); err != nil {
		problems = append(problems, "Dead-download state could not be saved: "+err.Error())
	}
	return removed, problems, nil
}

// ArrRemoval is a dead download that still sits in a library app's queue, so
// the app would keep retrying it. Only that queue row is removed.
type ArrRemoval struct {
	App        string    `json:"app"`
	Title      string    `json:"title"`
	Hash       string    `json:"hash"`
	SavePath   string    `json:"savePath"`
	Reason     string    `json:"reason,omitempty"` // missing | stalled
	QueueID    int       `json:"queueId"`
	EligibleAt time.Time `json:"eligibleAt,omitempty"`
}

// DeadArrQueued lists dead downloads still waiting in an app queue. Every app
// that is connected must answer: one unreadable queue means nothing is listed,
// so the caller never acts on a partial picture.
func DeadArrQueued(c config.Config, home string, now time.Time) ([]ArrRemoval, []string, error) {
	cands, _, err := DeadDownloads(c, home, now)
	if err != nil {
		return nil, nil, err
	}
	byHash := map[string]DeadCandidate{}
	for _, cand := range cands {
		byHash[cand.Hash] = cand
	}
	out := []ArrRemoval{}
	problems := []string{}
	for _, a := range arrs(c) {
		entries, err := a.QueueEntries()
		if err != nil {
			problems = append(problems, a.Name+" queue could not be read: "+err.Error())
			continue
		}
		for _, e := range entries {
			cand, ok := byHash[strings.ToUpper(e.DownloadID)]
			if !ok || e.ID <= 0 {
				continue
			}
			out = append(out, ArrRemoval{App: a.Name, Title: cand.Name, Hash: cand.Hash, SavePath: cand.SavePath, Reason: cand.Reason, QueueID: e.ID, EligibleAt: cand.EligibleAt})
		}
	}
	if len(problems) > 0 {
		return nil, problems, errors.New("at least one app queue could not be read, so nothing is listed")
	}
	return out, nil, nil
}

// SweepDeadArr removes the queue row of a dead, unimported download once the
// grace has passed. Files, history and library records always stay. A missing
// row only loses its queue row — the release was fine, the files were moved. A
// stalled row is also dropped from the download client and blocklisted, and the
// app is left to search for a different release for the same episode or movie.
func SweepDeadArr(c config.Config, home string, now time.Time, execute bool) ([]ArrRemoval, []string, error) {
	if !c.DeadArrCleanupOn {
		return nil, nil, nil
	}
	items, problems, err := DeadArrQueued(c, home, now)
	if err != nil {
		return nil, problems, err
	}
	removed := []ArrRemoval{}
	for _, item := range items {
		if item.EligibleAt.IsZero() || now.Before(item.EligibleAt) {
			continue
		}
		removed = append(removed, item)
		if !execute {
			continue
		}
		a, err := arr(c, item.App)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		if item.Reason == "stalled" {
			err = a.RemoveStalled(item.QueueID)
		} else {
			err = a.RemoveQueue(item.QueueID)
		}
		if err != nil {
			problems = append(problems, item.App+" could not remove its queue row for "+item.Title+": "+err.Error())
			continue
		}
		RecordEvent(home, Item{ID: item.Hash, Kind: "download-arr-removed", Title: item.Title, App: item.App, Status: item.Reason, SourcePath: item.SavePath, When: now})
	}
	return removed, problems, nil
}

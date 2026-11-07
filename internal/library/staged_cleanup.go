package library

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"librarystudio/internal/config"
	"librarystudio/internal/platform"
	"librarystudio/internal/stock"
)

// ErrCleanupDisabled means the owner has not switched staged cleanup on.
var ErrCleanupDisabled = errors.New("staged cleanup is off")

const cleanupStateFile = "staged_cleanup.json"

// clockSkew tolerates small clock differences between this app and a stock app.
const clockSkew = 5 * time.Minute

const (
	phaseStaging = "staging"
	phaseManaged = "managed_trash"
	phaseNative  = "native_trash"
	phaseRemoved = "removed"
	phaseRestore = "restored"
)

// CleanupSource is the live stock-app surface. Tests replace it with fakes.
type CleanupSource interface {
	Torrents() ([]stock.CleanupTorrent, error)
	Files(hash string) ([]stock.TorrentFile, error)
	Proofs() (map[string][]stock.ImportProof, []string, error)
	Queued() (map[string]string, []string, error)
	Stop(hash string) error
	RemoveMetadata(hash string) error
}

type cleanupRecord struct {
	Hash         string        `json:"hash"`
	Name         string        `json:"name"`
	App          string        `json:"app"`
	DownloadID   string        `json:"downloadId"`
	ImportedAt   time.Time     `json:"importedAt"`
	ImportedPath string        `json:"importedPath,omitempty"`
	VerifiedAt   time.Time     `json:"verifiedAt"`
	State        string        `json:"state"`
	SavePath     string        `json:"savePath"`
	MoveRoot     string        `json:"moveRoot"`
	Label        string        `json:"label"`
	Files        []CleanupFile `json:"files"`
	Bytes        int64         `json:"bytes"`

	MetadataRemoved bool      `json:"metadataRemoved,omitempty"`
	StagedAt        time.Time `json:"stagedAt,omitempty"`
	ManagedPath     string    `json:"managedPath,omitempty"`
	ManagedRoot     string    `json:"managedRoot,omitempty"`
	NativeAt        time.Time `json:"nativeAt,omitempty"`
	NativeName      string    `json:"nativeName,omitempty"`
	RestoredAt      time.Time `json:"restoredAt,omitempty"`
	RemovedAt       time.Time `json:"removedAt,omitempty"`
	LastBlocked     string    `json:"lastBlocked,omitempty"`
}

type cleanupStore struct {
	Version int              `json:"version"`
	Items   []*cleanupRecord `json:"items"`
}

// CleanupPhase describes one policy stage for the dashboard.
type CleanupPhase struct {
	Name string `json:"name"`
	Days int    `json:"days"`
	Note string `json:"note"`
}

// CleanupItem is one tracked download as shown to the owner.
type CleanupItem struct {
	Hash           string     `json:"hash"`
	Name           string     `json:"name"`
	App            string     `json:"app"`
	Phase          string     `json:"phase"`
	VerifiedImport time.Time  `json:"verifiedImport"`
	ImportPath     string     `json:"importPath,omitempty"`
	NextDue        *time.Time `json:"nextDue,omitempty"`
	Proposed       string     `json:"proposed"`
	Blocked        []string   `json:"blocked,omitempty"`
	RestorePath    string     `json:"restorePath,omitempty"`
	SourcePath     string     `json:"sourcePath,omitempty"`
	ManagedPath    string     `json:"managedPath,omitempty"`
	NativePath     string     `json:"nativePath,omitempty"`
	Files          int        `json:"files"`
	Bytes          int64      `json:"bytes"`
}

// CleanupStatus is the read-only contract for GET /api/cleanup/status.
// FreeBytes is nil when unknown, never 0; all byte values are bytes and the
// threshold is also shown in GB so the unit is explicit.
type CleanupStatus struct {
	Enabled                 bool           `json:"enabled"`
	EngineAvailable         bool           `json:"engineAvailable"`
	NativeTrash             bool           `json:"nativeTrash"`
	EngineBlocked           []string       `json:"engineBlocked,omitempty"`
	StagedDays              int            `json:"stagedDays"`
	ManagedTrashDays        int            `json:"managedTrashDays"`
	TotalDays               int            `json:"totalDays"`
	NativeTrashDays         int            `json:"nativeTrashDays"`
	PressureBypassRetention bool           `json:"pressureBypassRetention"`
	PressureFreeGB          int            `json:"pressureFreeGB"`
	PressureFreeBytes       int64          `json:"pressureFreeBytes"`
	SeedRatio               float64        `json:"seedRatio"`
	SeedMinutes             int            `json:"seedMinutes"`
	ManagedTrashPath        string         `json:"managedTrashPath,omitempty"`
	ManagedTrashLocal       bool           `json:"managedTrashLocal"`
	TrashDir                string         `json:"trashDir,omitempty"`
	FreeBytes               *int64         `json:"freeBytes"`
	FreeFrom                string         `json:"freeFrom,omitempty"`
	Phases                  []CleanupPhase `json:"phases"`
	Items                   []CleanupItem  `json:"items"`
	Blocked                 []string       `json:"blocked"`
	NextDue                 *time.Time     `json:"nextDue,omitempty"`
}

// CleanupResult is returned by preview, run and restore.
type CleanupResult struct {
	Status   CleanupStatus `json:"status"`
	Actions  []string      `json:"actions"`
	Purged   int           `json:"purged"`
	Problems []string      `json:"problems,omitempty"`
}

// StagedCleanup runs the owner-approved staged local-copy policy. The default
// source reads the real apps; tests inject a fake and a virtual free-space
// reading, and never touch real downloads or Trash.
type StagedCleanup struct {
	Home        string
	Now         func() time.Time
	Source      CleanupSource
	FreeBytes   func(string) (int64, error)
	LocalVolume func(string) (bool, error)
	TrashDir    func() (string, error)
	// SaveFile is replaceable in tests to simulate a state write failure.
	SaveFile func(string, []byte, os.FileMode) error

	mu sync.Mutex
}

func NewStagedCleanup(get func() config.Config, home string) *StagedCleanup {
	return &StagedCleanup{
		Home: home, Now: time.Now, Source: liveCleanupSource{get: get},
		FreeBytes: platform.FreeBytes, LocalVolume: platform.LocalVolume, TrashDir: platform.TrashDir,
	}
}

// Status is local-only: it reports saved state and capabilities without
// contacting any stock app.
func (e *StagedCleanup) Status(c config.Config) CleanupStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.baseStatus(c)
	now := e.Now()
	recs, err := e.load()
	if err != nil {
		st.Blocked = append(st.Blocked, "Saved cleanup state could not be read: "+err.Error())
		return st
	}
	if err := validateRecords(recs, managedRoot(c), now, e.LocalVolume); err != nil {
		st.Blocked = append(st.Blocked, "Saved cleanup state is unsafe: "+err.Error())
		return st
	}
	e.render(&st, recs, c, now)
	return st
}

// Preview is the dry run: it reads the apps and verifies candidates, but never
// stops a torrent, moves a file or changes saved state.
func (e *StagedCleanup) Preview(c config.Config) (CleanupResult, error) {
	return e.plan(c, false)
}

// Run applies every stage that is due, and refuses when the owner has not
// switched the policy on.
func (e *StagedCleanup) Run(c config.Config) (CleanupResult, error) {
	if !c.StagedCleanupOn {
		return CleanupResult{Status: e.Status(c)}, ErrCleanupDisabled
	}
	return e.plan(c, true)
}

func (e *StagedCleanup) plan(c config.Config, execute bool) (CleanupResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.Now()
	st := e.baseStatus(c)
	res := CleanupResult{Status: st, Actions: []string{}}
	recs, err := e.load()
	if err != nil {
		return res, err
	}
	// Corrupt or hand-edited state must never steer a filesystem write.
	if err := validateRecords(recs, managedRoot(c), now, e.LocalVolume); err != nil {
		return res, fmt.Errorf("saved cleanup state is unsafe, so nothing was changed: %w", err)
	}
	byHash := map[string]*cleanupRecord{}
	for _, r := range recs {
		byHash[strings.ToUpper(r.Hash)] = r
	}
	if !st.EngineAvailable || st.ManagedTrashPath == "" || !st.ManagedTrashLocal {
		// A capability or path problem is reported, never worked around.
		e.render(&st, recs, c, now)
		res.Status = st
		return res, nil
	}
	e.reconcile(recs, c, now)

	block := func(reason string) { st.Blocked = append(st.Blocked, reason) }
	torrents, err := e.Source.Torrents()
	torrents = append([]stock.CleanupTorrent(nil), torrents...)
	if err != nil {
		block("qBittorrent could not be read, so nothing is changed this round.")
	}
	byTorrent := map[string]stock.CleanupTorrent{}
	for _, t := range torrents {
		byTorrent[strings.ToUpper(t.Hash)] = t
	}
	proofs, proofProblems, err := e.Source.Proofs()
	if err != nil {
		block("Library history could not be read, so nothing is changed this round.")
	}
	for _, p := range proofProblems {
		block(p)
	}
	queued, queueProblems, err := e.Source.Queued()
	if err != nil {
		block("Library queues could not be read, so nothing is changed this round.")
	}
	for _, p := range queueProblems {
		block(p)
	}
	global := len(st.Blocked) > 0
	ctx := &settleCtx{e: e, c: c, now: now, execute: execute, torrents: byTorrent, queued: queued, res: &res}
	if !global && c.PressureBypassRetention && execute {
		// Free already-verified space before hashing any new large downloads.
		ctx.purge(recs, false)
		if execute {
			if err := e.save(recs); err != nil {
				return res, err
			}
		}
		sort.SliceStable(torrents, func(i, j int) bool {
			left, right := proofs[strings.ToUpper(torrents[i].Hash)], proofs[strings.ToUpper(torrents[j].Hash)]
			if len(left) != 1 {
				return false
			}
			if len(right) != 1 {
				return true
			}
			return left[0].ImportedAt.Before(right[0].ImportedAt)
		})
	}

	// Verify new whole imports only when every connected app answered. A
	// partial answer never becomes an eligible deletion.
	if !global {
		for _, t := range torrents {
			h := strings.ToUpper(t.Hash)
			if !validCleanupHash(h) || byHash[h] != nil {
				continue
			}
			if t.Progress < 1 {
				continue
			}
			if app, ok := queued[h]; ok {
				_ = app // still being handled: protected until the app finishes it
				continue
			}
			ps := proofs[h]
			if len(ps) == 0 {
				continue // untracked or not wholly imported: never guessed
			}
			if len(ps) > 1 {
				res.Problems = append(res.Problems, shortHash(h)+" is reported imported by several apps; it is left protected.")
				continue
			}
			p := ps[0]
			// A missing or future import time would skip or misplace every
			// retention wait, so it is never guessed.
			if p.ImportedAt.IsZero() || p.ImportedAt.After(now.Add(clockSkew)) {
				res.Problems = append(res.Problems, shortHash(h)+" has no valid import date, so it is left protected.")
				continue
			}
			fs, err := e.Source.Files(h)
			if err != nil || !allFilesComplete(fs) {
				res.Problems = append(res.Problems, shortHash(h)+" files could not be verified, so it is left protected.")
				continue
			}
			if len(fs) > 1 {
				info, err := os.Lstat(p.ImportedPath)
				if err != nil {
					res.Problems = append(res.Problems, shortHash(h)+" library path is unavailable; it stays protected.")
					continue
				}
				if info.Mode().IsRegular() {
					// Episode imports report one renamed file; prove every payload byte in that season folder.
					p.ImportedPath = filepath.Dir(p.ImportedPath)
				}
			}
			root, files, bytes, err := sourceLayout(t, fs, e.LocalVolume)
			if err != nil {
				res.Problems = append(res.Problems, shortHash(h)+" is left protected: "+err.Error())
				continue
			}
			if err := proveImported(p.ImportedPath, files); err != nil {
				res.Problems = append(res.Problems, shortHash(h)+" import isn't fully proven: "+err.Error())
				continue
			}
			rec := &cleanupRecord{
				Hash: h, Name: t.Name, App: p.App, DownloadID: p.DownloadID,
				ImportedAt: p.ImportedAt, ImportedPath: p.ImportedPath, VerifiedAt: now, State: phaseStaging,
				SavePath: t.SavePath, MoveRoot: root, Label: filepath.Base(root),
				Files: files, Bytes: bytes,
			}
			recs = append(recs, rec)
			byHash[h] = rec
			if execute && c.PressureBypassRetention {
				ctx.purge(recs, false)
				if err := e.save(recs); err != nil {
					return res, err
				}
				if free, err := e.FreeBytes(st.TrashDir); err == nil && c.PressureFreeGB > 0 && free >= int64(c.PressureFreeGB)*1_000_000_000 {
					break
				}
			}
			if execute {
				res.Actions = append(res.Actions, "Verified whole import of "+rec.Name+" ("+fmt.Sprintf("%s", humanBytes(bytes))+").")
			} else {
				res.Actions = append(res.Actions, "Would track "+rec.Name+" from its import at "+rec.ImportedAt.Format(time.RFC3339)+".")
			}
		}
	}

	if !global {
		for _, rec := range recs {
			switch rec.State {
			case phaseStaging, phaseManaged, phaseNative:
				ctx.settle(rec)
			}
		}
		ctx.purge(recs, true)
		if len(ctx.problems) > 0 {
			res.Problems = append(res.Problems, ctx.problems...)
		}
	}
	if execute {
		if err := e.save(recs); err != nil {
			return res, fmt.Errorf("cleanup state could not be saved: %w", err)
		}
	}
	e.render(&st, recs, c, now)
	res.Status = st
	return res, nil
}

type settleCtx struct {
	e        *StagedCleanup
	c        config.Config
	now      time.Time
	execute  bool
	torrents map[string]stock.CleanupTorrent
	queued   map[string]string
	res      *CleanupResult
	problems []string
}

func (x *settleCtx) block(rec *cleanupRecord, reason string) {
	rec.LastBlocked = reason
	x.problems = append(x.problems, rec.Name+": "+reason)
}

func (x *settleCtx) settle(rec *cleanupRecord) {
	if app, ok := x.queued[strings.ToUpper(rec.Hash)]; ok {
		x.block(rec, "still handled by "+app+", so it stays protected.")
		return
	}
	rec.LastBlocked = ""
	switch rec.State {
	case phaseStaging:
		x.settleStaging(rec)
	case phaseManaged:
		x.settleManaged(rec)
	case phaseNative:
		x.settleNative(rec)
	}
}

func (x *settleCtx) settleStaging(rec *cleanupRecord) {
	// Stop-seeding is independent of the 3-day local-retention wait. Only a
	// recorded, verified whole import is touched; untracked torrents are never.
	t, listed := x.torrents[strings.ToUpper(rec.Hash)]
	if listed {
		if rec.MetadataRemoved {
			x.block(rec, "qBittorrent still lists the torrent after its removal request; files stay protected.")
			return
		}
		state := strings.ToLower(t.State)
		if t.Progress < 1 || strings.Contains(state, "check") || state == "moving" {
			x.block(rec, "qBittorrent is downloading, checking or moving this torrent; it stays protected.")
			return
		}
		if torrentStateError(t.State) {
			x.block(rec, "qBittorrent reports an error for this torrent; it is left protected.")
			return
		}
		if seedRuleMet(x.c, t) && !torrentStopped(t.State) {
			if !x.execute {
				x.res.Actions = append(x.res.Actions, "Would stop seeding "+rec.Name+" because its limit is met.")
			} else {
				if err := x.e.Source.Stop(rec.Hash); err != nil {
					x.block(rec, "seeding could not be stopped; nothing was moved.")
					return
				}
				after, err := x.e.Source.Torrents()
				if err != nil {
					x.block(rec, "qBittorrent could not be re-read after stopping; nothing was moved.")
					return
				}
				confirmed := false
				for _, t2 := range after {
					if strings.EqualFold(t2.Hash, rec.Hash) && torrentStopped(t2.State) {
						t, confirmed = t2, true
						break
					}
				}
				if !confirmed {
					x.block(rec, "stopped seeding could not be confirmed; nothing was moved.")
					return
				}
				x.torrents[strings.ToUpper(rec.Hash)] = t
				x.res.Actions = append(x.res.Actions, "Stopped seeding "+rec.Name+" because its limit is met.")
			}
		}
	}
	due := rec.ImportedAt.Add(days(x.c.StagedDays))
	if x.now.Before(due) {
		return // the copy stays in the download folder; no premature removal
	}
	if err := verifyTree(rec.MoveRoot, rec.Files); err != nil {
		// A crash after the move but before saving state is adopted only when
		// the destination holds exactly the verified bytes.
		dest := filepath.Join(managedRoot(x.c), entryName(rec))
		if verifyTree(dest, rec.Files) == nil {
			rec.State, rec.ManagedPath, rec.ManagedRoot, rec.StagedAt, rec.MetadataRemoved, rec.LastBlocked = phaseManaged, dest, managedRoot(x.c), x.now, true, ""
			x.res.Actions = append(x.res.Actions, "Recovered an interrupted move of "+rec.Name+" into Library's _trash.")
			return
		}
		x.block(rec, "the local copy can't be verified: "+err.Error())
		return
	}
	ok, err := x.e.LocalVolume(filepath.Dir(rec.MoveRoot))
	if err != nil || !ok {
		x.block(rec, "the local copy is not on a proven local volume.")
		return
	}
	if listed {
		if !seedRuleMet(x.c, t) {
			x.block(rec, fmt.Sprintf("still seeding (ratio %.2f of %g, %d of %d minutes).", t.Ratio, x.c.SeedRatio, t.SeedingTime/60, x.c.SeedMinutes))
			return
		}
		if !torrentStopped(t.State) {
			if !x.execute {
				x.res.Actions = append(x.res.Actions, "Would stop seeding "+rec.Name+" once its limit is met.")
			}
			return
		}
		if !rec.MetadataRemoved {
			if !x.execute {
				x.res.Actions = append(x.res.Actions, "Would remove qBittorrent metadata only (files kept) for "+rec.Name+".")
				return
			}
			if err := x.e.Source.RemoveMetadata(rec.Hash); err != nil {
				x.block(rec, "qBittorrent metadata could not be removed; files were not moved.")
				return
			}
			rec.MetadataRemoved = true
			after, err := x.e.Source.Torrents()
			if err != nil {
				x.block(rec, "torrent metadata removal could not be confirmed; files were not moved.")
				return
			}
			for _, active := range after {
				if strings.EqualFold(active.Hash, rec.Hash) {
					x.block(rec, "qBittorrent still lists this torrent; files were not moved.")
					return
				}
			}
			delete(x.torrents, strings.ToUpper(rec.Hash))
		}
	} else if !rec.MetadataRemoved {
		x.block(rec, "qBittorrent no longer lists this torrent, so its seeding state can't be verified.")
		return
	}
	dest := filepath.Join(managedRoot(x.c), entryName(rec))
	if !pathIsLocal(x.e.LocalVolume, managedRoot(x.c)) {
		x.block(rec, "Library's _trash is not on a local volume; nothing was moved.")
		return
	}
	if !x.execute {
		x.res.Actions = append(x.res.Actions, "Would move "+rec.Name+" into Library's _trash ("+dest+").")
		return
	}
	if err := moveVerified(rec.MoveRoot, dest, rec.Files); err != nil {
		x.block(rec, "moving to Library's _trash failed: "+err.Error())
		return
	}
	rec.State, rec.ManagedPath, rec.ManagedRoot, rec.StagedAt, rec.LastBlocked = phaseManaged, dest, managedRoot(x.c), x.now, ""
	x.res.Actions = append(x.res.Actions, "Moved "+rec.Name+" into Library's _trash.")
}

func (x *settleCtx) settleManaged(rec *cleanupRecord) {
	due := rec.ImportedAt.Add(days(x.c.StagedDays + x.c.ManagedTrashDays))
	if x.now.Before(due) {
		return
	}
	if err := verifyTree(rec.ManagedPath, rec.Files); err != nil {
		// Adopt a completed but unsaved move into the system Trash.
		if trash, terr := x.e.TrashDir(); terr == nil {
			dest := filepath.Join(trash, entryName(rec))
			if verifyTree(dest, rec.Files) == nil {
				rec.State, rec.NativeName, rec.NativeAt, rec.ManagedPath, rec.ManagedRoot, rec.LastBlocked = phaseNative, filepath.Base(dest), x.now, "", "", ""
				x.res.Actions = append(x.res.Actions, "Recovered an interrupted move of "+rec.Name+" into the system Trash.")
				return
			}
		}
		x.block(rec, "the managed _trash copy can't be verified: "+err.Error())
		return
	}
	trash, err := x.e.TrashDir()
	if err != nil {
		x.block(rec, "the system Trash isn't available: "+err.Error())
		return
	}
	dest := filepath.Join(trash, entryName(rec))
	if !x.execute {
		x.res.Actions = append(x.res.Actions, "Would move "+rec.Name+" into the system Trash.")
		return
	}
	if err := moveVerified(rec.ManagedPath, dest, rec.Files); err != nil {
		x.block(rec, "moving to the system Trash failed: "+err.Error())
		return
	}
	rec.State, rec.NativeName, rec.NativeAt, rec.ManagedPath, rec.ManagedRoot, rec.LastBlocked = phaseNative, filepath.Base(dest), x.now, "", "", ""
	x.res.Actions = append(x.res.Actions, "Moved "+rec.Name+" into the system Trash.")
}

func (x *settleCtx) settleNative(rec *cleanupRecord) {
	if rec.NativeAt.IsZero() || x.now.Before(rec.NativeAt.Add(days(x.c.NativeTrashDays))) {
		return
	}
	x.purgeOne(rec, fmt.Sprintf("its %d-day system Trash retention ended", x.c.NativeTrashDays))
}

// purge removes only saved, identity-and-content-verified Library-owned entries
// from this user's own Trash. Oldest verified import first, re-measuring free
// space after each removal and stopping at the threshold.
func (x *settleCtx) purge(recs []*cleanupRecord, reportShortfall bool) {
	if x.c.PressureFreeGB <= 0 {
		return
	}
	trash, err := x.e.TrashDir()
	if err != nil {
		return
	}
	threshold := int64(x.c.PressureFreeGB) * 1_000_000_000
	free, err := x.e.FreeBytes(trash)
	if err != nil {
		x.problems = append(x.problems, "Free space could not be read, so no pressure purge ran.")
		return
	}
	if free >= threshold {
		return
	}
	eligible := []*cleanupRecord{}
	for _, r := range recs {
		if r.State == phaseNative || (x.c.PressureBypassRetention && (r.State == phaseStaging || r.State == phaseManaged)) {
			eligible = append(eligible, r)
		}
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		if !eligible[i].ImportedAt.Equal(eligible[j].ImportedAt) {
			return eligible[i].ImportedAt.Before(eligible[j].ImportedAt)
		}
		return eligible[i].NativeAt.Before(eligible[j].NativeAt)
	})
	for _, rec := range eligible {
		if free >= threshold {
			break
		}
		if app, ok := x.queued[strings.ToUpper(rec.Hash)]; ok {
			x.block(rec, "still handled by "+app+", so it stays protected.")
			continue
		}
		if t, ok := x.torrents[strings.ToUpper(rec.Hash)]; ok {
			state := strings.ToLower(t.State)
			if t.Progress < 1 || torrentStateError(state) || strings.Contains(state, "check") || state == "moving" {
				x.block(rec, "the torrent is incomplete, being checked or moving; it stays protected.")
				continue
			}
		}
		if x.c.PressureBypassRetention && rec.State != phaseNative {
			if err := proveImported(rec.ImportedPath, rec.Files); err != nil {
				x.block(rec, "the library copy can't be verified: "+err.Error())
				continue
			}
			path := rec.MoveRoot
			if rec.State == phaseManaged {
				path = rec.ManagedPath
			}
			if !pathIsLocal(x.e.LocalVolume, path) {
				x.block(rec, "the copy is not on a proven local volume.")
				continue
			}
			if err := verifyTree(path, rec.Files); err != nil {
				x.block(rec, "the local copy can't be verified: "+err.Error())
				continue
			}
			if !x.execute {
				x.res.Actions = append(x.res.Actions, "Low space: would skip retention waits, safely stop completed seeding if needed, and permanently remove "+rec.Name+" after fresh library verification.")
				break
			}
			// Reuse the normal ownership, confirmed-stop and recoverable-move checks.
			urgent := *x
			urgent.c.StagedDays, urgent.c.ManagedTrashDays = 0, 0
			urgent.c.SeedRatio, urgent.c.SeedMinutes = 0, 0
			urgent.problems = nil
			for rec.State == phaseStaging || rec.State == phaseManaged {
				before := rec.State
				urgent.settle(rec)
				if rec.State == before {
					break
				}
			}
			x.problems = append(x.problems, urgent.problems...)
		}
		if rec.State != phaseNative {
			continue
		}
		x.purgeOne(rec, "free space is below "+humanBytes(threshold)+" (oldest verified import first)")
		if !x.execute {
			break
		}
		if rec.State != phaseRemoved {
			continue
		}
		free, err = x.e.FreeBytes(trash)
		if err != nil {
			x.problems = append(x.problems, "Free space could not be re-measured; the pressure purge stopped.")
			return
		}
	}
	if reportShortfall && free < threshold {
		x.problems = append(x.problems, fmt.Sprintf("Free space is %s, below %s; no more eligible Library-owned copies can be removed.", humanBytes(free), humanBytes(threshold)))
	}
}

func (x *settleCtx) purgeOne(rec *cleanupRecord, why string) {
	if _, listed := x.torrents[strings.ToUpper(rec.Hash)]; listed {
		x.block(rec, "qBittorrent tracks this Trash item again; it stays protected.")
		return
	}
	if app, ok := x.queued[strings.ToUpper(rec.Hash)]; ok {
		x.block(rec, "still handled by "+app+", so it stays protected.")
		return
	}
	path, err := x.trashPath(rec)
	if err != nil {
		x.block(rec, err.Error())
		return
	}
	if err := verifyTree(path, rec.Files); err != nil {
		x.block(rec, "the system Trash entry can't be verified: "+err.Error())
		return
	}
	// Fresh NAS proof right before the permanent removal: the copy could have
	// disappeared or changed since it was first verified.
	if err := proveImported(rec.ImportedPath, rec.Files); err != nil {
		x.block(rec, "the library copy can't be verified: "+err.Error())
		return
	}
	if !x.execute {
		x.res.Actions = append(x.res.Actions, "Would permanently remove "+rec.Name+" from the system Trash because "+why+".")
		return
	}
	if err := os.RemoveAll(path); err != nil {
		x.block(rec, "the system Trash entry could not be removed: "+err.Error())
		return
	}
	rec.State, rec.RemovedAt, rec.LastBlocked = phaseRemoved, x.now, ""
	x.res.Purged++
	x.res.Actions = append(x.res.Actions, "Permanently removed "+rec.Name+" from the system Trash because "+why+".")
}

// trashPath is the only way a saved Trash name becomes a deletion target. It
// requires the Library-owned prefix, a plain basename, containment inside this
// user's Trash, a proven local volume and a non-symlink entry.
func (x *settleCtx) trashPath(rec *cleanupRecord) (string, error) {
	trash, err := x.e.TrashDir()
	if err != nil {
		return "", errors.New("the system Trash isn't available")
	}
	name := rec.NativeName
	if !safeNativeName(name) {
		return "", errors.New("the saved Trash name is unsafe; nothing was removed")
	}
	path := filepath.Join(trash, name)
	rel, err := filepath.Rel(trash, path)
	if err != nil || rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New("the saved Trash entry is outside this user's Trash; nothing was removed")
	}
	if !pathIsLocal(x.e.LocalVolume, path) {
		return "", errors.New("the system Trash is not on a proven local volume; nothing was removed")
	}
	if fi, err := os.Lstat(path); err != nil {
		return "", errors.New("the saved Trash entry can't be read: " + err.Error())
	} else if fi.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("the saved Trash entry is a symlink; nothing was removed")
	}
	return path, nil
}

func safeNativeName(name string) bool {
	return name != "" && name != "." && name != ".." &&
		!strings.ContainsAny(name, `/\`) && filepath.Base(name) == name &&
		strings.HasPrefix(name, "library-studio-")
}

// Restore puts one managed or native-Trash item back at its original download
// path. It never overwrites something already there.
func (e *StagedCleanup) Restore(c config.Config, hash string) (CleanupResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.Now()
	hash = strings.ToUpper(strings.TrimSpace(hash))
	st := e.baseStatus(c)
	res := CleanupResult{Status: st, Actions: []string{}}
	recs, err := e.load()
	if err != nil {
		return res, err
	}
	if err := validateRecords(recs, managedRoot(c), now, e.LocalVolume); err != nil {
		return res, fmt.Errorf("saved cleanup state is unsafe, so nothing was changed: %w", err)
	}
	var rec *cleanupRecord
	for _, r := range recs {
		if strings.EqualFold(r.Hash, hash) {
			rec = r
			break
		}
	}
	if rec == nil {
		return res, errors.New("that download isn't tracked")
	}
	var src string
	switch rec.State {
	case phaseManaged:
		src = rec.ManagedPath
	case phaseNative:
		trash, err := e.TrashDir()
		if err != nil {
			return res, err
		}
		if rec.NativeName == "" || filepath.Base(rec.NativeName) != rec.NativeName {
			return res, errors.New("the saved Trash name is unsafe")
		}
		src = filepath.Join(trash, rec.NativeName)
	default:
		return res, errors.New("that item isn't in Library's _trash or the system Trash")
	}
	if _, err := os.Lstat(rec.MoveRoot); err == nil {
		if verifyTree(rec.MoveRoot, rec.Files) != nil {
			return res, errors.New("its original download location already exists with different content; nothing was overwritten")
		}
		// Finish a restore that was interrupted after the copy but before the
		// saved state changed. The stored copy may already be gone.
		if _, serr := os.Lstat(src); serr == nil {
			if err := removeVerified(src, rec.Files); err != nil {
				return res, err
			}
		} else if !errors.Is(serr, os.ErrNotExist) {
			return res, serr
		}
		rec.State, rec.RestoredAt, rec.ManagedPath, rec.ManagedRoot, rec.NativeName, rec.LastBlocked = phaseRestore, now, "", "", "", ""
		res.Actions = append(res.Actions, "Finished restoring "+rec.Name+" to "+rec.MoveRoot+".")
		if err := e.save(recs); err != nil {
			return res, err
		}
		e.render(&st, recs, c, now)
		res.Status = st
		return res, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return res, err
	}
	if err := verifyTree(src, rec.Files); err != nil {
		return res, fmt.Errorf("the stored copy can't be verified: %w", err)
	}
	if err := moveVerified(src, rec.MoveRoot, rec.Files); err != nil {
		return res, err
	}
	rec.State, rec.RestoredAt, rec.ManagedPath, rec.ManagedRoot, rec.NativeName, rec.LastBlocked = phaseRestore, now, "", "", "", ""
	res.Actions = append(res.Actions, "Restored "+rec.Name+" to "+rec.MoveRoot+".")
	if err := e.save(recs); err != nil {
		return res, err
	}
	e.render(&st, recs, c, now)
	res.Status = st
	return res, nil
}

func (e *StagedCleanup) baseStatus(c config.Config) CleanupStatus {
	st := CleanupStatus{
		Enabled: c.StagedCleanupOn, StagedDays: c.StagedDays, ManagedTrashDays: c.ManagedTrashDays,
		TotalDays: c.StagedDays + c.ManagedTrashDays, NativeTrashDays: c.NativeTrashDays,
		PressureBypassRetention: c.PressureBypassRetention, PressureFreeGB: c.PressureFreeGB, PressureFreeBytes: int64(c.PressureFreeGB) * 1_000_000_000,
		SeedRatio: c.SeedRatio, SeedMinutes: c.SeedMinutes, ManagedTrashPath: managedRoot(c),
		Phases: []CleanupPhase{
			{Name: phaseStaging, Days: c.StagedDays, Note: "In the normal download folder after a verified whole import."},
			{Name: phaseManaged, Days: c.StagedDays + c.ManagedTrashDays, Note: "Moved to Library's _trash; permanent deadline counted from the verified import."},
			{Name: phaseNative, Days: c.NativeTrashDays, Note: "In the system Trash; kept this many more days before permanent removal."},
		},
		Items: []CleanupItem{}, Blocked: []string{},
	}
	trash, err := e.TrashDir()
	if err != nil {
		st.EngineBlocked = append(st.EngineBlocked, "Native Trash isn't available on this system: "+err.Error())
	} else if ok, err := e.LocalVolume(trash); err != nil || !ok {
		st.EngineBlocked = append(st.EngineBlocked, "The user's Trash is not on a proven local volume.")
	} else {
		st.EngineAvailable, st.NativeTrash, st.TrashDir = true, true, trash
		if f, err := e.FreeBytes(trash); err == nil {
			st.FreeBytes, st.FreeFrom = &f, trash
		} else {
			st.EngineBlocked = append(st.EngineBlocked, "Free space on the system Trash volume could not be read.")
		}
	}
	st.ManagedTrashLocal = st.ManagedTrashPath != "" && pathIsLocal(e.LocalVolume, st.ManagedTrashPath)
	if c.DownloadPath == "" && st.ManagedTrashPath == "" {
		st.Blocked = append(st.Blocked, "Set the local download folder so Library's _trash can live beside the staging copy.")
	}
	if st.ManagedTrashPath != "" && !st.ManagedTrashLocal {
		st.Blocked = append(st.Blocked, "Library's _trash must be on a local volume, never the NAS library.")
	}
	return st
}

// pathIsLocal checks the nearest existing ancestor so a not-yet-created
// _trash folder inherits the volume of the folder that will hold it.
func pathIsLocal(local func(string) (bool, error), path string) bool {
	p := filepath.Clean(path)
	for {
		if _, err := os.Lstat(p); err == nil {
			ok, err := local(p)
			return err == nil && ok
		}
		parent := filepath.Dir(p)
		if parent == p {
			return false
		}
		p = parent
	}
}

func (e *StagedCleanup) render(st *CleanupStatus, recs []*cleanupRecord, c config.Config, now time.Time) {
	if st.TrashDir != "" {
		free, err := e.FreeBytes(st.TrashDir)
		if err == nil {
			st.FreeBytes = &free
		} else {
			st.FreeBytes = nil
		}
	}
	items := make([]CleanupItem, 0, len(recs))
	for _, rec := range recs {
		it := CleanupItem{
			Hash: rec.Hash, Name: rec.Name, App: rec.App, Phase: rec.State,
			VerifiedImport: rec.ImportedAt, Files: len(rec.Files), Bytes: rec.Bytes,
			ImportPath: rec.ImportedPath, SourcePath: rec.SavePath, ManagedPath: rec.ManagedPath,
		}
		if rec.LastBlocked != "" {
			it.Blocked = append(it.Blocked, rec.LastBlocked)
		}
		switch rec.State {
		case phaseStaging:
			due := rec.ImportedAt.Add(days(c.StagedDays))
			it.NextDue = &due
			if now.Before(due) {
				it.Proposed = "Keep in the download folder."
			} else {
				it.Proposed = "Move to Library's _trash once seeding has stopped."
			}
		case phaseManaged:
			due := rec.ImportedAt.Add(days(c.StagedDays + c.ManagedTrashDays))
			it.NextDue = &due
			it.RestorePath = rec.MoveRoot
			if err := verifyTreeLite(rec.ManagedPath); err != nil {
				it.Blocked = append(it.Blocked, "The managed _trash copy is missing or unreadable.")
			}
			if now.Before(due) {
				it.Proposed = "Keep in Library's _trash."
			} else {
				it.Proposed = "Move to the system Trash."
			}
		case phaseNative:
			due := rec.NativeAt.Add(days(c.NativeTrashDays))
			it.NextDue = &due
			it.RestorePath = rec.MoveRoot
			if st.TrashDir != "" && rec.NativeName != "" {
				it.NativePath = filepath.Join(st.TrashDir, rec.NativeName)
				if err := verifyTreeLite(it.NativePath); err != nil {
					it.Blocked = append(it.Blocked, "The system Trash copy is missing or unreadable.")
				}
			}
			if now.Before(due) {
				it.Proposed = "Keep in the system Trash."
			} else {
				it.Proposed = "Permanently remove from the system Trash."
			}
		case phaseRemoved:
			it.Proposed = "Permanently removed."
		case phaseRestore:
			it.Proposed = "Restored by the owner; kept."
			it.RestorePath = rec.MoveRoot
		}
		if c.PressureBypassRetention && st.FreeBytes != nil && *st.FreeBytes < st.PressureFreeBytes && len(it.Blocked) == 0 && (rec.State == phaseStaging || rec.State == phaseManaged || rec.State == phaseNative) {
			due := now
			it.NextDue = &due
			it.Proposed = "Low space: skip retention waits and permanently remove, oldest verified import first, after fresh NAS verification."
		}
		items = append(items, it)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].VerifiedImport.Before(items[j].VerifiedImport) })
	st.Items = items
	st.NextDue = nil
	for _, it := range items {
		if it.NextDue == nil || it.Phase == phaseRemoved || it.Phase == phaseRestore {
			continue
		}
		if st.NextDue == nil || it.NextDue.Before(*st.NextDue) {
			d := *it.NextDue
			st.NextDue = &d
		}
	}
}

func verifyTreeLite(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return errors.New("symlink")
	}
	return nil
}

func (e *StagedCleanup) load() ([]*cleanupRecord, error) {
	b, err := os.ReadFile(filepath.Join(e.Home, cleanupStateFile))
	if errors.Is(err, os.ErrNotExist) {
		return []*cleanupRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	var s cleanupStore
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("%s is damaged: %w", cleanupStateFile, err)
	}
	out := make([]*cleanupRecord, 0, len(s.Items))
	for _, r := range s.Items {
		if r == nil || strings.TrimSpace(r.Hash) == "" || len(r.Files) == 0 {
			continue
		}
		r.Hash = strings.ToUpper(r.Hash)
		// Old single-file records keyed the item by its original basename; the
		// identity is now root-independent ("").
		if len(r.Files) == 1 && r.Files[0].Rel == filepath.Base(r.MoveRoot) {
			r.Files[0].Rel = ""
		}
		out = append(out, r)
	}
	return out, nil
}

// validateRecords refuses corrupt or hand-edited saved state before any
// filesystem write. A managed path may live under the root that was current
// when it was moved, because DownloadPath and ManagedTrashPath can change
// later, but only with a proven local, symlink-free Library-owned entry.
// Clock fields that drive eligibility must be real past times.
func validateRecords(recs []*cleanupRecord, managed string, now time.Time, local func(string) (bool, error)) error {
	for _, r := range recs {
		if !validCleanupHash(r.Hash) {
			return fmt.Errorf("record %q has an invalid download ID", shortHash(r.Hash))
		}
		if !validLedgerTime(r.ImportedAt, now) {
			return fmt.Errorf("record %s has no valid import time", shortHash(r.Hash))
		}
		if r.SavePath == "" || !filepath.IsAbs(r.SavePath) || r.MoveRoot == "" || !filepath.IsAbs(r.MoveRoot) {
			return fmt.Errorf("record %s has an unsafe source path", shortHash(r.Hash))
		}
		if filepath.Dir(filepath.Clean(r.MoveRoot)) != filepath.Clean(r.SavePath) {
			return fmt.Errorf("record %s points outside its download folder", shortHash(r.Hash))
		}
		if r.Label == "" || filepath.Base(r.Label) != r.Label {
			return fmt.Errorf("record %s has an unsafe label", shortHash(r.Hash))
		}
		for _, f := range r.Files {
			if f.Rel == "" {
				continue
			}
			if filepath.IsAbs(f.Rel) || f.Rel == ".." || strings.HasPrefix(f.Rel, ".."+string(os.PathSeparator)) || strings.HasPrefix(f.Rel, "../") {
				return fmt.Errorf("record %s has an unsafe file key", shortHash(r.Hash))
			}
		}
		switch r.State {
		case phaseStaging:
		case phaseManaged:
			if err := validateManagedPath(r, managed, local); err != nil {
				return err
			}
			if !validLedgerTime(r.StagedAt, now) {
				return fmt.Errorf("record %s has no valid staging time", shortHash(r.Hash))
			}
		case phaseNative:
			if !safeNativeName(r.NativeName) {
				return fmt.Errorf("record %s has an unsafe system Trash name", shortHash(r.Hash))
			}
			if !validLedgerTime(r.StagedAt, now) {
				return fmt.Errorf("record %s has no valid staging time", shortHash(r.Hash))
			}
			if !validLedgerTime(r.NativeAt, now) {
				return fmt.Errorf("record %s has no valid system Trash time", shortHash(r.Hash))
			}
		case phaseRemoved, phaseRestore:
		default:
			return fmt.Errorf("record %s has an unknown state", shortHash(r.Hash))
		}
	}
	return nil
}

// validLedgerTime rejects a zero or implausibly future clock value. It never
// substitutes a guessed time.
func validLedgerTime(t, now time.Time) bool {
	return !t.IsZero() && !t.After(now.Add(clockSkew))
}

// validateManagedPath accepts the root recorded at move time, or the current
// root for older state, so a settings change cannot brick a safe stored copy.
// The item's exact Library-owned name and a local, symlink-free ancestor are
// required; the content itself is verified again before every move or restore.
func validateManagedPath(r *cleanupRecord, current string, local func(string) (bool, error)) error {
	if !filepath.IsAbs(r.ManagedPath) {
		return fmt.Errorf("record %s has an unsafe managed _trash path", shortHash(r.Hash))
	}
	root := filepath.Clean(strings.TrimSpace(r.ManagedRoot))
	if strings.TrimSpace(r.ManagedRoot) == "" {
		// Older state has no recorded root: only the current root or the
		// default DownloadPath/_trash layout can be proven; a custom
		// historical root fails closed.
		parent := filepath.Dir(filepath.Clean(r.ManagedPath))
		if parent != filepath.Clean(current) && filepath.Base(parent) != "_trash" {
			return fmt.Errorf("record %s has an unsafe managed _trash root", shortHash(r.Hash))
		}
		root = parent
	} else if !filepath.IsAbs(root) || root != strings.TrimSpace(r.ManagedRoot) {
		return fmt.Errorf("record %s has an unsafe managed _trash root", shortHash(r.Hash))
	}
	if r.ManagedPath != filepath.Join(root, entryName(r)) {
		return fmt.Errorf("record %s has an unsafe managed _trash path", shortHash(r.Hash))
	}
	if local == nil || !pathIsLocal(local, root) {
		return fmt.Errorf("record %s has a managed _trash path outside a local volume", shortHash(r.Hash))
	}
	for i, p := range []string{root, r.ManagedPath} {
		fi, err := os.Lstat(p)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("record %s managed _trash path can't be read: %w", shortHash(r.Hash), err)
		}
		if fi.Mode()&os.ModeSymlink != 0 || (i == 0 && !fi.IsDir()) {
			return fmt.Errorf("record %s has an unsafe managed _trash entry", shortHash(r.Hash))
		}
	}
	return nil
}

// reconcile repairs only conclusive physical outcomes after a crash or an
// external restore. It never deletes anything and never guesses about content.
func (e *StagedCleanup) reconcile(recs []*cleanupRecord, c config.Config, now time.Time) {
	for _, r := range recs {
		switch r.State {
		case phaseManaged:
			if _, err := os.Lstat(r.ManagedPath); err == nil {
				continue
			}
			if verifyTree(r.MoveRoot, r.Files) == nil {
				r.State, r.RestoredAt, r.ManagedPath, r.ManagedRoot, r.LastBlocked = phaseRestore, now, "", "", "Restored outside Library Studio; state reconciled."
				continue
			}
			if trash, err := e.TrashDir(); err == nil && safeNativeName(entryName(r)) {
				if verifyTree(filepath.Join(trash, entryName(r)), r.Files) == nil {
					r.State, r.NativeName, r.NativeAt, r.ManagedPath, r.ManagedRoot = phaseNative, entryName(r), now, "", ""
				}
			}
		case phaseNative:
			path, err := e.savedTrashPath(r)
			if err != nil {
				continue
			}
			if _, err := os.Lstat(path); err == nil {
				continue
			}
			if verifyTree(r.MoveRoot, r.Files) == nil {
				r.State, r.RestoredAt, r.NativeName, r.LastBlocked = phaseRestore, now, "", "Restored outside Library Studio; state reconciled."
			} else {
				r.State, r.RemovedAt, r.LastBlocked = phaseRemoved, now, "The system Trash entry was missing; reconciled as already removed."
			}
		}
	}
}

// savedTrashPath validates a saved native name without requiring the entry to
// exist, so recovery can tell "gone" apart from "unsafe".
func (e *StagedCleanup) savedTrashPath(rec *cleanupRecord) (string, error) {
	trash, err := e.TrashDir()
	if err != nil {
		return "", err
	}
	if !safeNativeName(rec.NativeName) {
		return "", errors.New("unsafe Trash name")
	}
	return filepath.Join(trash, rec.NativeName), nil
}

func (e *StagedCleanup) save(recs []*cleanupRecord) error {
	if err := os.MkdirAll(e.Home, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cleanupStore{Version: 1, Items: recs}, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(e.Home, cleanupStateFile)
	tmp := path + ".tmp"
	write := e.SaveFile
	if write == nil {
		write = os.WriteFile
	}
	if err := write(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func managedRoot(c config.Config) string {
	if p := strings.TrimSpace(c.ManagedTrashPath); p != "" {
		if abs, err := filepath.Abs(p); err == nil {
			return abs
		}
		return filepath.Clean(p)
	}
	if p := strings.TrimSpace(c.DownloadPath); p != "" {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = filepath.Clean(p)
		}
		return filepath.Join(abs, "_trash")
	}
	return ""
}

func entryName(rec *cleanupRecord) string {
	h := strings.ToLower(rec.Hash)
	if len(h) > 12 {
		h = h[:12]
	}
	label := filepath.Base(rec.Label)
	if label == "" || label == "." || label == ".." || label == string(os.PathSeparator) {
		label = "item"
	}
	label = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == 0 {
			return '-'
		}
		return r
	}, label)
	return "library-studio-" + h + "-" + label
}

func days(n int) time.Duration { return time.Duration(n) * 24 * time.Hour }

func allFilesComplete(fs []stock.TorrentFile) bool {
	if len(fs) == 0 {
		return false
	}
	for _, f := range fs {
		if f.Progress < 1 {
			return false
		}
	}
	return true
}

func seedRuleMet(c config.Config, t stock.CleanupTorrent) bool {
	if c.SeedRatio <= 0 && c.SeedMinutes <= 0 {
		return true // both limits off: only a confirmed stop is required
	}
	if c.SeedRatio > 0 && t.Ratio >= c.SeedRatio {
		return true
	}
	return c.SeedMinutes > 0 && t.SeedingTime >= int64(c.SeedMinutes)*60
}

func torrentStopped(state string) bool {
	s := strings.ToLower(state)
	return strings.Contains(s, "stopped") || strings.Contains(s, "paused")
}

func torrentStateError(state string) bool {
	s := strings.ToLower(state)
	return strings.Contains(s, "error") || strings.Contains(s, "missing")
}

func validCleanupHash(h string) bool {
	if len(h) != 40 && len(h) != 64 {
		return false
	}
	_, err := hex.DecodeString(h)
	return err == nil
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

func humanBytes(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// liveCleanupSource wires the real stock clients behind the engine.
type liveCleanupSource struct{ get func() config.Config }

func (l liveCleanupSource) Torrents() ([]stock.CleanupTorrent, error) {
	qb, err := qbit(l.get())
	if err != nil {
		return nil, err
	}
	return qb.CleanupTorrents()
}

func (l liveCleanupSource) Files(hash string) ([]stock.TorrentFile, error) {
	qb, err := qbit(l.get())
	if err != nil {
		return nil, err
	}
	return qb.TorrentFiles(hash)
}

func (l liveCleanupSource) Proofs() (map[string][]stock.ImportProof, []string, error) {
	out, problems := map[string][]stock.ImportProof{}, []string{}
	for _, a := range arrs(l.get()) {
		imports, err := a.WholeImports()
		if err != nil {
			problems = append(problems, sourceError(a.Name+" history", err))
			continue
		}
		for id, p := range imports {
			out[id] = append(out[id], p)
		}
	}
	return out, problems, nil
}

func (l liveCleanupSource) Queued() (map[string]string, []string, error) {
	out, problems := map[string]string{}, []string{}
	for _, a := range arrs(l.get()) {
		entries, err := a.QueueEntries()
		if err != nil {
			problems = append(problems, sourceError(a.Name+" queue", err))
			continue
		}
		for _, e := range entries {
			out[e.DownloadID] = a.Name
		}
	}
	return out, problems, nil
}

func (l liveCleanupSource) Stop(hash string) error {
	qb, err := qbit(l.get())
	if err != nil {
		return err
	}
	return qb.TorrentAction([]string{hash}, "pause")
}

func (l liveCleanupSource) RemoveMetadata(hash string) error {
	qb, err := qbit(l.get())
	if err != nil {
		return err
	}
	return qb.RemoveMetadata(hash)
}

package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"librarystudio/internal/config"
	"librarystudio/internal/platform"
	"librarystudio/internal/stock"
)

type fakeSource struct {
	torrents      []stock.CleanupTorrent
	files         map[string][]stock.TorrentFile
	proofs        map[string][]stock.ImportProof
	queued        map[string]string
	proofProblems []string
	stopCalls     int
	metadataCalls []string
}

func (f *fakeSource) Torrents() ([]stock.CleanupTorrent, error) { return f.torrents, nil }
func (f *fakeSource) Files(hash string) ([]stock.TorrentFile, error) {
	fs, ok := f.files[strings.ToUpper(hash)]
	if !ok {
		return nil, errors.New("no files")
	}
	return fs, nil
}
func (f *fakeSource) Proofs() (map[string][]stock.ImportProof, []string, error) {
	return f.proofs, f.proofProblems, nil
}
func (f *fakeSource) Queued() (map[string]string, []string, error) { return f.queued, nil, nil }
func (f *fakeSource) Stop(hash string) error {
	f.stopCalls++
	for i := range f.torrents {
		if strings.EqualFold(f.torrents[i].Hash, hash) {
			f.torrents[i].State = "stoppedUP"
		}
	}
	return nil
}
func (f *fakeSource) RemoveMetadata(hash string) error {
	f.metadataCalls = append(f.metadataCalls, strings.ToUpper(hash))
	out := f.torrents[:0]
	for _, t := range f.torrents {
		if !strings.EqualFold(t.Hash, hash) {
			out = append(out, t)
		}
	}
	f.torrents = out
	return nil
}

type testItem struct {
	hash, name, root, src, lib string
}

type harness struct {
	t          *testing.T
	cfg        config.Config
	src        *fakeSource
	clock      *time.Time
	download   string
	library    string
	managed    string
	trash      string
	home       string
	seq        int
	local      bool
	libraryNAS bool
	trashErr   error
	freeFn     func() int64
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	base := t.TempDir()
	h := &harness{
		t: t, download: filepath.Join(base, "downloads"), library: filepath.Join(base, "library"),
		trash: filepath.Join(base, "Trash"), home: filepath.Join(base, "home"),
		local: true, seq: 0,
		src: &fakeSource{
			files: map[string][]stock.TorrentFile{}, proofs: map[string][]stock.ImportProof{},
			queued: map[string]string{},
		},
	}
	for _, d := range []string{h.download, h.library, h.trash, h.home} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	h.managed = filepath.Join(h.download, "_trash")
	h.cfg = config.Default()
	h.cfg.StagedCleanupOn = true
	h.cfg.DownloadPath = h.download
	h.cfg.LibraryPath = h.library
	h.cfg.PressureFreeGB = 0
	h.freeFn = func() int64 { return 1 << 40 }
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	h.clock = &now
	return h
}

func (h *harness) add(name string, importedAt time.Time) testItem {
	h.t.Helper()
	h.seq++
	hash := fmt.Sprintf("%040x", h.seq)
	content := []byte("verified content for " + name)
	root := filepath.Join(h.download, name)
	lib := filepath.Join(h.library, name)
	if err := os.MkdirAll(root, 0o700); err != nil {
		h.t.Fatal(err)
	}
	if err := os.MkdirAll(lib, 0o700); err != nil {
		h.t.Fatal(err)
	}
	src := filepath.Join(root, name+".mkv")
	if err := os.WriteFile(src, content, 0o600); err != nil {
		h.t.Fatal(err)
	}
	// The library app may rename the file; content matching must still prove it.
	if err := os.WriteFile(filepath.Join(lib, "renamed-by-app.mkv"), content, 0o600); err != nil {
		h.t.Fatal(err)
	}
	h.src.torrents = append(h.src.torrents, stock.CleanupTorrent{
		Hash: hash, Name: name, SavePath: h.download, State: "stoppedUP",
		Progress: 1, Ratio: 9, SeedingTime: 999999,
	})
	h.src.files[hash] = []stock.TorrentFile{{Name: name + "/" + name + ".mkv", Size: int64(len(content)), Progress: 1}}
	h.src.proofs[hash] = []stock.ImportProof{{App: "Radarr", DownloadID: hash, ImportedAt: importedAt, ImportedPath: lib}}
	return testItem{hash: hash, name: name, root: root, src: src, lib: lib}
}

func (h *harness) engine() *StagedCleanup {
	return &StagedCleanup{
		Home: h.home, Now: func() time.Time { return *h.clock }, Source: h.src,
		FreeBytes: func(string) (int64, error) { return h.freeFn(), nil },
		LocalVolume: func(path string) (bool, error) {
			if strings.HasPrefix(path, h.download) {
				return h.local, nil
			}
			if h.libraryNAS && strings.HasPrefix(path, h.library) {
				return false, nil
			}
			return true, nil
		},
		TrashDir: func() (string, error) {
			if h.trashErr != nil {
				return "", h.trashErr
			}
			return h.trash, nil
		},
	}
}

func (h *harness) run() CleanupResult {
	h.t.Helper()
	res, err := h.engine().Run(h.cfg)
	if err != nil {
		h.t.Fatalf("run: %v", err)
	}
	return res
}

func (h *harness) preview() CleanupResult {
	h.t.Helper()
	res, err := h.engine().Preview(h.cfg)
	if err != nil {
		h.t.Fatalf("preview: %v", err)
	}
	return res
}

func phaseOf(t *testing.T, res CleanupResult, hash string) string {
	t.Helper()
	for _, it := range res.Status.Items {
		if strings.EqualFold(it.Hash, hash) {
			return it.Phase
		}
	}
	t.Fatalf("item %s not in status: %+v", hash, res.Status.Items)
	return ""
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func trashEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestStagedTransitionsFromVerifiedImportToPermanentRemoval(t *testing.T) {
	h := newHarness(t)
	importedAt := *h.clock
	it := h.add("Movie", importedAt)

	h.clockNow(importedAt.Add(time.Hour))
	res := h.run()
	if phaseOf(t, res, it.hash) != phaseStaging {
		t.Fatalf("new verified import must start in staging: %+v", res.Status.Items)
	}
	if !exists(it.src) || len(h.src.metadataCalls) != 0 {
		t.Fatal("nothing may move before the staging wait")
	}

	h.clockNow(importedAt.Add(3 * 24 * time.Hour))
	res = h.run()
	if phaseOf(t, res, it.hash) != phaseManaged {
		t.Fatalf("day 3 must move into managed _trash: %+v", res.Status.Items)
	}
	if exists(it.root) {
		t.Fatal("the download-folder copy must be gone after the move")
	}
	if len(h.src.metadataCalls) != 1 {
		t.Fatalf("qBittorrent metadata must be removed once, got %v", h.src.metadataCalls)
	}
	if len(trashEntries(t, h.managed)) != 1 {
		t.Fatal("managed _trash must hold the copy")
	}

	// A new engine instance proves the clocks and proof survive a restart.
	h.clockNow(importedAt.Add(7 * 24 * time.Hour))
	res = h.run()
	if phaseOf(t, res, it.hash) != phaseNative {
		t.Fatalf("day 7 total must move into the system Trash: %+v", res.Status.Items)
	}
	if len(trashEntries(t, h.managed)) != 0 || len(trashEntries(t, h.trash)) != 1 {
		t.Fatal("copy must be in the system Trash and gone from _trash")
	}

	h.clockNow(importedAt.Add(36 * 24 * time.Hour))
	res = h.run()
	if phaseOf(t, res, it.hash) != phaseNative {
		t.Fatal("the system Trash copy must remain before its 30-day deadline")
	}
	h.clockNow(importedAt.Add(37 * 24 * time.Hour))
	res = h.run()
	if phaseOf(t, res, it.hash) != phaseRemoved {
		t.Fatalf("day 37 must permanently remove the copy: %+v", res.Status.Items)
	}
	if len(trashEntries(t, h.trash)) != 0 {
		t.Fatal("the system Trash entry must be gone")
	}
}

func (h *harness) clockNow(t time.Time) { *h.clock = t }

func TestDisabledDoesNothingAndRunRefuses(t *testing.T) {
	h := newHarness(t)
	it := h.add("Movie", *h.clock)
	h.cfg.StagedCleanupOn = false
	h.clockNow(h.clockNowAdd(10 * 24 * time.Hour))
	if _, err := h.engine().Run(h.cfg); !errors.Is(err, ErrCleanupDisabled) {
		t.Fatalf("run must refuse while off, got %v", err)
	}
	if !exists(it.src) || len(h.src.metadataCalls) != 0 {
		t.Fatal("run while off must not touch anything")
	}
	res := h.preview()
	if len(res.Status.Items) != 1 {
		t.Fatalf("preview may show the candidate while off: %+v", res.Status.Items)
	}
	if !exists(it.src) || len(h.src.metadataCalls) != 0 {
		t.Fatal("preview while off must not touch anything")
	}
	if _, err := os.Stat(filepath.Join(h.home, cleanupStateFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preview must not write cleanup state")
	}
}

func (h *harness) clockNowAdd(d time.Duration) time.Time { return h.clock.Add(d) }

// rewriteState edits the saved state the engine just wrote, as a stale or
// hand-edited file would look.
func (h *harness) rewriteState(fn func(*cleanupStore)) {
	h.t.Helper()
	path := filepath.Join(h.home, cleanupStateFile)
	b, err := os.ReadFile(path)
	if err != nil {
		h.t.Fatal(err)
	}
	var s cleanupStore
	if err := json.Unmarshal(b, &s); err != nil {
		h.t.Fatal(err)
	}
	fn(&s)
	b, err = json.Marshal(s)
	if err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		h.t.Fatal(err)
	}
}

// atManaged runs the engine through the day-3 move and returns the managed
// path it created.
func (h *harness) atManaged(it testItem, importedAt time.Time) string {
	h.t.Helper()
	h.clockNow(importedAt.Add(3 * 24 * time.Hour))
	if phase := phaseOf(h.t, h.run(), it.hash); phase != phaseManaged {
		h.t.Fatalf("day 3 must move into the managed staging trash, got %s", phase)
	}
	names := trashEntries(h.t, h.managed)
	if len(names) != 1 {
		h.t.Fatalf("want one managed entry, got %v", names)
	}
	return filepath.Join(h.managed, names[0])
}

func TestPreviewIsReadOnly(t *testing.T) {
	h := newHarness(t)
	importedAt := *h.clock
	it := h.add("Movie", importedAt)
	h.clockNow(importedAt.Add(3 * 24 * time.Hour))
	res := h.preview()
	if phaseOf(t, res, it.hash) != phaseStaging {
		t.Fatalf("preview must show the staged candidate: %+v", res.Status.Items)
	}
	if !exists(it.src) || len(h.src.metadataCalls) != 0 || h.src.stopCalls != 0 {
		t.Fatal("preview must not stop, move or remove anything")
	}
	if _, err := os.Stat(filepath.Join(h.home, cleanupStateFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preview must not write state")
	}
	if len(res.Actions) == 0 || !strings.Contains(strings.Join(res.Actions, " "), "Would") {
		t.Fatalf("preview must propose actions: %v", res.Actions)
	}
}

func TestPartialQueuedAndUnprovenDownloadsStayProtected(t *testing.T) {
	cases := []struct {
		name string
		set  func(h *harness, it testItem)
	}{
		{"still queued", func(h *harness, it testItem) { h.src.queued[it.hash] = "Radarr" }},
		{"torrent unfinished", func(h *harness, it testItem) { h.src.torrents[0].Progress = 0.4 }},
		{"no whole import", func(h *harness, it testItem) { delete(h.src.proofs, it.hash) }},
		{"two apps claim it", func(h *harness, it testItem) {
			h.src.proofs[it.hash] = append(h.src.proofs[it.hash], stock.ImportProof{App: "Sonarr", DownloadID: it.hash, ImportedAt: *h.clock, ImportedPath: it.lib})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			importedAt := *h.clock
			it := h.add("Movie", importedAt)
			tc.set(h, it)
			h.clockNow(importedAt.Add(4 * 24 * time.Hour))
			res := h.run()
			if len(res.Status.Items) != 0 {
				t.Fatalf("protected download became tracked: %+v", res.Status.Items)
			}
			if !exists(it.src) || len(h.src.metadataCalls) != 0 {
				t.Fatal("protected download must not be moved")
			}
		})
	}
}

func TestOwnershipNeedsRealLibraryContent(t *testing.T) {
	h := newHarness(t)
	importedAt := *h.clock
	it := h.add("Movie", importedAt)
	if err := os.WriteFile(filepath.Join(it.lib, "renamed-by-app.mkv"), []byte("tampered content here"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.clockNow(importedAt.Add(4 * 24 * time.Hour))
	res := h.run()
	if len(res.Status.Items) != 0 || !exists(it.src) {
		t.Fatalf("content mismatch must stay protected: %+v", res.Status.Items)
	}
	if !strings.Contains(strings.Join(res.Problems, " "), "isn't fully proven") {
		t.Fatalf("the reason must be shown: %v", res.Problems)
	}
}

func TestSourceSymlinkAndExtraUserFileAreRefused(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		h := newHarness(t)
		importedAt := *h.clock
		it := h.add("Movie", importedAt)
		outside := filepath.Join(t.TempDir(), "outside.mkv")
		if err := os.WriteFile(outside, []byte("verified content for Movie"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(it.src); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, it.src); err != nil {
			t.Fatal(err)
		}
		h.clockNow(importedAt.Add(4 * 24 * time.Hour))
		res := h.run()
		if len(res.Status.Items) != 0 || len(h.src.metadataCalls) != 0 {
			t.Fatalf("symlinked source must stay protected: %+v", res.Status.Items)
		}
	})
	t.Run("extra user file", func(t *testing.T) {
		h := newHarness(t)
		importedAt := *h.clock
		it := h.add("Movie", importedAt)
		if err := os.WriteFile(filepath.Join(it.root, "personal-notes.txt"), []byte("do not move"), 0o600); err != nil {
			t.Fatal(err)
		}
		h.clockNow(importedAt.Add(4 * 24 * time.Hour))
		res := h.run()
		if len(res.Status.Items) != 0 || !exists(it.src) {
			t.Fatalf("a folder with extra user data must stay protected: %+v", res.Status.Items)
		}
	})
}

func TestLocalVolumeIsRequired(t *testing.T) {
	h := newHarness(t)
	h.cfg.ManagedTrashPath = filepath.Join(h.home, "_trash") // local, so the source check runs
	importedAt := *h.clock
	it := h.add("Movie", importedAt)
	h.local = false
	h.clockNow(importedAt.Add(4 * 24 * time.Hour))
	res := h.run()
	if len(res.Status.Items) != 0 || !exists(it.src) {
		t.Fatal("a non-local source must never be tracked")
	}
	if !strings.Contains(strings.Join(res.Problems, " "), "local volume") {
		t.Fatalf("the reason must be explicit: %v", res.Problems)
	}
}

func TestSeedingRuleAndStopAreRequired(t *testing.T) {
	t.Run("below the limit", func(t *testing.T) {
		h := newHarness(t)
		importedAt := *h.clock
		it := h.add("Movie", importedAt)
		h.src.torrents[0].State, h.src.torrents[0].Ratio, h.src.torrents[0].SeedingTime = "uploading", 1, 60
		h.clockNow(importedAt.Add(4 * 24 * time.Hour))
		res := h.run()
		if phaseOf(t, res, it.hash) != phaseStaging || len(h.src.metadataCalls) != 0 {
			t.Fatal("a torrent below its seeding limit must not be relocated")
		}
		if !strings.Contains(strings.Join(res.Problems, " "), "still seeding") {
			t.Fatalf("the reason must be shown: %v", res.Problems)
		}
	})
	t.Run("limit met then stopped", func(t *testing.T) {
		h := newHarness(t)
		importedAt := *h.clock
		it := h.add("Movie", importedAt)
		h.src.torrents[0].State, h.src.torrents[0].Ratio = "uploading", 2.5
		h.clockNow(importedAt.Add(4 * 24 * time.Hour))
		res := h.run()
		if phaseOf(t, res, it.hash) != phaseManaged {
			t.Fatalf("a met limit must stop seeding and move: %+v", res.Status.Items)
		}
		if h.src.stopCalls != 1 {
			t.Fatalf("stop must be requested once, got %d", h.src.stopCalls)
		}
	})
}

func TestRestoreFromManagedTrashAndOwnerRestoreIsProtected(t *testing.T) {
	h := newHarness(t)
	importedAt := *h.clock
	it := h.add("Movie", importedAt)
	h.clockNow(importedAt.Add(3 * 24 * time.Hour))
	h.run()
	if exists(it.root) {
		t.Fatal("day 3 should have moved the copy")
	}
	res, err := h.engine().Restore(h.cfg, it.hash)
	if err != nil {
		t.Fatal(err)
	}
	if phaseOf(t, res, it.hash) != phaseRestore {
		t.Fatalf("restore must end in the restored phase: %+v", res.Status.Items)
	}
	if !exists(it.src) {
		t.Fatal("restore must put the copy back at its original path")
	}
	h.clockNow(importedAt.Add(100 * 24 * time.Hour))
	res = h.run()
	if phaseOf(t, res, it.hash) != phaseRestore || !exists(it.src) {
		t.Fatal("an owner-restored item must never be cleaned again")
	}
}

func TestPressurePurgesOldestLibraryOwnedTrashOnly(t *testing.T) {
	h := newHarness(t)
	importedAt := *h.clock
	older := h.add("Older", importedAt)
	newer := h.add("Newer", importedAt.Add(time.Hour))
	h.clockNow(importedAt.Add(7*24*time.Hour + 2*time.Hour))
	if res := h.run(); len(res.Status.Items) != 2 {
		t.Fatalf("both items should be tracked: %+v", res.Status.Items)
	}
	unrelated := filepath.Join(h.trash, "someone-elses-data.txt")
	if err := os.WriteFile(unrelated, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.cfg.PressureFreeGB = 50
	threshold := int64(50) * 1_000_000_000
	h.freeFn = func() int64 {
		n := 0
		for _, name := range trashEntries(t, h.trash) {
			if strings.HasPrefix(name, "library-studio-") {
				n++
			}
		}
		return threshold - int64(n-1)*1_000_000_000
	}
	res := h.run()
	if res.Purged != 1 {
		t.Fatalf("pressure purge must stop at the threshold after one removal, purged %d", res.Purged)
	}
	if len(res.Actions) == 0 || !strings.Contains(res.Actions[len(res.Actions)-1], "Older") {
		t.Fatalf("the oldest verified import must be removed first: %v", res.Actions)
	}
	if phaseOf(t, res, older.hash) != phaseRemoved || phaseOf(t, res, newer.hash) != phaseNative {
		t.Fatalf("wrong items purged: %+v", res.Status.Items)
	}
	if !exists(unrelated) {
		t.Fatal("unrelated Trash content must never be touched")
	}
}

func TestUnsupportedNativeTrashIsExplicit(t *testing.T) {
	h := newHarness(t)
	importedAt := *h.clock
	it := h.add("Movie", importedAt)
	h.trashErr = platform.ErrTrashUnsupported
	h.clockNow(importedAt.Add(4 * 24 * time.Hour))
	res := h.run()
	if res.Status.EngineAvailable || res.Status.NativeTrash {
		t.Fatal("native Trash support must not be claimed")
	}
	if len(res.Status.EngineBlocked) == 0 || len(res.Status.Items) != 0 {
		t.Fatalf("unsupported systems must report a blocker and do nothing: %+v", res.Status)
	}
	if !exists(it.src) || len(h.src.metadataCalls) != 0 {
		t.Fatal("nothing may move without a supported native Trash")
	}
}

func (h *harness) addFile(name string, importedAt time.Time) testItem {
	h.t.Helper()
	h.seq++
	hash := fmt.Sprintf("%040x", h.seq)
	content := []byte("single file content for " + name)
	src := filepath.Join(h.download, name+".mkv")
	if err := os.WriteFile(src, content, 0o600); err != nil {
		h.t.Fatal(err)
	}
	lib := filepath.Join(h.library, name)
	if err := os.MkdirAll(lib, 0o700); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lib, "renamed.mkv"), content, 0o600); err != nil {
		h.t.Fatal(err)
	}
	h.src.torrents = append(h.src.torrents, stock.CleanupTorrent{
		Hash: hash, Name: name, SavePath: h.download, State: "stoppedUP",
		Progress: 1, Ratio: 9, SeedingTime: 999999,
	})
	h.src.files[hash] = []stock.TorrentFile{{Name: name + ".mkv", Size: int64(len(content)), Progress: 1}}
	h.src.proofs[hash] = []stock.ImportProof{{App: "Radarr", DownloadID: hash, ImportedAt: importedAt, ImportedPath: lib}}
	return testItem{hash: hash, name: name, root: src, src: src, lib: lib}
}

func TestSingleFileRootSurvivesAllStagesAndRestore(t *testing.T) {
	h := newHarness(t)
	importedAt := *h.clock
	it := h.addFile("Movie", importedAt)
	h.clockNow(importedAt.Add(3 * 24 * time.Hour))
	res := h.run()
	if phaseOf(t, res, it.hash) != phaseManaged {
		t.Fatalf("single-file torrent must move to managed _trash: %+v", res.Status.Items)
	}
	if exists(it.src) || len(trashEntries(t, h.managed)) != 1 {
		t.Fatal("single-file root identity must survive the managed move")
	}
	h.clockNow(importedAt.Add(7 * 24 * time.Hour))
	res = h.run()
	if phaseOf(t, res, it.hash) != phaseNative || len(trashEntries(t, h.trash)) != 1 {
		t.Fatalf("single-file torrent must move to native Trash: %+v", res.Status.Items)
	}
	h.clockNow(importedAt.Add(37 * 24 * time.Hour))
	res = h.run()
	if phaseOf(t, res, it.hash) != phaseRemoved || len(trashEntries(t, h.trash)) != 0 {
		t.Fatalf("single-file torrent must reach permanent removal: %+v", res.Status.Items)
	}

	h2 := newHarness(t)
	t2 := *h2.clock
	it2 := h2.addFile("Restore Me", t2)
	h2.clockNow(t2.Add(3 * 24 * time.Hour))
	h2.run()
	res2, err := h2.engine().Restore(h2.cfg, it2.hash)
	if err != nil || phaseOf(t, res2, it2.hash) != phaseRestore || !exists(it2.src) {
		t.Fatalf("single-file restore failed: %v %+v", err, res2.Status.Items)
	}
}

func TestUnsafeSavedTrashNameCannotDeleteOutsideTrash(t *testing.T) {
	h := newHarness(t)
	importedAt := *h.clock
	h.add("Movie", importedAt)
	h.clockNow(importedAt.Add(3 * 24 * time.Hour))
	h.run()
	h.clockNow(importedAt.Add(7 * 24 * time.Hour))
	h.run()
	valuable := filepath.Join(filepath.Dir(h.trash), "valuable")
	if err := os.WriteFile(valuable, []byte("must survive"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.home, cleanupStateFile)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var store cleanupStore
	if err := json.Unmarshal(b, &store); err != nil {
		t.Fatal(err)
	}
	store.Items[0].NativeName = "../valuable"
	corrupt, _ := json.Marshal(store)
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := h.engine().Run(h.cfg); err == nil {
		t.Fatal("unsafe saved Trash state must fail closed")
	}
	if !exists(valuable) {
		t.Fatal("state corruption must never delete outside Trash")
	}
}

func TestSeedingStopsAtItsLimitBeforeTheThreeDayMove(t *testing.T) {
	h := newHarness(t)
	importedAt := *h.clock
	it := h.add("Movie", importedAt)
	h.clockNow(importedAt.Add(time.Hour))
	h.run()
	h.src.torrents[0].State = "uploading"
	h.src.torrents[0].Ratio = 2.5
	h.clockNow(importedAt.Add(2 * time.Hour))
	res := h.run()
	if h.src.stopCalls != 1 || len(h.src.metadataCalls) != 0 || !exists(it.src) {
		t.Fatalf("seeding must stop at its limit before the local move: stop=%d meta=%v src=%v", h.src.stopCalls, h.src.metadataCalls, exists(it.src))
	}
	if phaseOf(t, res, it.hash) != phaseStaging {
		t.Fatalf("the local copy must remain until day 3: %+v", res.Status.Items)
	}
	h.clockNow(importedAt.Add(3 * 24 * time.Hour))
	res = h.run()
	if phaseOf(t, res, it.hash) != phaseManaged {
		t.Fatalf("day 3 must move the already-stopped torrent: %+v", res.Status.Items)
	}
}

func TestSaveFailureAfterRestoreIsReconciled(t *testing.T) {
	h := newHarness(t)
	importedAt := *h.clock
	it := h.add("Movie", importedAt)
	h.clockNow(importedAt.Add(3 * 24 * time.Hour))
	h.run()
	eng := h.engine()
	eng.SaveFile = func(string, []byte, os.FileMode) error { return errors.New("disk full") }
	if _, err := eng.Restore(h.cfg, it.hash); err == nil {
		t.Fatal("the injected save failure must surface")
	}
	if !exists(it.src) {
		t.Fatal("the physical restore must have happened")
	}
	res, err := h.engine().Restore(h.cfg, it.hash)
	if err != nil || phaseOf(t, res, it.hash) != phaseRestore {
		t.Fatalf("a later restore must reconcile the physical state: %v %+v", err, res.Status.Items)
	}
}

func TestSaveFailureAfterPurgeIsReconciled(t *testing.T) {
	h := newHarness(t)
	importedAt := *h.clock
	it := h.add("Movie", importedAt)
	h.clockNow(importedAt.Add(3 * 24 * time.Hour))
	h.run()
	h.clockNow(importedAt.Add(7 * 24 * time.Hour))
	h.run()
	h.clockNow(importedAt.Add(37 * 24 * time.Hour))
	eng := h.engine()
	eng.SaveFile = func(string, []byte, os.FileMode) error { return errors.New("disk full") }
	if _, err := eng.Run(h.cfg); err == nil {
		t.Fatal("the injected save failure must surface")
	}
	if len(trashEntries(t, h.trash)) != 0 {
		t.Fatal("the verified purge should have completed")
	}
	res := h.run()
	if phaseOf(t, res, it.hash) != phaseRemoved {
		t.Fatalf("a later run must reconcile the physically removed entry: %+v", res.Status.Items)
	}
}

func TestHumanBytesUsesDecimalGB(t *testing.T) {
	if got := humanBytes(50_000_000_000); got != "50.0 GB" {
		t.Fatalf("50 GB threshold displayed as %q", got)
	}
	if got := humanBytes(1_500_000_000); got != "1.5 GB" {
		t.Fatalf("decimal units wrong: %q", got)
	}
}

func TestStockReadFailureBlocksEveryAction(t *testing.T) {
	h := newHarness(t)
	importedAt := *h.clock
	it := h.add("Movie", importedAt)
	h.clockNow(importedAt.Add(4 * 24 * time.Hour))
	h.src.proofProblems = []string{"Sonarr history could not be read."}
	res := h.run()
	if len(res.Status.Items) != 0 || !exists(it.src) {
		t.Fatal("an unanswered connected app must block every action")
	}
	if len(res.Status.Blocked) == 0 {
		t.Fatal("the block must be visible")
	}
}

func TestCopyItemCopiesFilesAndFolders(t *testing.T) {
	base := t.TempDir()
	srcFile := filepath.Join(base, "one.mkv")
	if err := os.WriteFile(srcFile, []byte("file-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	dstFile := filepath.Join(base, "out", "one.mkv")
	if err := os.MkdirAll(filepath.Dir(dstFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := copyItem(srcFile, dstFile); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(dstFile); err != nil || string(b) != "file-content" {
		t.Fatalf("file copy failed: %q %v", b, err)
	}
	srcDir := filepath.Join(base, "folder")
	if err := os.MkdirAll(filepath.Join(srcDir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "sub", "two.mkv"), []byte("nested"), 0o600); err != nil {
		t.Fatal(err)
	}
	dstDir := filepath.Join(base, "out", "folder")
	if err := copyItem(srcDir, dstDir); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dstDir, "sub", "two.mkv")); err != nil || string(b) != "nested" {
		t.Fatalf("folder copy failed: %q %v", b, err)
	}
}

func TestInterruptedMovesAreRecovered(t *testing.T) {
	t.Run("staging to managed", func(t *testing.T) {
		h := newHarness(t)
		importedAt := *h.clock
		it := h.add("Movie", importedAt)
		h.clockNow(importedAt.Add(time.Hour))
		h.run() // save the proof and staging clock
		dest := filepath.Join(h.managed, entryName(&cleanupRecord{Hash: it.hash, Label: filepath.Base(it.root)}))
		if err := os.MkdirAll(h.managed, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(it.root, dest); err != nil {
			t.Fatal(err)
		}
		h.clockNow(importedAt.Add(3 * 24 * time.Hour))
		res := h.run()
		if phaseOf(t, res, it.hash) != phaseManaged || !exists(dest) || exists(it.root) {
			t.Fatalf("a completed but unsaved move must be adopted: %+v", res.Status.Items)
		}
	})
	t.Run("managed to system Trash", func(t *testing.T) {
		h := newHarness(t)
		importedAt := *h.clock
		it := h.add("Movie", importedAt)
		h.clockNow(importedAt.Add(3 * 24 * time.Hour))
		h.run()
		var managed string
		for _, name := range trashEntries(t, h.managed) {
			managed = filepath.Join(h.managed, name)
		}
		dest := filepath.Join(h.trash, entryName(&cleanupRecord{Hash: it.hash, Label: filepath.Base(it.root)}))
		if err := os.Rename(managed, dest); err != nil {
			t.Fatal(err)
		}
		h.clockNow(importedAt.Add(7 * 24 * time.Hour))
		res := h.run()
		if phaseOf(t, res, it.hash) != phaseNative || !exists(dest) || len(trashEntries(t, h.managed)) != 0 {
			t.Fatalf("a completed but unsaved Trash move must be adopted: %+v", res.Status.Items)
		}
	})
	t.Run("restore", func(t *testing.T) {
		h := newHarness(t)
		importedAt := *h.clock
		it := h.add("Movie", importedAt)
		h.clockNow(importedAt.Add(3 * 24 * time.Hour))
		h.run()
		var managed string
		for _, name := range trashEntries(t, h.managed) {
			managed = filepath.Join(h.managed, name)
		}
		if err := copyItem(managed, it.root); err != nil {
			t.Fatal(err)
		}
		res, err := h.engine().Restore(h.cfg, it.hash)
		if err != nil {
			t.Fatal(err)
		}
		if phaseOf(t, res, it.hash) != phaseRestore || !exists(it.src) || exists(managed) {
			t.Fatalf("an interrupted restore must be finished: %+v", res.Status.Items)
		}
	})
}

func TestManagedTrashDefaultsToLocalDownloadFolderNotNAS(t *testing.T) {
	h := newHarness(t)
	importedAt := *h.clock
	it := h.add("Movie", importedAt)
	h.clockNow(importedAt.Add(3 * 24 * time.Hour))
	res := h.run()
	if phaseOf(t, res, it.hash) != phaseManaged {
		t.Fatalf("day 3 must move into the managed staging trash: %+v", res.Status.Items)
	}
	var managed string
	for _, name := range trashEntries(t, h.managed) {
		managed = filepath.Join(h.managed, name)
	}
	if managed == "" || !strings.HasPrefix(managed, h.download) {
		t.Fatalf("managed _trash must default to the local download folder, got %q", managed)
	}
	if exists(filepath.Join(h.library, "_trash")) {
		t.Fatal("managed _trash must never default to the NAS library")
	}
}

func TestNASManagedTrashIsRefused(t *testing.T) {
	h := newHarness(t)
	h.libraryNAS = true
	h.cfg.ManagedTrashPath = filepath.Join(h.library, "_trash")
	importedAt := *h.clock
	it := h.add("Movie", importedAt)
	h.clockNow(importedAt.Add(4 * 24 * time.Hour))
	res := h.run()
	if res.Status.ManagedTrashLocal || len(res.Status.Items) != 0 {
		t.Fatalf("a NAS managed _trash must be refused: %+v", res.Status)
	}
	if !exists(it.src) || len(h.src.metadataCalls) != 0 {
		t.Fatal("nothing may be moved when the managed trash is not local")
	}
	if !strings.Contains(strings.Join(res.Status.Blocked, " "), "local volume") {
		t.Fatalf("the refusal must be explicit: %v", res.Status.Blocked)
	}
}

func TestPressureThresholdAtExactDecimalLimitDoesNotPurge(t *testing.T) {
	h := newHarness(t)
	importedAt := *h.clock
	h.add("Movie", importedAt)
	h.clockNow(importedAt.Add(7 * 24 * time.Hour))
	h.run() // move to managed
	h.clockNow(importedAt.Add(7*24*time.Hour + time.Hour))
	h.run() // move to native
	h.cfg.PressureFreeGB = 50
	h.freeFn = func() int64 { return 50_000_000_000 }
	res := h.run()
	if res.Purged != 0 || len(trashEntries(t, h.trash)) != 1 {
		t.Fatalf("exactly 50 GB must not trigger a purge: purged=%d", res.Purged)
	}
	if res.Status.PressureFreeBytes != 50_000_000_000 {
		t.Fatalf("threshold must be decimal GB, got %d", res.Status.PressureFreeBytes)
	}
}

func TestManagedCopySurvivesLocalPathChanges(t *testing.T) {
	t.Run("run and later stages", func(t *testing.T) {
		h := newHarness(t)
		importedAt := *h.clock
		it := h.add("Movie", importedAt)
		managed := h.atManaged(it, importedAt)

		// The owner changes both settings after the item was already staged.
		h.cfg.DownloadPath = filepath.Join(filepath.Dir(h.download), "downloads2")
		h.cfg.ManagedTrashPath = filepath.Join(filepath.Dir(h.download), "local-trash2")
		res := h.run()
		if phase := phaseOf(t, res, it.hash); phase != phaseManaged {
			t.Fatalf("a saved managed path must still be accepted, got %s", phase)
		}

		h.clockNow(importedAt.Add(7 * 24 * time.Hour))
		res = h.run()
		if phase := phaseOf(t, res, it.hash); phase != phaseNative {
			t.Fatalf("the stored copy must still reach the system Trash, got %s: %v", phase, res.Problems)
		}
		if exists(managed) || len(trashEntries(t, h.trash)) != 1 {
			t.Fatal("the historical managed copy must be moved, not stranded")
		}
	})

	t.Run("restore", func(t *testing.T) {
		h := newHarness(t)
		importedAt := *h.clock
		it := h.add("Movie", importedAt)
		managed := h.atManaged(it, importedAt)

		h.cfg.DownloadPath = filepath.Join(filepath.Dir(h.download), "downloads2")
		res, err := h.engine().Restore(h.cfg, it.hash)
		if err != nil {
			t.Fatalf("restore must accept the historical managed path: %v", err)
		}
		if phaseOf(t, res, it.hash) != phaseRestore || !exists(it.src) || exists(managed) {
			t.Fatalf("the stored copy must restore to its original location: %+v", res.Status.Items)
		}
	})
}

func TestUnsafeManagedStateFailsClosed(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(*harness)
		tamper func(*harness, *cleanupRecord)
	}{
		{"wrong root", nil, func(h *harness, r *cleanupRecord) {
			r.ManagedRoot = filepath.Join(filepath.Dir(h.download), "elsewhere")
		}},
		{"NAS root", func(h *harness) { h.libraryNAS = true }, func(h *harness, r *cleanupRecord) {
			r.ManagedRoot = h.library
			r.ManagedPath = filepath.Join(h.library, entryName(r))
		}},
		{"corrupt name", nil, func(h *harness, r *cleanupRecord) {
			r.ManagedPath = filepath.Join(r.ManagedRoot, "library-studio-evil")
		}},
		{"symlink", nil, func(h *harness, r *cleanupRecord) {
			real := r.ManagedPath + ".real"
			if err := os.Rename(r.ManagedPath, real); err != nil {
				h.t.Fatal(err)
			}
			if err := os.Symlink(real, r.ManagedPath); err != nil {
				h.t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			if tc.setup != nil {
				tc.setup(h)
			}
			importedAt := *h.clock
			it := h.add("Movie", importedAt)
			managed := h.atManaged(it, importedAt)
			h.rewriteState(func(s *cleanupStore) {
				tc.tamper(h, s.Items[0])
			})
			if _, err := h.engine().Run(h.cfg); err == nil || !strings.Contains(err.Error(), "unsafe") {
				t.Fatalf("run must fail closed, got %v", err)
			}
			if _, err := h.engine().Restore(h.cfg, it.hash); err == nil || !strings.Contains(err.Error(), "unsafe") {
				t.Fatalf("restore must fail closed, got %v", err)
			}
			if !exists(managed) {
				t.Fatal("nothing may move or be removed when saved state is unsafe")
			}
		})
	}
}

func TestZeroImportTimeCannotSkipWaits(t *testing.T) {
	h := newHarness(t)
	importedAt := *h.clock
	it := h.add("Movie", importedAt)
	h.run() // save the staging record
	h.rewriteState(func(s *cleanupStore) {
		for _, r := range s.Items {
			r.ImportedAt = time.Time{}
		}
	})
	h.clockNow(importedAt.Add(3 * 24 * time.Hour))
	if _, err := h.engine().Run(h.cfg); err == nil || !strings.Contains(err.Error(), "import time") {
		t.Fatalf("a zero import time must be refused, got %v", err)
	}
	if !exists(it.src) || len(trashEntries(t, h.managed)) != 0 {
		t.Fatal("a zero import time must never make the copy immediately eligible")
	}
}

func TestProofWithoutImportDateIsLeftProtected(t *testing.T) {
	h := newHarness(t)
	importedAt := *h.clock
	it := h.add("Movie", importedAt)
	h.src.proofs[it.hash] = []stock.ImportProof{{
		App: "Radarr", DownloadID: it.hash, ImportedAt: time.Time{}, ImportedPath: it.lib,
	}}
	h.clockNow(importedAt.Add(4 * 24 * time.Hour))
	res := h.run()
	if len(res.Status.Items) != 0 || !strings.Contains(strings.Join(res.Problems, " "), "valid import date") {
		t.Fatalf("an import without a date must stay untracked: %+v", res)
	}
	if !exists(it.src) || len(trashEntries(t, h.managed)) != 0 || len(h.src.metadataCalls) != 0 {
		t.Fatal("an import without a date must never be moved or removed")
	}
}

func TestFutureOrImpossibleLedgerTimesFailClosed(t *testing.T) {
	t.Run("future import time", func(t *testing.T) {
		h := newHarness(t)
		importedAt := *h.clock
		it := h.add("Movie", importedAt)
		h.run() // save the staging record
		h.clockNow(importedAt.Add(3 * 24 * time.Hour))
		h.rewriteState(func(s *cleanupStore) {
			s.Items[0].ImportedAt = h.clock.Add(time.Hour)
		})
		if _, err := h.engine().Run(h.cfg); err == nil || !strings.Contains(err.Error(), "import time") {
			t.Fatalf("a future import time must be refused, got %v", err)
		}
		if !exists(it.src) || len(trashEntries(t, h.managed)) != 0 || h.src.stopCalls != 0 || len(h.src.metadataCalls) != 0 {
			t.Fatal("an invalid import time must never move or remove anything")
		}
	})

	t.Run("zero staging time", func(t *testing.T) {
		h := newHarness(t)
		importedAt := *h.clock
		it := h.add("Movie", importedAt)
		h.atManaged(it, importedAt)
		h.clockNow(importedAt.Add(4 * 24 * time.Hour))
		h.rewriteState(func(s *cleanupStore) {
			s.Items[0].StagedAt = time.Time{}
		})
		if _, err := h.engine().Run(h.cfg); err == nil || !strings.Contains(err.Error(), "staging time") {
			t.Fatalf("a zero staging time must be refused, got %v", err)
		}
		if len(trashEntries(t, h.managed)) != 1 {
			t.Fatal("an invalid staging time must never move or remove anything")
		}
	})

	cases := []struct {
		name string
		at   func(time.Time) time.Time
	}{
		{"zero native time", func(time.Time) time.Time { return time.Time{} }},
		{"future native time", func(now time.Time) time.Time { return now.Add(time.Hour) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			importedAt := *h.clock
			it := h.add("Movie", importedAt)
			h.atManaged(it, importedAt)
			h.clockNow(importedAt.Add(7 * 24 * time.Hour))
			if phase := phaseOf(t, h.run(), it.hash); phase != phaseNative {
				t.Fatalf("day 7 must reach the system Trash, got %s", phase)
			}
			if len(trashEntries(t, h.trash)) != 1 {
				t.Fatal("setup must leave one system Trash entry")
			}
			h.clockNow(importedAt.Add(37 * 24 * time.Hour))
			h.rewriteState(func(s *cleanupStore) {
				s.Items[0].NativeAt = tc.at(*h.clock)
			})
			res, err := h.engine().Run(h.cfg)
			if err == nil || !strings.Contains(err.Error(), "system Trash time") {
				t.Fatalf("an invalid native Trash time must be refused, got %v", err)
			}
			if res.Purged != 0 || len(trashEntries(t, h.trash)) != 1 {
				t.Fatal("an invalid native Trash time must never purge the stored copy")
			}
		})
	}
}

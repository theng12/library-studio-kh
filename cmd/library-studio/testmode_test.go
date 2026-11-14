package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"librarystudio/internal/config"
	"librarystudio/internal/library"
	"librarystudio/internal/metadata"
	"librarystudio/internal/stock"
)

// emptySource is a connected stock surface with no live torrents, proofs or queue
// entries, so the seeded records settle on their own saved state (c6's torrent is
// already removed). It lets the seed test prove the due copy actually moves.
type emptySource struct{}

func (emptySource) Torrents() ([]stock.CleanupTorrent, error) { return nil, nil }
func (emptySource) Files(string) ([]stock.TorrentFile, error) { return nil, nil }
func (emptySource) Proofs(string) (map[string][]stock.ImportProof, []string, error) {
	return map[string][]stock.ImportProof{}, nil, nil
}
func (emptySource) Queued() (map[string]string, []string, error) {
	return map[string]string{}, nil, nil
}
func (emptySource) Stop(string) error           { return nil }
func (emptySource) RemoveMetadata(string) error { return nil }

func TestSeededDueItemMovesAndRestores(t *testing.T) {
	e := fixtureEnv(t)
	store, err := config.Open(e.Home)
	if err != nil {
		t.Fatal(err)
	}
	trash := t.TempDir()
	eng := library.NewStagedCleanup(store.Get, e.Home)
	eng.LocalVolume = func(string) (bool, error) { return true, nil }
	eng.TrashDir = func() (string, error) { return trash, nil }
	eng.FreeBytes = func(string) (int64, error) { return 1e12, nil }
	eng.Source = emptySource{}
	c := store.Get()

	// Preview proposes only the due copy's move.
	pv, err := eng.Preview(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(pv.Actions) != 1 || !strings.Contains(pv.Actions[0], "Would move "+testDueName) {
		t.Fatalf("preview actions = %v", pv.Actions)
	}
	// Run moves it into Library's _trash; the row becomes managed_trash while the
	// imported movie (c2) stays in staging.
	run, err := eng.Run(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Actions) != 1 || !strings.Contains(run.Actions[0], "Moved "+testDueName) {
		t.Fatalf("run actions = %v", run.Actions)
	}
	byHash := map[string]library.CleanupItem{}
	for _, it := range eng.Status(c).Items {
		byHash[it.Hash] = it
	}
	if due := byHash[testDueHash]; due.Phase != "managed_trash" || due.RestorePath == "" {
		t.Fatalf("after run, due item = %+v", due)
	}
	if quiet := byHash[testQuietHash]; quiet.Phase != "staging" {
		t.Fatalf("c2 should stay staging, got %+v", quiet)
	}
	// Restore brings the copy back to the download folder.
	if _, err := eng.Restore(c, testDueHash); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.Downloads, testDueName)); err != nil {
		t.Fatalf("restored copy missing: %v", err)
	}
}

func fixtureEnv(t *testing.T) testEnv {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir()) // macOS /var is a symlink; the ledger refuses symlinks
	if err != nil {
		t.Fatal(err)
	}
	e := testEnv{Home: filepath.Join(root, "home"), Stock: "http://127.0.0.1:1", Downloads: filepath.Join(root, "downloads"),
		Library: filepath.Join(root, "library"), Metadata: "offline"}
	if err := writeTestConfig(e); err != nil {
		t.Fatal(err)
	}
	if err := seedFixtures(e, time.Now()); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestSeededRecordPassesLibraryValidation(t *testing.T) {
	e := fixtureEnv(t)
	store, err := config.Open(e.Home)
	if err != nil {
		t.Fatal(err)
	}
	trash := t.TempDir()
	eng := library.NewStagedCleanup(store.Get, e.Home)
	eng.LocalVolume = func(string) (bool, error) { return true, nil }
	eng.TrashDir = func() (string, error) { return trash, nil }
	eng.FreeBytes = func(string) (int64, error) { return 1e12, nil }
	st := eng.Status(store.Get())
	if len(st.Blocked) != 0 || len(st.Items) != 4 {
		t.Fatalf("blocked=%v items=%d", st.Blocked, len(st.Items))
	}
	byHash := map[string]library.CleanupItem{}
	for _, it := range st.Items {
		byHash[it.Hash] = it
	}
	it := byHash[testImportedHash]
	if it.Phase != "staging" || it.Proposed != "Keep in the download folder." || it.Files != 1 || it.Verified != "hash" {
		t.Fatalf("staging item = %+v", it)
	}
	if managed := byHash[testManagedHash]; managed.Phase != "managed_trash" || managed.Proposed != "Keep in Library's _trash." || managed.RestorePath == "" || managed.Verified != "hash" {
		t.Fatalf("managed item = %+v", managed)
	}
	// c2 is the imported-movie proof: staging, still inside its wait, verified.
	if quiet := byHash[testQuietHash]; quiet.Phase != "staging" || quiet.Proposed != "Keep in the download folder." || quiet.Files != 1 || quiet.Verified != "hash" || quiet.App != "Radarr" {
		t.Fatalf("quiet film item = %+v", quiet)
	}
	// c6 is past its wait and movable: the due label, verified, no blocker.
	if due := byHash[testDueHash]; due.Phase != "staging" || due.Proposed != "Move to Library's _trash once seeding has stopped." || due.Verified != "hash" || len(due.Blocked) != 0 {
		t.Fatalf("due item = %+v", due)
	}
	b, err := os.ReadFile(filepath.Join(e.Downloads, testImportedFile))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	var saved struct{ Items []testRecord }
	raw, _ := os.ReadFile(filepath.Join(e.Home, "staged_cleanup.json"))
	if err := json.Unmarshal(raw, &saved); err != nil || saved.Items[0].Files[0].SHA256 != hex.EncodeToString(sum[:]) || it.Bytes != int64(len(b)) {
		t.Fatalf("record hash/size does not match the file on disk: %v", err)
	}
}

func TestSeededCacheKeyIsWhatMetadataReads(t *testing.T) {
	e := fixtureEnv(t)
	c := &metadata.Client{Dir: filepath.Join(e.Home, "metadata")}
	for _, ft := range testTitles {
		info, ok := c.Cached(ft.Kind, ft.Title, ft.Info.Year)
		if !ok || info.Overview != ft.Info.Overview || filepath.Base(info.PosterPath) != ft.Poster {
			t.Fatalf("%s: ok=%v info=%+v", ft.Title, ok, info)
		}
		if _, err := os.Stat(filepath.Join(e.Home, "metadata", "posters", ft.Poster)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSeedTwiceKeepsOneRecord(t *testing.T) {
	e := fixtureEnv(t)
	if err := seedFixtures(e, time.Now()); err != nil {
		t.Fatal(err)
	}
	var saved struct{ Items []json.RawMessage }
	raw, _ := os.ReadFile(filepath.Join(e.Home, "staged_cleanup.json"))
	if err := json.Unmarshal(raw, &saved); err != nil || len(saved.Items) != 4 {
		t.Fatalf("items=%d err=%v", len(saved.Items), err)
	}
}

func TestWriteTestConfigKeepsExistingValues(t *testing.T) {
	e := fixtureEnv(t)
	store, _ := config.Open(e.Home)
	c := store.Get()
	if c.QBit.URL != e.Stock || c.Sonarr.URL != e.Stock+"/sonarr" || !c.StagedCleanupOn || c.DownloadPath != e.Downloads {
		t.Fatalf("fresh config = %+v", c)
	}
	c.Appearance = "dark"
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}
	e.Stock = "http://127.0.0.1:2"
	if err := writeTestConfig(e); err != nil {
		t.Fatal(err)
	}
	again, _ := config.Open(e.Home)
	if got := again.Get(); got.Appearance != "dark" || got.QBit.URL != "http://127.0.0.1:1" {
		t.Fatalf("rerun replaced settings: %+v", got)
	}
}

func TestOfflineMetadataFailsFast(t *testing.T) {
	start := time.Now()
	if _, err := testMetadataClient("offline").Get("https://itunes.apple.com/search?term=x"); err == nil {
		t.Fatal("offline request succeeded")
	}
	c := &metadata.Client{Dir: t.TempDir(), HTTP: testMetadataClient("offline")}
	if info := c.Lookup(context.Background(), "movie", "Example Film", 2024); info.Source != "placeholder" {
		t.Fatalf("offline lookup = %+v", info)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("offline metadata took %s", d)
	}
}

func TestStubMetadataRewritesProviders(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(r.URL.Path)) }))
	defer stub.Close()
	resp, err := testMetadataClient(stub.URL).Get("https://api.tvmaze.com/singlesearch/shows?q=x")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b := make([]byte, 64)
	n, _ := resp.Body.Read(b)
	if string(b[:n]) != "/singlesearch/shows" {
		t.Fatalf("stub saw %q", b[:n])
	}
}

func TestReadTestEnvRefusesMissingHome(t *testing.T) {
	t.Setenv("KH_TEST_HOME", "")
	if _, err := readTestEnv(); err == nil || !strings.Contains(err.Error(), "KH_TEST_HOME") {
		t.Fatalf("err = %v", err)
	}
}

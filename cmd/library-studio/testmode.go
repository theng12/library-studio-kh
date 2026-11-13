package main

// Headless test entry (-test-mode, -seed-fixtures). It serves the real UI on
// 127.0.0.1 from an isolated home under $TMPDIR, never opens a window and never
// reads the owner's live data. scripts/test-ui.sh drives it.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"librarystudio/internal/config"
	"librarystudio/internal/library"
	"librarystudio/internal/metadata"
	"librarystudio/internal/platform"
)

// Fixture names match tests/mock-stock/fixture_ui.go.
const (
	testImportedFile = "Sample Series S02E06 1080p.mkv"
	testImportedRel  = "Sample Series/Season 02/" + testImportedFile
	testFilmRel      = "Example Film (2024)/Example Film (2024) 2160p.mkv"
	testUser         = "owner"
	testPass         = "testpass123"
)

// testImportedHash is the mock's c1 torrent, upper-case as the cleanup ledger stores it.
var testImportedHash = strings.ToUpper(strings.Repeat("c1", 20))

// testManagedHash is a second tracked import whose local copy is already inside
// Library's _trash, so Restore is visible and exercisable in the headless test.
var testManagedHash = strings.ToUpper(strings.Repeat("e1", 20))

const testManagedName = "Old Show S01E01 720p"

type testEnv struct {
	Home, Stock, Downloads, Library, Metadata string
	Port                                      int
}

// readTestEnv refuses anything that could reach the owner's live data.
func readTestEnv() (testEnv, error) {
	e := testEnv{
		Home: os.Getenv("KH_TEST_HOME"), Stock: os.Getenv("KH_TEST_STOCK"),
		Downloads: os.Getenv("KH_TEST_DOWNLOADS"), Library: os.Getenv("KH_TEST_LIBRARY"),
		Metadata: os.Getenv("KH_TEST_METADATA"),
	}
	if e.Home == "" {
		return e, errors.New("test mode needs KH_TEST_HOME: an empty folder under $TMPDIR (scripts/test-ui.sh makes one); it never uses the app's real data")
	}
	for name, p := range map[string]string{"KH_TEST_HOME": e.Home, "KH_TEST_DOWNLOADS": e.Downloads, "KH_TEST_LIBRARY": e.Library} {
		if !filepath.IsAbs(p) {
			return e, fmt.Errorf("test mode needs %s as an absolute folder, got %q", name, p)
		}
	}
	if base, err := os.UserConfigDir(); err == nil && filepath.Clean(e.Home) == filepath.Join(base, "Library Studio KH") {
		return e, errors.New("KH_TEST_HOME is the app's real settings folder; use a folder under $TMPDIR")
	}
	if u, err := url.Parse(e.Stock); err != nil || u.Scheme != "http" || u.Host == "" {
		return e, fmt.Errorf("test mode needs KH_TEST_STOCK as the mock-stock address like http://127.0.0.1:7890, got %q", e.Stock)
	}
	if e.Metadata != "" && e.Metadata != "offline" {
		if u, err := url.Parse(e.Metadata); err != nil || u.Scheme != "http" || u.Host == "" {
			return e, fmt.Errorf("KH_TEST_METADATA must be offline or a stub address like http://127.0.0.1:7891, got %q", e.Metadata)
		}
	}
	if p := os.Getenv("KH_TEST_PORT"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 65535 {
			return e, fmt.Errorf("KH_TEST_PORT must be 0-65535, got %q", p)
		}
		e.Port = n
	}
	return e, nil
}

// writeTestConfig writes settings only when the isolated home has none, so a
// rerun keeps whatever the tester changed in the UI.
func writeTestConfig(e testEnv) error {
	if _, err := os.Stat(filepath.Join(e.Home, "settings.json")); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	store, err := config.Open(e.Home)
	if err != nil {
		return err
	}
	c := store.Get()
	stock := strings.TrimRight(e.Stock, "/")
	c.QBit = config.QBit{URL: stock, Username: "admin", Password: "test-password"}
	c.Sonarr = config.App{URL: stock + "/sonarr", APIKey: "test-sonarr-key"}
	c.Radarr = config.App{URL: stock + "/radarr", APIKey: "test-radarr-key"}
	c.Lidarr = config.App{URL: stock + "/lidarr", APIKey: "test-lidarr-key"}
	c.DownloadPath, c.LibraryPath = e.Downloads, e.Library
	// The seeded import is 1 day old, so it stays inside its 3-day staging wait.
	c.StagedCleanupOn, c.StagedDays, c.ManagedTrashDays, c.NativeTrashDays = true, 3, 4, 30
	c.SeedRatio, c.SeedMinutes = 2, 2880
	return store.Save(c)
}

// testRecord mirrors library's saved cleanupRecord JSON for the fields the seed sets.
type testRecord struct {
	Hash         string                `json:"hash"`
	Name         string                `json:"name"`
	App          string                `json:"app"`
	DownloadID   string                `json:"downloadId"`
	ImportedAt   time.Time             `json:"importedAt"`
	ImportedPath string                `json:"importedPath,omitempty"`
	VerifiedAt   time.Time             `json:"verifiedAt"`
	State        string                `json:"state"`
	SavePath     string                `json:"savePath"`
	MoveRoot     string                `json:"moveRoot"`
	ManagedPath  string                `json:"managedPath,omitempty"`
	ManagedRoot  string                `json:"managedRoot,omitempty"`
	StagedAt     time.Time             `json:"stagedAt,omitempty"`
	Label        string                `json:"label"`
	Files        []library.CleanupFile `json:"files"`
	Bytes        int64                 `json:"bytes"`
}

type testFixtureTitle struct {
	Kind, Title, Poster string
	Info                metadata.Info
}

var testTitles = []testFixtureTitle{
	{"series", "Sample Series", "fixture-sample-series.jpg", metadata.Info{Year: 2023, Overview: "A made-up series about a small town archive, used only by the headless UI test.",
		Genres: []string{"Drama", "Mystery"}, RuntimeMinutes: 45, Rating: 7.8, Source: "tvmaze"}},
	{"movie", "Example Film", "fixture-example-film.jpg", metadata.Info{Year: 2024, Overview: "A made-up film used only by the headless UI test.",
		Genres: []string{"Adventure"}, RuntimeMinutes: 128, Source: "itunes"}},
	{"movie", "A Quiet Film", "fixture-a-quiet-film.jpg", metadata.Info{Year: 1998, Overview: "A quiet made-up drama used only by the headless UI test.",
		Genres: []string{"Drama"}, RuntimeMinutes: 96, Source: "itunes"}},
}

// testCacheKey is metadata's cache file name: sha256("kind|lower(title)|year"), first 16 bytes as hex.
func testCacheKey(kind, title string, year int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", kind, strings.ToLower(title), year)))
	return hex.EncodeToString(sum[:16])
}

// seedFixtures is idempotent: files are rewritten with the same bytes and the
// cleanup record is added only when its hash is not already saved.
func seedFixtures(e testEnv, now time.Time) error {
	episode := bytes.Repeat([]byte("Library Studio KH headless test episode fixture\n"), 1024)
	film := bytes.Repeat([]byte("Library Studio KH headless test film fixture\n"), 1024)
	for path, b := range map[string][]byte{
		filepath.Join(e.Library, testImportedRel):    episode,
		filepath.Join(e.Library, testFilmRel):        film,
		filepath.Join(e.Downloads, testImportedFile): episode,
		// Real payloads for the moving and completed-not-imported rows, so only
		// the deliberately missing source reads as missing.
		filepath.Join(e.Downloads, "A Quiet Film (1998) 1080p"):   film,
		filepath.Join(e.Downloads, "Old Download Without Import"): film,
	} {
		if err := writeFile(path, b); err != nil {
			return err
		}
	}
	sum := sha256.Sum256(episode)
	importedAt := now.Add(-24 * time.Hour).UTC()
	rec := testRecord{
		Hash: testImportedHash, Name: testImportedFile, App: "Sonarr", DownloadID: testImportedHash,
		// One import instant for everything the proof and the cleanup waits show.
		ImportedAt: importedAt, ImportedPath: filepath.Join(e.Library, testImportedRel), VerifiedAt: importedAt,
		State: "staging", SavePath: e.Downloads, MoveRoot: filepath.Join(e.Downloads, testImportedFile), Label: testImportedFile,
		Files: []library.CleanupFile{{Rel: "", Size: int64(len(episode)), SHA256: hex.EncodeToString(sum[:])}}, Bytes: int64(len(episode)),
	}
	if err := addCleanupRecord(filepath.Join(e.Home, "staged_cleanup.json"), rec); err != nil {
		return err
	}
	// A copy already in Library's _trash: the row offers Restore and its proof
	// carries the same one import time.
	managedRoot := filepath.Join(e.Downloads, "_trash")
	managedPath := filepath.Join(managedRoot, "library-studio-"+strings.ToLower(testManagedHash)[:12]+"-"+testManagedName)
	managedBytes := bytes.Repeat([]byte("Library Studio KH headless test managed fixture\n"), 512)
	managedSum := sha256.Sum256(managedBytes)
	if err := writeFile(managedPath, managedBytes); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(e.Library, "Old Show", "Season 01", testManagedName+".mkv"), managedBytes); err != nil {
		return err
	}
	managedAt := now.Add(-5 * 24 * time.Hour).UTC()
	rec = testRecord{
		Hash: testManagedHash, Name: testManagedName, App: "Sonarr", DownloadID: testManagedHash,
		ImportedAt: managedAt, ImportedPath: filepath.Join(e.Library, "Old Show", "Season 01", testManagedName+".mkv"), VerifiedAt: managedAt,
		State: "managed_trash", SavePath: e.Downloads, MoveRoot: filepath.Join(e.Downloads, testManagedName), Label: testManagedName,
		ManagedPath: managedPath, ManagedRoot: managedRoot, StagedAt: now.Add(-4 * 24 * time.Hour).UTC(),
		Files: []library.CleanupFile{{Rel: "", Size: int64(len(managedBytes)), SHA256: hex.EncodeToString(managedSum[:])}}, Bytes: int64(len(managedBytes)),
	}
	if err := addCleanupRecord(filepath.Join(e.Home, "staged_cleanup.json"), rec); err != nil {
		return err
	}
	meta := filepath.Join(e.Home, "metadata")
	jpg, err := tinyJPEG()
	if err != nil {
		return err
	}
	for _, t := range testTitles {
		poster := filepath.Join(meta, "posters", t.Poster)
		if err := writeFile(poster, jpg); err != nil {
			return err
		}
		info := t.Info
		info.PosterPath = poster // the real cache stores the full poster path
		b, _ := json.Marshal(info)
		if err := writeFile(filepath.Join(meta, "cache", testCacheKey(t.Kind, t.Title, t.Info.Year)+".json"), b); err != nil {
			return err
		}
	}
	return nil
}

func addCleanupRecord(path string, rec testRecord) error {
	store := struct {
		Version int               `json:"version"`
		Items   []json.RawMessage `json:"items"`
	}{Version: 1}
	b, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(b, &store); err != nil {
			return fmt.Errorf("%s is damaged: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, raw := range store.Items {
		var seen struct{ Hash string }
		if json.Unmarshal(raw, &seen) == nil && strings.EqualFold(seen.Hash, rec.Hash) {
			return nil // keep the tester's current state for this item
		}
	}
	item, _ := json.Marshal(rec)
	store.Items = append(store.Items, item)
	out, _ := json.MarshalIndent(store, "", "  ")
	return writeFile(path, out)
}

func writeFile(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func tinyJPEG() ([]byte, error) {
	// A real 2:3 poster at 300x450, so layout, aspect and file size are the
	// same shape a real provider image would have.
	img := image.NewRGBA(image.Rect(0, 0, 300, 450))
	for x := 0; x < 300; x++ {
		for y := 0; y < 450; y++ {
			img.Set(x, y, color.RGBA{uint8(60 + y/6), uint8(70 + x/9), uint8(105 + y/12), 255})
		}
	}
	var b bytes.Buffer
	err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 60})
	return b.Bytes(), err
}

// metadataTransport keeps metadata off the real internet: offline fails every
// request at once; otherwise iTunes and TVmaze go to the stub address.
type metadataTransport struct {
	stub *url.URL // nil = offline
	base http.RoundTripper
}

func (t metadataTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if t.stub == nil {
		return nil, errors.New("test mode: metadata is offline")
	}
	if r.URL.Host == "itunes.apple.com" || r.URL.Host == "api.tvmaze.com" {
		r = r.Clone(r.Context())
		r.URL.Scheme, r.URL.Host, r.Host = t.stub.Scheme, t.stub.Host, ""
	}
	return t.base.RoundTrip(r)
}

func testMetadataClient(setting string) *http.Client {
	t := metadataTransport{base: http.DefaultTransport}
	if setting != "" && setting != "offline" {
		t.stub, _ = url.Parse(setting) // validated by readTestEnv
	}
	return &http.Client{Timeout: 6 * time.Second, Transport: t}
}

// runTestMode serves until SIGTERM or Ctrl-C. It skips Tailscale, the hourly
// cleanup and speed sampling, and never prints the bootstrap token.
func runTestMode(seed, serveUI bool) {
	e, err := readTestEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := writeTestConfig(e); err != nil {
		log.Fatalf("test mode could not write its settings: %v", err)
	}
	if seed {
		if err := seedFixtures(e, time.Now()); err != nil {
			log.Fatalf("test fixtures could not be seeded: %v", err)
		}
		fmt.Printf("TEST_SEEDED=%s\n", e.Home)
	}
	if !serveUI {
		return
	}
	release, err := platform.LockConfigHome(e.Home)
	if err != nil {
		log.Fatalf("another test server already uses %s", e.Home)
	}
	defer release()
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(e.Port)))
	if err != nil {
		log.Fatal(listenProblem(net.JoinHostPort("127.0.0.1", strconv.Itoa(e.Port)), err))
	}
	s, err := openServer(e.Home, ln.Addr().String())
	if err != nil {
		log.Fatal(err)
	}
	s.Meta.HTTP = testMetadataClient(e.Metadata)
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	base := "http://" + ln.Addr().String() + "/"
	// The owner account goes through the same setup call the desktop UI makes.
	if err := testAccount(base, s.Token); err != nil {
		log.Fatalf("test account could not be created: %v", err)
	}
	fmt.Printf("TEST_LOGIN=%s:%s\n", testUser, testPass)
	fmt.Printf("TEST_URL=%s\n", base) // last line: everything above is ready
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// testAccount creates the owner account; an existing one (a rerun) is fine.
func testAccount(base, token string) error {
	body, _ := json.Marshal(map[string]string{"username": testUser, "password": testPass})
	req, _ := http.NewRequest(http.MethodPost, base+"api/auth/setup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Library-Token", token)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		return fmt.Errorf("setup answered %s", resp.Status)
	}
	return nil
}

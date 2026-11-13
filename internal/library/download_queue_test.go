package library

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"librarystudio/internal/config"
)

const (
	hashOne  = "1111111111111111111111111111111111111111" // Sonarr series 7
	hashTwo  = "2222222222222222222222222222222222222222" // Radarr movie 9
	hashOld  = "3333333333333333333333333333333333333333" // imported, no queue row
	hashFree = "4444444444444444444444444444444444444444" // claimed by nobody
	hashBoth = "5555555555555555555555555555555555555555" // in Sonarr and Radarr
)

func dlQBit(t *testing.T, status int, torrents []map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/torrents/info" {
			http.NotFound(w, r)
			return
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		json.NewEncoder(w).Encode(torrents)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// dlArr serves one library app's official endpoints; fail paths answer 500.
func dlArr(t *testing.T, routes map[string]string, fail map[string]bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if fail[r.URL.Path] {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func dlCfg(qbitURL, sonarrURL, radarrURL string) config.Config {
	c := config.Default()
	c.QBit = config.QBit{URL: qbitURL}
	if sonarrURL != "" {
		c.Sonarr = config.App{URL: sonarrURL, APIKey: "key"}
	}
	if radarrURL != "" {
		c.Radarr = config.App{URL: radarrURL, APIKey: "key"}
	}
	return c
}

func byHash(items []Download) map[string]Download {
	out := map[string]Download{}
	for _, d := range items {
		out[strings.ToUpper(d.Hash)] = d
	}
	return out
}

func noLeak(t *testing.T, what, s string) {
	t.Helper()
	for _, leak := range []string{"http", "://", "key"} {
		if strings.Contains(s, leak) {
			t.Fatalf("%s leaked %q: %q", what, leak, s)
		}
	}
}

func TestDownloadQueueMapsParentsByHashNotTitle(t *testing.T) {
	qb := dlQBit(t, http.StatusOK, []map[string]any{
		{"hash": strings.ToLower(hashOne), "name": "Show S01E01", "progress": 0.5},
		{"hash": hashTwo, "name": "Film", "progress": 1, "completion_on": 1},
		{"hash": hashOld, "name": "Old Show", "progress": 1, "completion_on": 1},
		{"hash": hashFree, "name": "Show by hand", "progress": 0.9},
	})
	// Season pack: one download, one series, listed twice — IDs must dedup.
	sonarr := dlArr(t, map[string]string{
		"/api/v3/queue": fmt.Sprintf(`{"page":1,"totalRecords":2,"records":[
			{"downloadId":%q,"seriesId":7},
			{"downloadId":%q,"series":{"id":7}}]}`, hashOne, strings.ToLower(hashOne)),
		"/api/v3/history": fmt.Sprintf(`{"records":[
			{"eventType":"downloadFolderImported","downloadId":%q,"date":"2026-11-01T00:00:00Z"}]}`, hashOld),
	}, nil)
	radarr := dlArr(t, map[string]string{
		"/api/v3/queue":   fmt.Sprintf(`{"totalRecords":1,"records":[{"downloadId":%q,"movieId":9}]}`, hashTwo),
		"/api/v3/history": `{"records":[]}`,
	}, nil)

	items, problems, err := DownloadQueue(dlCfg(qb.URL, sonarr.URL, radarr.URL), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
	got := byHash(items)
	if len(got) != 4 {
		t.Fatalf("want 4 torrents, got %d", len(got))
	}
	if d := got[hashOne]; d.App != "Sonarr" || len(d.MediaIDs) != 1 || d.MediaIDs[0] != 7 {
		t.Fatalf("series mapping wrong: %+v", d)
	}
	if d := got[hashTwo]; d.App != "Radarr" || len(d.MediaIDs) != 1 || d.MediaIDs[0] != 9 {
		t.Fatalf("movie mapping wrong: %+v", d)
	}
	// Import history claims the hash only; its title must not borrow IDs.
	if d := got[hashOld]; d.App != "Sonarr" || len(d.MediaIDs) != 0 {
		t.Fatalf("import-owned download wrong: %+v", d)
	}
	if d := got[hashFree]; d.App != "Unmatched" || len(d.MediaIDs) != 0 {
		t.Fatalf("title look-alike must stay Unmatched: %+v", d)
	}
}

func TestDownloadQueueWalksEveryQueuePage(t *testing.T) {
	var page1 strings.Builder
	page1.WriteString(`{"page":1,"pageSize":1000,"totalRecords":1001,"records":[`)
	for i := 0; i < 1000; i++ {
		if i > 0 {
			page1.WriteString(",")
		}
		fmt.Fprintf(&page1, `{"downloadId":%q,"seriesId":7}`, hashOne)
	}
	page1.WriteString(`]}`)
	qb := dlQBit(t, http.StatusOK, []map[string]any{
		{"hash": hashOne, "name": "Pack", "progress": 0.3},
		{"hash": hashTwo, "name": "Second", "progress": 0.4},
	})
	sonarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "1":
			w.Write([]byte(page1.String()))
		case "2":
			w.Write([]byte(fmt.Sprintf(`{"page":2,"totalRecords":1001,"records":[{"downloadId":%q,"seriesId":9}]}`, hashTwo)))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(sonarr.Close)

	items, problems, err := DownloadQueue(dlCfg(qb.URL, sonarr.URL, ""), "", nil)
	if err != nil || len(problems) != 0 {
		t.Fatalf("err=%v problems=%v", err, problems)
	}
	got := byHash(items)
	if d := got[hashOne]; d.App != "Sonarr" || len(d.MediaIDs) != 1 || d.MediaIDs[0] != 7 {
		t.Fatalf("page-1 mapping wrong: %+v", d)
	}
	if d := got[hashTwo]; d.App != "Sonarr" || len(d.MediaIDs) != 1 || d.MediaIDs[0] != 9 {
		t.Fatalf("page-2 mapping wrong: %+v", d)
	}
}

func TestDownloadQueueRefusesToGuessBetweenApps(t *testing.T) {
	qb := dlQBit(t, http.StatusOK, []map[string]any{{"hash": hashBoth, "name": "Shared", "progress": 0.5}})
	sonarr := dlArr(t, map[string]string{
		"/api/v3/queue":   fmt.Sprintf(`{"totalRecords":1,"records":[{"downloadId":%q,"seriesId":7}]}`, hashBoth),
		"/api/v3/history": `{"records":[]}`,
	}, nil)
	radarr := dlArr(t, map[string]string{
		"/api/v3/queue":   fmt.Sprintf(`{"totalRecords":1,"records":[{"downloadId":%q,"movieId":9}]}`, hashBoth),
		"/api/v3/history": `{"records":[]}`,
	}, nil)

	items, problems, err := DownloadQueue(dlCfg(qb.URL, sonarr.URL, radarr.URL), "", nil)
	if err != nil || len(problems) != 0 {
		t.Fatalf("err=%v problems=%v", err, problems)
	}
	d := byHash(items)[hashBoth]
	if d.App != "Multiple apps" || len(d.MediaIDs) != 0 {
		t.Fatalf("shared hash must not be attributed: %+v", d)
	}
}

func TestDownloadQueuePartialFailureKeepsTorrentsVisible(t *testing.T) {
	qb := dlQBit(t, http.StatusOK, []map[string]any{
		{"hash": hashTwo, "name": "Owned", "progress": 0.5},
		{"hash": hashFree, "name": "Orphan", "progress": 0.5},
	})
	sonarr := dlArr(t, map[string]string{
		"/api/v3/history": `{"records":[]}`,
	}, map[string]bool{"/api/v3/queue": true})
	radarr := dlArr(t, map[string]string{
		"/api/v3/queue":   fmt.Sprintf(`{"totalRecords":1,"records":[{"downloadId":%q,"movieId":9}]}`, hashTwo),
		"/api/v3/history": `{"records":[]}`,
	}, nil)

	items, problems, err := DownloadQueue(dlCfg(qb.URL, sonarr.URL, radarr.URL), "", nil)
	if err != nil {
		t.Fatalf("partial failure must not be fatal: %v", err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "Sonarr") {
		t.Fatalf("want one sanitized Sonarr problem, got %v", problems)
	}
	noLeak(t, "problem", problems[0])
	got := byHash(items)
	if len(got) != 2 {
		t.Fatalf("torrents must stay visible, got %d", len(got))
	}
	if d := got[hashTwo]; d.App != "Radarr" || len(d.MediaIDs) != 1 {
		t.Fatalf("known owner must survive the other app's failure: %+v", d)
	}
	if d := got[hashFree]; d.App != "Unknown" {
		t.Fatalf("unread ownership must read Unknown, not Unmatched: %+v", d)
	}
}

func TestDownloadQueueFailsOnlyWhenTheDownloaderIsUnreadable(t *testing.T) {
	qb := dlQBit(t, http.StatusInternalServerError, nil)
	items, problems, err := DownloadQueue(dlCfg(qb.URL, "", ""), "", nil)
	if err == nil || items != nil || problems != nil {
		t.Fatalf("qBittorrent failure must be fatal, got items=%v problems=%v err=%v", items, problems, err)
	}
	noLeak(t, "fatal error", err.Error())

	items, _, err = DownloadQueue(config.Default(), "", nil) // no downloader configured
	if err == nil || !strings.Contains(err.Error(), "qBittorrent isn't connected") {
		t.Fatalf("missing downloader must say so, got %v", err)
	}
}

func TestDownloadQueueJoinsSavedProofByHash(t *testing.T) {
	const hashGone = "6666666666666666666666666666666666666666"
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "present"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	qb := dlQBit(t, http.StatusOK, []map[string]any{
		{"hash": strings.ToLower(hashOne), "name": "Show S01", "progress": 1, "state": "uploading", "completion_on": 1700000000},
		{"hash": hashOld, "name": "Old Film", "progress": 1, "state": "uploading"},
		{"hash": hashTwo, "name": "Big.Fish.2003.1080p", "progress": 0.5, "state": "downloading"},
		{"hash": hashFree, "name": "Lost", "progress": 1, "state": "missingFiles"},
		{"hash": hashBoth, "name": "present", "progress": 1, "state": "uploading", "save_path": src},
		{"hash": hashGone, "name": "absent", "progress": 1, "state": "uploading", "save_path": src},
	})
	radarr := dlArr(t, map[string]string{
		"/api/v3/queue":   fmt.Sprintf(`{"totalRecords":1,"records":[{"downloadId":%q,"movieId":9}]}`, hashTwo),
		"/api/v3/history": `{"records":[]}`,
		"/api/v3/movie":   `[{"id":9,"title":"Big Fish","year":2003}]`,
	}, nil)
	home := t.TempDir()
	sha := strings.Repeat("ab", 32)
	ledger := fmt.Sprintf(`{"version":1,"items":[
		{"hash":%q,"importedAt":"2026-11-01T00:00:00Z","verifiedAt":"2026-11-02T00:00:00Z","importedPath":"/Volumes/NAS/TV/Show","savePath":"/downloads/tv","files":[{"rel":"a.mkv","size":1,"sha256":%q},{"rel":"b.mkv","size":1,"sha256":%q}]},
		{"hash":%q,"importedAt":"2026-11-01T00:00:00Z","verifiedAt":"2026-11-02T00:00:00Z","importedPath":"/Users/me/Films/Old","savePath":"/downloads/film","files":[{"rel":"","size":1,"sha256":%q},{"rel":"b","size":1,"sha256":""}]}]}`,
		strings.ToLower(hashOne), sha, sha, hashOld, sha)
	if err := os.WriteFile(filepath.Join(home, cleanupStateFile), []byte(ledger), 0o600); err != nil {
		t.Fatal(err)
	}

	items, problems, err := DownloadQueue(dlCfg(qb.URL, "", radarr.URL), home, nil)
	if err != nil || len(problems) != 0 {
		t.Fatalf("err=%v problems=%v", err, problems)
	}
	got := byHash(items)
	d := got[hashOne]
	if d.Verified != "hash" || d.VerifiedAt != "2026-11-01T00:00:00Z" || d.Status != "copied" ||
		d.TargetPath != "/Volumes/NAS/TV/Show" || d.TargetVolume != "NAS" || d.SourcePath != "/downloads/tv" ||
		d.StatusAt != "2023-11-14T22:13:20Z" || d.SourceGone {
		t.Fatalf("hash-verified proof wrong: %+v", d)
	}
	if !d.LocalCopy {
		t.Fatal("a row with a saved record must offer the tracked-copy Verify")
	}
	if got[hashTwo].LocalCopy {
		t.Fatal("a row without a saved record must not claim a tracked copy")
	}
	if d := got[hashOld]; d.Verified != "size" || d.Status != "copied" || d.TargetVolume != "Startup disk" || d.SourceGone {
		t.Fatalf("a file without SHA-256 must read size: %+v", d)
	}
	if d := got[hashTwo]; d.Status != "downloading" || d.MediaTitle != "Big Fish" || d.Verified != "" || d.TargetPath != "" || d.PosterPath != "" {
		t.Fatalf("active download wrong: %+v", d)
	}
	if d := got[hashFree]; !d.SourceGone || d.Verified != "" || d.VerifiedAt != "" || d.StatusAt != "" || d.MediaTitle != "Lost" {
		t.Fatalf("missingFiles must read sourceGone with no proof: %+v", d)
	}
	if d := got[hashBoth]; d.SourceGone || d.Verified != "" || d.Status != "on-disk" {
		t.Fatalf("readable source without proof wrong: %+v", d)
	}
	if d := got[hashGone]; !d.SourceGone {
		t.Fatalf("finished torrent whose files are absent must read sourceGone: %+v", d)
	}

	// A damaged ledger is reported and nothing reads as verified.
	if err := os.WriteFile(filepath.Join(home, cleanupStateFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	items, problems, err = DownloadQueue(dlCfg(qb.URL, "", radarr.URL), home, nil)
	if err != nil || len(problems) != 1 || !strings.Contains(problems[0], "cleanup proof") {
		t.Fatalf("damaged ledger must be one problem: err=%v problems=%v", err, problems)
	}
	if d := byHash(items)[hashOne]; d.Verified != "" || d.Status == "copied" {
		t.Fatalf("no readable proof must not verify: %+v", d)
	}
}

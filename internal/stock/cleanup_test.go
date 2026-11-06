package stock

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func historyServer(t *testing.T, pages []string) (*httptest.Server, *[]url.Values) {
	t.Helper()
	seen := &[]url.Values{}
	i := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.URL.Query())
		if !strings.HasPrefix(r.URL.Path, "/api/v3/history") {
			http.NotFound(w, r)
			return
		}
		if i >= len(pages) {
			http.Error(w, "extra page", http.StatusInternalServerError)
			return
		}
		w.Write([]byte(pages[i]))
		i++
	}))
	t.Cleanup(srv.Close)
	return srv, seen
}

func TestWholeImportsReadsEveryPageAndFailsClosedOnGaps(t *testing.T) {
	page1 := `{"page":1,"totalRecords":3,"records":[
		{"eventType":"downloadFolderImported","downloadId":"` + strings.ToLower(hashA) + `","date":"2026-01-01T10:00:00Z","data":{"importedPath":"/library/Movie"}},
		{"eventType":"grabbed","downloadId":"` + hashA + `","date":"2026-01-01T09:00:00Z","data":{}}]}`
	page2 := `{"page":2,"totalRecords":3,"records":[
		{"eventType":"trackFileImported","downloadId":"` + hashB + `","date":"2026-01-02T10:00:00Z","data":{"importedPath":"/library/Album"}}]}`
	srv, seen := historyServer(t, []string{page1, page2})
	got, err := NewArr("Sonarr", srv.URL, "k").WholeImports()
	if err != nil {
		t.Fatal(err)
	}
	p, ok := got[strings.ToUpper(hashA)]
	if !ok || p.ImportedPath != "/library/Movie" || !p.ImportedAt.Equal(time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("the whole import wasn't proven: %+v", got)
	}
	if _, ok := got[strings.ToUpper(hashB)]; ok {
		t.Fatal("a per-file import event must never count as a whole import")
	}
	if len(*seen) != 2 || (*seen)[0].Get("page") != "1" || (*seen)[1].Get("page") != "2" || (*seen)[0].Get("pageSize") != "1000" {
		t.Fatalf("pagination wasn't followed: %v", *seen)
	}

	cases := []struct {
		name  string
		pages []string
		want  string
	}{
		{"missing totalRecords", []string{`{"page":1,"records":[]}`}, "malformed"},
		{"stops early", []string{
			`{"page":1,"totalRecords":5,"records":[{"eventType":"downloadFolderImported","downloadId":"` + hashA + `","date":"2026-01-01T10:00:00Z","data":{"importedPath":"/library/Movie"}}]}`,
			`{"page":2,"totalRecords":5,"records":[]}`,
		}, "stopped early"},
		{"missing library path", []string{
			`{"page":1,"totalRecords":1,"records":[{"eventType":"downloadFolderImported","downloadId":"` + hashA + `","date":"2026-01-01T10:00:00Z","data":{}}]}`,
		}, "without its library path"},
		{"missing import date", []string{
			`{"page":1,"totalRecords":1,"records":[{"eventType":"downloadFolderImported","downloadId":"` + hashA + `","data":{"importedPath":"/library/Movie"}}]}`,
		}, "valid import date"},
		{"zero import date", []string{
			`{"page":1,"totalRecords":1,"records":[{"eventType":"downloadFolderImported","downloadId":"` + hashA + `","date":"0001-01-01T00:00:00Z","data":{"importedPath":"/library/Movie"}}]}`,
		}, "valid import date"},
		{"future import date", []string{
			`{"page":1,"totalRecords":1,"records":[{"eventType":"downloadFolderImported","downloadId":"` + hashA + `","date":"` + time.Now().Add(48*time.Hour).UTC().Format(time.RFC3339) + `","data":{"importedPath":"/library/Movie"}}]}`,
		}, "valid import date"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := historyServer(t, tc.pages)
			if _, err := NewArr("Radarr", srv.URL, "k").WholeImports(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q error, got %v", tc.want, err)
			}
		})
	}
}

func qbitServer(t *testing.T, body string, seenForm *url.Values, seenQuery *url.Values) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/torrents/info":
			w.Write([]byte(body))
		case "/api/v2/torrents/files":
			if seenQuery != nil {
				*seenQuery = r.URL.Query()
			}
			w.Write([]byte(`[{"name":"Root/a.mkv","size":11,"progress":1}]`))
		case "/api/v2/torrents/delete":
			r.ParseForm()
			if seenForm != nil {
				*seenForm = r.PostForm
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCleanupTorrentsFilesAndMetadataOnlyDelete(t *testing.T) {
	body := `[{"hash":"` + hashA + `","name":"Movie","save_path":"/downloads","state":"stoppedUP","progress":1,"ratio":2,"seeding_time":2880,"completion_on":10,"added_on":5,"size":11}]`
	var form, query url.Values
	srv := qbitServer(t, body, &form, &query)
	q := NewQBit(srv.URL, "", "")
	ts, err := q.CleanupTorrents()
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 1 || ts[0].SavePath != "/downloads" || ts[0].SeedingTime != 2880 || ts[0].Ratio != 2 {
		t.Fatalf("snake_case fields weren't decoded: %+v", ts)
	}
	fs, err := q.TorrentFiles(hashA)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].Name != "Root/a.mkv" || query.Get("hash") != hashA {
		t.Fatalf("files request wrong: %+v %v", fs, query)
	}
	if err := q.RemoveMetadata(hashA); err != nil {
		t.Fatal(err)
	}
	if form.Get("deleteFiles") != "false" || form.Get("hashes") != hashA {
		t.Fatalf("metadata removal must never delete files: %v", form)
	}
	for key, values := range form {
		for _, v := range values {
			if key == "deleteFiles" && v != "false" {
				t.Fatalf("deleteFiles was %q", v)
			}
		}
	}
}

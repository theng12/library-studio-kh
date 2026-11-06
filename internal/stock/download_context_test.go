package stock

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// queueServer answers /queue with the pages given, in order, and fails if a
// later page is requested than supplied.
func queueServer(t *testing.T, api string, pages []string) (*httptest.Server, *[]string) {
	t.Helper()
	seen := &[]string{}
	i := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.URL.RawQuery)
		if !strings.HasPrefix(r.URL.Path, "/api/"+api+"/queue") {
			http.NotFound(w, r)
			return
		}
		if i >= len(pages) {
			http.Error(w, "extra page requested", http.StatusInternalServerError)
			return
		}
		w.Write([]byte(pages[i]))
		i++
	}))
	t.Cleanup(srv.Close)
	return srv, seen
}

func TestQueueEntriesPaginatesAndUppercasesIDs(t *testing.T) {
	// Season pack: two rows, one download ID, one series — the caller dedups.
	page1 := fmt.Sprintf(`{"page":1,"pageSize":1000,"totalRecords":3,"records":[
		{"downloadId":"%s","seriesId":7},
		{"downloadId":"%s","series":{"id":7}}]}`, hashA, strings.ToLower(hashA))
	page2 := `{"page":2,"pageSize":1000,"totalRecords":3,"records":[{"downloadId":"` + hashB + `","seriesId":9}]}`
	srv, seen := queueServer(t, "v3", []string{page1, page2})

	entries, err := NewArr("Sonarr", srv.URL, "k").QueueEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("want 3 entries, got %d: %+v", len(entries), entries)
	}
	for _, e := range entries {
		if e.DownloadID != strings.ToUpper(e.DownloadID) || e.DownloadID == "" {
			t.Fatalf("download ID not upper-cased: %+v", e)
		}
	}
	if entries[0].MediaID != 7 || entries[1].MediaID != 7 || entries[2].MediaID != 9 {
		t.Fatalf("parent IDs wrong: %+v", entries)
	}
	if len(*seen) != 2 || !strings.Contains((*seen)[0], "page=1") || !strings.Contains((*seen)[1], "page=2") {
		t.Fatalf("pagination not followed: %v", *seen)
	}
	for _, q := range *seen {
		if !strings.Contains(q, "pageSize=1000") {
			t.Fatalf("pageSize missing from %q", q)
		}
	}
}

func TestQueueEntriesParentIDPerApp(t *testing.T) {
	cases := []struct {
		app, api, body, wantID string
	}{
		{"Radarr", "v3", `{"totalRecords":1,"records":[{"downloadId":"` + hashA + `","movieId":4}]}`, "4"},
		{"Radarr", "v3", `{"totalRecords":1,"records":[{"downloadId":"` + hashA + `","movie":{"id":5}}]}`, "5"},
		{"Lidarr", "v1", `{"totalRecords":1,"records":[{"downloadId":"` + hashA + `","artistId":6}]}`, "6"},
		{"Lidarr", "v1", `{"totalRecords":1,"records":[{"downloadId":"` + hashA + `","artist":{"id":8}}]}`, "8"},
		{"Sonarr", "v3", `{"totalRecords":1,"records":[{"downloadId":"` + hashA + `","seriesId":3}]}`, "3"},
	}
	for _, tc := range cases {
		srv, _ := queueServer(t, tc.api, []string{tc.body})
		entries, err := NewArr(tc.app, srv.URL, "k").QueueEntries()
		if err != nil {
			t.Fatalf("%s: %v", tc.app, err)
		}
		want, _ := strconv.Atoi(tc.wantID)
		if len(entries) != 1 || entries[0].MediaID != want || entries[0].DownloadID != strings.ToUpper(hashA) {
			t.Fatalf("%s %s: got %+v", tc.app, tc.body, entries)
		}
	}
}

func TestQueueEntriesRejectsMalformedPagination(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"truncated", `{"page":1,"totalRecords":5000,"records":[]}`, "stopped early"},
		{"wrong page echo", `{"page":2,"totalRecords":1,"records":[{"downloadId":"` + hashA + `"}]}`, "malformed queue page"},
		{"negative total", `{"page":1,"totalRecords":-1,"records":[]}`, "malformed queue page"},
		{"bad record", `{"page":1,"totalRecords":1,"records":[123]}`, "malformed queue entry"},
		{"missing total", `{"records":[]}`, "malformed queue page"},
		{"missing records", `{"totalRecords":0}`, "malformed queue page"},
		{"null entry", `{"totalRecords":1,"records":[null]}`, "malformed queue entry"},
		{"records not a list", `{"page":1,"totalRecords":1,"records":"nope"}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			_, err := NewArr("Sonarr", srv.URL, "k").QueueEntries()
			if err == nil {
				t.Fatal("malformed pagination must be an error, never a short list")
			}
			if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q in %q", tc.want, err)
			}
		})
	}
}

func TestQueueEntriesNeverRunsForever(t *testing.T) {
	// A server that ignores the page number and reports an unreachable total.
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(map[string]any{
			"totalRecords": 1 << 40,
			"records":      []map[string]any{{"downloadId": hashA}},
		})
	}))
	defer srv.Close()
	if _, err := NewArr("Sonarr", srv.URL, "k").QueueEntries(); err == nil || !strings.Contains(err.Error(), "did not advance") {
		t.Fatalf("want bounded-run error, got %v", err)
	}
	if calls != maxQueuePages {
		t.Fatalf("want %d requests, got %d", maxQueuePages, calls)
	}
}

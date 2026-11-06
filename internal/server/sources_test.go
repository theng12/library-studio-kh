package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"librarystudio/internal/config"
)

func TestSourcesRoute(t *testing.T) {
	stockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/indexer":
			w.Write([]byte(`[{"id":1,"name":"Nzbgeek","enable":true}]`))
		default:
			w.Write([]byte(`[]`))
		}
	}))
	defer stockSrv.Close()

	home := t.TempDir()
	store, _ := config.Open(home)
	c := config.Default()
	c.Prowlarr = config.App{URL: stockSrv.URL, APIKey: "{{KEY_f40270de}}"}
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(store, home).registerSources(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/sources", nil))
	if w.Code != 200 {
		t.Fatalf("GET: %d %s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"configured", "indexers", "links", "health", "libraries", "problems"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing key %q: %s", key, w.Body.String())
		}
	}
	if got["configured"] != true {
		t.Fatalf("configured: %v", got["configured"])
	}
	if strings.Contains(w.Body.String(), stockSrv.URL) || strings.Contains(w.Body.String(), "{{KEY_f40270de}}") {
		t.Fatalf("response leaked url or key: %s", w.Body.String())
	}

	// GET only: no mutation endpoints in this batch.
	for _, method := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		req := httptest.NewRequest(method, "/api/sources", strings.NewReader("{}"))
		rw := httptest.NewRecorder()
		mux.ServeHTTP(rw, req)
		if rw.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s must be refused: %d", method, rw.Code)
		}
	}
}

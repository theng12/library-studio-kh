package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"librarystudio/internal/config"
)

func companionServer(t *testing.T) (*config.Store, *Server, *int) {
	t.Helper()
	prefs := map[string]any{
		"max_ratio_enabled": false, "max_ratio": 2.0, "max_ratio_act": 0,
		"max_seeding_time_enabled": true, "max_seeding_time": 2880.0, "dl_limit": 0.0,
	}
	var writes int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v2/app/version" && r.Method == "GET":
			w.Write([]byte("v5.2.3"))
		case r.URL.Path == "/api/v2/app/preferences" && r.Method == "GET":
			json.NewEncoder(w).Encode(prefs)
		case r.URL.Path == "/api/v2/app/setPreferences" && r.Method == "POST":
			var raw map[string]any
			json.Unmarshal([]byte(r.FormValue("json")), &raw)
			prefs = raw
			writes++
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	home := t.TempDir()
	store, _ := config.Open(home)
	c := config.Default()
	c.Sonarr = config.App{URL: srv.URL, APIKey: "test-sonarr-key"}
	c.QBit = config.QBit{URL: srv.URL}
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}
	s := New(store, home)
	return store, s, &writes
}

func TestCompanionSettingsRoutesReadAndApply(t *testing.T) {
	store, s, writes := companionServer(t)
	c := store.Get()
	mux := http.NewServeMux()
	s.RegisterCompanionSettings(mux)
	call := func(method, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, "/api/companion/settings", strings.NewReader(body)))
		return w
	}

	if w := call("GET", ""); w.Code != 200 {
		t.Fatalf("GET: %d %s", w.Code, w.Body.String())
	} else if !strings.Contains(w.Body.String(), `"key":"max_ratio"`) || !strings.Contains(w.Body.String(), `"value":2`) ||
		(c.Sonarr.APIKey != "" && strings.Contains(w.Body.String(), c.Sonarr.APIKey)) ||
		(c.QBit.Password != "" && strings.Contains(w.Body.String(), c.QBit.Password)) {
		t.Fatalf("GET must report actual values without secrets: %s", w.Body.String())
	}
	if *writes != 0 {
		t.Fatalf("GET must not write, got %d writes", *writes)
	}

	if w := call("POST", `{"app":"qBittorrent","max_ratio":3}`); w.Code != 200 {
		t.Fatalf("POST: %d %s", w.Code, w.Body.String())
	} else if !strings.Contains(w.Body.String(), `"value":3`) {
		t.Fatalf("readback missing: %s", w.Body.String())
	}
	if *writes != 1 {
		t.Fatalf("one explicit apply should write once, got %d", *writes)
	}

	if w := call("POST", `{"app":"qBittorrent","max_ratio_act":3}`); w.Code != 400 || *writes != 1 {
		t.Fatalf("removal action must be refused without writing: %d %s", w.Code, w.Body.String())
	}
	if w := call("POST", `{"app":"qBittorrent","deleteTorrentAndFiles":true}`); w.Code != 400 || *writes != 1 {
		t.Fatalf("unknown field must be refused: %d %s", w.Code, w.Body.String())
	}
	if w := call("POST", `{"app":"Sonarr","removeCompletedDownloads":true}`); w.Code != 400 || *writes != 1 {
		t.Fatalf("turning immediate removal on must be refused: %d %s", w.Code, w.Body.String())
	}
	if w := call("POST", "{not json"); w.Code != 400 || *writes != 1 {
		t.Fatalf("malformed JSON must be refused without writing: %d %s", w.Code, w.Body.String())
	}
	if w := call("POST", `{"app":"qBittorrent","max_ratio":"lots"}`); w.Code != 400 || *writes != 1 {
		t.Fatalf("wrong value type must be refused without writing: %d %s", w.Code, w.Body.String())
	}
}

func TestCompanionSettingsPartialApplyReturnsFreshState(t *testing.T) {
	handlingOn := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v3/config/downloadclient" && r.Method == "GET":
			json.NewEncoder(w).Encode(map[string]any{"id": 4, "enableCompletedDownloadHandling": handlingOn})
		case r.URL.Path == "/api/v3/config/downloadclient/4" && r.Method == "PUT":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			handlingOn, _ = body["enableCompletedDownloadHandling"].(bool)
		case r.URL.Path == "/api/v3/downloadclient" && r.Method == "GET":
			json.NewEncoder(w).Encode([]map[string]any{})
		case r.URL.Path == "/api/v3/config/naming" && r.Method == "GET":
			json.NewEncoder(w).Encode([]map[string]any{{"id": 2, "renameEpisodes": false}})
		case r.URL.Path == "/api/v3/config/naming/2" && r.Method == "PUT":
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`[{"errorMessage":"naming blocked"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	home := t.TempDir()
	store, _ := config.Open(home)
	c := config.Default()
	c.Sonarr = config.App{URL: srv.URL, APIKey: "test-sonarr-key"}
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}
	s := New(store, home)
	mux := http.NewServeMux()
	s.RegisterCompanionSettings(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/companion/settings",
		strings.NewReader(`{"app":"Sonarr","enableCompletedDownloadHandling":true,"renameEpisodes":true}`)))
	if w.Code != 502 || !strings.Contains(w.Body.String(), `"applied":["enableCompletedDownloadHandling"]`) ||
		!strings.Contains(w.Body.String(), `"app"`) || !strings.Contains(w.Body.String(), `"error":`) {
		t.Fatalf("partial apply must return 502 with fresh state and applied fields: %d %s", w.Code, w.Body.String())
	}
	if !handlingOn {
		t.Fatal("the successful first write must stay applied")
	}
}

func TestCompanionSettingsPostNeedsTokenAndCSRFHeader(t *testing.T) {
	_, s, writes := companionServer(t)
	token := s.Token
	mux := http.NewServeMux()
	s.RegisterCompanionSettings(mux)
	h := s.guard(mux)
	req := func(body string, headers map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://127.0.0.1:7000/api/companion/settings", strings.NewReader(body))
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	body := `{"app":"qBittorrent","max_ratio":3}`
	if w := req(body, nil); w.Code != 403 {
		t.Fatalf("missing token must be refused: %d", w.Code)
	}
	if w := req(body, map[string]string{"X-Library-Token": token, "Sec-Fetch-Site": "cross-site"}); w.Code != 403 {
		t.Fatalf("cross-site write must be refused even with a token: %d", w.Code)
	}
	if w := req(body, map[string]string{"X-Library-Token": token, "X-Library-Request": "1"}); w.Code != 200 {
		t.Fatalf("authenticated explicit write must pass the existing guard: %d %s", w.Code, w.Body.String())
	}
	if *writes != 1 {
		t.Fatalf("refused requests must not write, got %d", *writes)
	}
}

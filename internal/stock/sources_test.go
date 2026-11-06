package stock

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSourceReadsWhitelistAndSanitize(t *testing.T) {
	const apiKey = "s3cr3t-api-key-value"
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		switch r.URL.Path {
		case "/api/v1/indexer":
			w.Write([]byte(`[{"id":7,"name":"Nzbgeek","enable":true,"priority":25,"protocol":"usenet",
				"fields":[{"name":"apiKey","value":"` + apiKey + `"},{"name":"baseUrl","value":"http://indexer.example:8080"}],
				"baseUrl":"http://indexer.example:8080","password":"hunter2","downloadUrl":"http://indexer.example/get?t=abc"}]`))
		case "/api/v1/indexerstatus":
			w.Write([]byte(`[{"id":3,"indexerId":7,"disabledTill":"2026-11-06T10:00:00Z"}]`))
		case "/api/v1/applications":
			w.Write([]byte(`[{"id":1,"name":"Sonarr","syncLevel":"FullSync","fields":[{"name":"apiKey","value":"appkey"}]}]`))
		case "/api/v1/health":
			w.Write([]byte(`[{"type":"warning","message":"Indexer error: call http://10.0.0.5:8989/api?apikey=` + apiKey + ` passkey deadbeef magnet:?xt=urn:btih:aaa"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := NewArr("Prowlarr", srv.URL, apiKey)
	idx, err := p.Indexers()
	if err != nil || len(idx) != 1 {
		t.Fatal(err, idx)
	}
	raw := idx[0]
	if raw.ID != 7 || raw.Name != "Nzbgeek" || raw.Enabled == nil || !*raw.Enabled || raw.Priority == nil || *raw.Priority != 25 || raw.Protocol != "usenet" {
		t.Fatalf("indexer fields wrong: %+v", raw)
	}
	st, err := p.IndexerStatuses()
	if err != nil || !st[7].Equal(time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)) {
		t.Fatal(err, st)
	}
	links, err := p.Applications()
	if err != nil || len(links) != 1 || links[0].Name != "Sonarr" || links[0].SyncLevel != "FullSync" {
		t.Fatal(err, links)
	}
	health, err := p.Health()
	if err != nil || len(health) != 1 {
		t.Fatal(err, health)
	}
	msg := health[0].Message
	for _, leak := range []string{"http", "10.0.0.5", "passkey", "deadbeef", "magnet", apiKey} {
		if strings.Contains(strings.ToLower(msg), strings.ToLower(leak)) {
			t.Fatalf("health message leaked %q: %q", leak, msg)
		}
	}
	if health[0].Type != "warning" {
		t.Fatalf("type wrong: %+v", health[0])
	}
	for _, m := range methods {
		if m != http.MethodGet {
			t.Fatalf("mutation request sent: %s", m)
		}
	}
}

func TestSourceIndexerStatusFailureIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	if _, err := NewArr("Prowlarr", srv.URL, "k").IndexerStatuses(); err == nil {
		t.Fatal("status failure must be an error, never read as healthy")
	}
}

func TestSanitizeStripsOwnBaseAndEnvKey(t *testing.T) {
	t.Setenv("PROWLARR__APIKEY", "env-secret-key")
	a := NewArr("Prowlarr", "http://127.0.0.1:9696", "local-key")
	got := a.sanitizeText("failed calling http://127.0.0.1:9696/api/v1/health key local-key env-secret-key")
	for _, leak := range []string{"http://127.0.0.1:9696", "local-key", "env-secret-key"} {
		if strings.Contains(got, leak) {
			t.Fatalf("leaked %q: %q", leak, got)
		}
	}
}

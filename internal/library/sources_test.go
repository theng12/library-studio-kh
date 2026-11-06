package library

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"librarystudio/internal/config"
)

func newApp(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(h)
}

func TestSourcesMissingProwlarr(t *testing.T) {
	got := Sources(config.Default())
	if got.Configured {
		t.Fatal("no config must be configured=false")
	}
	if len(got.Indexers) != 0 || len(got.Links) != 0 || len(got.Health) != 0 || len(got.Problems) == 0 {
		t.Fatalf("empty collections and a problem required: %+v", got)
	}
	if len(got.Libraries) != 3 {
		t.Fatalf("library apps must still be listed: %+v", got.Libraries)
	}
	for _, lib := range got.Libraries {
		if lib.Configured || lib.Error != "" || lib.Indexers == nil {
			t.Fatalf("unconfigured library wrong: %+v", lib)
		}
	}
}

func TestSourcesPartialOutageCannotLookHealthy(t *testing.T) {
	prowlarr := newApp(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/indexer":
			w.Write([]byte(`[{"id":7,"name":"Nzbgeek","enable":true,"priority":25,"protocol":"usenet"}]`))
		case "/api/v1/applications":
			w.Write([]byte(`[{"id":1,"name":"Sonarr","syncLevel":"FullSync"}]`))
		default: // indexerstatus and health fail
			http.Error(w, "boom", http.StatusInternalServerError)
		}
	})
	defer prowlarr.Close()
	sonarr := newApp(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":1,"name":"Nzbgeek","enable":true}]`))
	})
	defer sonarr.Close()
	radarr := newApp(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
	defer radarr.Close()

	c := config.Default()
	c.Prowlarr = config.App{URL: prowlarr.URL, APIKey: "{{KEY_1ce1a07b}}"}
	c.Sonarr = config.App{URL: sonarr.URL, APIKey: "sk"}
	c.Radarr = config.App{URL: radarr.URL, APIKey: "rk"} // Lidarr stays unconfigured

	got := Sources(c)
	if !got.Configured {
		t.Fatal("Prowlarr configured must be true")
	}
	if len(got.Indexers) != 1 || got.Indexers[0].DisabledUntil != nil {
		t.Fatalf("indexers wrong: %+v", got.Indexers)
	}
	if len(got.Health) != 0 {
		t.Fatalf("failed health read must stay empty, not healthy: %+v", got.Health)
	}
	if len(got.Links) != 1 {
		t.Fatalf("links: %+v", got.Links)
	}
	// status failure and health failure are top-level problems; Radarr reports in its library entry.
	if len(got.Problems) < 2 {
		t.Fatalf("every partial failure needs a problem: %+v", got.Problems)
	}
	body, _ := json.Marshal(got)
	for _, leak := range []string{"http://", "127.0.0.1", "boom", "Unauthorized", "{{KEY_1ce1a07b}}", "dial tcp", "refused"} {
		if strings.Contains(string(body), leak) {
			t.Fatalf("response leaked %q: %s", leak, body)
		}
	}
	if len(got.Libraries) != 3 {
		t.Fatalf("libraries: %+v", got.Libraries)
	}
	if !got.Libraries[0].Configured || len(got.Libraries[0].Indexers) != 1 || got.Libraries[0].Error != "" {
		t.Fatalf("Sonarr wrong: %+v", got.Libraries[0])
	}
	if !got.Libraries[1].Configured || got.Libraries[1].Error == "" || len(got.Libraries[1].Indexers) != 0 {
		t.Fatalf("Radarr failure must read as an error: %+v", got.Libraries[1])
	}
	if got.Libraries[2].Configured || got.Libraries[2].Error != "" {
		t.Fatalf("Lidarr not connected: %+v", got.Libraries[2])
	}
}

func TestSourcesDisabledStatusAndSanitizedHealth(t *testing.T) {
	const apiKey = "{{KEY_112233aa}}"
	prowlarr := newApp(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/indexer":
			w.Write([]byte(`[{"id":7,"name":"Nzbgeek","enable":false,"priority":25,"protocol":"usenet"}]`))
		case "/api/v1/indexerstatus":
			w.Write([]byte(`[{"id":3,"indexerId":7,"disabledTill":"2026-11-06T10:00:00Z"}]`))
		case "/api/v1/applications":
			w.Write([]byte(`[{"id":1,"name":"Sonarr","syncLevel":"FullSync"}]`))
		case "/api/v1/health":
			w.Write([]byte(`[{"type":"warning","message":"Indexer failed: http://10.0.0.5:8989/api?apikey=` + apiKey + ` passkey deadbeef"}]`))
		default:
			http.NotFound(w, r)
		}
	})
	defer prowlarr.Close()

	c := config.Default()
	c.Prowlarr = config.App{URL: prowlarr.URL, APIKey: apiKey}

	got := Sources(c)
	if len(got.Indexers) != 1 {
		t.Fatalf("indexers: %+v", got.Indexers)
	}
	idx := got.Indexers[0]
	if idx.Enabled == nil || *idx.Enabled || idx.DisabledUntil == nil {
		t.Fatalf("actual disabled status missing: %+v", idx)
	}
	if len(got.Health) != 1 || got.Health[0].Type != "warning" {
		t.Fatalf("health: %+v", got.Health)
	}
	body, _ := json.Marshal(got)
	for _, leak := range []string{"http://", "10.0.0.5", "passkey", "deadbeef", apiKey, "apikey="} {
		if strings.Contains(string(body), leak) {
			t.Fatalf("response leaked %q: %s", leak, body)
		}
	}
}

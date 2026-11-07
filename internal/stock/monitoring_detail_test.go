package stock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDetailPreservesUnreportedMonitoring(t *testing.T) {
	for _, app := range []string{"Sonarr", "Radarr", "Lidarr"} {
		for _, state := range []string{"absent", "off", "on"} {
			t.Run(app+"/"+state, func(t *testing.T) {
				var expected any
				if state != "absent" {
					expected = state == "on"
				}
				withMonitoring := func(row map[string]any) map[string]any {
					if state != "absent" {
						row["monitored"] = expected
					}
					return row
				}
				parent := withMonitoring(map[string]any{"title": "Fixture", "seasons": []any{withMonitoring(map[string]any{"seasonNumber": 1})}})
				child := withMonitoring(map[string]any{"id": 11, "seriesId": 1, "artistId": 1, "title": "Child", "seasonNumber": 1, "episodeNumber": 1})
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/v3/series/1", "/api/v3/movie/1", "/api/v1/artist/1":
						json.NewEncoder(w).Encode(parent)
					case "/api/v3/episode", "/api/v1/album":
						json.NewEncoder(w).Encode([]any{child})
					default:
						http.NotFound(w, r)
					}
				}))
				defer srv.Close()
				detail, err := NewArr(app, srv.URL, "fixture").Detail(1)
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := json.Marshal(detail)
				if err != nil {
					t.Fatal(err)
				}
				var output map[string]any
				if err := json.Unmarshal(encoded, &output); err != nil {
					t.Fatal(err)
				}
				if value, exists := output["monitored"]; !exists || value != expected {
					t.Fatalf("parent monitoring: %v, want %v", value, expected)
				}
				for _, group := range []string{"seasons", "episodes", "albums"} {
					for _, row := range output[group].([]any) {
						if value, exists := row.(map[string]any)["monitored"]; !exists || value != expected {
							t.Fatalf("%s monitoring: %v, want %v", group, value, expected)
						}
					}
				}
			})
		}
	}
}

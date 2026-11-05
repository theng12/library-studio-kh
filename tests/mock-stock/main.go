// Command mock-stock pretends to be qBittorrent, Sonarr, Radarr and Lidarr with made-up items,
// so the dashboard can be looked at without real apps. Every app uses the same address.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"strings"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:7890", "address")
	flag.Parse()
	now := time.Now()
	ago := func(d int) string { return now.Add(-time.Duration(d) * 24 * time.Hour).Format(time.RFC3339) }
	ahead := func(d int) string { return now.Add(time.Duration(d) * 24 * time.Hour).Format(time.RFC3339) }
	routes := map[string]any{
		"/api/v2/torrents/info": []map[string]any{
			{"hash": "a1", "name": "Sample Series S02E04 1080p", "progress": 0.62},
			{"hash": "a2", "name": "Example Film (2024) 2160p", "progress": 1},
			{"hash": "a3", "name": "Demo Artist - First Album (FLAC)", "progress": 1},
			{"hash": "a4", "name": "Sample Series S02E03 1080p", "progress": 1},
			{"hash": "a5", "name": "home-videos-backup", "progress": 1},
		},
		"/api/v2/app/preferences": map[string]any{"max_seeding_time_enabled": false, "max_seeding_time": -1, "max_ratio_act": 0},
		"/api/v2/sync/maindata":   map[string]any{"server_state": map[string]any{"free_space_on_disk": 412e9}},
	}
	arr := map[string]map[string]any{
		"/api/v3/history": {"records": []map[string]any{
			{"eventType": "downloadFolderImported", "downloadId": "A4", "date": ago(3)},
			{"eventType": "downloadFolderImported", "downloadId": "A2", "date": ago(1)},
		}},
		"/api/v1/history":               {"records": []map[string]any{{"eventType": "downloadImported", "downloadId": "A3", "date": ago(6)}}},
		"/api/v3/queue":                 {"records": []map[string]any{{"downloadId": "A1"}}},
		"/api/v1/queue":                 {"records": []map[string]any{}},
		"/api/v3/config/downloadclient": {"id": 1, "enableCompletedDownloadHandling": true},
		"/api/v1/config/downloadclient": {"id": 1, "enableCompletedDownloadHandling": true},
	}
	cal := []map[string]any{{"title": "New Beginnings", "seasonNumber": 2, "episodeNumber": 5, "airDateUtc": ahead(3), "series": map[string]any{"title": "Sample Series"}}}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/api/v2/app/setPreferences" || p == "/api/v2/torrents/delete" || r.Method == "PUT":
			log.Printf("%s %s %s", r.Method, p, r.FormValue("json"))
			return
		case strings.HasSuffix(p, "/downloadclient") && !strings.Contains(p, "/config/"):
			json.NewEncoder(w).Encode([]map[string]any{{"id": 1, "implementation": "QBittorrent", "removeCompletedDownloads": true}})
			return
		case strings.HasSuffix(p, "/calendar"):
			if strings.Contains(p, "v1") {
				json.NewEncoder(w).Encode([]any{})
			} else {
				json.NewEncoder(w).Encode(cal)
			}
			return
		case strings.HasSuffix(p, "/system/status"):
			json.NewEncoder(w).Encode(map[string]string{"version": "mock"})
			return
		}
		if v, ok := routes[p]; ok {
			json.NewEncoder(w).Encode(v)
		} else if v, ok := arr[p]; ok {
			json.NewEncoder(w).Encode(v)
		} else {
			http.NotFound(w, r)
		}
	})
	log.Fatal(http.ListenAndServe(*addr, nil))
}

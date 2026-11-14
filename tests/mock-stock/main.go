// Command mock-stock pretends to be qBittorrent, Sonarr, Radarr and Lidarr with made-up items,
// so the dashboard can be looked at without real apps. Every app uses the same address.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:7890", "address (port 0 picks a free one)")
	downloads := flag.String("downloads", "/Users/me/Downloads", "download folder the UI fixture torrents report")
	nas := flag.String("library", "/Volumes/NAS/Media", "library folder the UI fixture import points into")
	uiOnly := flag.Bool("ui-fixtures", false, "serve only the c1-c5 headless UI fixture torrents (seed shapes from tests/mock-stock/fixture_ui.go)")
	emptyLibrary := flag.Bool("empty-library", false, "list no shows, films or artists, for a first-run empty state")
	flag.Parse()
	now := time.Now()
	ago := func(d int) string { return now.Add(-time.Duration(d) * 24 * time.Hour).Format(time.RFC3339) }
	ahead := func(d int) string { return now.Add(time.Duration(d) * 24 * time.Hour).Format(time.RFC3339) }
	unix := func(h float64) int64 { return now.Add(-time.Duration(h * float64(time.Hour))).Unix() }
	hashOf := func(short string) string { return strings.Repeat(short, 20) }
	var mu sync.Mutex
	paused := map[string]bool{}
	removed := map[string]bool{} // metadata-only removals, like qBittorrent's delete with files off
	ui := uiFixture{downloads: *downloads, library: *nas, now: now, paused: paused}
	done := func(hash, name string, size float64, added, finished float64) map[string]any {
		state := "stalledUP"
		if paused[hashOf(hash)] {
			state = "stoppedUP"
		}
		return map[string]any{"hash": hashOf(hash), "name": name, "progress": 1, "state": state, "size": size, "downloaded": size, "uploaded": size * 0.8,
			"ratio": 0.8, "added_on": unix(added), "completion_on": unix(finished), "num_seeds": 0, "num_leechs": 1, "save_path": "/Users/me/Downloads", "category": "tv-sonarr"}
	}
	routes := map[string]any{
		"/api/v2/app/preferences": map[string]any{"max_seeding_time_enabled": false, "max_seeding_time": -1, "max_ratio_act": 0},
		"/api/v2/sync/maindata":   map[string]any{"server_state": map[string]any{"free_space_on_disk": 412e9}},
	}
	arr := map[string]map[string]any{
		"/api/v3/history": {"records": []map[string]any{
			{"eventType": "downloadFolderImported", "downloadId": hashOf("A4"), "date": ago(3)},
			{"eventType": "downloadFolderImported", "downloadId": hashOf("A2"), "date": ago(1)},
		}},
		"/api/v1/history":               {"records": []map[string]any{{"eventType": "downloadImported", "downloadId": hashOf("A3"), "date": ago(6)}}},
		"/api/v3/queue":                 {"records": []map[string]any{{"downloadId": hashOf("A1")}}},
		"/api/v1/queue":                 {"records": []map[string]any{}},
		"/api/v3/config/downloadclient": {"id": 1, "enableCompletedDownloadHandling": true},
		"/api/v1/config/downloadclient": {"id": 1, "enableCompletedDownloadHandling": true},
	}
	cal := []map[string]any{{"title": "New Beginnings", "seasonNumber": 2, "episodeNumber": 5, "airDateUtc": ahead(3), "series": map[string]any{"title": "Sample Series"}}}
	media := map[string]any{
		"/api/v3/series": []map[string]any{
			{"id": 1, "title": "Sample Series", "year": 2023, "added": ago(40), "statistics": map[string]any{"episodeFileCount": 3}},
			{"id": 2, "title": "Another Show", "year": 2019, "added": ago(5)},
		},
		"/api/v3/series/lookup": []map[string]any{
			{"title": "Lookup Show", "year": 2025, "tvdbId": 11, "overview": "A made-up show for testing.", "added": "0001-01-01T00:00:00Z"},
			{"id": 1, "title": "Sample Series", "year": 2023},
		},
		"/api/v3/movie": []map[string]any{
			{"id": 1, "title": "Example Film", "year": 2024, "hasFile": true, "added": ago(1)},
			{"id": 2, "title": "A Quiet Film", "year": 1998, "hasFile": true, "added": ago(90)},
		},
		"/api/v3/movie/lookup":   []map[string]any{{"title": "Lookup Film", "year": 2021, "tmdbId": 22, "overview": "A made-up film for testing."}},
		"/api/v1/artist":         []map[string]any{{"id": 1, "artistName": "Demo Artist", "added": ago(6), "statistics": map[string]any{"trackFileCount": 10}}},
		"/api/v1/artist/lookup":  []map[string]any{{"artistName": "Lookup Artist", "foreignArtistId": "x", "overview": "A made-up artist for testing."}},
		"/api/v3/qualityprofile": []map[string]any{{"id": 1}}, "/api/v1/qualityprofile": []map[string]any{{"id": 1}},
		"/api/v3/rootfolder": []map[string]any{{"id": 1, "path": "/Volumes/NAS/Media"}}, "/api/v1/rootfolder": []map[string]any{{"id": 1, "path": "/Volumes/NAS/Music"}},
		"/api/v1/metadataprofile": []map[string]any{{"id": 1}},
	}
	tracking := newTrackingFixture(media, *nas)
	feature := newFeatureFixture(media)
	subtitles := newSubtitleFixture()
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		p := r.URL.Path
		app := ""
		for _, name := range []string{"sonarr", "radarr", "lidarr", "prowlarr", "bazarr"} {
			if strings.HasPrefix(p, "/"+name+"/") {
				app = name
				p = strings.TrimPrefix(p, "/"+name)
				r.URL.Path = p
				break
			}
		}
		if app == "bazarr" && subtitles(w, r) {
			return
		}
		if app != "" && strings.HasSuffix(p, "/queue") {
			records := []map[string]any{}
			switch app {
			case "sonarr":
				records = []map[string]any{{"id": 1, "downloadId": hashOf("A1"), "seriesId": 1}, {"id": 2, "downloadId": hashOf("A6"), "seriesId": 1}}
			case "radarr":
				records = []map[string]any{{"id": 3, "downloadId": hashOf("A7"), "movieId": 1}, {"id": 4, "downloadId": uiHash("c2"), "movieId": 2}}
			case "lidarr":
				records = []map[string]any{{"id": 5, "downloadId": hashOf("A8"), "artistId": 1}}
			}
			json.NewEncoder(w).Encode(map[string]any{"records": records, "totalRecords": len(records)})
			return
		}
		if app != "" && r.Method == "DELETE" && strings.Contains(p, "/queue/") {
			log.Printf("QUEUE DELETE %s", p)
			w.WriteHeader(200)
			return
		}
		if app != "" && strings.HasSuffix(p, "/history") {
			records := []map[string]any{}
			switch app {
			case "sonarr":
				records = []map[string]any{{"eventType": "downloadFolderImported", "downloadId": hashOf("A4"), "date": ago(3), "seriesId": 1,
					"data": map[string]any{"importedPath": "/Volumes/NAS/Media/Sample Series/Season 02/Sample Series S02E03 1080p.mkv"}}, ui.sonarrHistory()}
			case "radarr":
				records = []map[string]any{{"eventType": "downloadFolderImported", "downloadId": hashOf("A2"), "date": ago(1), "movieId": 1,
					"data": map[string]any{"importedPath": "/Volumes/NAS/Media/Example Film (2024)/Example Film (2024) 2160p.mkv"}}}
			case "lidarr":
				records = []map[string]any{{"eventType": "downloadImported", "downloadId": hashOf("A3"), "date": ago(6), "artistId": 1,
					"data": map[string]any{"importedPath": "/Volumes/NAS/Music/Demo Artist/First Album"}}}
			}
			json.NewEncoder(w).Encode(map[string]any{"records": records, "totalRecords": len(records)})
			return
		}
		if feature.handle(w, r, app) {
			return
		}
		if tracking(w, r) {
			return
		}
		if p == "/api/v2/torrents/stop" || p == "/api/v2/torrents/start" {
			for _, hash := range strings.Split(r.FormValue("hashes"), "|") {
				paused[hash] = strings.HasSuffix(p, "/stop")
			}
			return
		}
		for _, kind := range []string{"/api/v3/movie", "/api/v3/series", "/api/v1/artist"} {
			if strings.HasPrefix(p, kind+"/") {
				id, err := strconv.Atoi(strings.TrimPrefix(p, kind+"/"))
				if err != nil {
					continue
				}
				for i, item := range media[kind].([]map[string]any) {
					if item["id"] != id {
						continue
					}
					if r.Method == "PUT" {
						var changed map[string]any
						if json.NewDecoder(r.Body).Decode(&changed) != nil {
							http.Error(w, "bad item", 400)
							return
						}
						changed["id"] = id
						media[kind].([]map[string]any)[i] = changed
						item = changed
					}
					json.NewEncoder(w).Encode(item)
					return
				}
				http.NotFound(w, r)
				return
			}
		}
		if p == "/api/v2/torrents/files" {
			files, ok := ui.files(r.URL.Query().Get("hash"))
			if !ok {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(files)
			return
		}
		if p == "/api/v2/torrents/info" {
			if *emptyLibrary {
				json.NewEncoder(w).Encode([]any{}) // a first run has no downloads either
				return
			}
			// The one active download changes speed on every read, so the app's sampler has something to record.
			speed := rand.Int63n(30e6)
			if rand.Intn(5) == 0 {
				speed = 0
			}
			state := "downloading"
			if paused[hashOf("a1")] {
				state = "stoppedDL"
				speed = 0
			}
			base := []map[string]any{
				{"hash": hashOf("a1"), "name": "Sample Series S02E04 1080p", "progress": 0.62, "state": state, "size": 2.4e9, "downloaded": 1.49e9,
					"priority": 1, "dlspeed": speed, "upspeed": 120e3, "eta": 61, "added_on": unix(0.4), "num_seeds": 14, "num_leechs": 3, "save_path": "/Users/me/Downloads", "category": "tv-sonarr"},
				done("a2", "Example Film (2024) 2160p", 18e9, 30, 29.2),
				done("a3", "Demo Artist - First Album (FLAC)", 0.6e9, 150, 149.9),
				done("a4", "Sample Series S02E03 1080p", 2.3e9, 80, 79.7),
				done("a5", "home-videos-backup", 40e9, 300, 290),
				{"hash": hashOf("a6"), "name": "Sample Series S02E05 queued", "state": "queuedDL", "progress": 0, "priority": 2, "size": 2e9},
				{"hash": hashOf("a7"), "name": "Example Film paused", "state": "stoppedDL", "progress": 0.35, "priority": 3, "size": 4e9, "downloaded": 1.4e9},
				{"hash": hashOf("a8"), "name": "Demo Artist missing files", "state": "missingFiles", "progress": 0.4, "priority": 4, "size": 0.8e9},
			}
			if *uiOnly {
				base = nil // the headless UI sees only its own fixture rows
			}
			uiRows := []map[string]any{}
			for _, t := range ui.torrents() {
				if !removed[strings.ToUpper(t["hash"].(string))] {
					uiRows = append(uiRows, t)
				}
			}
			json.NewEncoder(w).Encode(append(base, uiRows...))
			return
		}
		if p == "/api/v2/torrents/delete" {
			for _, hash := range strings.Split(r.FormValue("hashes"), "|") {
				removed[strings.ToUpper(hash)] = true
			}
			return
		}
		if p == "/api/v2/torrents/add" {
			r.ParseMultipartForm(32 << 20)
			n := 0
			if r.MultipartForm != nil {
				n = len(r.MultipartForm.File["torrents"])
			}
			log.Printf("ADD TORRENTS links=%q files=%d", r.FormValue("urls"), n)
			w.Write([]byte("Ok."))
			return
		}
		if r.Method == "POST" && media[p] != nil {
			var item map[string]any
			if json.NewDecoder(r.Body).Decode(&item) != nil {
				http.Error(w, "bad item", 400)
				return
			}
			items := media[p].([]map[string]any)
			item["id"] = len(items) + 1
			item["added"] = time.Now().Format(time.RFC3339)
			media[p] = append(items, item)
			log.Printf("ADD %s", p)
			w.WriteHeader(201)
			return
		}
		if v, ok := media[p]; ok {
			// Searches return the made-up results only when the term contains "look".
			if strings.HasSuffix(p, "lookup") && !strings.Contains(strings.ToLower(r.URL.Query().Get("term")), "look") {
				v = []any{}
			}
			if *emptyLibrary && (p == "/api/v3/series" || p == "/api/v3/movie" || p == "/api/v1/artist") {
				v = []any{} // a first-run library: nothing added yet
			}
			json.NewEncoder(w).Encode(v)
			return
		}
		switch {
		case p == "/api/v2/app/setPreferences" || p == "/api/v2/torrents/delete" || r.Method == "PUT":
			log.Printf("%s %s %s", r.Method, p, r.FormValue("json"))
			return
		case strings.HasSuffix(p, "/downloadclient") && !strings.Contains(p, "/config/"):
			json.NewEncoder(w).Encode([]map[string]any{{"id": 1, "implementation": "QBittorrent", "removeCompletedDownloads": true}})
			return
		case strings.HasSuffix(p, "/calendar"):
			if *emptyLibrary {
				json.NewEncoder(w).Encode([]any{})
				return
			}
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
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	// scripts/test-ui.sh reads this exact line to learn the picked port.
	fmt.Printf("MOCK_STOCK=http://%s\n", ln.Addr())
	log.Fatal(http.Serve(ln, nil))
}

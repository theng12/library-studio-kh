package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Shared under main's mutex so browser checks can observe monitoring changes.
func newTrackingFixture(media map[string]any) func(http.ResponseWriter, *http.Request) bool {
	for _, kind := range []string{"/api/v3/series", "/api/v3/movie", "/api/v1/artist"} {
		for _, item := range media[kind].([]map[string]any) {
			item["monitored"] = true
			item["qualityProfileId"] = 1
			item["path"] = "/Volumes/NAS/Media/Fixture"
			if strings.HasSuffix(kind, "series") {
				item["seasons"] = []map[string]any{{"seasonNumber": 0, "monitored": false}, {"seasonNumber": 1, "monitored": true}}
			}
		}
	}
	for _, key := range []string{"/api/v3/qualityprofile", "/api/v1/qualityprofile", "/api/v1/metadataprofile"} {
		for _, item := range media[key].([]map[string]any) {
			item["name"] = "Fixture profile"
		}
	}
	episodes := []map[string]any{
		{"id": 11, "seriesId": 1, "seasonNumber": 0, "episodeNumber": 1, "title": "A special beginning", "monitored": false, "hasFile": false, "airDate": "2025-01-01", "airDateUtc": "2025-01-01T00:00:00Z"},
		{"id": 12, "seriesId": 1, "seasonNumber": 1, "episodeNumber": 1, "title": "First episode", "monitored": true, "hasFile": true, "airDate": "2025-02-01", "airDateUtc": "2025-02-01T00:00:00Z"},
		{"id": 13, "seriesId": 2, "seasonNumber": 1, "episodeNumber": 1, "title": "Another series episode", "monitored": true, "hasFile": false},
	}
	albums := []map[string]any{
		{"id": 21, "artistId": 1, "title": "First Album", "monitored": true, "releaseDate": "2025-01-01T00:00:00Z", "statistics": map[string]any{"trackCount": 10, "trackFileCount": 7}},
		{"id": 22, "artistId": 1, "title": "Next Album", "monitored": false, "releaseDate": "2027-01-01T00:00:00Z"},
	}
	return func(w http.ResponseWriter, r *http.Request) bool {
		p := r.URL.Path
		for _, group := range []struct {
			base, param, field, ids string
			items                   []map[string]any
		}{
			{"/api/v3/episode", "seriesId", "seriesId", "episodeIds", episodes},
			{"/api/v1/album", "artistId", "artistId", "albumIds", albums},
		} {
			if p == group.base+"/monitor" && r.Method == "PUT" {
				var req map[string]json.RawMessage
				if json.NewDecoder(r.Body).Decode(&req) != nil {
					http.Error(w, "bad monitoring request", 400)
					return true
				}
				var ids []int
				var monitored bool
				if json.Unmarshal(req[group.ids], &ids) != nil || json.Unmarshal(req["monitored"], &monitored) != nil {
					http.Error(w, "missing monitoring choice", 400)
					return true
				}
				for _, item := range group.items {
					for _, id := range ids {
						if item["id"] == id {
							item["monitored"] = monitored
						}
					}
				}
				w.WriteHeader(202)
				return true
			}
			if p == group.base {
				id, _ := strconv.Atoi(r.URL.Query().Get(group.param))
				childID, _ := strconv.Atoi(r.URL.Query().Get(group.ids))
				out := []map[string]any{}
				for _, item := range group.items {
					if (childID > 0 && item["id"] == childID) || (childID == 0 && item[group.field] == id) {
						out = append(out, item)
					}
				}
				json.NewEncoder(w).Encode(out)
				return true
			}
			if strings.HasPrefix(p, group.base+"/") {
				id, err := strconv.Atoi(strings.TrimPrefix(p, group.base+"/"))
				if err != nil {
					continue
				}
				for _, item := range group.items {
					if item["id"] == id {
						json.NewEncoder(w).Encode(item)
						return true
					}
				}
				http.NotFound(w, r)
				return true
			}
		}
		if p == "/api/v3/release" || p == "/api/v1/release" {
			if r.Method == "POST" {
				var item map[string]any
				if json.NewDecoder(r.Body).Decode(&item) != nil || item["guid"] != "fixture-eligible" {
					http.Error(w, "unsupported fixture release", 400)
					return true
				}
				w.WriteHeader(201)
				return true
			}
			json.NewEncoder(w).Encode([]map[string]any{
				{"guid": "fixture-eligible", "indexerId": 1, "indexer": "Fixture Archive", "title": "Permitted fixture release 1080p", "quality": map[string]any{"quality": map[string]any{"name": "HD-1080p"}}, "protocol": "torrent", "size": 600000000, "seeders": 12, "ageMinutes": 45, "approved": true, "rejected": false, "downloadAllowed": true, "rejections": []string{}, "downloadUrl": "https://fixture.invalid/private-value"},
				{"guid": "fixture-rejected", "indexerId": 2, "indexer": "Fixture Other", "title": "Rejected fixture release", "quality": map[string]any{"quality": map[string]any{"name": "SD"}}, "protocol": "torrent", "size": 200000000, "seeders": 0, "approved": false, "rejected": true, "downloadAllowed": false, "rejections": []string{"Quality below the selected profile"}},
			})
			return true
		}
		if p == "/api/v1/indexer" || p == "/api/v3/indexer" {
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "name": "Fixture Archive", "enable": true, "enableRss": true, "enableAutomaticSearch": true, "enableInteractiveSearch": true, "protocol": "torrent", "priority": 10, "fields": []map[string]any{{"name": "apiKey", "value": "private-value"}}},
				{"id": 2, "name": "Fixture Other", "enable": true, "enableRss": true, "enableAutomaticSearch": false, "enableInteractiveSearch": true, "protocol": "torrent", "priority": 20},
			})
			return true
		}
		if p == "/api/v1/indexerstatus" {
			json.NewEncoder(w).Encode([]map[string]any{{"indexerId": 2, "disabledTill": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}})
			return true
		}
		if p == "/api/v1/health" {
			json.NewEncoder(w).Encode([]map[string]any{{"type": "warning", "message": "One fixture source is temporarily unavailable"}})
			return true
		}
		if p == "/api/v1/applications" {
			json.NewEncoder(w).Encode([]map[string]any{{"id": 1, "name": "Sonarr", "syncLevel": "fullSync", "fields": []map[string]any{{"name": "apiKey", "value": "private-value"}}}, {"id": 2, "name": "Radarr", "syncLevel": "fullSync"}, {"id": 3, "name": "Lidarr", "syncLevel": "fullSync"}})
			return true
		}
		return false
	}
}

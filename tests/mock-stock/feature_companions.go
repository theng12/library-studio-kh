package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

type arrCompanionState struct {
	downloadConfig map[string]any
	clients        []map[string]any
	naming         map[string]any
}

func featureCompanionStates() map[string]*arrCompanionState {
	out := map[string]*arrCompanionState{}
	for _, app := range []string{"Sonarr", "Radarr", "Lidarr"} {
		naming := map[string]any{"id": 2, "replaceIllegalCharacters": true, "fixtureOnly": "keep"}
		switch app {
		case "Sonarr":
			naming["renameEpisodes"] = true
		case "Radarr":
			naming["renameMovies"] = true
		}
		out[strings.ToLower(app)] = &arrCompanionState{
			downloadConfig: map[string]any{"id": 1, "enableCompletedDownloadHandling": true, "autoRedownloadFailed": true},
			clients: []map[string]any{
				{"id": 9, "name": "qBittorrent Fixture", "implementation": "QBittorrent", "removeCompletedDownloads": true, "host": "fixture-qbit", "apiKey": "fixture-client-key", "enable": true},
				{"id": 10, "name": "Other Downloader Fixture", "implementation": "Other", "removeCompletedDownloads": true, "enable": true},
			},
			naming: naming,
		}
	}
	return out
}

func (f *featureFixture) handleCompanion(w http.ResponseWriter, r *http.Request, app string) bool {
	st := f.companions[app]
	if st == nil {
		return false
	}
	for _, version := range []string{"/api/v3", "/api/v1"} {
		switch {
		case r.URL.Path == version+"/config/downloadclient" && r.Method == "GET":
			writeFeatureJSON(w, st.downloadConfig)
			return true
		case r.URL.Path == version+"/downloadclient" && r.Method == "GET":
			writeFeatureJSON(w, st.clients)
			return true
		case r.URL.Path == version+"/config/naming" && r.Method == "GET":
			writeFeatureJSON(w, st.naming)
			return true
		case r.Method == "PUT" && strings.HasPrefix(r.URL.Path, version+"/config/downloadclient/"):
			id, ok := pathID(r.URL.Path, version+"/config/downloadclient/")
			if !ok {
				return false
			}
			item, ok := decodeFeatureObject(w, r)
			if !ok {
				return true
			}
			item["id"] = float64(id)
			st.downloadConfig = item
			w.WriteHeader(http.StatusAccepted)
			return true
		case r.Method == "PUT" && strings.HasPrefix(r.URL.Path, version+"/downloadclient/"):
			id, ok := pathID(r.URL.Path, version+"/downloadclient/")
			if !ok {
				return false
			}
			item, ok := decodeFeatureObject(w, r)
			if !ok {
				return true
			}
			if !replaceCompanionItem(st.clients, id, item) {
				http.NotFound(w, r)
				return true
			}
			w.WriteHeader(http.StatusAccepted)
			return true
		case r.Method == "PUT" && strings.HasPrefix(r.URL.Path, version+"/config/naming/"):
			id, ok := pathID(r.URL.Path, version+"/config/naming/")
			if !ok {
				return false
			}
			item, ok := decodeFeatureObject(w, r)
			if !ok {
				return true
			}
			item["id"] = float64(id)
			st.naming = item
			w.WriteHeader(http.StatusAccepted)
			return true
		}
	}
	return false
}

func replaceCompanionItem(items []map[string]any, id int, next map[string]any) bool {
	for i, item := range items {
		if featureInt(item["id"]) == id {
			next["id"] = float64(id)
			items[i] = next
			return true
		}
	}
	return false
}

func featureQBitPrefs() map[string]any {
	return map[string]any{
		"max_ratio_enabled": false, "max_ratio": 2.0, "max_ratio_act": 0,
		"max_seeding_time_enabled": true, "max_seeding_time": 2880.0,
		"dl_limit": 0.0, "up_limit": 0.0, "save_path": "/fixture/downloads",
		"queueing_enabled": true,
	}
}

func (f *featureFixture) handleQBit(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/api/v2/auth/login":
		if r.Method == "POST" {
			_, _ = w.Write([]byte("Ok."))
			return true
		}
	case "/api/v2/app/version":
		if r.Method == "GET" {
			_, _ = w.Write([]byte("v5.2.3"))
			return true
		}
	case "/api/v2/app/preferences":
		if r.Method == "GET" {
			writeFeatureJSON(w, f.qbitPrefs)
			return true
		}
	case "/api/v2/app/setPreferences":
		if r.Method == "POST" {
			_ = r.ParseForm()
			var patch map[string]any
			if json.Unmarshal([]byte(r.FormValue("json")), &patch) != nil {
				http.Error(w, "bad preferences", http.StatusBadRequest)
				return true
			}
			for key, value := range patch {
				f.qbitPrefs[key] = value
			}
			w.WriteHeader(http.StatusOK)
			return true
		}
	}
	return false
}

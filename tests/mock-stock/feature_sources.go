package main

import (
	"net/http"
	"strings"
)

// Fixture secrets are fake strings. They exist so update/test readback can prove
// the client sends a stored secret back unchanged.
func featureIndexerSchemas() []map[string]any {
	return []map[string]any{
		{
			"name": "Example Source", "definitionName": "example-usenet", "implementation": "Cardigann",
			"configContract": "CardigannSettings", "protocol": "usenet", "privacy": "private",
			"fields": []map[string]any{
				{"name": "baseUrl", "label": "Base URL", "type": "textbox", "value": "http://fixture.invalid"},
				{"name": "apiKey", "label": "API Key", "type": "textbox", "value": "fixture-saved-secret", "required": true},
				{"name": "username", "label": "Username", "type": "textbox", "value": "fixture-user"},
				{"name": "password", "label": "Password", "type": "password", "value": "fixture-password"},
				{"name": "categories", "label": "Categories", "type": "select", "required": true, "selectOptions": []map[string]any{{"value": 1, "label": "TV"}, {"value": 2, "label": "Movies"}}},
				{"name": "freeleech", "label": "Freeleech", "type": "checkbox", "value": false},
				{"name": "note", "label": "Read me", "type": "info", "value": "Fixture note"},
			},
		},
		{
			"name": "Example Torrents", "definitionName": "example-torrents", "implementation": "Cardigann",
			"configContract": "CardigannSettings", "protocol": "torrent", "privacy": "public",
			"fields": []map[string]any{
				{"name": "baseUrl", "label": "Base URL", "type": "textbox", "value": "http://fixture.invalid"},
				{"name": "username", "label": "Username", "type": "textbox"},
				{"name": "password", "label": "Password", "type": "password"},
				{"name": "freeleech", "label": "Freeleech", "type": "checkbox", "value": true},
			},
		},
	}
}

func featureIndexers() []map[string]any {
	return []map[string]any{
		{
			"id": 1, "name": "Example Source", "enable": true, "priority": 10,
			"implementation": "Cardigann", "definitionName": "example-usenet",
			"configContract": "CardigannSettings", "protocol": "usenet",
			"capabilities": map[string]any{"limitsMax": 100}, "added": "2026-01-01T00:00:00Z",
			"fields": []map[string]any{
				{"name": "baseUrl", "value": "http://fixture.invalid"},
				{"name": "apiKey", "value": "fixture-saved-secret"},
				{"name": "username", "value": "fixture-user"},
				{"name": "password", "value": "fixture-password"},
				{"name": "categories", "value": 1},
				{"name": "freeleech", "value": false},
				{"name": "nativeOnly", "value": "keep"},
			},
		},
		{
			"id": 2, "name": "Example Torrents", "enable": true, "priority": 20,
			"implementation": "Cardigann", "definitionName": "example-torrents",
			"configContract": "CardigannSettings", "protocol": "torrent",
			"added": "2026-01-02T00:00:00Z",
			"fields": []map[string]any{
				{"name": "baseUrl", "value": "http://fixture.invalid"},
				{"name": "username", "value": "fixture-torrent-user"},
				{"name": "password", "value": "fixture-torrent-password"},
				{"name": "freeleech", "value": true},
			},
		},
	}
}

func (f *featureFixture) handleProwlarr(w http.ResponseWriter, r *http.Request) bool {
	p := r.URL.Path
	switch p {
	case "/api/v1/indexer/schema":
		if r.Method == "GET" {
			writeFeatureJSON(w, f.schemas)
			return true
		}
	case "/api/v1/indexer":
		switch r.Method {
		case "GET":
			writeFeatureJSON(w, f.indexers)
			return true
		case "POST":
			item, ok := decodeFeatureObject(w, r)
			if !ok {
				return true
			}
			item["id"] = float64(f.nextIndexerID())
			f.indexers = append(f.indexers, item)
			writeFeatureJSON(w, item)
			return true
		}
	case "/api/v1/indexer/test":
		if r.Method == "POST" {
			w.WriteHeader(http.StatusOK)
			return true
		}
	}
	if strings.HasPrefix(p, "/api/v1/indexer/") {
		id, ok := pathID(p, "/api/v1/indexer/")
		if !ok {
			http.NotFound(w, r)
			return true
		}
		switch r.Method {
		case "GET":
			if item := f.indexer(id); item != nil {
				writeFeatureJSON(w, item)
				return true
			}
			http.NotFound(w, r)
			return true
		case "PUT":
			item, ok := decodeFeatureObject(w, r)
			if !ok {
				return true
			}
			if f.indexer(id) == nil {
				http.NotFound(w, r)
				return true
			}
			item["id"] = float64(id)
			f.replaceIndexer(id, item)
			w.WriteHeader(http.StatusAccepted)
			return true
		}
	}
	return false
}

func (f *featureFixture) indexer(id int) map[string]any {
	for _, item := range f.indexers {
		if featureInt(item["id"]) == id {
			return item
		}
	}
	return nil
}

func (f *featureFixture) replaceIndexer(id int, next map[string]any) {
	for i, item := range f.indexers {
		if featureInt(item["id"]) == id {
			f.indexers[i] = next
			return
		}
	}
}

func (f *featureFixture) nextIndexerID() int {
	max := 0
	for _, item := range f.indexers {
		if id := featureInt(item["id"]); id > max {
			max = id
		}
	}
	return max + 1
}

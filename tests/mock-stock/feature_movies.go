package main

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (f *featureFixture) handleRadarr(w http.ResponseWriter, r *http.Request) bool {
	p := r.URL.Path
	switch {
	case p == "/api/v3/collection" && r.Method == "GET":
		writeFeatureJSON(w, f.collection)
		return true
	case p == "/api/v3/movie/lookup" && r.Method == "GET":
		term := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("term")))
		if !strings.HasPrefix(term, "tmdb:") {
			return false // leave ordinary title searches to the default fixture
		}
		id, err := strconv.Atoi(strings.TrimPrefix(term, "tmdb:"))
		out := []map[string]any{}
		if err == nil {
			if item, ok := f.lookup[id]; ok {
				out = append(out, item)
			}
		}
		writeFeatureJSON(w, out)
		return true
	case strings.HasPrefix(p, "/MediaCover/") && r.Method == "GET":
		parts := strings.Split(strings.TrimPrefix(p, "/MediaCover/"), "/")
		if len(parts) != 2 {
			http.NotFound(w, r)
			return true
		}
		id, err := strconv.Atoi(parts[0])
		name := strings.ToLower(parts[1])
		if err != nil || !f.hasMovie(id) || (name != "poster.jpg" && name != "fanart.jpg") {
			http.NotFound(w, r)
			return true
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Length", strconv.Itoa(len(f.poster)))
		_, _ = w.Write(f.poster)
		return true
	}
	return false
}

func (f *featureFixture) hasMovie(id int) bool {
	for _, m := range f.movies {
		if featureInt(m["id"]) == id {
			return true
		}
	}
	return false
}

// enrichMovies makes the existing two films realistic Radarr objects and adds a
// third one that is in the library without a file. The collection then has one
// of each state: owned, in library but no file, and missing.
func (f *featureFixture) enrichMovies() {
	for _, m := range f.movies {
		switch featureInt(m["id"]) {
		case 1:
			mergeFacts(m, map[string]any{
				"tmdbId": 1001, "imdbId": "tt9900001", "studio": "Fixture Pictures",
				"certification": "PG-13", "runtime": 128, "status": "released",
				"genres": []string{"Adventure", "Science Fiction"},
				"images": []map[string]any{{"coverType": "poster"}},
				"ratings": map[string]any{
					"imdb":           map[string]any{"value": 7.4, "votes": 152000},
					"tmdb":           map[string]any{"value": 7.1, "votes": 890},
					"rottenTomatoes": map[string]any{"value": 6.8, "votes": 0},
				},
				"collection": map[string]any{"title": "Example Collection", "tmdbId": 9001},
				"overview":   "A made-up film used only to exercise the mock stock apps.",
			})
		case 2:
			mergeFacts(m, map[string]any{
				"tmdbId": 1002, "imdbId": "tt9900002", "studio": "Fixture Indie",
				"certification": "R", "runtime": 96, "status": "released",
				"genres": []string{"Drama"},
				"images": []map[string]any{{"coverType": "fanart"}},
				"ratings": map[string]any{
					"imdb": map[string]any{"value": 7.9, "votes": 21000},
					"tmdb": map[string]any{"value": 7.5, "votes": 140},
				},
				"overview": "A quiet made-up film used only to exercise the mock stock apps.",
			})
		}
	}
	f.movies = append(f.movies, map[string]any{
		"id": 3, "title": "Example Film: Next Chapter", "year": 2026, "hasFile": false,
		"added":     time.Now().Add(-10 * 24 * time.Hour).Format(time.RFC3339),
		"monitored": true, "qualityProfileId": 1, "path": "/Volumes/NAS/Media/Fixture",
		"overview": "The made-up sequel, in the library but without a file.",
		"tmdbId":   1003, "imdbId": "tt9900003", "studio": "Fixture Pictures",
		"certification": "PG-13", "runtime": 124, "status": "released",
		"genres": []string{"Adventure"},
		"images": []map[string]any{{"coverType": "poster"}},
		"ratings": map[string]any{
			"imdb": map[string]any{"value": 6.6, "votes": 4200},
			"tmdb": map[string]any{"value": 6.8, "votes": 170},
		},
		"collection": map[string]any{"title": "Example Collection", "tmdbId": 9001},
	})
}

func mergeFacts(target, facts map[string]any) {
	for key, value := range facts {
		target[key] = value
	}
}

func featureCollection() []map[string]any {
	return []map[string]any{{
		"id": 10, "tmdbId": 9001, "title": "Example Collection",
		"overview":  "A made-up collection used to exercise owned, no-file and missing states.",
		"monitored": true, "missingMovies": 1,
		"movies": []map[string]any{
			{"tmdbId": 1001, "title": "Example Film", "year": 2024, "status": "released", "runtime": 128, "isExisting": true, "isExcluded": false, "digitalRelease": "2024-05-01T00:00:00Z"},
			{"tmdbId": 1003, "title": "Example Film: Next Chapter", "year": 2026, "status": "released", "runtime": 124, "isExisting": true, "isExcluded": false, "digitalRelease": "2026-08-01T00:00:00Z"},
			{"tmdbId": 1004, "title": "Example Film: Finale", "year": 2028, "status": "announced", "isExisting": false, "isExcluded": true},
		},
	}}
}

func featureLookupMovies() map[int]map[string]any {
	facts := func(title string, year, tmdbID int, imdbID, status, overview string) map[string]any {
		return map[string]any{
			"title": title, "year": year, "tmdbId": tmdbID, "status": status,
			"imdbId": imdbID, "studio": "Fixture Pictures",
			"overview": overview,
			"images":   []map[string]any{{"coverType": "poster"}},
		}
	}
	return map[int]map[string]any{
		1001: facts("Example Film", 2024, 1001, "tt9900001", "released", "A made-up film used only to exercise the mock stock apps."),
		1002: facts("A Quiet Film", 1998, 1002, "tt9900002", "released", "A quiet made-up film used only to exercise the mock stock apps."),
		1003: facts("Example Film: Next Chapter", 2026, 1003, "tt9900003", "released", "The made-up sequel, in the library but without a file."),
		1004: facts("Example Film: Finale", 2028, 1004, "tt9900004", "announced", "The made-up third film, not yet in the library."),
	}
}

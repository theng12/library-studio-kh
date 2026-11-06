package main

import (
	"encoding/json"
	"net/http"
	"strconv"
)

func newSubtitleFixture() func(http.ResponseWriter, *http.Request) bool {
	english := map[string]any{"name": "English", "code2": "en", "forced": false, "hi": false}
	episodes := []map[string]any{
		{"sonarrSeriesId": 1, "sonarrEpisodeId": 11, "season": 0, "episode": 1, "title": "A special beginning", "profileId": 1, "path": "", "missing_subtitles": []any{}, "subtitles": []any{}},
		{"sonarrSeriesId": 1, "sonarrEpisodeId": 12, "season": 1, "episode": 1, "title": "First episode", "profileId": 1, "path": "/private/fixture/episode.mkv", "audio_language": map[string]string{"name": "French"}, "missing_subtitles": []any{english}, "subtitles": []any{}},
	}
	movie := map[string]any{"radarrId": 2, "title": "A Quiet Film", "profileId": 1, "path": "/private/fixture/movie.mkv", "audio_language": map[string]string{"name": "English"}, "missing_subtitles": []any{english}, "subtitles": []any{}}
	return func(w http.ResponseWriter, r *http.Request) bool {
		out := func(v any) { json.NewEncoder(w).Encode(v) }
		switch r.URL.Path {
		case "/api/system/status":
			out(map[string]any{"data": map[string]string{"bazarr_version": "1.6.2"}})
		case "/api/system/languages/profiles":
			out([]map[string]any{{"profileId": 1, "name": "English", "items": []map[string]string{{"language": "en"}}}})
		case "/api/system/settings":
			out(map[string]any{"general": map[string]any{"enabled_providers": []string{"fixture-provider"}}, "auth": map[string]string{"apikey": "fixture-private-config-value"}})
		case "/api/episodes":
			id, _ := strconv.Atoi(r.URL.Query().Get("seriesid[]"))
			items := []map[string]any{}
			for _, e := range episodes {
				if e["sonarrSeriesId"] == id {
					items = append(items, e)
				}
			}
			out(map[string]any{"data": items})
		case "/api/movies":
			if r.Method == "POST" {
				r.ParseForm()
				if r.FormValue("radarrid") != "2" {
					http.Error(w, "unknown movie", 400)
				} else {
					movie["profileId"] = 1
					w.WriteHeader(204)
				}
				return true
			}
			id, _ := strconv.Atoi(r.URL.Query().Get("radarrid[]"))
			items := []map[string]any{}
			if id == 2 {
				items = append(items, movie)
			}
			out(map[string]any{"data": items, "total": len(items)})
		case "/api/series":
			r.ParseForm()
			if r.Method != "POST" || r.FormValue("seriesid") != "1" {
				http.Error(w, "unknown series", 400)
			} else {
				for _, e := range episodes {
					e["profileId"] = 1
				}
				w.WriteHeader(204)
			}
		case "/api/movies/subtitles", "/api/episodes/subtitles":
			r.ParseForm()
			if r.Method != "PATCH" || r.FormValue("language") != "en" {
				http.Error(w, "bad subtitle request", 400)
				return true
			}
			if r.URL.Path == "/api/movies/subtitles" {
				if r.FormValue("radarrid") != "2" {
					http.Error(w, "unknown movie", 400)
					return true
				}
				movie["subtitles"] = []any{english}
				movie["missing_subtitles"] = []any{}
			} else {
				if r.FormValue("seriesid") != "1" || r.FormValue("episodeid") != "12" {
					http.Error(w, "unknown episode", 400)
					return true
				}
				episodes[1]["subtitles"] = []any{english}
				episodes[1]["missing_subtitles"] = []any{}
			}
			w.WriteHeader(204)
		default:
			return false
		}
		return true
	}
}

package stock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func bazarrTestClient(t *testing.T, h http.HandlerFunc) *Bazarr {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewBazarr(srv.URL, "test-key")
}

func writeBody(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(body))
}

func TestBazarrAllowlistedFieldsOnly(t *testing.T) {
	movie := `{"data":[{"radarrId":42,"title":"Example","profileId":1,"path":"/private/movie.mkv",
		"audio_language":{"name":"English"},
		"missing_subtitles":[{"name":"English","code2":"en","code3":"eng","forced":false,"hi":false}],
		"subtitles":[{"name":"English","code2":"en","code3":"eng","path":"/private/movie.en.srt","forced":false,"hi":false,"embedded_track_id":null,"id":17}],
		"apiKey":"leaked-key","providerUrl":"http://provider.example/secret"}],"total":1}`
	b := bazarrTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/system/status":
			writeBody(w, `{"data":{"bazarr_version":"1.6.2","radarr_version":"5.0.0","bazarr_directory":"/private/config"}}`)
		case "/api/system/languages/profiles":
			writeBody(w, `[{"profileId":1,"name":"English","items":[{"language":"en","hi":false}],"mustContain":["leaked-key"],"url":"http://provider.example"}]`)
		case "/api/movies":
			writeBody(w, movie)
		default:
			http.NotFound(w, r)
		}
	})

	version, err := b.Status()
	if err != nil || version != "1.6.2" {
		t.Fatalf("Status() = %q, %v", version, err)
	}
	profiles, err := b.Profiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 || profiles[0].ID != 1 || len(profiles[0].Languages) != 1 || profiles[0].Languages[0] != "en" {
		t.Fatalf("Profiles() = %+v", profiles)
	}
	profileJSON, _ := json.Marshal(profiles)
	if strings.Contains(string(profileJSON), "mustContain") || strings.Contains(string(profileJSON), "http") {
		t.Fatalf("profile leaked fields: %s", profileJSON)
	}

	items, err := b.Subtitles(AppRadarr, 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("Subtitles() = %+v", items)
	}
	m := items[0]
	if m.App != AppRadarr || m.ParentID != 42 || m.ID != 42 || m.ProfileID != 1 || !m.FileReported {
		t.Fatalf("unexpected media: %+v", m)
	}
	if len(m.Existing) != 1 || m.Existing[0].Code != "en" || len(m.Missing) != 1 || m.Missing[0].Code != "en" {
		t.Fatalf("unexpected languages: %+v", m)
	}
	if m.AudioLanguage == nil || *m.AudioLanguage != "English" {
		t.Fatalf("audio language = %v", m.AudioLanguage)
	}
	out, _ := json.Marshal(m)
	for _, forbidden := range []string{"/private", "provider.example", "leaked-key", `"path"`, `"radarrId"`} {
		if strings.Contains(string(out), forbidden) {
			t.Fatalf("media leaked %q: %s", forbidden, out)
		}
	}
	if !strings.Contains(string(out), `"fileReported":true`) {
		t.Fatalf("media did not expose fileReported: %s", out)
	}
}

func TestBazarrRefusesCrossParentAndDoesNotSearch(t *testing.T) {
	var patches atomic.Int32
	b := bazarrTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patches.Add(1)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeBody(w, `{"data":[{"radarrId":99,"title":"Wrong","profileId":1,"path":"/movies/wrong.mkv","subtitles":[]}]}`)
	})

	if _, err := b.Subtitles(AppRadarr, 42); err == nil {
		t.Fatal("Subtitles accepted another movie")
	}
	if err := b.EnglishSearch(AppRadarr, 42, 42); err == nil {
		t.Fatal("EnglishSearch accepted another movie")
	}
	if patches.Load() != 0 {
		t.Fatalf("sent %d PATCH requests", patches.Load())
	}
}

func TestBazarrEnglishSearchSchedulesMovieAndEpisode(t *testing.T) {
	cases := []struct {
		name, app, getPath, patchPath, body string
		parent, item                        int
		want                                map[string]string
	}{
		{
			name: "movie", app: AppRadarr, parent: 42, item: 42,
			getPath:   "/api/movies",
			patchPath: "/api/movies/subtitles",
			body:      `{"data":[{"radarrId":42,"title":"Example","profileId":1,"path":"/movies/example.mkv","subtitles":[]}]}`,
			want:      map[string]string{"radarrid": "42", "language": "en", "forced": "false", "hi": "false"},
		},
		{
			name: "episode", app: AppSonarr, parent: 7, item: 101,
			getPath:   "/api/episodes",
			patchPath: "/api/episodes/subtitles",
			body:      `{"data":[{"sonarrSeriesId":7,"sonarrEpisodeId":101,"title":"Episode","profileId":1,"path":"/tv/example.mkv","subtitles":[]}]}`,
			want:      map[string]string{"seriesid": "7", "episodeid": "101", "language": "en", "forced": "false", "hi": "false"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var patches atomic.Int32
			b := bazarrTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == tc.getPath {
					writeBody(w, tc.body)
					return
				}
				if r.URL.Path != tc.patchPath || r.Method != http.MethodPatch {
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
					return
				}
				patches.Add(1)
				if r.Header.Get("X-API-KEY") != "test-key" {
					t.Errorf("API key header = %q", r.Header.Get("X-API-KEY"))
				}
				if err := r.ParseForm(); err != nil {
					t.Errorf("parse form: %v", err)
				}
				for key, value := range tc.want {
					if got := r.Form.Get(key); got != value {
						t.Errorf("form %s = %q, want %q", key, got, value)
					}
				}
				for _, key := range []string{"provider", "subtitle", "url"} {
					if r.Form.Has(key) {
						t.Errorf("unexpected form field %s", key)
					}
				}
				w.WriteHeader(http.StatusNoContent)
			})
			if err := b.EnglishSearch(tc.app, tc.parent, tc.item); err != nil {
				t.Fatal(err)
			}
			if patches.Load() != 1 {
				t.Fatalf("sent %d PATCH requests", patches.Load())
			}
		})
	}
}

func TestBazarrEnglishSearchRefusesMissingFileOrProfile(t *testing.T) {
	cases := []struct {
		name, app, body string
		parent, item    int
	}{
		{"missing file", AppRadarr, `{"data":[{"radarrId":42,"profileId":1,"path":"","subtitles":[]}]}`, 42, 42},
		{"missing profile", AppRadarr, `{"data":[{"radarrId":42,"profileId":0,"path":"/movies/a.mkv","subtitles":[]}]}`, 42, 42},
		{"unknown item", AppRadarr, `{"data":[{"radarrId":42,"profileId":1,"path":"/movies/a.mkv","subtitles":[]}]}`, 42, 43},
		{"unknown app", "lidarr", `{"data":[]}`, 42, 42},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var patches atomic.Int32
			b := bazarrTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPatch {
					patches.Add(1)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				writeBody(w, tc.body)
			})
			if err := b.EnglishSearch(tc.app, tc.parent, tc.item); err == nil {
				t.Fatal("EnglishSearch accepted an ineligible item")
			}
			if patches.Load() != 0 {
				t.Fatalf("sent %d PATCH requests", patches.Load())
			}
		})
	}
}

func TestBazarrErrorsDoNotLeakSecrets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		writeBody(w, `{"error":"http://provider.example/api?apikey=top-secret"}`)
	}))
	b := NewBazarr(srv.URL, "test-key")
	_, err := b.Status()
	if err == nil {
		t.Fatal("Status accepted HTTP 500")
	}
	for _, secret := range []string{"top-secret", "test-key", srv.URL, "provider.example", "/system/status"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked %q: %v", secret, err)
		}
	}
	srv.Close()
	_, err = b.Status()
	if err == nil || !strings.Contains(err.Error(), "not reachable") {
		t.Fatalf("unreachable error = %v", err)
	}
	if strings.Contains(err.Error(), srv.URL) || strings.Contains(err.Error(), "test-key") {
		t.Fatalf("unreachable error leaked: %v", err)
	}
}

func TestBazarrAssignEnglishProfile(t *testing.T) {
	var posts atomic.Int32
	b := bazarrTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/system/languages/profiles":
			writeBody(w, `[{"profileId":1,"name":"English","items":[{"language":"en"}]},{"profileId":2,"name":"Mixed","items":[{"language":"en"},{"language":"fr"}]}]`)
		case "/api/movies":
			if r.Method != http.MethodPost {
				t.Errorf("method = %s", r.Method)
			}
			posts.Add(1)
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse form: %v", err)
			}
			if r.Form.Get("radarrid") != "42" || r.Form.Get("profileid") != "1" {
				t.Errorf("form = %v", r.Form)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	if err := b.AssignEnglishProfile(AppRadarr, 42, 2); err == nil {
		t.Fatal("accepted a mixed-language profile")
	}
	if posts.Load() != 0 {
		t.Fatalf("assigned after refusal: %d POSTs", posts.Load())
	}
	if err := b.AssignEnglishProfile(AppRadarr, 42, 1); err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 1 {
		t.Fatalf("POSTs = %d", posts.Load())
	}
}

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"librarystudio/internal/stock"
)

func featureTestMedia() map[string]any {
	return map[string]any{"/api/v3/movie": []map[string]any{
		{"id": 1, "title": "Example Film", "year": 2024, "hasFile": true},
		{"id": 2, "title": "A Quiet Film", "year": 1998, "hasFile": true},
	}}
}

// featureTestServer adds the plain Radarr movie list/detail routes that the real
// mock keeps in main.go, then delegates every feature route to the fixture.
func featureTestServer(t *testing.T, f *featureFixture, app string, media map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.handle(w, r, app) {
			return
		}
		if app != "radarr" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/api/v3/movie" && r.Method == "GET" {
			writeFeatureJSON(w, media["/api/v3/movie"])
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v3/movie/") {
			id, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/api/v3/movie/"))
			if err == nil {
				for _, movie := range media["/api/v3/movie"].([]map[string]any) {
					if featureInt(movie["id"]) == id {
						writeFeatureJSON(w, movie)
						return
					}
				}
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFeatureRadarrExtrasAndArtwork(t *testing.T) {
	media := featureTestMedia()
	f := newFeatureFixture(media)
	srv := featureTestServer(t, f, "radarr", media)
	a := stock.NewArr("Radarr", srv.URL, "fixture-key")

	x, err := a.MovieExtras(1)
	if err != nil {
		t.Fatal(err)
	}
	if x.Title != "Example Film" || x.TMDBID != 1001 || x.IMDbID != "tt9900001" || x.Studio != "Fixture Pictures" {
		t.Fatalf("film facts wrong: %+v", x)
	}
	if x.RuntimeMinutes == nil || *x.RuntimeMinutes != 128 || !x.Poster || len(x.Genres) != 2 || len(x.Ratings) != 3 {
		t.Fatalf("reported extras wrong: %+v", x)
	}
	c := x.Collection
	if c == nil || c.Title != "Example Collection" || len(c.Members) != 3 || !x.CatalogKnown {
		t.Fatalf("collection wrong: %+v", c)
	}
	if c.OwnedCount != 1 || c.NoFileCount != 1 || c.Missing != 1 {
		t.Fatalf("collection counts wrong: owned=%d nofile=%d missing=%d", c.OwnedCount, c.NoFileCount, c.Missing)
	}
	states := map[int]string{}
	for _, m := range c.Members {
		switch {
		case m.InLibrary && m.HaveFile != nil && *m.HaveFile:
			states[m.TMDBID] = "owned"
		case m.InLibrary:
			states[m.TMDBID] = "nofile"
		default:
			states[m.TMDBID] = "missing"
		}
	}
	if states[1001] != "owned" || states[1003] != "nofile" || states[1004] != "missing" {
		t.Fatalf("member states wrong: %v", states)
	}

	body, ct, err := a.Artwork(1, "poster.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if ct != "image/png" || !bytes.HasPrefix(body, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("artwork must be a local PNG: type=%q bytes=%d", ct, len(body))
	}
	if _, _, err := a.Artwork(999, "poster.jpg"); err == nil {
		t.Fatal("unknown film artwork must 404")
	}
}

func TestFeatureRadarrTMDbLookupExact(t *testing.T) {
	media := featureTestMedia()
	f := newFeatureFixture(media)
	srv := featureTestServer(t, f, "radarr", media)
	a := stock.NewArr("Radarr", srv.URL, "fixture-key")

	got, err := a.Lookup("tmdb:1004")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Example Film: Finale" || got[0].Year != 2028 || got[0].InLibrary || len(got[0].Raw) == 0 {
		t.Fatalf("exact tmdb lookup wrong: %+v", got)
	}
	got, err = a.Lookup("tmdb:999999")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("unknown tmdb id must be empty: %+v", got)
	}
}

func TestFeatureProwlarrSourcesAndWrites(t *testing.T) {
	media := featureTestMedia()
	f := newFeatureFixture(media)
	srv := featureTestServer(t, f, "prowlarr", media)
	p := stock.NewArr("Prowlarr", srv.URL, "fixture-prowlarr-key")

	indexers, err := p.Indexers()
	if err != nil || len(indexers) != 2 || indexers[0].ID != 1 {
		t.Fatal(err, indexers)
	}
	catalog, err := p.IndexerCatalog()
	if err != nil || len(catalog) != 2 || catalog[0].Key != "definition:example-usenet" || catalog[0].Protocol != "usenet" {
		t.Fatal(err, catalog)
	}
	edit, err := p.IndexerForEdit(1)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range edit.Fields {
		if field.Name == "apiKey" && (field.Value != nil || !field.HasValue || !field.Secret) {
			t.Fatalf("saved secret must stay hidden but known: %+v", field)
		}
	}

	if err := p.UpdateIndexer(stock.SourceInput{ID: 1, Name: "Renamed Fixture", Fields: map[string]any{
		"apiKey": "", "username": "fixture-updated",
	}}); err != nil {
		t.Fatal(err)
	}
	raw := getFeatureJSON(t, srv.URL+"/api/v1/indexer/1")
	if raw["name"] != "Renamed Fixture" {
		t.Fatalf("name not saved: %v", raw)
	}
	if featureFieldValue(raw, "apiKey") != "fixture-saved-secret" || featureFieldValue(raw, "username") != "fixture-updated" {
		t.Fatalf("saved secret must survive a blank update: %v", raw["fields"])
	}
	if featureFieldValue(raw, "nativeOnly") != "keep" {
		t.Fatalf("native field dropped: %v", raw["fields"])
	}

	id, err := p.CreateIndexer(stock.SourceInput{Key: "definition:example-usenet", Name: "Added Fixture",
		Fields: map[string]any{"apiKey": "fixture-new-secret", "username": "new-user", "categories": float64(2), "freeleech": true}})
	if err != nil || id <= 0 {
		t.Fatal(err, id)
	}
	indexers, err = p.Indexers()
	if err != nil || len(indexers) != 3 {
		t.Fatal(err, indexers)
	}
	if err := p.TestIndexer(stock.SourceInput{ID: id, Fields: map[string]any{"apiKey": "", "username": "tester"}}); err != nil {
		t.Fatal(err)
	}
}

func TestFeatureCompanionSettingsReadback(t *testing.T) {
	media := featureTestMedia()
	f := newFeatureFixture(media)

	srv := featureTestServer(t, f, "sonarr", media)
	s := stock.NewArr("Sonarr", srv.URL, "fixture-key")
	got, err := s.CompanionSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Handling || got.HandlingID != 1 || got.RenameEpisodes == nil || !*got.RenameEpisodes || got.NamingID != 2 {
		t.Fatalf("sonarr settings: %+v", got)
	}
	if len(got.Clients) != 2 || got.Clients[0].ID != 9 || !got.Clients[0].RemoveCompletedDownloads {
		t.Fatalf("sonarr clients: %+v", got.Clients)
	}
	if err := s.SetCompletedDownloadHandling(false); err != nil {
		t.Fatal(err)
	}
	got, err = s.CompanionSettings()
	if err != nil || got.Handling {
		t.Fatalf("handling readback: %+v %v", got, err)
	}
	if err := s.DisableRemoveCompletedDownloads(9); err != nil {
		t.Fatal(err)
	}
	got, err = s.CompanionSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.Clients[0].RemoveCompletedDownloads || !got.Clients[1].RemoveCompletedDownloads || got.Clients[0].Name != "qBittorrent Fixture" {
		t.Fatalf("client readback: %+v", got.Clients)
	}
	if err := s.SetRename("renameEpisodes", false); err != nil {
		t.Fatal(err)
	}
	got, err = s.CompanionSettings()
	if err != nil || got.RenameEpisodes == nil || *got.RenameEpisodes {
		t.Fatalf("rename readback: %+v %v", got, err)
	}

	rsrv := featureTestServer(t, f, "radarr", media)
	r := stock.NewArr("Radarr", rsrv.URL, "fixture-key")
	rgot, err := r.CompanionSettings()
	if err != nil || rgot.RenameMovies == nil || !*rgot.RenameMovies || rgot.RenameEpisodes != nil {
		t.Fatalf("radarr naming: %+v %v", rgot, err)
	}
	if err := r.SetRename("renameMovies", false); err != nil {
		t.Fatal(err)
	}
	rgot, err = r.CompanionSettings()
	if err != nil || rgot.RenameMovies == nil || *rgot.RenameMovies {
		t.Fatalf("radarr rename readback: %+v %v", rgot, err)
	}

	lsrv := featureTestServer(t, f, "lidarr", media)
	l := stock.NewArr("Lidarr", lsrv.URL, "fixture-key")
	lgot, err := l.CompanionSettings()
	if err != nil || !lgot.Handling || len(lgot.Clients) != 2 {
		t.Fatalf("lidarr v1 readback: %+v %v", lgot, err)
	}
}

func TestFeatureQBitPreferencesReadback(t *testing.T) {
	media := featureTestMedia()
	f := newFeatureFixture(media)
	srv := featureTestServer(t, f, "", media)
	q := stock.NewQBit(srv.URL, "fixture", "fixture")

	got, err := q.Seeding()
	if err != nil {
		t.Fatal(err)
	}
	if got.MaxRatio == nil || *got.MaxRatio != 2 || got.MaxSeedingTime == nil || *got.MaxSeedingTime != 2880 ||
		got.MaxSeedingTimeEnabled == nil || !*got.MaxSeedingTimeEnabled || got.MaxRatioAct == nil || *got.MaxRatioAct != 0 {
		t.Fatalf("seeding readback: %+v", got)
	}
	ratio, minutes := 3.0, 1440
	if err := q.ApplySeedingPatch(stock.SeedingPatch{MaxRatio: &ratio, MaxSeedingTime: &minutes}); err != nil {
		t.Fatal(err)
	}
	got, err = q.Seeding()
	if err != nil || *got.MaxRatio != 3 || *got.MaxSeedingTime != 1440 {
		t.Fatalf("seeding after patch: %+v %v", got, err)
	}
	prefs := getFeatureJSON(t, srv.URL+"/api/v2/app/preferences")
	if prefs["dl_limit"] != 0.0 || prefs["up_limit"] != 0.0 || prefs["save_path"] != "/fixture/downloads" || prefs["queueing_enabled"] != true {
		t.Fatalf("unrelated qBittorrent preferences were not preserved: %v", prefs)
	}
}

func getFeatureJSON(t *testing.T, url string) map[string]any {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %s", url, resp.Status)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func featureFieldValue(raw map[string]any, name string) any {
	fields, _ := raw["fields"].([]any)
	for _, x := range fields {
		field, _ := x.(map[string]any)
		if field["name"] == name {
			return field["value"]
		}
	}
	return nil
}

package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strconv"
	"strings"
)

// featureFixture serves the movie extras, source management and companion
// settings contracts added in this batch. All state is in memory; it never
// touches a real app, file or network.
type featureFixture struct {
	movies     []map[string]any
	lookup     map[int]map[string]any
	collection []map[string]any
	poster     []byte
	indexers   []map[string]any
	schemas    []map[string]any
	companions map[string]*arrCompanionState
	qbitPrefs  map[string]any
}

// newFeatureFixture enriches the existing fake Radarr films and adds the extra
// state the feature routes read. It runs after tracking so its film facts win.
func newFeatureFixture(media map[string]any) *featureFixture {
	movies, _ := media["/api/v3/movie"].([]map[string]any)
	f := &featureFixture{
		movies:     movies,
		lookup:     featureLookupMovies(),
		collection: featureCollection(),
		poster:     tinyPNG(),
		indexers:   featureIndexers(),
		schemas:    featureIndexerSchemas(),
		companions: featureCompanionStates(),
		qbitPrefs:  featureQBitPrefs(),
	}
	f.enrichMovies()
	media["/api/v3/movie"] = f.movies
	return f
}

// handle returns true when it answered the request. It must run before tracking
// so the source schema, indexer writes and companion writes win over the
// read-only tracking fixtures.
func (f *featureFixture) handle(w http.ResponseWriter, r *http.Request, app string) bool {
	switch app {
	case "prowlarr":
		return f.handleProwlarr(w, r)
	case "sonarr", "radarr", "lidarr":
		if f.handleCompanion(w, r, app) {
			return true
		}
		if app == "radarr" {
			return f.handleRadarr(w, r)
		}
	case "":
		return f.handleQBit(w, r)
	}
	return false
}

func writeFeatureJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func decodeFeatureObject(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	var item map[string]any
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil || item == nil {
		http.Error(w, "bad item", http.StatusBadRequest)
		return nil, false
	}
	return item, true
}

func featureInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}

func pathID(path, prefix string) (int, bool) {
	id, err := strconv.Atoi(strings.TrimPrefix(path, prefix))
	return id, err == nil && id > 0
}

func tinyPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 32, G: 120, B: 200, A: 255})
	img.Set(1, 0, color.RGBA{R: 240, G: 190, B: 60, A: 255})
	img.Set(0, 1, color.RGBA{R: 240, G: 190, B: 60, A: 255})
	img.Set(1, 1, color.RGBA{R: 32, G: 120, B: 200, A: 255})
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

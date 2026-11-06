package stock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewLidarrRootUsesLiveProfileChoicesAndReadsThemBack(t *testing.T) {
	var saved map[string]any
	posts := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/qualityprofile":
			json.NewEncoder(w).Encode([]map[string]any{{"id": 11, "name": "Actual quality"}})
		case "/api/v1/metadataprofile":
			json.NewEncoder(w).Encode([]map[string]any{{"id": 7, "name": "Actual metadata"}})
		case "/api/v1/rootfolder":
			if r.Method == "POST" {
				posts++
				json.NewDecoder(r.Body).Decode(&saved)
				saved["id"] = 1
				saved["accessible"] = true
				w.WriteHeader(201)
				return
			}
			if saved == nil {
				json.NewEncoder(w).Encode([]any{})
			} else {
				json.NewEncoder(w).Encode([]any{saved})
			}
		default:
			t.Errorf("unexpected native endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	a := NewArr("Lidarr", s.URL, "fixture")
	if _, err := a.EnsureRootFolder(RootFolderInput{Path: "/selected/Music"}); err == nil || posts != 0 {
		t.Fatal("missing profiles caused a root write")
	}
	result, err := a.EnsureRootFolder(RootFolderInput{Path: "/selected/Music", QualityProfileID: 11, MetadataProfileID: 7})
	if err != nil || result.Readback != CheckPassed || posts != 1 {
		t.Fatal("new music root did not pass native readback")
	}
	if saved["name"] != "Music" || saved["defaultQualityProfileId"] != float64(11) || saved["defaultMetadataProfileId"] != float64(7) {
		t.Fatal("native root did not receive the selected profiles and name")
	}
	if _, err := a.EnsureRootFolder(RootFolderInput{Path: "/selected/Music", QualityProfileID: 99, MetadataProfileID: 99}); err != nil || posts != 1 {
		t.Fatal("existing root settings were changed")
	}
}

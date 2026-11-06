package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"librarystudio/internal/config"
)

func TestOptionalSubtitleRoutesAndSecretPreservation(t *testing.T) {
	home := t.TempDir()
	store, _ := config.Open(home)
	c := config.Default()
	c.Bazarr = config.App{URL: "http://127.0.0.1:6767", APIKey: "private-subtitle-value"}
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}
	pub, saved := public(c)
	encoded, _ := json.Marshal(pub)
	if strings.Contains(string(encoded), c.Bazarr.APIKey) || !saved["bazarr"] || pub.Bazarr.APIKey != "" {
		t.Fatal("Bazarr credential exposed")
	}
	s := New(store, home)
	mux := http.NewServeMux()
	s.registerSubtitles(mux)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}
	if w := request("GET", "/api/media/subtitles?app=Sonarr&id=1", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatal("disabled subtitles must not block library")
	}
	for _, path := range []string{"english", "profile"} {
		if w := request("POST", "/api/media/subtitles/"+path, `{"app":"Sonarr","id":1,"itemId":2,"profileId":1}`); w.Code != 400 {
			t.Fatal("disabled subtitle action accepted")
		}
	}
	if w := request("GET", "/api/media/subtitles?app=Lidarr&id=1", ""); w.Code != 400 {
		t.Fatal("unsupported parent accepted")
	}
	body, _ := json.Marshal(pub)
	w := httptest.NewRecorder()
	s.saveSettings(w, httptest.NewRequest("POST", "/api/settings", strings.NewReader(string(body))))
	if w.Code != 200 || store.Get().Bazarr.APIKey != c.Bazarr.APIKey {
		t.Fatal("blank subtitle key did not preserve saved credential")
	}
}

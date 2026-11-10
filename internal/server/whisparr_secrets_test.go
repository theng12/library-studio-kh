package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"librarystudio/internal/config"
)

func TestWhisparrPublicSettingsAndBlankSaveProtectKey(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			home := t.TempDir()
			store, err := config.Open(home)
			if err != nil {
				t.Fatal(err)
			}
			c := store.Get()
			c.WhisparrOn = enabled
			c.Whisparr = config.App{APIKey: "private-whisparr-fixture"}
			if err := store.Save(c); err != nil {
				t.Fatal(err)
			}
			s := New(store, home)
			w := httptest.NewRecorder()
			s.state(w, httptest.NewRequest("GET", "/api/state", nil))
			if w.Code != 200 || strings.Contains(w.Body.String(), c.Whisparr.APIKey) {
				t.Fatal("state exposed Whisparr key")
			}
			var body struct {
				Settings config.Config   `json:"settings"`
				Saved    map[string]bool `json:"saved"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Settings.Whisparr.APIKey != "" || !body.Saved["whisparr"] || body.Settings.WhisparrOn != enabled {
				t.Fatal("public key state is incorrect")
			}
			w = httptest.NewRecorder()
			s.saveSettings(w, httptest.NewRequest("POST", "/api/settings", strings.NewReader(`{"whisparr":{"apiKey":""}}`)))
			if w.Code != 200 || store.Get() != c || strings.Contains(w.Body.String(), c.Whisparr.APIKey) {
				t.Fatal("blank settings save lost or exposed private account")
			}
			w = httptest.NewRecorder()
			s.saveSettings(w, httptest.NewRequest("POST", "/api/settings", strings.NewReader(`{"whisparr":{"apiKey":"replacement-fixture"}}`)))
			if w.Code != 200 || store.Get().Whisparr.APIKey != "replacement-fixture" {
				t.Fatal("explicit key replacement was not saved")
			}
		})
	}
}

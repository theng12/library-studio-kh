package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"librarystudio/internal/config"
)

func TestSixDigitPasswordSetupAndRecovery(t *testing.T) {
	home := t.TempDir()
	store, err := config.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	s := New(store, home)
	call := func(path, password string, owner bool) int {
		body, _ := json.Marshal(map[string]any{"username": "fixture-owner", "password": password})
		r := httptest.NewRequest("POST", "http://localhost"+path, strings.NewReader(string(body)))
		r.RemoteAddr = "127.0.0.1:8000"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Library-Request", "1")
		if owner {
			r.Header.Set("X-Library-Token", s.Token)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w.Code
	}
	if call("/api/auth/setup", "12345", true) != 400 {
		t.Fatal("five characters accepted during setup")
	}
	if call("/api/auth/setup", "123456", true) != 200 {
		t.Fatal("six numeric characters refused during setup")
	}
	if call("/api/auth/password", "54321", true) != 400 {
		t.Fatal("five characters accepted during recovery")
	}
	if call("/api/auth/password", "654321", true) != 200 {
		t.Fatal("six numeric characters refused during recovery")
	}
	if call("/api/auth/login", "654321", false) != 200 {
		t.Fatal("six-digit password did not authenticate")
	}
	r := httptest.NewRequest("GET", "http://localhost/api/auth/status", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var status struct {
		MinPasswordLength int `json:"minPasswordLength"`
	}
	if json.Unmarshal(w.Body.Bytes(), &status) != nil || status.MinPasswordLength != 6 {
		t.Fatal("frontend policy does not match backend minimum")
	}
}

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"librarystudio/internal/config"
)

// holdRequest drives the maintenance endpoint from loopback, the only place it answers.
func holdRequest(t *testing.T, s *Server, method, path string, remote string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	r := httptest.NewRequest(method, "http://127.0.0.1:7000"+path, nil)
	r.Header.Set("X-Library-Token", s.Token)
	r.RemoteAddr = remote
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w, out
}

func holdServer(t *testing.T) *Server {
	t.Helper()
	home := t.TempDir()
	store, err := config.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	return New(store, home)
}

func TestHoldRefusesNewWorkAndSaysWhy(t *testing.T) {
	s := holdServer(t)
	if w, body := holdRequest(t, s, "POST", "/api/maintenance/hold?timeout_s=1&pause_s=60", "127.0.0.1:5000"); w.Code != 200 {
		t.Fatalf("hold: %d %v", w.Code, body)
	}
	// A new job must not start while the app is held, and the refusal must be readable.
	w, _ := cleanupRequest(t, s, "POST", "/api/cleanup/run", "")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("held run must be refused, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "pausing for an update") {
		t.Fatalf("refusal must say why in plain words: %s", w.Body.String())
	}
	// DELETE resumes at once.
	if w, body := holdRequest(t, s, "DELETE", "/api/maintenance/hold", "127.0.0.1:5000"); w.Code != 200 || body["safe"] != true {
		t.Fatalf("resume: %d %v", w.Code, body)
	}
	if s.hold.pausedNow() {
		t.Fatal("pause must be gone after DELETE")
	}
}

func TestHoldReportsRealInFlightAndNamesUnsettled(t *testing.T) {
	s := holdServer(t)
	release, ok := s.hold.claim("cleanup-run")
	if !ok {
		t.Fatal("claim while running must succeed")
	}
	// timeout_s=0 is the honest deadline test: it must not wait for the work to finish.
	start := time.Now()
	w, body := holdRequest(t, s, "POST", "/api/maintenance/hold?timeout_s=0&pause_s=30", "127.0.0.1:5000")
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("hold waited %v; the settle must be bounded", elapsed)
	}
	if w.Code != 200 {
		t.Fatalf("hold: %d %v", w.Code, body)
	}
	if body["safe"] != false {
		t.Fatalf("work in flight must not report safe: %v", body)
	}
	if got := body["in_flight"].(float64); got != 1 {
		t.Fatalf("in_flight must be real, got %v", got)
	}
	unsettled, _ := body["unsettled"].([]any)
	if len(unsettled) != 1 || !strings.HasPrefix(unsettled[0].(string), "cleanup-run#") {
		t.Fatalf("unsettled must name a resumable id: %v", body["unsettled"])
	}
	release()
	if w, body := holdRequest(t, s, "POST", "/api/maintenance/hold?timeout_s=1&pause_s=30", "127.0.0.1:5000"); w.Code != 200 || body["safe"] != true || body["in_flight"].(float64) != 0 {
		t.Fatalf("settled work must report safe: %v", body)
	}
}

func TestPauseExpiresByItselfWithNoResumeCall(t *testing.T) {
	s := holdServer(t)
	now := time.Now()
	s.hold.nowFunc = func() time.Time { return now }
	s.hold.pauseFor(120 * time.Second)
	if !s.hold.pausedNow() {
		t.Fatal("pause must hold while it lasts")
	}
	now = now.Add(121 * time.Second)
	if s.hold.pausedNow() {
		t.Fatal("an aborted install must resume on its own after pause_s")
	}
	if _, ok := s.hold.claim("cleanup-run"); !ok {
		t.Fatal("work must start again after the pause expires")
	}
}

func TestHoldCeilingAndLoopbackOnly(t *testing.T) {
	s := holdServer(t)
	// A pause longer than the ceiling is clamped rather than trusted.
	if w, body := holdRequest(t, s, "POST", "/api/maintenance/hold?timeout_s=900&pause_s=999999", "127.0.0.1:5000"); w.Code != 200 {
		t.Fatalf("hold: %d %v", w.Code, body)
	} else if got := body["dispatch_paused_s"].(float64); got != holdMaxPause {
		t.Fatalf("pause must be clamped to %d, got %v", holdMaxPause, got)
	}
	if got := holdQueryInt(httptest.NewRequest("POST", "/x?timeout_s=900", nil), "timeout_s", holdDefaultTimeout, holdMaxTimeout); got != holdMaxTimeout {
		t.Fatalf("timeout must be clamped to %d, got %d", holdMaxTimeout, got)
	}
	// A request from another machine is refused outright.
	if w, _ := holdRequest(t, s, "POST", "/api/maintenance/hold", "192.0.2.9:5000"); w.Code != http.StatusForbidden {
		t.Fatalf("non-loopback hold must be refused, got %d", w.Code)
	}
}

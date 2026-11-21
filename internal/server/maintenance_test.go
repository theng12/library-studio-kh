package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"librarystudio/internal/config"
)

const holdTestToken = "{{SECRET_1w2jpgp9}}"

// holdRequest drives the maintenance endpoint from loopback with the installer's
// credential, the only way it answers.
func holdRequest(t *testing.T, s *Server, method, path string, remote string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	r := httptest.NewRequest(method, "http://127.0.0.1:7000"+path, nil)
	r.Header.Set("X-Library-Token", s.Token)
	r.Header.Set("X-Hub-Token", holdTestToken)
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
	tokenFile := filepath.Join(t.TempDir(), "hold-token")
	if err := os.WriteFile(tokenFile, []byte(holdTestToken), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(holdTokenFileEnv, tokenFile)
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

// The dashboard listener registers the same hold path for the app's own page. It must
// ask for the installer's credential too: being on this computer, or signed in to the
// page, is never permission to pause the app.
func TestDashboardHoldRouteNeedsTheInstallerToken(t *testing.T) {
	s := holdServer(t)
	call := func(method, remote string, headers map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:7881/api/maintenance/hold", nil)
		r.Header.Set("X-Library-Token", s.Token) // past the page's own sign-in gate
		r.RemoteAddr = remote
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if w := call("POST", "127.0.0.1:5000", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("a dashboard hold with no hold credential must get 401, got %d %s", w.Code, w.Body.String())
	}
	if s.hold.pausedNow() {
		t.Fatal("an uncredentialed dashboard hold must not pause the app")
	}
	if w := call("POST", "127.0.0.1:5000", map[string]string{"X-Hub-Token": "not-the-token"}); w.Code != http.StatusUnauthorized || s.hold.pausedNow() {
		t.Fatalf("a wrong hold credential must be refused: %d", w.Code)
	}
	// A web page never drives an update, even with the right credential.
	if w := call("POST", "127.0.0.1:5000", map[string]string{"X-Hub-Token": holdTestToken, "Origin": "http://127.0.0.1:7881"}); w.Code != http.StatusUnauthorized || s.hold.pausedNow() {
		t.Fatalf("an Origin-bearing dashboard hold must be refused: %d", w.Code)
	}
	// The updater's own credential still works here exactly as on the published listener.
	if w := call("POST", "127.0.0.1:5000", map[string]string{"X-Hub-Token": holdTestToken}); w.Code != 200 || !s.hold.pausedNow() {
		t.Fatalf("the installer's credential must still hold the dashboard route: %d %s", w.Code, w.Body.String())
	}
	if w := call("DELETE", "127.0.0.1:5000", map[string]string{"Authorization": "Bearer " + holdTestToken}); w.Code != 200 || s.hold.pausedNow() {
		t.Fatalf("resume with a bearer credential: %d", w.Code)
	}
	if w := call("POST", "192.0.2.9:5000", map[string]string{"X-Hub-Token": holdTestToken}); w.Code != http.StatusForbidden {
		t.Fatalf("a non-loopback dashboard hold must still be refused: %d", w.Code)
	}
}

// The shipped shape: the hold on its own listener, bound to this computer and opened
// only by the updater's own credential. This is the address the signed feed advertises.
func TestHoldHandlerNeedsTheInstallerTokenAndStaysOnThisComputer(t *testing.T) {
	s := holdServer(t)
	tokenFile := filepath.Join(t.TempDir(), "hold-token")
	if err := os.WriteFile(tokenFile, []byte("launcher-token-0123456789abcdef\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(holdTokenFileEnv, tokenFile)
	call := func(method, remote string, header, value string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:8643/api/maintenance/hold", nil)
		r.RemoteAddr = remote
		if header != "" {
			r.Header.Set(header, value)
		}
		w := httptest.NewRecorder()
		s.HoldHandler().ServeHTTP(w, r)
		return w
	}
	const token = "launcher-token-0123456789abcdef\n"
	// Being on this computer is not proof of being the installer.
	for _, c := range []struct{ name, header, value string }{
		{"no credential", "", ""},
		{"wrong credential", "X-Hub-Token", "not-the-token"},
		{"a cookie", "Cookie", "kh_hub_token=" + token},
	} {
		if w := call("POST", "127.0.0.1:51000", c.header, c.value); w.Code != http.StatusUnauthorized {
			t.Fatalf("%s must be refused, got %d", c.name, w.Code)
		}
		if s.hold.pausedNow() {
			t.Fatalf("%s must not have paused the app", c.name)
		}
	}
	if w := call("POST", "127.0.0.1:51000", "X-Hub-Token", token); w.Code != 200 {
		t.Fatalf("the installer's credential must be accepted: %d %s", w.Code, w.Body.String())
	}
	if !s.hold.pausedNow() {
		t.Fatal("an accepted hold must actually pause the app")
	}
	if w := call("DELETE", "127.0.0.1:51000", "Authorization", "Bearer "+token); w.Code != 200 || s.hold.pausedNow() {
		t.Fatalf("resume with a bearer credential: %d", w.Code)
	}
	// A web page never drives an update, whatever it presents.
	r := httptest.NewRequest("POST", "http://127.0.0.1:8643/api/maintenance/hold", nil)
	r.RemoteAddr = "127.0.0.1:51000"
	r.Header.Set("X-Hub-Token", token)
	r.Header.Set("Origin", "http://127.0.0.1:7881")
	w := httptest.NewRecorder()
	s.HoldHandler().ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized || s.hold.pausedNow() {
		t.Fatalf("an Origin-bearing hold must be refused, got %d", w.Code)
	}
	// Even with the right credential, another machine gets nothing.
	if w := call("POST", "192.0.2.9:51000", "X-Hub-Token", token); w.Code != http.StatusForbidden {
		t.Fatalf("the fixed listener must refuse another machine, got %d", w.Code)
	}
}

// The credential is read per request, so the Launcher can rotate it without a restart,
// and a file that is not a usable secret is no credential at all.
func TestHoldTokenIsReadFreshAndRefusedWhenNotPrivate(t *testing.T) {
	s := holdServer(t)
	tokenFile := filepath.Join(t.TempDir(), "hold-token")
	t.Setenv(holdTokenFileEnv, tokenFile)
	call := func(method, token string) int {
		r := httptest.NewRequest(method, "http://127.0.0.1:8643/api/maintenance/hold", nil)
		r.RemoteAddr = "127.0.0.1:51000"
		if token != "" {
			r.Header.Set("X-Hub-Token", token)
		}
		w := httptest.NewRecorder()
		s.HoldHandler().ServeHTTP(w, r)
		return w.Code
	}
	if call("POST", "anything") != http.StatusUnauthorized {
		t.Fatal("a missing token file must refuse the hold")
	}
	if err := os.WriteFile(tokenFile, []byte("   \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if call("POST", "anything") != http.StatusUnauthorized {
		t.Fatal("a blank token file must refuse the hold")
	}
	if err := os.WriteFile(tokenFile, []byte("first-token-0123456789abcdef\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if call("POST", "first-token-0123456789abcdef") != 200 {
		t.Fatal("the first token must open the hold")
	}
	// Rotate on disk without restarting: the old value stops working at once.
	if err := os.WriteFile(tokenFile, []byte("second-token-0123456789abcde\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if call("DELETE", "first-token-0123456789abcdef") != http.StatusUnauthorized {
		t.Fatal("the rotated-away token must be refused")
	}
	if call("DELETE", "second-token-0123456789abcde") != 200 {
		t.Fatal("the new token must work with no restart")
	}
	// World-readable, then symlinked: neither is a credential.
	if err := os.Chmod(tokenFile, 0o644); err != nil {
		t.Fatal(err)
	}
	if call("POST", "second-token-0123456789abcde") != http.StatusUnauthorized {
		t.Fatal("a world-readable token file must be refused")
	}
	if err := os.Chmod(tokenFile, 0o600); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(t.TempDir(), "real-token")
	if err := os.WriteFile(real, []byte("second-token-0123456789abcde\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(tokenFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, tokenFile); err != nil {
		t.Fatal(err)
	}
	if call("POST", "second-token-0123456789abcde") != http.StatusUnauthorized {
		t.Fatal("a symlinked token file must be refused")
	}
}

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

func TestAccountSessionLifecycle(t *testing.T) {
	home := t.TempDir()
	store, err := config.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	s := New(store, home)
	call := func(srv *Server, method, path, body, peer string, bootstrap, marker bool, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://localhost:7881"+path, strings.NewReader(body))
		r.RemoteAddr = peer
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if bootstrap {
			r.Header.Set("X-Library-Token", srv.Token)
		}
		if marker {
			r.Header.Set("X-Library-Request", "1")
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, r)
		return w
	}
	const account = `{"username":"owner","password":"test passphrase only"}`
	if call(s, "POST", "/api/auth/setup", account, "127.0.0.1:8000", false, true, nil).Code != 403 {
		t.Fatal("setup accepted without desktop key")
	}
	if call(s, "POST", "/api/auth/setup", account, "100.80.1.2:8000", true, true, nil).Code != 403 {
		t.Fatal("remote setup accepted")
	}
	if w := call(s, "POST", "/api/auth/setup", account, "127.0.0.1:8000", true, true, nil); w.Code != 200 {
		t.Fatalf("setup status %d", w.Code)
	}
	if call(s, "POST", "/api/auth/setup", account, "127.0.0.1:8000", true, true, nil).Code != 409 {
		t.Fatal("account overwritten by repeated setup")
	}
	if call(s, "POST", "/api/auth/login", account, "127.0.0.1:8000", false, false, nil).Code != 403 {
		t.Fatal("login without request marker accepted")
	}
	if call(s, "POST", "/api/auth/login", `{"username":"owner","password":"wrong"}`, "127.0.0.1:8000", false, true, nil).Code != 401 {
		t.Fatal("wrong password accepted")
	}
	login := call(s, "POST", "/api/auth/login", `{"username":"owner","password":"test passphrase only","remember":true}`, "127.0.0.1:8000", false, true, nil)
	if login.Code != 200 {
		t.Fatalf("login status %d", login.Code)
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing session cookie")
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.MaxAge != rememberDays*86400 {
		t.Fatal("unsafe cookie attributes")
	}
	data, err := os.ReadFile(filepath.Join(home, "access.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), cookie.Value) || strings.Contains(string(data), "test passphrase only") {
		t.Fatal("raw session/password persisted")
	}
	again := New(store, home)
	if again.Token != s.Token {
		t.Fatal("identity changed across restart")
	}
	w := call(again, "GET", "/api/auth/status", "", "127.0.0.1:8000", false, false, cookie)
	var status struct{ Authenticated bool }
	if json.Unmarshal(w.Body.Bytes(), &status) != nil || !status.Authenticated {
		t.Fatal("remembered session lost across restart")
	}
	r := httptest.NewRequest("POST", "http://localhost:7881/api/auth/logout", strings.NewReader("{}"))
	r.RemoteAddr = "127.0.0.1:8000"
	r.AddCookie(cookie)
	r.Header.Set("X-Library-Request", "1")
	r.Header.Set("Origin", "https://other.invalid")
	denied := httptest.NewRecorder()
	again.Handler().ServeHTTP(denied, r)
	if denied.Code != 403 {
		t.Fatal("foreign-origin write accepted")
	}
	if call(again, "POST", "/api/auth/logout", "{}", "127.0.0.1:8000", false, false, cookie).Code != 403 {
		t.Fatal("cookie write without CSRF marker accepted")
	}
	sessions := call(again, "GET", "/api/auth/sessions", "", "127.0.0.1:8000", false, false, cookie)
	var list struct {
		Items []struct {
			ID      string
			Current bool
		}
	}
	if json.Unmarshal(sessions.Body.Bytes(), &list) != nil || len(list.Items) != 1 || !list.Items[0].Current {
		t.Fatal("session listing failed")
	}
	revoke, _ := json.Marshal(map[string]string{"id": list.Items[0].ID})
	if call(again, "POST", "/api/auth/revoke", string(revoke), "127.0.0.1:8000", false, true, cookie).Code != 200 {
		t.Fatal("revoke failed")
	}
	if call(New(store, home), "GET", "/api/auth/sessions", "", "127.0.0.1:8000", false, false, cookie).Code != 403 {
		t.Fatal("revoked session accepted after restart")
	}
	// A separately created nonremembered session has no persistent cookie and expires server-side.
	normal := call(again, "POST", "/api/auth/login", account, "127.0.0.1:8000", false, true, nil).Result().Cookies()
	if len(normal) != 1 || normal[0].MaxAge != 0 {
		t.Fatal("normal browser session unexpectedly persisted")
	}
	again.Now = func() time.Time { return time.Now().Add((sessionHours + 1) * time.Hour) }
	if call(again, "GET", "/api/auth/sessions", "", "127.0.0.1:8000", false, false, normal[0]).Code != 403 {
		t.Fatal("expired session accepted")
	}
}

func TestCorruptAccessFailsClosed(t *testing.T) {
	home := t.TempDir()
	store, _ := config.Open(home)
	if err := os.WriteFile(filepath.Join(home, "access.json"), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	s := New(store, home)
	r := httptest.NewRequest("GET", "http://localhost/api/auth/status", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 503 || s.Token != "" {
		t.Fatal("corrupt identity did not fail closed")
	}
}

func TestFirstIdentityIsPrivateAndPersistent(t *testing.T) {
	home := t.TempDir()
	store, err := config.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	first := New(store, home)
	if len(first.Token) != 64 {
		t.Fatal("no identity created")
	}
	info, err := os.Stat(filepath.Join(home, "access.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0077 != 0 {
		t.Fatalf("access.json is not private: %o", info.Mode().Perm())
	}
	if second := New(store, home); second.Token != first.Token {
		t.Fatal("identity changed on restart")
	}
	leftovers, _ := filepath.Glob(filepath.Join(home, ".access-*"))
	if len(leftovers) != 0 {
		t.Fatalf("temporary files left behind: %v", leftovers)
	}
}

func TestHostProofIsLoopbackOnly(t *testing.T) {
	home := t.TempDir()
	store, err := config.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	s := New(store, home)
	challenge := strings.Repeat("ab", 32)
	remote := httptest.NewRequest("GET", "http://localhost:7881/api/host-proof?challenge="+challenge, nil)
	remote.RemoteAddr = "100.80.1.2:8000"
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, remote)
	if w.Code != 403 {
		t.Fatalf("remote proof status %d", w.Code)
	}

	local := httptest.NewRequest("GET", "http://localhost:7881/api/host-proof?challenge="+challenge, nil)
	local.RemoteAddr = "127.0.0.1:8000"
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, local)
	var got struct {
		OK      bool
		Version string
		Proof   string
	}
	body := w.Body.String()
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Proof != HostProof(s.Token, challenge) || strings.Contains(body, s.Token) {
		t.Fatalf("loopback proof failed: %d %s", w.Code, body)
	}
}

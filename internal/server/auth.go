package server

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const minPasswordCharacters = 6
const passwordRounds = 600000
const sessionHours = 12
const rememberDays = 30
const sessionCookie = "library_session"

type browserSession struct {
	Created  time.Time `json:"created"`
	Expires  time.Time `json:"expires"`
	Device   string    `json:"device,omitempty"`   // browser and system, from the sign-in request
	Peer     string    `json:"peer,omitempty"`     // "this Mac" or the connecting address, so equal browsers differ
	LastSeen time.Time `json:"lastSeen,omitempty"` // last authenticated request, kept coarse
}
type accessData struct {
	Token    string                    `json:"token"`
	Username string                    `json:"username,omitempty"`
	Salt     []byte                    `json:"salt,omitempty"`
	Verifier []byte                    `json:"verifier,omitempty"`
	Sessions map[string]browserSession `json:"sessions"`
}
type loginAttempts struct {
	Count int
	Until time.Time
}
type accessState struct {
	mu       sync.Mutex
	data     accessData
	attempts map[string]loginAttempts
	err      error
}
type remoteOriginHostKey struct{}

func randomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Keep identity across app restarts; no access key is written to service logs.
func (s *Server) initAccess() {
	s.access.attempts = map[string]loginAttempts{}
	path := filepath.Join(s.Home, "access.json")
	b, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(b, &s.access.data)
	} else if os.IsNotExist(err) {
		s.access.data, err = s.createAccess(path)
	}
	if err == nil {
		err = validateAccess(s.access.data)
	}
	if err == nil {
		info, e := os.Stat(path)
		if e != nil {
			err = e
		} else if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
			err = fmt.Errorf("access configuration must be private (0600)")
		}
	}
	if s.access.data.Sessions == nil {
		s.access.data.Sessions = map[string]browserSession{}
	}
	if err != nil {
		s.access.err = fmt.Errorf("Library sign-in storage could not be opened; check the host's private configuration folder")
		return
	}
	s.Token = s.access.data.Token
}

// AccessError reports why sign-in storage is unusable, so a host can refuse to
// start before it does any background work.
func (s *Server) AccessError() error { return s.access.err }

func validateAccess(data accessData) error {
	if len(data.Token) != 64 || (data.Username != "" && (len(data.Salt) != 32 || len(data.Verifier) != 32)) {
		return fmt.Errorf("invalid access configuration")
	}
	return nil
}

// createAccess publishes the first identity only once the whole private file is
// on disk, and never replaces a file another process published first.
func (s *Server) createAccess(path string) (accessData, error) {
	secret, err := randomSecret()
	if err != nil {
		return accessData{}, err
	}
	data := accessData{Token: secret, Sessions: map[string]browserSession{}}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return accessData{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".access-*")
	if err != nil {
		return accessData{}, err
	}
	defer os.Remove(tmp.Name())
	if err := json.NewEncoder(tmp).Encode(data); err != nil {
		tmp.Close()
		return accessData{}, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return accessData{}, err
	}
	if err := tmp.Close(); err != nil {
		return accessData{}, err
	}
	if err := os.Link(tmp.Name(), path); err != nil {
		if !os.IsExist(err) {
			return accessData{}, err
		}
		// Another process published first; adopt its identity.
		b, err := os.ReadFile(path)
		if err != nil {
			return accessData{}, err
		}
		var other accessData
		if err := json.Unmarshal(b, &other); err != nil {
			return accessData{}, err
		}
		return other, nil
	}
	return data, nil
}

func (s *Server) saveAccessLocked() error {
	b, err := json.Marshal(s.access.data)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.Home, ".access-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(s.Home, "access.json"))
}
func (s *Server) bootstrap(r *http.Request) bool {
	return s.Token != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Library-Token")), []byte(s.Token)) == 1
}

// describeDevice names a sign-in from its own User-Agent: browser and system,
// never a guessed machine name.
func describeDevice(ua string) string {
	browser := "Browser"
	switch {
	case strings.Contains(ua, "Edg/"):
		browser = "Edge"
	case strings.Contains(ua, "Chrome/"):
		browser = "Chrome"
	case strings.Contains(ua, "Firefox/"):
		browser = "Firefox"
	case strings.Contains(ua, "Safari/"):
		browser = "Safari"
	}
	system := "Unknown system"
	switch {
	case strings.Contains(ua, "iPhone"):
		system = "iPhone"
	case strings.Contains(ua, "iPad"):
		system = "iPad"
	case strings.Contains(ua, "Android"):
		system = "Android"
	case strings.Contains(ua, "Macintosh"), strings.Contains(ua, "Mac OS X"):
		system = "macOS"
	case strings.Contains(ua, "Windows"):
		system = "Windows"
	case strings.Contains(ua, "Linux"):
		system = "Linux"
	}
	return browser + " on " + system
}

// describePeer tells two look-alike browsers apart: this Mac, or the address
// the remote session came from.
func describePeer(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || h == "" {
		return ""
	}
	if ip := net.ParseIP(h); ip != nil && ip.IsLoopback() {
		return "this Mac"
	}
	return h
}

func sessionID(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil || len(c.Value) != 64 {
		return ""
	}
	h := sha256.Sum256([]byte(c.Value))
	return hex.EncodeToString(h[:])
}
func (s *Server) authorized(r *http.Request) bool {
	if s.access.err != nil {
		return false
	}
	if s.bootstrap(r) {
		return true
	}
	id := sessionID(r)
	s.access.mu.Lock()
	defer s.access.mu.Unlock()
	record, ok := s.access.data.Sessions[id]
	now := s.Now()
	if !ok || !now.Before(record.Expires) {
		return false
	}
	// Last seen is shown to the owner; keep it current without a write per request.
	if now.Sub(record.LastSeen) > 5*time.Minute {
		record.LastSeen = now
		s.access.data.Sessions[id] = record
		_ = s.saveAccessLocked()
	}
	return true
}
func loopbackPeer(r *http.Request) bool {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	return err == nil && net.ParseIP(h).IsLoopback()
}
func (s *Server) safeWrite(r *http.Request) bool {
	if r.Method == "GET" || r.Method == "HEAD" {
		return true
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	host := r.Host
	if original, ok := r.Context().Value(remoteOriginHostKey{}).(string); ok {
		host = original
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != host || (u.Scheme != "http" && u.Scheme != "https") {
			return false
		}
	}
	return s.bootstrap(r) || r.Header.Get("X-Library-Request") == "1"
}
func (s *Server) registerAuth(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/auth/status", s.authStatus)
	mux.HandleFunc("POST /api/auth/setup", s.authSetup)
	mux.HandleFunc("POST /api/auth/login", s.authLogin)
	mux.HandleFunc("POST /api/auth/logout", s.authLogout)
	mux.HandleFunc("GET /api/auth/sessions", s.authSessions)
	mux.HandleFunc("POST /api/auth/revoke", s.authRevoke)
	mux.HandleFunc("POST /api/auth/password", s.authPassword)
}
func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	s.access.mu.Lock()
	setup := s.access.data.Username == ""
	s.access.mu.Unlock()
	writeJSON(w, 200, map[string]any{"authenticated": s.authorized(r), "setupRequired": setup, "rememberDays": rememberDays, "sessionHours": sessionHours, "minPasswordLength": minPasswordCharacters})
}
func readAuth(w http.ResponseWriter, r *http.Request, out any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") || json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(out) != nil {
		writeJSON(w, 400, map[string]string{"error": "Sign-in details could not be read."})
		return false
	}
	return true
}

type accountInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Remember bool   `json:"remember"`
}

func (s *Server) setAccount(w http.ResponseWriter, r *http.Request, recovery bool) {
	if !loopbackPeer(r) || !s.bootstrap(r) {
		writeJSON(w, 403, map[string]string{"error": "Set up or recover the account from the Library desktop on its host."})
		return
	}
	var req accountInput
	if !readAuth(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || len(req.Username) > 80 || len(req.Password) < minPasswordCharacters || len(req.Password) > 1024 {
		writeJSON(w, 400, map[string]string{"error": fmt.Sprintf("Enter a username (up to 80 characters) and a password of at least %d characters (up to 1024 bytes).", minPasswordCharacters)})
		return
	}
	s.access.mu.Lock()
	defer s.access.mu.Unlock()
	if !recovery && s.access.data.Username != "" {
		writeJSON(w, 409, map[string]string{"error": "An owner account already exists. Sign in instead."})
		return
	}
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		writeJSON(w, 500, map[string]string{"error": "A secure account could not be created."})
		return
	}
	hash, err := pbkdf2.Key(sha256.New, req.Password, salt, passwordRounds, 32)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "The password could not be secured."})
		return
	}
	old := s.access.data
	s.access.data = accessData{Token: old.Token, Username: req.Username, Salt: salt, Verifier: hash, Sessions: map[string]browserSession{}}
	if err = s.saveAccessLocked(); err != nil {
		s.access.data = old
		writeJSON(w, 500, map[string]string{"error": "Account could not be saved. Check host storage."})
		return
	}
	s.access.attempts = map[string]loginAttempts{}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) authSetup(w http.ResponseWriter, r *http.Request)    { s.setAccount(w, r, false) }
func (s *Server) authPassword(w http.ResponseWriter, r *http.Request) { s.setAccount(w, r, true) }
func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	var req accountInput
	if !readAuth(w, r, &req) {
		return
	}
	if len(req.Username) > 80 || len(req.Password) > 1024 {
		writeJSON(w, 400, map[string]string{"error": "Sign-in details are too long."})
		return
	}
	peer, _, _ := net.SplitHostPort(r.RemoteAddr)
	s.access.mu.Lock()
	defer s.access.mu.Unlock()
	now := s.Now()
	attempt := s.access.attempts[peer]
	if !now.Before(attempt.Until) {
		attempt = loginAttempts{Until: now.Add(15 * time.Minute)}
	}
	if attempt.Count >= 5 {
		w.Header().Set("Retry-After", "900")
		writeJSON(w, 429, map[string]string{"error": "Too many sign-in attempts. Try again in 15 minutes."})
		return
	}
	if s.access.data.Username == "" {
		writeJSON(w, 409, map[string]string{"error": "Set up the owner account on the host desktop first."})
		return
	}
	hash, err := pbkdf2.Key(sha256.New, req.Password, s.access.data.Salt, passwordRounds, 32)
	if err != nil || subtle.ConstantTimeCompare(hash, s.access.data.Verifier) != 1 || strings.TrimSpace(req.Username) != s.access.data.Username {
		attempt.Count++
		s.access.attempts[peer] = attempt
		writeJSON(w, 401, map[string]string{"error": "The username or password wasn't accepted."})
		return
	}
	secret, err := randomSecret()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "A secure session could not be created."})
		return
	}
	sum := sha256.Sum256([]byte(secret))
	id := hex.EncodeToString(sum[:])
	lifetime := sessionHours * time.Hour
	if req.Remember {
		lifetime = rememberDays * 24 * time.Hour
	}
	// Bounded session list; expired records are dropped before saving a new session.
	for k, v := range s.access.data.Sessions {
		if !now.Before(v.Expires) {
			delete(s.access.data.Sessions, k)
		}
	}
	if len(s.access.data.Sessions) >= 32 {
		writeJSON(w, 409, map[string]string{"error": "There are 32 signed-in sessions. Revoke one from the host before adding another."})
		return
	}
	s.access.data.Sessions[id] = browserSession{Created: now, Expires: now.Add(lifetime), Device: describeDevice(r.UserAgent()), Peer: describePeer(r), LastSeen: now}
	if err = s.saveAccessLocked(); err != nil {
		delete(s.access.data.Sessions, id)
		writeJSON(w, 500, map[string]string{"error": "Sign-in could not be saved. Check host storage."})
		return
	}
	delete(s.access.attempts, peer)
	// Secure only over TLS: the Tailscale listener is plain HTTP inside WireGuard, and a
	// Secure cookie there would be dropped by the browser, breaking phone sign-in.
	cookie := &http.Cookie{Name: sessionCookie, Value: secret, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil}
	if req.Remember {
		cookie.MaxAge = int(lifetime.Seconds())
		cookie.Expires = now.Add(lifetime)
	}
	http.SetCookie(w, cookie)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}
func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	s.access.mu.Lock()
	defer s.access.mu.Unlock()
	id := sessionID(r)
	old, exists := s.access.data.Sessions[id]
	delete(s.access.data.Sessions, id)
	if err := s.saveAccessLocked(); err != nil {
		if exists {
			s.access.data.Sessions[id] = old
		}
		writeJSON(w, 500, map[string]string{"error": "Sign-out could not be saved. Try again."})
		return
	}
	clearSessionCookie(w)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) authSessions(w http.ResponseWriter, r *http.Request) {
	s.access.mu.Lock()
	defer s.access.mu.Unlock()
	items := []map[string]any{}
	current := sessionID(r)
	for id, v := range s.access.data.Sessions {
		if s.Now().Before(v.Expires) {
			items = append(items, map[string]any{"id": id, "created": v.Created, "expires": v.Expires, "device": v.Device, "peer": v.Peer, "lastSeen": v.LastSeen, "current": id == current})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i]["created"].(time.Time).After(items[j]["created"].(time.Time)) })
	writeJSON(w, 200, map[string]any{"items": items})
}
func (s *Server) authRevoke(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if !readAuth(w, r, &req) {
		return
	}
	s.access.mu.Lock()
	defer s.access.mu.Unlock()
	old, ok := s.access.data.Sessions[req.ID]
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "That session is no longer active."})
		return
	}
	delete(s.access.data.Sessions, req.ID)
	if err := s.saveAccessLocked(); err != nil {
		s.access.data.Sessions[req.ID] = old
		writeJSON(w, 500, map[string]string{"error": "Session could not be revoked. Try again."})
		return
	}
	if req.ID == sessionID(r) {
		clearSessionCookie(w)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

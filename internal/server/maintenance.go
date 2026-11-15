package server

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Update hold: the installer asks the app to stop taking new work, waits a bounded
// time for what is running, and is told whether a restart now is safe. The pause is
// in memory only, so a restart always resumes and an aborted install cannot leave an
// app that has stopped taking work. Contract: _orchestra/youtube-stack/update-hold.
const (
	holdDefaultTimeout = 20
	holdMaxTimeout     = 30
	holdDefaultPause   = 120
	holdMaxPause       = 3600
)

// holdState is the in-memory pause plus the work claimed under it.
type holdState struct {
	mu      sync.Mutex
	paused  time.Time // zero when running normally; expires by itself
	work    map[int64]string
	nextID  int64
	nowFunc func() time.Time
}

func (h *holdState) now() time.Time {
	if h.nowFunc != nil {
		return h.nowFunc()
	}
	return time.Now()
}

// pausedNow reports whether new work must be refused, clearing an expired pause.
func (h *holdState) pausedNow() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.paused.IsZero() {
		return false
	}
	if !h.now().Before(h.paused) {
		h.paused = time.Time{}
		return false
	}
	return true
}

func (h *holdState) pauseFor(d time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.paused = h.now().Add(d)
}

func (h *holdState) resume() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.paused = time.Time{}
}

// claim registers work and refuses it while paused. The returned func releases the
// claim; a caller that starts work must call it when the work ends.
func (h *holdState) claim(label string) (func(), bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.paused.IsZero() && h.now().Before(h.paused) {
		return func() {}, false
	}
	if h.work == nil {
		h.work = map[int64]string{}
	}
	h.nextID++
	id := h.nextID
	h.work[id] = label
	var once sync.Once
	return func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			delete(h.work, id)
		})
	}, true
}

// running lists the work still in flight, newest first, as stable ids a later start
// can follow.
func (h *holdState) running() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.work) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(h.work))
	for id := len(h.work); id >= 1; id-- {
		if label, ok := h.work[int64(id)]; ok {
			out = append(out, fmt.Sprintf("%s#%d", label, id))
		}
	}
	if len(out) == 0 {
		for id, label := range h.work {
			out = append(out, fmt.Sprintf("%s#%d", label, id))
		}
	}
	return out
}

// claimWork is what every work-starting handler calls. When Library is pausing for
// an update the request is refused in plain words, so nothing new starts.
func (s *Server) claimWork(w http.ResponseWriter, label string) (func(), bool) {
	release, ok := s.hold.claim(label)
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Library is pausing for an update, so nothing new starts yet. Try again in a moment.",
		})
		return nil, false
	}
	return release, true
}

func holdQueryInt(r *http.Request, name string, def, max int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

// maintenanceHold pauses new work and reports whether a restart is safe right now.
func (s *Server) maintenanceHold(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "The update hold only answers on this computer."})
		return
	}
	timeout := holdQueryInt(r, "timeout_s", holdDefaultTimeout, holdMaxTimeout)
	pause := holdQueryInt(r, "pause_s", holdDefaultPause, holdMaxPause)
	inFlight := len(s.hold.running())
	s.hold.pauseFor(time.Duration(pause) * time.Second)
	log.Printf("update hold: paused %ds, %d in flight", pause, inFlight)

	// Bounded settle: never waits for the work to finish, only up to the deadline.
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	for time.Now().Before(deadline) {
		if len(s.hold.running()) == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	unsettled := s.hold.running()
	writeJSON(w, 200, map[string]any{
		"safe":              len(unsettled) == 0,
		"unsettled":         unsettled,
		"dispatch_paused_s": pause,
		"in_flight":         inFlight,
	})
}

// maintenanceResume ends the pause at once. The installer does not have to call it:
// the pause expires by itself and a restart always resumes.
func (s *Server) maintenanceResume(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "The update hold only answers on this computer."})
		return
	}
	s.hold.resume()
	log.Printf("update hold: resumed")
	writeJSON(w, 200, map[string]any{
		"safe":              true,
		"unsettled":         []string{},
		"dispatch_paused_s": 0,
		"in_flight":         len(s.hold.running()),
	})
}

// HoldHandler is the hold on its own listener, the fixed loopback port the signed
// feed advertises. It carries no token: the installer has none, and the listener is
// bound to 127.0.0.1, so only this computer can pause the app. The pause expires.
func (s *Server) HoldHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/maintenance/hold", s.maintenanceHold)
	mux.HandleFunc("DELETE /api/maintenance/hold", s.maintenanceResume)
	return mux
}

// isLoopbackRequest is the hold's own gate: only this computer may pause the app.
func isLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

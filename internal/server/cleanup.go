package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"librarystudio/internal/library"
)

// registerCleanup exposes the staged policy. GET status is read-only; the POST
// routes stay behind the existing loopback, origin and sign-in guard.
func (s *Server) registerCleanup(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/cleanup/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, s.cleanup.Status(s.Store.Get()))
	})
	mux.HandleFunc("POST /api/cleanup/preview", func(w http.ResponseWriter, r *http.Request) {
		res, err := s.cleanup.Preview(s.Store.Get())
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, res)
	})
	mux.HandleFunc("POST /api/cleanup/run", func(w http.ResponseWriter, r *http.Request) {
		res, err := s.cleanup.Run(s.Store.Get())
		if errors.Is(err, library.ErrCleanupDisabled) {
			writeJSON(w, 409, map[string]string{"error": "Turn staged cleanup on in Settings first."})
			return
		}
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, res)
	})
	mux.HandleFunc("POST /api/cleanup/restore", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Hash string `json:"hash"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || strings.TrimSpace(req.Hash) == "" {
			writeJSON(w, 400, map[string]string{"error": "Choose a tracked download to restore."})
			return
		}
		res, err := s.cleanup.Restore(s.Store.Get(), req.Hash)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, res)
	})
}

// runStagedCleanup is the hourly job. A switched-off policy does nothing at all.
func (s *Server) runStagedCleanup() {
	res, err := s.cleanup.Run(s.Store.Get())
	s.mu.Lock()
	defer s.mu.Unlock()
	last := &lastRun{At: s.Now(), Removed: res.Purged, Errors: res.Problems}
	if err != nil && !errors.Is(err, library.ErrCleanupDisabled) {
		last.Errors = append(last.Errors, err.Error())
	}
	if len(res.Status.Blocked) > 0 || len(res.Status.EngineBlocked) > 0 {
		last.Blocked = strings.Join(append(append([]string{}, res.Status.Blocked...), res.Status.EngineBlocked...), " ")
	}
	s.last = last
}

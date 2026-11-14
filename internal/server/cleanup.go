package server

import (
	"encoding/json"
	"errors"
	"log"
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
		res, err := s.cleanup.PreviewApp(s.Store.Get(), r.URL.Query().Get("app"))
		if errors.Is(err, library.ErrCleanupApp) {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		log.Printf("cleanup: preview %d proposed action(s), %d problem(s), nothing changed", len(res.Actions), len(res.Problems))
		writeJSON(w, 200, res)
	})
	mux.HandleFunc("POST /api/cleanup/run", func(w http.ResponseWriter, r *http.Request) {
		res, err := s.cleanup.RunApp(s.Store.Get(), r.URL.Query().Get("app"))
		if errors.Is(err, library.ErrCleanupApp) {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if errors.Is(err, library.ErrCleanupDisabled) {
			writeJSON(w, 409, map[string]string{"error": "Turn staged cleanup on in Settings first."})
			return
		}
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		log.Printf("cleanup: run %d removed, %d action(s), %d problem(s)", res.Purged, len(res.Actions), len(res.Problems))
		if len(res.Actions) > 0 {
			library.RecordEvent(s.Home, library.Item{ID: "cleanup-pass", Kind: "cleanup-pass", Title: "Cleanup pass", SourcePath: strings.Join(res.Actions, " "), When: s.Now()})
		}
		s.invalidateData()
		writeJSON(w, 200, res)
	})
	mux.HandleFunc("POST /api/cleanup/verify", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Hash string `json:"hash"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || strings.TrimSpace(req.Hash) == "" {
			writeJSON(w, 400, map[string]string{"error": "Choose a tracked download to verify."})
			return
		}
		res, err := s.cleanup.Verify(s.Store.Get(), req.Hash)
		if err != nil {
			log.Printf("cleanup: verify %s failed: %v", req.Hash, err)
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		log.Printf("cleanup: verify %s %s", req.Hash, strings.Join(res.Actions, " "))
		s.invalidateData()
		writeJSON(w, 200, res)
	})
	mux.HandleFunc("POST /api/cleanup/remove-now", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Hash string `json:"hash"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || strings.TrimSpace(req.Hash) == "" {
			writeJSON(w, 400, map[string]string{"error": "Choose a tracked download to remove."})
			return
		}
		res, err := s.cleanup.RemoveNow(s.Store.Get(), req.Hash)
		if err != nil {
			log.Printf("cleanup: remove-now %s refused: %v", req.Hash, err)
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		log.Printf("cleanup: remove-now %s moved its local copy to Library's _trash", req.Hash)
		s.recordCleanupEvent(res, req.Hash, "cleanup-moved")
		s.invalidateData()
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
			log.Printf("cleanup: restore %s refused: %v", req.Hash, err)
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		log.Printf("cleanup: restore %s %s", req.Hash, strings.Join(res.Actions, " "))
		s.recordCleanupEvent(res, req.Hash, "cleanup-restored")
		s.invalidateData()
		writeJSON(w, 200, res)
	})
}

// recordCleanupEvent writes one owner-visible Activity event for a cleanup
// action, naming the copy and its path so the timeline can prove what moved.
func (s *Server) recordCleanupEvent(res library.CleanupResult, hash, kind string) {
	it := library.Item{ID: strings.ToUpper(hash), Kind: kind, Title: "Download " + hash, When: s.Now()}
	for _, item := range res.Status.Items {
		if strings.EqualFold(item.Hash, hash) {
			it.Title, it.SourcePath = item.Name, item.RestorePath
			break
		}
	}
	library.RecordEvent(s.Home, it)
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
	log.Printf("cleanup: hourly run removed=%d blocked=%q problems=%q", res.Purged, last.Blocked, strings.Join(last.Errors, " "))
}

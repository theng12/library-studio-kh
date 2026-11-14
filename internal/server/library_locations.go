package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"librarystudio/internal/library"
	"librarystudio/internal/platform"
)

// The detail panel's two location actions: remember the folder the owner picks,
// and show the current one on the Mac that holds it.
func (s *Server) registerLibraryLocations(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/library/location", s.setLibraryLocation)
	mux.HandleFunc("POST /api/library/open-location", s.openLibraryLocation)
}

func (s *Server) setLibraryLocation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		App  string `json:"app"`
		ID   int    `json:"id"`
		Path string `json:"path"`
	}
	if !readFolderBody(w, r, &req) {
		return
	}
	req.Path = strings.TrimSpace(req.Path)
	if !filepath.IsAbs(req.Path) {
		writeJSON(w, 400, map[string]string{"error": "Choose the folder that holds this title."})
		return
	}
	info, err := os.Stat(req.Path)
	if err != nil || !info.IsDir() {
		writeJSON(w, 400, map[string]string{"error": "That folder isn't on this Mac. Choose a folder that exists."})
		return
	}
	if err := library.SaveLocation(s.Home, req.App, req.ID, req.Path); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"path": req.Path})
}

func (s *Server) openLibraryLocation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !readFolderBody(w, r, &req) {
		return
	}
	path := strings.TrimSpace(req.Path)
	if !filepath.IsAbs(path) {
		writeJSON(w, 400, map[string]string{"error": "There is no recorded folder to open yet."})
		return
	}
	// Only the desktop shell may open a window on this Mac; a phone gets words.
	if !s.bootstrap(r) {
		writeJSON(w, 200, map[string]bool{"remote": true})
		return
	}
	if _, err := os.Lstat(path); err != nil {
		writeJSON(w, 400, map[string]string{"error": "That folder isn't on this Mac anymore."})
		return
	}
	if err := platform.Reveal(path); err != nil {
		writeJSON(w, 500, map[string]string{"error": "Finder could not open that folder: " + err.Error()})
		return
	}
	writeJSON(w, 200, map[string]bool{"opened": true})
}

func readFolderBody(w http.ResponseWriter, r *http.Request, out any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") ||
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(out) != nil {
		writeJSON(w, 400, map[string]string{"error": "Choose a folder and try again."})
		return false
	}
	return true
}

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"librarystudio/internal/platform"
)

// Path fields are never typed: the desktop shell opens the native panel, and a
// remote/phone page opens this app's own directory browser instead.

type folderChoice struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type folderListing struct {
	Path    string         `json:"path"`
	Parent  string         `json:"parent"`
	Dirs    []folderChoice `json:"dirs"`
	Volumes []folderChoice `json:"volumes"`
}

func (s *Server) registerFolders(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/pick-folder", s.pickFolder)
	mux.HandleFunc("GET /api/browse-dirs", s.browseDirs)
}

func (s *Server) pickFolder(w http.ResponseWriter, r *http.Request) {
	// Only the desktop shell carries the launch token; a phone session never does.
	if !s.bootstrap(r) {
		writeJSON(w, 200, map[string]bool{"remote": true})
		return
	}
	var req struct {
		Prompt string `json:"prompt"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req)
	prompt := strings.TrimSpace(req.Prompt)
	if len(prompt) > 200 {
		prompt = prompt[:200]
	}
	path, err := platform.PickFolder(prompt)
	if errors.Is(err, platform.ErrFolderDialogUnsupported) {
		writeJSON(w, 200, map[string]bool{"remote": true})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "The folder panel could not open: " + err.Error()})
		return
	}
	if path == "" {
		writeJSON(w, 200, map[string]bool{"canceled": true})
		return
	}
	writeJSON(w, 200, map[string]string{"path": path})
}

func (s *Server) browseDirs(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "This computer's home folder could not be found."})
			return
		}
		path = home
	}
	if !filepath.IsAbs(path) {
		writeJSON(w, 400, map[string]string{"error": "Choose a full folder path."})
		return
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		writeJSON(w, 400, map[string]string{"error": "That folder can't be opened."})
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "That folder can't be read."})
		return
	}
	out := folderListing{Path: path, Dirs: []folderChoice{}, Volumes: mountVolumes()}
	if parent := filepath.Dir(path); parent != path {
		out.Parent = parent
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		if isFolder(filepath.Join(path, name), entry) {
			out.Dirs = append(out.Dirs, folderChoice{Name: name, Path: filepath.Join(path, name)})
		}
	}
	sort.Slice(out.Dirs, func(i, j int) bool { return strings.ToLower(out.Dirs[i].Name) < strings.ToLower(out.Dirs[j].Name) })
	writeJSON(w, 200, out)
}

// mountVolumes names the disks a phone or the in-app browser can choose from.
// Windows lists its drive roots; Linux reports none and starts in the home folder.
func mountVolumes() []folderChoice {
	out := []folderChoice{}
	if runtime.GOOS == "windows" {
		for c := 'A'; c <= 'Z'; c++ {
			root := string(c) + `:\`
			if info, err := os.Stat(root); err == nil && info.IsDir() {
				out = append(out, folderChoice{Name: string(c) + ":", Path: root})
			}
		}
		return out
	}
	entries, err := os.ReadDir("/Volumes")
	if err != nil {
		return out
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join("/Volumes", name)
		if isFolder(full, entry) {
			out = append(out, folderChoice{Name: name, Path: full})
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func isFolder(path string, entry os.DirEntry) bool {
	if entry.IsDir() {
		return true
	}
	if entry.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

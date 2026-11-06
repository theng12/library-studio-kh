package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"librarystudio/internal/library"
	"librarystudio/internal/stock"
)

// registerSourceManagement adds the explicit catalog, edit, add, save and test
// routes. The existing guard, sign-in and CSRF header cover all of them; nothing
// here enables, deletes, bulk-edits or synchronizes sources.
func (s *Server) registerSourceManagement(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sources/catalog", func(w http.ResponseWriter, r *http.Request) {
		items, err := library.SourceCatalog(s.Store.Get())
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		if items == nil {
			items = []stock.SourceCatalogEntry{}
		}
		writeJSON(w, 200, map[string]any{"items": items})
	})

	mux.HandleFunc("GET /api/sources/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil || id <= 0 {
			writeJSON(w, 400, map[string]string{"error": "Choose a configured source."})
			return
		}
		item, err := library.SourceEdit(s.Store.Get(), id)
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"item": item})
	})

	mux.HandleFunc("POST /api/sources", func(w http.ResponseWriter, r *http.Request) {
		in, ok := readSourceInput(w, r)
		if !ok {
			return
		}
		in.ID = 0
		id, err := library.AddSource(s.Store.Get(), in)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]int{"id": id})
	})

	mux.HandleFunc("POST /api/sources/save", func(w http.ResponseWriter, r *http.Request) {
		in, ok := readSourceInput(w, r)
		if !ok {
			return
		}
		if in.ID <= 0 {
			writeJSON(w, 400, map[string]string{"error": "Choose a configured source."})
			return
		}
		if err := library.SaveSource(s.Store.Get(), in); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/sources/test", func(w http.ResponseWriter, r *http.Request) {
		in, ok := readSourceInput(w, r)
		if !ok {
			return
		}
		if err := library.TestSource(s.Store.Get(), in); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
}

func readSourceInput(w http.ResponseWriter, r *http.Request) (stock.SourceInput, bool) {
	var in stock.SourceInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "Those source settings could not be read."})
		return stock.SourceInput{}, false
	}
	return in, true
}

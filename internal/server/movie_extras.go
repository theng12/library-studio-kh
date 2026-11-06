package server

import (
	"net/http"
	"strconv"

	"librarystudio/internal/library"
	"librarystudio/internal/stock"
)

// registerMovieExtras adds Radarr's richer film facts and its protected
// MediaCover artwork. Both routes sit behind the existing guard and auth.
func (s *Server) registerMovieExtras(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/media/movie-extras", func(w http.ResponseWriter, r *http.Request) {
		app := r.URL.Query().Get("app")
		id, err := strconv.Atoi(r.URL.Query().Get("id"))
		if app != "Radarr" || err != nil || id <= 0 {
			writeJSON(w, 400, map[string]string{"error": "Choose a Radarr film."})
			return
		}
		x, err := library.MovieExtras(s.Store.Get(), app, id)
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, x)
	})

	mux.HandleFunc("GET /api/media/artwork", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		app, name := q.Get("app"), q.Get("name")
		id, err := strconv.Atoi(q.Get("id"))
		if app != "Radarr" || err != nil || id <= 0 || !stock.ArtworkName(name) {
			writeJSON(w, 400, map[string]string{"error": "That artwork is not available."})
			return
		}
		b, ct, err := library.MovieArtwork(s.Store.Get(), app, id, name)
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		w.WriteHeader(200)
		w.Write(b)
	})
}

package stock

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxArtworkBytes bounds what Radarr's MediaCover path may send back. A poster
// is normally well under 1 MB; anything larger is refused rather than buffered.
const maxArtworkBytes = 8 << 20

// allowedArtwork is the complete set of MediaCover file names this app asks
// Radarr for. The browser never supplies a path or a URL.
var allowedArtwork = map[string]bool{"poster.jpg": true, "fanart.jpg": true}

// MovieExtras is the normalized artwork and collection picture for one Radarr
// film. Absent facts stay empty or nil and are never guessed.
type MovieExtras struct {
	App            string           `json:"app"`
	ID             int              `json:"id"`
	Title          string           `json:"title"`
	TMDBID         int              `json:"tmdbId,omitempty"`
	IMDbID         string           `json:"imdbId,omitempty"`
	Studio         string           `json:"studio,omitempty"`
	Certification  string           `json:"certification,omitempty"`
	RuntimeMinutes *int             `json:"runtimeMinutes,omitempty"`
	Ratings        []Rating         `json:"ratings"`
	Poster         bool             `json:"poster"`
	Collection     *MovieCollection `json:"collection,omitempty"`
	CatalogKnown   bool             `json:"catalogKnown"`
	Problems       []string         `json:"problems"`
}

type Rating struct {
	Source string  `json:"source"`
	Value  float64 `json:"value"`
	Votes  *int    `json:"votes,omitempty"`
}

type MovieCollection struct {
	TMDBID      int                `json:"tmdbId"`
	Title       string             `json:"title"`
	Overview    string             `json:"overview,omitempty"`
	Monitored   *bool              `json:"monitored,omitempty"`
	OwnedCount  int                `json:"ownedCount"`
	NoFileCount int                `json:"noFileCount"`
	Missing     int                `json:"missingCount"`
	Members     []CollectionMember `json:"members"`
}

// CollectionMember joins Radarr's collection entry to its own movie list. Only
// the join proves a file is available; isExisting alone does not.
type CollectionMember struct {
	TMDBID         int    `json:"tmdbId"`
	Title          string `json:"title"`
	Year           *int   `json:"year,omitempty"`
	Status         string `json:"status,omitempty"`
	ReleaseDate    string `json:"releaseDate,omitempty"`
	Overview       string `json:"overview,omitempty"`
	RuntimeMinutes *int   `json:"runtimeMinutes,omitempty"`
	IsCurrent      bool   `json:"isCurrent"`
	IsExcluded     bool   `json:"isExcluded"`
	InLibrary      bool   `json:"inLibrary"`
	LibraryID      int    `json:"libraryId,omitempty"`
	HaveFile       *bool  `json:"haveFile,omitempty"` // nil = not known
}

type rawMovieExtras struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	TMDBID        int    `json:"tmdbId"`
	IMDbID        string `json:"imdbId"`
	Studio        string `json:"studio"`
	Certification string `json:"certification"`
	Runtime       *int   `json:"runtime"`
	Images        []struct {
		CoverType string `json:"coverType"`
	} `json:"images"`
	Ratings    map[string]rawRating `json:"ratings"`
	Collection *struct {
		Title  string `json:"title"`
		TMDBID int    `json:"tmdbId"`
	} `json:"collection"`
}

type rawRating struct {
	Value float64 `json:"value"`
	Votes int     `json:"votes"`
}

type rawCollection struct {
	TMDBID    int    `json:"tmdbId"`
	Title     string `json:"title"`
	Overview  string `json:"overview"`
	Monitored bool   `json:"monitored"`
	Movies    []struct {
		TMDBID          int     `json:"tmdbId"`
		Title           string  `json:"title"`
		Year            *int    `json:"year"`
		Status          string  `json:"status"`
		Overview        string  `json:"overview"`
		Runtime         *int    `json:"runtime"`
		IsCurrent       bool    `json:"isCurrent"`
		IsExisting      bool    `json:"isExisting"`
		IsExcluded      bool    `json:"isExcluded"`
		DigitalRelease  *string `json:"digitalRelease"`
		PhysicalRelease *string `json:"physicalRelease"`
		InCinemas       *string `json:"inCinemas"`
	} `json:"movies"`
}

type rawCatalogMovie struct {
	ID      int   `json:"id"`
	TMDBID  int   `json:"tmdbId"`
	HasFile *bool `json:"hasFile"`
}

// catalogEntry is one validated row. Rows without an id, tmdbId or hasFile are
// refused as an incomplete list rather than guessed at.
type catalogEntry struct {
	ID      int
	HasFile bool
}

var ratingSources = []struct{ key, label string }{
	{"imdb", "IMDb"}, {"tmdb", "TMDb"}, {"rottenTomatoes", "Rotten Tomatoes"},
	{"metacritic", "Metacritic"}, {"trakt", "Trakt"},
}

// MovieExtras reads one Radarr film's artwork facts and, when it belongs to a
// collection, the collection's members joined to Radarr's own movie list.
func (a *Arr) MovieExtras(id int) (MovieExtras, error) {
	x := MovieExtras{App: a.Name, ID: id, Ratings: []Rating{}, Problems: []string{}}
	if a.Name != "Radarr" {
		return x, errors.New("movie extras are available for Radarr films")
	}
	if id <= 0 {
		return x, errors.New("choose a film")
	}
	var m rawMovieExtras
	if err := a.do("GET", "/movie/"+strconv.Itoa(id), nil, &m); err != nil {
		return x, err
	}
	x.Title, x.TMDBID, x.IMDbID = m.Title, m.TMDBID, m.IMDbID
	x.Studio, x.Certification, x.RuntimeMinutes = m.Studio, m.Certification, m.Runtime
	for _, img := range m.Images {
		if img.CoverType == "poster" {
			x.Poster = true
			break
		}
	}
	for _, src := range ratingSources {
		if r, ok := m.Ratings[src.key]; ok && r.Value > 0 {
			rating := Rating{Source: src.label, Value: r.Value}
			if r.Votes > 0 {
				votes := r.Votes
				rating.Votes = &votes
			}
			x.Ratings = append(x.Ratings, rating)
		}
	}
	if m.Collection == nil || m.Collection.TMDBID <= 0 {
		return x, nil
	}
	var cols []rawCollection
	if err := a.do("GET", "/collection", nil, &cols); err != nil {
		x.Problems = append(x.Problems, "Radarr's collection list could not be read.")
		return x, nil
	}
	var coll *rawCollection
	for i := range cols {
		if cols[i].TMDBID == m.Collection.TMDBID {
			coll = &cols[i]
			break
		}
	}
	if coll == nil {
		x.Problems = append(x.Problems, "Radarr didn't report this film's collection.")
		return x, nil
	}
	catalog, catalogErr := a.movieCatalog()
	if catalogErr != nil {
		x.Problems = append(x.Problems, "Radarr's movie list could not be read, so own and missing states are unknown.")
	} else {
		x.CatalogKnown = true
	}
	out := MovieCollection{TMDBID: coll.TMDBID, Title: coll.Title, Overview: coll.Overview, Members: []CollectionMember{}}
	monitored := coll.Monitored
	out.Monitored = &monitored
	for _, cm := range coll.Movies {
		member := CollectionMember{
			TMDBID: cm.TMDBID, Title: cm.Title, Year: cm.Year, Status: cm.Status,
			Overview: cm.Overview, RuntimeMinutes: cm.Runtime,
			IsCurrent:   cm.IsCurrent || cm.TMDBID == x.TMDBID,
			IsExcluded:  cm.IsExcluded,
			ReleaseDate: firstDate(cm.DigitalRelease, cm.PhysicalRelease, cm.InCinemas),
		}
		if cm.TMDBID > 0 {
			if entry, ok := catalog[cm.TMDBID]; ok && x.CatalogKnown {
				member.InLibrary, member.LibraryID, member.HaveFile = true, entry.ID, &entry.HasFile
				switch {
				case entry.HasFile:
					out.OwnedCount++
				default:
					out.NoFileCount++
				}
			} else if x.CatalogKnown {
				missing := false
				member.HaveFile = &missing
				out.Missing++
			}
		}
		out.Members = append(out.Members, member)
	}
	// Members keep Radarr's order; the UI orders sequels by year for display.
	x.Collection = &out
	return x, nil
}

// movieCatalog maps tmdbId to the library id and file flag Radarr reports. A
// nil or incomplete list is an error so callers never read it as "all missing".
func (a *Arr) movieCatalog() (map[int]catalogEntry, error) {
	var raw json.RawMessage
	if err := a.do("GET", "/movie", nil, &raw); err != nil {
		return nil, err
	}
	var rows []rawCatalogMovie
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	if rows == nil { // JSON null is not an empty, valid library
		return nil, errors.New("Radarr sent no movie list")
	}
	out := make(map[int]catalogEntry, len(rows))
	for _, m := range rows {
		if m.ID <= 0 || m.TMDBID <= 0 || m.HasFile == nil {
			return nil, errors.New("Radarr sent an incomplete movie list")
		}
		out[m.TMDBID] = catalogEntry{ID: m.ID, HasFile: *m.HasFile}
	}
	return out, nil
}

func firstDate(values ...*string) string {
	for _, v := range values {
		if v != nil && *v != "" {
			return dateOnly(*v)
		}
	}
	return ""
}

// Artwork reads one allowlisted MediaCover file for a numeric Radarr movie id.
// It never follows redirects and never accepts a browser-supplied URL, so the
// Radarr API key cannot leak through a redirect or be sent to another host.
func (a *Arr) Artwork(id int, name string) ([]byte, string, error) {
	if a.Name != "Radarr" {
		return nil, "", errors.New("artwork is available for Radarr films")
	}
	name = strings.ToLower(name)
	if id <= 0 || !allowedArtwork[name] {
		return nil, "", errors.New("that artwork is not available")
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequest("GET", a.base+"/MediaCover/"+strconv.Itoa(id)+"/"+name, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("X-Api-Key", a.key)
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("%s is not reachable: %w", a.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, "", fmt.Errorf("%s refused the API key", a.Name)
	}
	if resp.StatusCode/100 != 2 {
		return nil, "", fmt.Errorf("%s answered %s for artwork", a.Name, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxArtworkBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("%s sent artwork that could not be read", a.Name)
	}
	if len(b) > maxArtworkBytes {
		return nil, "", fmt.Errorf("%s sent artwork larger than %d MB", a.Name, maxArtworkBytes>>20)
	}
	ct := http.DetectContentType(b)
	switch ct {
	case "image/jpeg", "image/png", "image/webp":
		return b, ct, nil
	}
	return nil, "", fmt.Errorf("%s sent something that isn't a supported image", a.Name)
}

// ArtworkName checks a requested name against the fixed allowlist.
func ArtworkName(name string) bool { return allowedArtwork[strings.ToLower(name)] }

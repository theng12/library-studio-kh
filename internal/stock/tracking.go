package stock

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Detail is the full picture of one library item and its children.
type Detail struct {
	App              string    `json:"app"`
	ID               int       `json:"id"`
	Title            string    `json:"title"`
	Monitored        bool      `json:"monitored"`
	HaveFiles        bool      `json:"haveFiles"`
	Status           string    `json:"status,omitempty"`
	Path             string    `json:"path,omitempty"`
	QualityProfileID int       `json:"qualityProfileId"`
	Seasons          []Season  `json:"seasons"`
	Episodes         []Episode `json:"episodes"`
	Albums           []Album   `json:"albums"`
}

type Season struct {
	SeasonNumber int  `json:"seasonNumber"`
	Monitored    bool `json:"monitored"`
}

type Episode struct {
	ID            int    `json:"id"`
	SeasonNumber  int    `json:"seasonNumber"`
	EpisodeNumber int    `json:"episodeNumber"`
	Title         string `json:"title"`
	Monitored     bool   `json:"monitored"`
	HaveFiles     bool   `json:"haveFiles"`
	When          string `json:"when,omitempty"`
}

type Album struct {
	ID             int    `json:"id"`
	Title          string `json:"title"`
	Monitored      bool   `json:"monitored"`
	HaveFiles      bool   `json:"haveFiles"`
	When           string `json:"when,omitempty"`
	TrackCount     *int   `json:"trackCount,omitempty"`
	TrackFileCount *int   `json:"trackFileCount,omitempty"`
}

type rawDetail struct {
	Title            string `json:"title"`
	ArtistName       string `json:"artistName"`
	Monitored        bool   `json:"monitored"`
	Status           string `json:"status"`
	Path             string `json:"path"`
	HasFile          bool   `json:"hasFile"`
	QualityProfileID int    `json:"qualityProfileId"`
	Statistics       *struct {
		EpisodeFileCount int `json:"episodeFileCount"`
		TrackFileCount   int `json:"trackFileCount"`
	} `json:"statistics"`
	Seasons []struct {
		SeasonNumber int  `json:"seasonNumber"`
		Monitored    bool `json:"monitored"`
	} `json:"seasons"`
}

// Detail reads one item plus its children. It never searches for releases.
func (a *Arr) Detail(id int) (Detail, error) {
	d := Detail{App: a.Name, ID: id, Seasons: []Season{}, Episodes: []Episode{}, Albums: []Album{}}
	if id <= 0 {
		return d, errors.New("choose a library item")
	}
	path := "/" + kinds[a.Name] + "/" + strconv.Itoa(id)
	var r rawDetail
	if err := a.do("GET", path, nil, &r); err != nil {
		return d, err
	}
	d.Title = r.Title
	if r.ArtistName != "" {
		d.Title = r.ArtistName
	}
	d.Monitored, d.Status, d.Path, d.QualityProfileID = r.Monitored, r.Status, r.Path, r.QualityProfileID
	d.HaveFiles = r.HasFile
	if s := r.Statistics; s != nil && s.EpisodeFileCount+s.TrackFileCount > 0 {
		d.HaveFiles = true
	}
	for _, se := range r.Seasons {
		d.Seasons = append(d.Seasons, Season{SeasonNumber: se.SeasonNumber, Monitored: se.Monitored})
	}
	switch a.Name {
	case "Sonarr":
		var eps []struct {
			ID            int    `json:"id"`
			SeasonNumber  int    `json:"seasonNumber"`
			EpisodeNumber int    `json:"episodeNumber"`
			Title         string `json:"title"`
			Monitored     bool   `json:"monitored"`
			HasFile       bool   `json:"hasFile"`
			AirDate       string `json:"airDate"`
		}
		if err := a.do("GET", "/episode?seriesId="+strconv.Itoa(id), nil, &eps); err != nil {
			return d, err
		}
		for _, e := range eps {
			d.Episodes = append(d.Episodes, Episode{ID: e.ID, SeasonNumber: e.SeasonNumber, EpisodeNumber: e.EpisodeNumber, Title: e.Title, Monitored: e.Monitored, HaveFiles: e.HasFile, When: dateOnly(e.AirDate)})
		}
	case "Lidarr":
		var als []struct {
			ID        int    `json:"id"`
			Title     string `json:"title"`
			Monitored bool   `json:"monitored"`
			When      string `json:"releaseDate"`
			Stats     *struct {
				TrackCount     int `json:"trackCount"`
				TrackFileCount int `json:"trackFileCount"`
			} `json:"statistics"`
		}
		if err := a.do("GET", "/album?artistId="+strconv.Itoa(id), nil, &als); err != nil {
			return d, err
		}
		for _, al := range als {
			album := Album{ID: al.ID, Title: al.Title, Monitored: al.Monitored, When: dateOnly(al.When)}
			if al.Stats != nil {
				album.TrackCount, album.TrackFileCount = &al.Stats.TrackCount, &al.Stats.TrackFileCount
				album.HaveFiles = al.Stats.TrackFileCount > 0
			}
			d.Albums = append(d.Albums, album)
		}
	}
	return d, nil
}

func dateOnly(s string) string {
	if i := strings.IndexByte(s, 'T'); i >= 0 {
		return s[:i]
	}
	return s
}

// SetTracking changes monitoring of one child of item id, after verifying the
// child belongs to that parent. It never creates or deletes media.
// kind is "season"/"episode" (Sonarr, seasonNumber may be 0) or "album" (Lidarr).
func (a *Arr) SetTracking(id int, kind string, childID, seasonNumber int, monitored bool) error {
	if id <= 0 {
		return errors.New("choose a library item")
	}
	switch a.Name {
	case "Sonarr":
		switch kind {
		case "season":
			return a.setSeasonMonitored(id, seasonNumber, monitored)
		case "episode":
			if childID <= 0 {
				return errors.New("choose an episode")
			}
			ep, err := a.episode(childID)
			if err != nil {
				return err
			}
			if ep.SeriesID != id {
				return fmt.Errorf("that episode isn't part of series %d", id)
			}
			return a.do("PUT", "/episode/monitor", map[string]any{"episodeIds": []int{childID}, "monitored": monitored}, nil)
		}
	case "Lidarr":
		if kind != "album" {
			return errors.New("Lidarr only tracks albums")
		}
		if childID <= 0 {
			return errors.New("choose an album")
		}
		if err := a.albumBelongs(childID, id); err != nil {
			return err
		}
		return a.do("PUT", "/album/monitor", map[string]any{"albumIds": []int{childID}, "monitored": monitored}, nil)
	default:
		return errors.New(a.Name + " has no child tracking")
	}
	return errors.New("choose a season or an episode to track")
}

// episode reads one episode by id; the app verifies the id exists.
func (a *Arr) episode(childID int) (struct {
	SeriesID int `json:"seriesId"`
}, error) {
	var eps []struct {
		SeriesID int `json:"seriesId"`
	}
	if err := a.do("GET", "/episode?episodeIds="+strconv.Itoa(childID), nil, &eps); err != nil {
		var zero struct {
			SeriesID int `json:"seriesId"`
		}
		return zero, err
	}
	if len(eps) == 0 {
		var zero struct {
			SeriesID int `json:"seriesId"`
		}
		return zero, fmt.Errorf("%s has no episode %d", a.Name, childID)
	}
	return eps[0], nil
}

// albumBelongs confirms an album is part of the given artist.
func (a *Arr) albumBelongs(childID, artistID int) error {
	var als []struct {
		ID int `json:"id"`
	}
	if err := a.do("GET", "/album?artistId="+strconv.Itoa(artistID), nil, &als); err != nil {
		return err
	}
	for _, al := range als {
		if al.ID == childID {
			return nil
		}
	}
	return fmt.Errorf("that album isn't part of artist %d", artistID)
}

// setSeasonMonitored PUTs the series back with only the chosen season's
// monitored flag changed, so every other upstream field is preserved.
func (a *Arr) setSeasonMonitored(id, seasonNumber int, monitored bool) error {
	path := "/series/" + strconv.Itoa(id)
	var raw map[string]any
	if err := a.do("GET", path, nil, &raw); err != nil {
		return err
	}
	seasons, _ := raw["seasons"].([]any)
	for _, sv := range seasons {
		se, ok := sv.(map[string]any)
		if !ok {
			continue
		}
		if n, ok := se["seasonNumber"].(float64); ok && int(n) == seasonNumber {
			se["monitored"] = monitored
			return a.do("PUT", path, raw, nil)
		}
	}
	return fmt.Errorf("that series has no season %d", seasonNumber)
}

// ReleaseContext names the movie, episode or album a release search is for.
type ReleaseContext struct {
	ID        int // the movie, series or artist
	EpisodeID int // Sonarr only
	AlbumID   int // Lidarr only
}

// Release is the sanitized form shown to the owner: no guid, URLs or credentials.
type Release struct {
	Key        string   `json:"key"`
	Title      string   `json:"title"`
	Indexer    string   `json:"indexer,omitempty"`
	Quality    string   `json:"quality,omitempty"`
	Protocol   string   `json:"protocol,omitempty"`
	Size       *int64   `json:"size,omitempty"`
	Seeders    *int     `json:"seeders,omitempty"`
	AgeMinutes *int     `json:"ageMinutes,omitempty"`
	Rejected   bool     `json:"rejected"`
	Rejections []string `json:"rejections"`
}

type rawRelease struct {
	GUID      string `json:"guid"`
	IndexerID int    `json:"indexerId"`
	Title     string `json:"title"`
	Indexer   string `json:"indexer"`
	Quality   *struct {
		Quality struct {
			Name string `json:"name"`
		} `json:"quality"`
	} `json:"quality"`
	Protocol        json.RawMessage `json:"protocol"`
	Size            *int64          `json:"size"`
	Seeders         *int            `json:"seeders"`
	AgeMinutes      *float64        `json:"ageMinutes"`
	Rejected        bool            `json:"rejected"`
	Rejections      []string        `json:"rejections"`
	DownloadAllowed bool            `json:"downloadAllowed"`
	Raw             json.RawMessage
}

// Releases deliberately searches the app for releases in the given context.
func (a *Arr) Releases(ctx ReleaseContext) ([]Release, error) {
	raws, err := a.fetchReleases(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Release, 0, len(raws))
	for _, r := range raws {
		out = append(out, r.release(a.Name, ctx))
	}
	return out, nil
}

// Grab re-fetches the releases for the context, matches the opaque key and
// sends the matched upstream release to the same app, which applies its own
// profiles and download clients. Rejected or disallowed results are refused.
func (a *Arr) Grab(ctx ReleaseContext, key string) error {
	if key == "" {
		return errors.New("choose a release to grab")
	}
	raws, err := a.fetchReleases(ctx)
	if err != nil {
		return err
	}
	for _, r := range raws {
		if releaseKey(a.Name, ctx, r) != key {
			continue
		}
		if r.Rejected || !r.DownloadAllowed || len(r.Rejections) > 0 {
			return errors.New("that release was refused by " + a.Name + "; choose an approved one")
		}
		return a.do("POST", "/release", r.Raw, nil)
	}
	return errors.New("that release is no longer available; search again")
}

// releaseKey is an opaque digest over the context, indexer and guid.
func releaseKey(app string, ctx ReleaseContext, r rawRelease) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		app, strconv.Itoa(ctx.ID), strconv.Itoa(ctx.EpisodeID), strconv.Itoa(ctx.AlbumID),
		strconv.Itoa(r.IndexerID), r.GUID,
	}, "|")))
	return hex.EncodeToString(sum[:])
}

func (r rawRelease) release(app string, ctx ReleaseContext) Release {
	rel := Release{Key: releaseKey(app, ctx, r), Title: r.Title, Indexer: r.Indexer, Rejected: r.Rejected || !r.DownloadAllowed || len(r.Rejections) > 0, Rejections: r.Rejections}
	if rel.Rejections == nil {
		rel.Rejections = []string{}
	}
	if r.Quality != nil {
		rel.Quality = r.Quality.Quality.Name
	}
	var protocolNumber int
	var protocolName string
	if json.Unmarshal(r.Protocol, &protocolNumber) == nil {
		rel.Protocol = map[int]string{1: "usenet", 2: "torrent"}[protocolNumber]
	} else if json.Unmarshal(r.Protocol, &protocolName) == nil {
		switch strings.ToLower(protocolName) {
		case "torrent", "usenet":
			rel.Protocol = strings.ToLower(protocolName)
		}
	}
	rel.Size, rel.Seeders = r.Size, r.Seeders
	if r.AgeMinutes != nil {
		mins := int(*r.AgeMinutes)
		rel.AgeMinutes = &mins
	}
	return rel
}

// fetchReleases verifies the context against the parent, then searches the
// app. Detail reads never call this.
func (a *Arr) fetchReleases(ctx ReleaseContext) ([]rawRelease, error) {
	if ctx.ID <= 0 {
		return nil, errors.New("choose a library item")
	}
	var raws []json.RawMessage
	var err error
	switch a.Name {
	case "Sonarr":
		if ctx.EpisodeID <= 0 {
			return nil, errors.New("choose an episode")
		}
		ep, err := a.episode(ctx.EpisodeID)
		if err != nil {
			return nil, err
		}
		if ep.SeriesID != ctx.ID {
			return nil, fmt.Errorf("that episode isn't part of series %d", ctx.ID)
		}
		err = a.do("GET", "/release?seriesId="+strconv.Itoa(ctx.ID)+"&episodeId="+strconv.Itoa(ctx.EpisodeID), nil, &raws)
	case "Radarr":
		var m struct {
			ID int `json:"id"`
		}
		if ge := a.do("GET", "/movie/"+strconv.Itoa(ctx.ID), nil, &m); ge != nil {
			return nil, ge
		}
		err = a.do("GET", "/release?movieId="+strconv.Itoa(ctx.ID), nil, &raws)
	case "Lidarr":
		if ctx.AlbumID <= 0 {
			return nil, errors.New("choose an album")
		}
		if be := a.albumBelongs(ctx.AlbumID, ctx.ID); be != nil {
			return nil, be
		}
		err = a.do("GET", "/release?albumId="+strconv.Itoa(ctx.AlbumID), nil, &raws)
	default:
		return nil, errors.New(a.Name + " has no release search")
	}
	if err != nil {
		return nil, err
	}
	out := make([]rawRelease, 0, len(raws))
	for _, b := range raws {
		var r rawRelease
		if json.Unmarshal(b, &r) != nil {
			return nil, fmt.Errorf("%s returned a release that could not be read; check its status and search again", a.Name)
		}
		r.Raw = b
		out = append(out, r)
	}
	return out, nil
}

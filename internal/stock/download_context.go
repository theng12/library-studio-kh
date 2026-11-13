package stock

import (
	"encoding/json"
	"fmt"
	"strings"
)

// QueueEntry is one row of a library app's official download queue: the
// download client's ID (a torrent hash under qBittorrent) and the parent
// media ID (series/movie/artist) that asked for it. MediaID is 0 when the
// row names no parent; callers must never invent one.
type QueueEntry struct {
	ID         int // the app's own queue row id, for its queue removal API
	DownloadID string
	MediaID    int
}

// maxQueuePages bounds a run whose totalRecords never comes true, so a
// lying or page-ignoring server cannot loop forever (100 x 1000 rows).
const maxQueuePages = 100

// QueueEntries reads every page of the app's download queue, driven by the
// app's own totalRecords/page/pageSize. A malformed page, a run that stops
// before totalRecords, or one that never completes is an error rather than
// a silently short list.
func (a *Arr) QueueEntries() ([]QueueEntry, error) {
	const pageSize = 1000
	out := make([]QueueEntry, 0, 64)
	read := 0
	for page := 1; page <= maxQueuePages; page++ {
		var p struct {
			Page         int               `json:"page"`
			TotalRecords *int              `json:"totalRecords"`
			Records      []json.RawMessage `json:"records"`
		}
		if err := a.do("GET", fmt.Sprintf("/queue?page=%d&pageSize=%d", page, pageSize), nil, &p); err != nil {
			return nil, err
		}
		if p.TotalRecords == nil || *p.TotalRecords < 0 || p.Records == nil || (p.Page != 0 && p.Page != page) {
			return nil, fmt.Errorf("%s returned a malformed queue page", a.Name)
		}
		for _, raw := range p.Records {
			e, err := queueEntry(a.Name, raw)
			if err != nil {
				return nil, fmt.Errorf("%s returned a malformed queue entry", a.Name)
			}
			if e.DownloadID != "" {
				out = append(out, e)
			}
		}
		read += len(p.Records)
		if read >= *p.TotalRecords {
			return out, nil
		}
		if len(p.Records) == 0 {
			return nil, fmt.Errorf("%s queue pagination stopped early", a.Name)
		}
	}
	return nil, fmt.Errorf("%s queue pagination did not advance within the %d-page safety limit", a.Name, maxQueuePages)
}

// queueEntry pulls the download ID and the parent media ID from one queue
// record. Sonarr, Radarr and Lidarr name the parent differently; some
// versions embed it (series/movie/artist) instead of flattening it.
func queueEntry(app string, raw json.RawMessage) (QueueEntry, error) {
	var r struct {
		ID         int    `json:"id"`
		DownloadID string `json:"downloadId"`
		SeriesID   int    `json:"seriesId"`
		MovieID    int    `json:"movieId"`
		ArtistID   int    `json:"artistId"`
		Series     *struct {
			ID int `json:"id"`
		} `json:"series"`
		Movie *struct {
			ID int `json:"id"`
		} `json:"movie"`
		Artist *struct {
			ID int `json:"id"`
		} `json:"artist"`
	}
	if string(raw) == "null" {
		return QueueEntry{}, fmt.Errorf("null queue entry")
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return QueueEntry{}, err
	}
	var id int
	switch app {
	case "Radarr":
		if id = r.MovieID; id == 0 && r.Movie != nil {
			id = r.Movie.ID
		}
	case "Lidarr":
		if id = r.ArtistID; id == 0 && r.Artist != nil {
			id = r.Artist.ID
		}
	default: // Sonarr
		if id = r.SeriesID; id == 0 && r.Series != nil {
			id = r.Series.ID
		}
	}
	if id < 0 {
		id = 0
	}
	return QueueEntry{ID: r.ID, DownloadID: strings.ToUpper(r.DownloadID), MediaID: id}, nil
}

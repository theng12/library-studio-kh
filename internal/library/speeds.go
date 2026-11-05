package library

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"librarystudio/internal/config"
	"librarystudio/internal/stock"
)

// qBittorrent keeps no speed history, so this app samples download speed while it is open
// and keeps a small summary per torrent (not every sample) in speeds.json.

const (
	SampleEvery = 15 * time.Second
	speedKeep   = 500 // torrents remembered, newest first
)

// Speed bands in bytes per second; time spent in each is shown on the details screen.
var bands = []struct {
	Label string
	Below int64
}{
	{"Stalled (0)", 1},
	{"Under 1 MB/s", 1e6},
	{"1–5 MB/s", 5e6},
	{"5–20 MB/s", 20e6},
	{"20 MB/s or more", 1 << 62},
}

type speedRec struct {
	First, Last time.Time
	Seconds     float64   // time measured while downloading
	Bytes       float64   // speed × time, for the measured average
	Min, Max    int64     // Min is the lowest speed above 0
	Bands       []float64 // seconds per band
}

type Band struct {
	Label   string  `json:"label"`
	Seconds float64 `json:"seconds"`
}

type Stats struct {
	Added      *time.Time `json:"added,omitempty"` // nil = qBittorrent didn't say
	Completed  *time.Time `json:"completed,omitempty"`
	Size       int64      `json:"size"`
	Downloaded int64      `json:"downloaded"`
	Uploaded   int64      `json:"uploaded"`
	Ratio      float64    `json:"ratio"`
	Seeds      int        `json:"seeds"`
	Peers      int        `json:"peers"`
	DlSpeed    int64      `json:"dlSpeed"`
	UpSpeed    int64      `json:"upSpeed"`
	ETA        int64      `json:"eta,omitempty"` // seconds, only while downloading
	State      string     `json:"state"`
	SavePath   string     `json:"savePath,omitempty"`
	Category   string     `json:"category,omitempty"`
	// From our own samples; Measured 0 means nothing was recorded for this download.
	Measured float64 `json:"measured"`
	AvgSpeed int64   `json:"avgMeasured,omitempty"`
	MinSpeed int64   `json:"minSpeed,omitempty"`
	MaxSpeed int64   `json:"maxSpeed,omitempty"`
	Bands    []Band  `json:"bands,omitempty"`
}

var speedMu sync.Mutex

func speedPath(home string) string { return filepath.Join(home, "speeds.json") }

func readSpeeds(home string) map[string]*speedRec {
	m := map[string]*speedRec{}
	if b, err := os.ReadFile(speedPath(home)); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

// Sample records one reading for every torrent that is still downloading.
func Sample(c config.Config, home string, now time.Time) error {
	if c.QBit.URL == "" {
		return nil
	}
	ts, err := stock.NewQBit(c.QBit.URL, c.QBit.Username, c.QBit.Password).Torrents()
	if err != nil {
		return err
	}
	speedMu.Lock()
	defer speedMu.Unlock()
	m := readSpeeds(home)
	changed := false
	for _, t := range ts {
		if t.Progress >= 1 || !downloading[t.State] {
			continue
		}
		r := m[t.Hash]
		if r == nil {
			r = &speedRec{First: now, Bands: make([]float64, len(bands))}
			m[t.Hash] = r
		}
		// A gap longer than two samples (app closed, computer asleep) isn't counted as measured time.
		dt := SampleEvery.Seconds()
		if !r.Last.IsZero() {
			if gap := now.Sub(r.Last).Seconds(); gap < 2*SampleEvery.Seconds() {
				dt = gap
			}
		}
		r.Last, r.Seconds, r.Bytes = now, r.Seconds+dt, r.Bytes+float64(t.DlSpeed)*dt
		if t.DlSpeed > 0 && (r.Min == 0 || t.DlSpeed < r.Min) {
			r.Min = t.DlSpeed
		}
		if t.DlSpeed > r.Max {
			r.Max = t.DlSpeed
		}
		for i, b := range bands {
			if t.DlSpeed < b.Below {
				r.Bands[i] += dt
				break
			}
		}
		changed = true
	}
	if !changed {
		return nil
	}
	if len(m) > speedKeep {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return m[keys[i]].Last.After(m[keys[j]].Last) })
		for _, k := range keys[speedKeep:] {
			delete(m, k)
		}
	}
	b, _ := json.Marshal(m)
	_ = os.MkdirAll(home, 0o700)
	tmp := speedPath(home) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, speedPath(home))
}

// qBittorrent states that mean the torrent is trying to download (stalledDL counts: speed 0 is real).
var downloading = map[string]bool{"downloading": true, "stalledDL": true, "metaDL": true, "forcedDL": true, "forcedMetaDL": true}

func unix(s int64) *time.Time {
	if s <= 0 {
		return nil
	}
	t := time.Unix(s, 0)
	return &t
}

func statsOf(t stock.Torrent, r *speedRec) *Stats {
	s := &Stats{
		Added: unix(t.AddedOn), Completed: unix(t.CompletionOn), Size: t.Size, Downloaded: t.Downloaded, Uploaded: t.Uploaded,
		Ratio: t.Ratio, Seeds: t.Seeds, Peers: t.Peers, DlSpeed: t.DlSpeed, UpSpeed: t.UpSpeed, State: t.State,
		SavePath: t.SavePath, Category: t.Category,
	}
	if t.Progress < 1 && t.ETA > 0 && t.ETA < 8640000 { // qBittorrent uses 8640000 for "unknown"
		s.ETA = t.ETA
	}
	if r != nil && r.Seconds > 0 {
		s.Measured, s.AvgSpeed, s.MinSpeed, s.MaxSpeed = r.Seconds, int64(r.Bytes/r.Seconds), r.Min, r.Max
		for i, b := range bands {
			if i < len(r.Bands) && r.Bands[i] > 0 {
				s.Bands = append(s.Bands, Band{b.Label, r.Bands[i]})
			}
		}
	}
	return s
}

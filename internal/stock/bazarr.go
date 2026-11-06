package stock

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// App values accepted by the Bazarr methods.
const (
	AppRadarr = "radarr"
	AppSonarr = "sonarr"
)

// Bazarr is a small client for Bazarr's own /api. Only allowlisted fields are
// decoded, so provider settings, credentials and paths never reach callers.
type Bazarr struct {
	base, key string
	c         *http.Client
}

func NewBazarr(base, key string) *Bazarr {
	return &Bazarr{base: strings.TrimRight(base, "/"), key: key, c: &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

type SubtitleProfile struct {
	ID        int      `json:"id"`
	Name      string   `json:"name"`
	Languages []string `json:"languages"`
}

type SubtitleLanguage struct {
	Name   string `json:"name"`
	Code   string `json:"code"`
	Forced bool   `json:"forced"`
	HI     bool   `json:"hi"`
}

type SubtitleMedia struct {
	App            string             `json:"app"`
	ParentID       int                `json:"parentId"`
	ID             int                `json:"id"`
	Title          string             `json:"title"`
	ProfileID      int                `json:"profileId"`
	AudioLanguage  *string            `json:"audioLanguage,omitempty"`
	AudioLanguages []string           `json:"audioLanguages,omitempty"`
	Existing       []SubtitleLanguage `json:"existing"`
	Missing        []SubtitleLanguage `json:"missing"`
	// FileReported means Bazarr reports a media path. It is not a filesystem check.
	FileReported bool `json:"fileReported"`
	Season       int  `json:"season,omitempty"`
	Episode      int  `json:"episode,omitempty"`
}

type bazarrLanguage struct {
	Name   string `json:"name"`
	Code2  string `json:"code2"`
	Forced bool   `json:"forced"`
	HI     bool   `json:"hi"`
}

type bazarrAudio struct {
	Name string `json:"name"`
}

// bazarrAudioLanguages accepts Bazarr 1.6's list and older single-object rows.
type bazarrAudioLanguages []bazarrAudio

func (l *bazarrAudioLanguages) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*l = nil
		return nil
	}
	if data[0] == '{' {
		var one bazarrAudio
		if err := json.Unmarshal(data, &one); err != nil {
			return fmt.Errorf("audio_language: %w", err)
		}
		*l = bazarrAudioLanguages{one}
		return nil
	}
	var list []bazarrAudio
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("audio_language is not a language object or list: %w", err)
	}
	*l = list
	return nil
}

func (l bazarrAudioLanguages) names() []string {
	names := make([]string, 0, len(l))
	for _, a := range l {
		if a.Name != "" {
			names = append(names, a.Name)
		}
	}
	return names
}

type bazarrMovie struct {
	RadarrID      int                  `json:"radarrId"`
	Title         string               `json:"title"`
	ProfileID     int                  `json:"profileId"`
	Path          string               `json:"path"`
	AudioLanguage bazarrAudioLanguages `json:"audio_language"`
	Missing       []bazarrLanguage     `json:"missing_subtitles"`
	Subtitles     []bazarrLanguage     `json:"subtitles"`
}

type bazarrEpisode struct {
	SonarrSeriesID  int                  `json:"sonarrSeriesId"`
	SonarrEpisodeID int                  `json:"sonarrEpisodeId"`
	Season          int                  `json:"season"`
	Episode         int                  `json:"episode"`
	Title           string               `json:"title"`
	ProfileID       int                  `json:"profileId"`
	Path            string               `json:"path"`
	AudioLanguage   bazarrAudioLanguages `json:"audio_language"`
	Missing         []bazarrLanguage     `json:"missing_subtitles"`
	Subtitles       []bazarrLanguage     `json:"subtitles"`
}

type bazarrProfile struct {
	ProfileID int    `json:"profileId"`
	Name      string `json:"name"`
	Items     []struct {
		Language string `json:"language"`
	} `json:"items"`
}

type bazarrEnvelope[T any] struct {
	Data []T `json:"data"`
}

func (b *Bazarr) do(method, path string, form url.Values, out any) error {
	var rd io.Reader
	if form != nil {
		rd = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, b.base+"/api"+path, rd)
	if err != nil {
		return errors.New("Bazarr request could not be built")
	}
	req.Header.Set("X-API-KEY", b.key)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := b.c.Do(req)
	if err != nil {
		return errors.New("Bazarr is not reachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Bazarr answered HTTP %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out); err != nil {
		return errors.New("Bazarr response is unreadable or exceeds the 8 MiB read limit")
	}
	return nil
}

// Status returns only Bazarr's reported version.
func (b *Bazarr) Status() (string, error) {
	var raw struct {
		Data struct {
			BazarrVersion string `json:"bazarr_version"`
		} `json:"data"`
	}
	if err := b.do(http.MethodGet, "/system/status", nil, &raw); err != nil {
		return "", err
	}
	if raw.Data.BazarrVersion == "" {
		return "", errors.New("Bazarr did not report a version")
	}
	return raw.Data.BazarrVersion, nil
}

// Profiles lists Bazarr language profiles with their language codes only.
func (b *Bazarr) Profiles() ([]SubtitleProfile, error) {
	var raw []bazarrProfile
	if err := b.do(http.MethodGet, "/system/languages/profiles", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]SubtitleProfile, 0, len(raw))
	for _, p := range raw {
		langs := make([]string, 0, len(p.Items))
		for _, item := range p.Items {
			if item.Language != "" {
				langs = append(langs, item.Language)
			}
		}
		out = append(out, SubtitleProfile{ID: p.ProfileID, Name: p.Name, Languages: langs})
	}
	return out, nil
}

// Subtitles reads Bazarr's view of one Radarr movie or one Sonarr series.
// Records belonging to another parent are refused.
func (b *Bazarr) Subtitles(app string, parentID int) ([]SubtitleMedia, error) {
	if parentID <= 0 {
		return nil, errors.New("choose a film or series")
	}
	app = strings.ToLower(strings.TrimSpace(app))
	out := []SubtitleMedia{}
	switch app {
	case AppRadarr:
		var raw bazarrEnvelope[bazarrMovie]
		if err := b.do(http.MethodGet, "/movies?radarrid[]="+strconv.Itoa(parentID), nil, &raw); err != nil {
			return nil, err
		}
		for _, m := range raw.Data {
			if m.RadarrID != parentID {
				return nil, errors.New("Bazarr returned a movie for another parent")
			}
			out = append(out, subtitleMedia(app, parentID, m.RadarrID, m.Title, m.ProfileID, m.Path,
				m.AudioLanguage, m.Missing, m.Subtitles, 0, 0))
		}
	case AppSonarr:
		var raw bazarrEnvelope[bazarrEpisode]
		if err := b.do(http.MethodGet, "/episodes?seriesid[]="+strconv.Itoa(parentID), nil, &raw); err != nil {
			return nil, err
		}
		for _, e := range raw.Data {
			if e.SonarrSeriesID != parentID {
				return nil, errors.New("Bazarr returned an episode for another series")
			}
			out = append(out, subtitleMedia(app, parentID, e.SonarrEpisodeID, e.Title, e.ProfileID, e.Path,
				e.AudioLanguage, e.Missing, e.Subtitles, e.Season, e.Episode))
		}
	default:
		return nil, errors.New("unknown library app")
	}
	return out, nil
}

func subtitleMedia(app string, parentID, id int, title string, profileID int, path string,
	audio bazarrAudioLanguages, missing, existing []bazarrLanguage, season, episode int) SubtitleMedia {
	m := SubtitleMedia{
		App:          app,
		ParentID:     parentID,
		ID:           id,
		Title:        title,
		ProfileID:    profileID,
		Existing:     mediaLanguages(existing),
		Missing:      mediaLanguages(missing),
		FileReported: path != "",
		Season:       season,
		Episode:      episode,
	}
	if names := audio.names(); len(names) > 0 {
		m.AudioLanguage = &names[0]
		m.AudioLanguages = names
	}
	return m
}

func mediaLanguages(in []bazarrLanguage) []SubtitleLanguage {
	out := make([]SubtitleLanguage, 0, len(in))
	for _, l := range in {
		out = append(out, SubtitleLanguage{Name: l.Name, Code: l.Code2, Forced: l.Forced, HI: l.HI})
	}
	return out
}

// EnglishSearch asks Bazarr to search and take the best English subtitle for one
// already-indexed item. A nil error only means the job was scheduled (HTTP 204).
func (b *Bazarr) EnglishSearch(app string, parentID, itemID int) error {
	if itemID <= 0 {
		return errors.New("choose a film or episode")
	}
	items, err := b.Subtitles(app, parentID)
	if err != nil {
		return err
	}
	var target *SubtitleMedia
	for i := range items {
		if items[i].ID == itemID {
			target = &items[i]
			break
		}
	}
	if target == nil {
		return errors.New("Bazarr does not report that item")
	}
	if !target.FileReported {
		return errors.New("Bazarr has not reported a media file for that item")
	}
	if target.ProfileID <= 0 {
		return errors.New("that item has no Bazarr language profile")
	}

	app = strings.ToLower(strings.TrimSpace(app))
	form := url.Values{"language": {"en"}, "forced": {"false"}, "hi": {"false"}}
	var path string
	switch app {
	case AppRadarr:
		form.Set("radarrid", strconv.Itoa(parentID))
		path = "/movies/subtitles"
	case AppSonarr:
		form.Set("seriesid", strconv.Itoa(parentID))
		form.Set("episodeid", strconv.Itoa(itemID))
		path = "/episodes/subtitles"
	default:
		return errors.New("unknown library app")
	}
	return b.do(http.MethodPatch, path, form, nil)
}

// AssignEnglishProfile sets a movie or series profile, but only when the
// requested Bazarr profile contains English and nothing else.
func (b *Bazarr) AssignEnglishProfile(app string, parentID, profileID int) error {
	if parentID <= 0 || profileID <= 0 {
		return errors.New("invalid Bazarr profile")
	}
	profiles, err := b.Profiles()
	if err != nil {
		return err
	}
	found := false
	for _, p := range profiles {
		if p.ID != profileID {
			continue
		}
		found = true
		if len(p.Languages) == 0 {
			return errors.New("that Bazarr profile has no languages")
		}
		for _, lang := range p.Languages {
			if !strings.EqualFold(lang, "en") {
				return errors.New("that Bazarr profile is not English-only")
			}
		}
	}
	if !found {
		return errors.New("Bazarr profile not found")
	}

	app = strings.ToLower(strings.TrimSpace(app))
	form := url.Values{"profileid": {strconv.Itoa(profileID)}}
	var path string
	switch app {
	case AppRadarr:
		form.Set("radarrid", strconv.Itoa(parentID))
		path = "/movies"
	case AppSonarr:
		form.Set("seriesid", strconv.Itoa(parentID))
		path = "/series"
	default:
		return errors.New("unknown library app")
	}
	return b.do(http.MethodPost, path, form, nil)
}

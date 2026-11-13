// Package metadata fills poster, overview and facts from keyless public sources
// (iTunes Search for films and TV, TVmaze for series). Results are cached on disk
// and a missing or offline provider yields a placeholder, never a blocked list.
package metadata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Info struct {
	PosterPath     string   `json:"posterPath,omitempty"`
	Overview       string   `json:"overview,omitempty"`
	Year           int      `json:"year,omitempty"`
	Genres         []string `json:"genres,omitempty"`
	RuntimeMinutes int      `json:"runtimeMinutes,omitempty"`
	Rating         float64  `json:"rating,omitempty"`
	Source         string   `json:"source,omitempty"` // itunes | tvmaze | cache | placeholder
}

type Client struct {
	Dir  string // cache root, e.g. <config home>/metadata
	HTTP *http.Client
	Now  func() time.Time

	mu       sync.Mutex
	lastCall time.Time

	// Overridden by tests.
	iTunesBase string
	tvmazeBase string
}

const minGap = 300 * time.Millisecond

func (c *Client) enabled() bool { return c != nil && c.Dir != "" }

func (c *Client) itunes() string {
	if c.iTunesBase != "" {
		return c.iTunesBase
	}
	return "https://itunes.apple.com"
}

func (c *Client) tvmaze() string {
	if c.tvmazeBase != "" {
		return c.tvmazeBase
	}
	return "https://api.tvmaze.com"
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 6 * time.Second}
}

// Lookup returns cached facts when present; otherwise it asks the keyless
// providers once and caches the outcome. Errors become a placeholder.
func (c *Client) Lookup(ctx context.Context, kind, title string, year int) Info {
	if !c.enabled() || strings.TrimSpace(title) == "" {
		return Info{Source: "placeholder"}
	}
	if info, ok := c.Cached(kind, title, year); ok {
		return info
	}
	key := cacheKey(kind, title, year)
	info := Info{Source: "placeholder"}
	var err error
	if kind == "series" {
		info, err = c.fromTVmaze(ctx, title)
		if err != nil {
			info, _ = c.fromITunes(ctx, title, "tvShow")
		}
	} else {
		info, err = c.fromITunes(ctx, title, "movie")
		if err != nil {
			info, _ = c.fromTVmaze(ctx, title)
		}
	}
	if err != nil || info.Source == "" {
		info = Info{Source: "placeholder"}
	}
	c.writeCache(key, info)
	return info
}

// Cached reads only the disk cache and never touches the network, so request
// paths stay fast offline. A nil or unconfigured client always misses.
func (c *Client) Cached(kind, title string, year int) (Info, bool) {
	if !c.enabled() || strings.TrimSpace(title) == "" {
		return Info{}, false
	}
	info, ok := c.readCache(cacheKey(kind, title, year))
	if ok {
		info.Source = "cache"
	}
	return info, ok
}

// Poster returns a cached file path for the remote artwork, downloading it once.
func (c *Client) Poster(ctx context.Context, remote string) string {
	if !c.enabled() || remote == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(remote))
	name := hex.EncodeToString(sum[:8]) + ".jpg"
	path := filepath.Join(c.Dir, "posters", name)
	if _, err := os.Stat(path); err == nil {
		return path
	}
	if err := c.wait(ctx); err != nil {
		return ""
	}
	resp, err := c.http().Get(remote)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return ""
	}
	defer resp.Body.Close()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return ""
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return ""
	}
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 8<<20)); err != nil {
		f.Close()
		os.Remove(tmp)
		return ""
	}
	f.Close()
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return ""
	}
	return path
}

func (c *Client) wait(ctx context.Context) error {
	c.mu.Lock()
	gap := minGap - time.Since(c.lastCall)
	c.lastCall = time.Now()
	c.mu.Unlock()
	if gap <= 0 {
		return ctx.Err()
	}
	select {
	case <-time.After(gap):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) fromITunes(ctx context.Context, title, media string) (Info, error) {
	if err := c.wait(ctx); err != nil {
		return Info{}, err
	}
	query := url.Values{"term": {title}, "media": {media}, "limit": {"1"}, "country": {"US"}}
	var answer struct {
		Results []struct {
			Artwork     string `json:"artworkUrl100"`
			Description string `json:"longDescription"`
			Short       string `json:"shortDescription"`
			Release     string `json:"releaseDate"`
			Genre       string `json:"primaryGenreName"`
			Millis      int    `json:"trackTimeMillis"`
		} `json:"results"`
	}
	if err := c.getJSON(ctx, c.itunes()+"/search?"+query.Encode(), &answer); err != nil {
		return Info{}, err
	}
	if len(answer.Results) == 0 {
		return Info{}, fmt.Errorf("itunes: no result")
	}
	hit := answer.Results[0]
	info := Info{Source: "itunes", Overview: strings.TrimSpace(first(hit.Description, hit.Short))}
	if len(hit.Release) >= 4 {
		fmt.Sscanf(hit.Release[:4], "%d", &info.Year)
	}
	if hit.Genre != "" {
		info.Genres = []string{hit.Genre}
	}
	if hit.Millis > 0 {
		info.RuntimeMinutes = hit.Millis / 60000
	}
	if hit.Artwork != "" {
		info.PosterPath = c.Poster(ctx, strings.Replace(hit.Artwork, "100x100", "600x600", 1))
	}
	return info, nil
}

func (c *Client) fromTVmaze(ctx context.Context, title string) (Info, error) {
	if err := c.wait(ctx); err != nil {
		return Info{}, err
	}
	var show struct {
		Image   *struct{ Original string } `json:"image"`
		Summary string                     `json:"summary"`
		Premier string                     `json:"premiered"`
		Genres  []string                   `json:"genres"`
		Average int                        `json:"averageRuntime"`
		Rating  struct{ Average float64 }  `json:"rating"`
	}
	if err := c.getJSON(ctx, c.tvmaze()+"/singlesearch/shows?q="+url.QueryEscape(title), &show); err != nil {
		return Info{}, err
	}
	info := Info{Source: "tvmaze", Overview: stripHTML(show.Summary), Genres: show.Genres, RuntimeMinutes: show.Average, Rating: show.Rating.Average}
	if len(show.Premier) >= 4 {
		fmt.Sscanf(show.Premier[:4], "%d", &info.Year)
	}
	if show.Image != nil {
		info.PosterPath = c.Poster(ctx, show.Image.Original)
	}
	return info, nil
}

func (c *Client) getJSON(ctx context.Context, address string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", address, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(into)
}

func cacheKey(kind, title string, year int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", kind, strings.ToLower(title), year)))
	return hex.EncodeToString(sum[:16])
}

func (c *Client) readCache(key string) (Info, bool) {
	b, err := os.ReadFile(filepath.Join(c.Dir, "cache", key+".json"))
	if err != nil {
		return Info{}, false
	}
	var info Info
	if json.Unmarshal(b, &info) != nil {
		return Info{}, false
	}
	return info, true
}

func (c *Client) writeCache(key string, info Info) {
	dir := filepath.Join(c.Dir, "cache")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	b, err := json.Marshal(info)
	if err != nil {
		return
	}
	path := filepath.Join(dir, key+".json")
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, path)
	}
}

var (
	htmlTag  = regexp.MustCompile(`<[^>]+>`)
	spaceRun = regexp.MustCompile(`\s+`)
)

func stripHTML(text string) string {
	return strings.TrimSpace(spaceRun.ReplaceAllString(html.UnescapeString(htmlTag.ReplaceAllString(text, " ")), " "))
}

func first(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

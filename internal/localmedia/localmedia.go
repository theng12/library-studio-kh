// Package localmedia lists video files from a folder the owner chose and derives
// clean titles from file names or Kodi .nfo sidecars. Metadata comes only from
// the metadata cache here; nothing in this package talks to a *arr app.
package localmedia

import (
	"encoding/xml"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// LocalItem is one video file as the local media page shows it.
type LocalItem struct {
	Path           string   `json:"path"`
	Title          string   `json:"title"`
	Year           int      `json:"year,omitempty"`
	RuntimeMinutes int      `json:"runtimeMinutes,omitempty"`
	Overview       string   `json:"overview,omitempty"`
	Genres         []string `json:"genres,omitempty"`
	Rating         float64  `json:"rating,omitempty"`
	// PosterPath is a bare file name under the metadata poster folder, so the
	// page can hand it to the server without exposing an absolute path.
	PosterPath string `json:"posterPath,omitempty"`
	// Source is where title details came from: filename, nfo, manual or ffprobe.
	Source string `json:"source,omitempty"`
	Manual bool   `json:"manual,omitempty"`
}

var videoExts = map[string]bool{
	".mkv": true, ".mp4": true, ".m4v": true, ".avi": true,
	".mov": true, ".ts": true, ".webm": true,
}

var (
	yearBracket = regexp.MustCompile(`[\(\[](\d{4})[\)\]]`)
	bracketTag  = regexp.MustCompile(`[\(\[]([^\)\]]*)[\)\]]`)
	yearToken   = regexp.MustCompile(`^(19|20)\d{2}$`)
)

// releaseTags are the tokens a scene name puts after the title. A match cuts
// the name there, which also drops a trailing release group.
var releaseTags = map[string]bool{
	"480p": true, "576p": true, "720p": true, "1080i": true, "1080p": true, "2160p": true,
	"4k": true, "8k": true, "uhd": true, "hdr": true, "hdr10": true, "dv": true, "dolby": true,
	"vision": true, "sdr": true, "10bit": true, "8bit": true, "60fps": true, "50fps": true, "24fps": true,
	"x264": true, "x265": true, "h264": true, "h265": true, "hevc": true, "avc": true,
	"xvid": true, "divx": true, "av1": true, "vp9": true,
	"web": true, "webrip": true, "webdl": true, "bluray": true, "bdrip": true, "brrip": true,
	"dvdrip": true, "dvd": true, "hdrip": true, "hdtv": true, "remux": true, "bdremux": true,
	"hdcam": true, "cam": true, "screener": true, "scr": true,
	"aac": true, "ac3": true, "eac3": true, "dts": true, "dtshd": true, "truehd": true,
	"flac": true, "mp3": true, "opus": true, "atmos": true, "dd": true, "ddp": true,
	"ddp5": true, "dd5": true, "lpcm": true, "pcm": true,
	"proper": true, "repack": true, "extended": true, "uncut": true, "unrated": true,
	"limited": true, "internal": true, "readnfo": true, "nfofix": true, "dirfix": true,
}

// deriveName turns a file name into a title and year. Bracketed years win;
// otherwise the last year-like token with a title in front of it does.
func deriveName(name string) (string, int) {
	base := strings.TrimSpace(name)
	if ext := filepath.Ext(base); videoExts[strings.ToLower(ext)] {
		base = strings.TrimSuffix(base, ext)
	}
	name = base
	year := 0
	if m := yearBracket.FindStringSubmatchIndex(name); m != nil {
		year, _ = strconv.Atoi(name[m[2]:m[3]])
		name = name[:m[0]] // everything after the year is release tags
	}
	name = bracketTag.ReplaceAllString(name, " ")
	name = strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(name)
	tokens := strings.Fields(name)
	if year == 0 {
		for i := len(tokens) - 1; i > 0; i-- {
			if yearToken.MatchString(tokens[i]) {
				year, _ = strconv.Atoi(tokens[i])
				tokens = tokens[:i]
				break
			}
		}
	}
	for i, token := range tokens {
		if releaseTags[strings.ToLower(strings.Trim(token, "[](){}.,"))] {
			tokens = tokens[:i]
			break
		}
	}
	title := strings.Join(tokens, " ")
	if title == "" {
		title = strings.TrimSpace(base)
	}
	return title, year
}

// nfoData is the Kodi movie sidecar subset local media shows.
type nfoData struct {
	Title   string   `xml:"title"`
	Year    int      `xml:"year"`
	Plot    string   `xml:"plot"`
	Genres  []string `xml:"genre"`
	Runtime int      `xml:"runtime"`
	Rating  string   `xml:"rating"`
}

func (n *nfoData) empty() bool {
	return n.Title == "" && n.Year == 0 && n.Plot == "" && len(n.Genres) == 0 && n.Runtime == 0 && n.Rating == ""
}

// nfoFor reads a video's sidecar: the same-named .nfo, then movie.nfo in its
// folder. A missing sidecar is silent; a broken one becomes a readable problem.
func nfoFor(videoPath string) (*nfoData, string) {
	dir := filepath.Dir(videoPath)
	stem := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	for _, name := range []string{stem + ".nfo", "movie.nfo"} {
		path := filepath.Join(dir, name)
		b, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Sprintf("%s: %s", path, err)
		}
		var data nfoData
		if err := xml.Unmarshal(b, &data); err != nil {
			return nil, fmt.Sprintf("%s: %s", path, err)
		}
		if data.empty() {
			return nil, fmt.Sprintf("%s: no title, year or facts", path)
		}
		return &data, ""
	}
	return nil, ""
}

func applyNFO(item *LocalItem, n *nfoData) {
	if title := strings.TrimSpace(n.Title); title != "" {
		item.Title = title
	}
	if n.Year > 0 {
		item.Year = n.Year
	}
	if plot := strings.TrimSpace(n.Plot); plot != "" {
		item.Overview = plot
	}
	genres := genresOf(n.Genres)
	if len(genres) > 0 {
		item.Genres = genres
	}
	if n.Runtime > 0 {
		item.RuntimeMinutes = n.Runtime
	}
	if rating, err := strconv.ParseFloat(strings.TrimSpace(n.Rating), 64); err == nil && rating > 0 {
		item.Rating = rating
	}
}

func genresOf(values []string) []string {
	var genres []string
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			genres = append(genres, value)
		}
	}
	return genres
}

// Scan lists the video files under root. Hidden and underscore folders are
// skipped, file problems come back as readable strings, and the list sorts by
// path so the page is stable between scans.
func Scan(root string) ([]LocalItem, []string) {
	var items []LocalItem
	var problems []string
	if strings.TrimSpace(root) == "" {
		return nil, []string{"local media folder is not set"}
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, []string{fmt.Sprintf("local media folder %s: %s", root, err)}
	}
	if !info.IsDir() {
		return nil, []string{fmt.Sprintf("local media folder is not a folder: %s", root)}
	}
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %s", path, err))
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if path == root {
				return nil
			}
			name := entry.Name()
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return fs.SkipDir
			}
			return nil
		}
		if !videoExts[strings.ToLower(filepath.Ext(entry.Name()))] {
			return nil
		}
		title, year := deriveName(entry.Name())
		item := LocalItem{Path: path, Title: title, Year: year, Source: "filename"}
		if data, problem := nfoFor(path); problem != "" {
			problems = append(problems, problem)
		} else if data != nil {
			applyNFO(&item, data)
			item.Source = "nfo"
		}
		items = append(items, item)
		return nil
	})
	if walkErr != nil {
		problems = append(problems, fmt.Sprintf("%s: %s", root, walkErr))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items, problems
}

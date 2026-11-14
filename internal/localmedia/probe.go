package localmedia

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ErrFFprobeMissing means the optional ffprobe tool is not on PATH. Callers
// fall back to the filename rather than blocking the list.
var ErrFFprobeMissing = errors.New("ffprobe is not installed")

// Probe asks ffprobe for format tags, duration and nothing else. The filename
// derivation stays the fallback whenever a tag is absent.
func Probe(ctx context.Context, path string) (LocalItem, error) {
	item := LocalItem{Path: path}
	if abs, err := filepath.Abs(path); err == nil {
		item.Path = abs
	}
	title, year := deriveName(filepath.Base(item.Path))
	item.Title, item.Year, item.Source = title, year, "filename"

	bin, err := exec.LookPath("ffprobe")
	if err != nil {
		return item, ErrFFprobeMissing
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-v", "quiet", "-print_format", "json", "-show_format", item.Path).Output()
	if err != nil {
		return item, fmt.Errorf("ffprobe %s: %w", item.Path, err)
	}
	var probe struct {
		Format struct {
			Duration string            `json:"duration"`
			Tags     map[string]string `json:"tags"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &probe); err != nil {
		return item, fmt.Errorf("ffprobe %s: %w", item.Path, err)
	}
	tags := lowerKeys(probe.Format.Tags)
	if value := strings.TrimSpace(tags["title"]); value != "" {
		item.Title, item.Source = value, "ffprobe"
	}
	if value := strings.TrimSpace(tags["genre"]); value != "" {
		if genres := splitList(value); len(genres) > 0 {
			item.Genres, item.Source = genres, "ffprobe"
		}
	}
	if value := strings.TrimSpace(tags["year"]); len(value) >= 4 {
		if y, err := strconv.Atoi(value[:4]); err == nil {
			item.Year, item.Source = y, "ffprobe"
		}
	} else if value := strings.TrimSpace(tags["date"]); len(value) >= 4 {
		if y, err := strconv.Atoi(value[:4]); err == nil {
			item.Year, item.Source = y, "ffprobe"
		}
	}
	if seconds, err := strconv.ParseFloat(probe.Format.Duration, 64); err == nil && seconds > 0 {
		item.RuntimeMinutes = int(math.Round(seconds / 60))
	}
	return item, nil
}

func lowerKeys(tags map[string]string) map[string]string {
	lowered := make(map[string]string, len(tags))
	for key, value := range tags {
		lowered[strings.ToLower(key)] = value
	}
	return lowered
}

func splitList(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == ',' || r == ';' })
	var list []string
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			list = append(list, part)
		}
	}
	return list
}

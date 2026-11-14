package localmedia

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeriveName(t *testing.T) {
	cases := []struct {
		name  string
		title string
		year  int
	}{
		{"Inception.2010.1080p.BluRay.x264-AMIABLE.mkv", "Inception", 2010},
		{"the_matrix_1999_720p.mkv", "the matrix", 1999},
		{"Spider-Man.No.Way.Home.2021.2160p.WEB-DL.DDP5.1.HDR.HEVC-GROUP.mkv", "Spider Man No Way Home", 2021},
		{"Movie Name (2019).mkv", "Movie Name", 2019},
		{"Movie Name [2019] 1080p.mkv", "Movie Name", 2019},
		{"2001.A.Space.Odyssey.1968.1080p.mkv", "2001 A Space Odyssey", 1968},
		{"1917.2019.1080p.BluRay.mkv", "1917", 2019},
		{"1917.mkv", "1917", 0},
		{"holiday.video.1080p.WEBRip.mkv", "holiday video", 0},
		{"Blade.Runner.2049.2017.REMUX.mkv", "Blade Runner 2049", 2017},
		{"Episode.One.mov", "Episode One", 0},
		{"That Thing You Do!.1996.mkv", "That Thing You Do!", 1996},
	}
	for _, c := range cases {
		title, year := deriveName(c.name)
		if title != c.title || year != c.year {
			t.Errorf("deriveName(%q) = %q, %d; want %q, %d", c.name, title, year, c.title, c.year)
		}
	}
}

const validNFO = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<movie>
  <title>Example Film</title>
  <year>2024</year>
  <plot>A test plot.</plot>
  <genre>Drama</genre>
  <genre>Science Fiction</genre>
  <runtime>121</runtime>
  <rating>7.8</rating>
</movie>`

func TestNFOFor(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "Example Film (2024).mkv")
	if err := os.WriteFile(filepath.Join(dir, "Example Film (2024).nfo"), []byte(validNFO), 0o600); err != nil {
		t.Fatal(err)
	}
	data, problem := nfoFor(video)
	if problem != "" {
		t.Fatalf("nfoFor problem: %s", problem)
	}
	if data == nil {
		t.Fatal("nfoFor returned no data")
	}
	if data.Title != "Example Film" || data.Year != 2024 || data.Plot != "A test plot." {
		t.Errorf("nfo facts = %+v", data)
	}
	if len(data.Genres) != 2 || data.Genres[0] != "Drama" || data.Genres[1] != "Science Fiction" {
		t.Errorf("nfo genres = %v", data.Genres)
	}
	if data.Runtime != 121 || data.Rating != "7.8" {
		t.Errorf("nfo runtime/rating = %d/%q", data.Runtime, data.Rating)
	}
}

func TestNFOForMalformed(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "Broken (2020).mkv")
	if err := os.WriteFile(filepath.Join(dir, "Broken (2020).nfo"), []byte("<movie><title>Broken</plot>"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, problem := nfoFor(video)
	if data != nil {
		t.Errorf("malformed nfo returned data: %+v", data)
	}
	if !strings.Contains(problem, "Broken (2020).nfo") {
		t.Errorf("problem does not name the sidecar: %q", problem)
	}

	empty := filepath.Join(dir, "Empty (2020).mkv")
	if err := os.WriteFile(filepath.Join(dir, "Empty (2020).nfo"), []byte("<movie></movie>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, problem := nfoFor(empty); !strings.Contains(problem, "no title, year or facts") {
		t.Errorf("empty nfo problem = %q", problem)
	}
}

func TestScan(t *testing.T) {
	root := t.TempDir()
	write := func(path, body string) {
		t.Helper()
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("Films/Example Film (2024)/Example Film (2024) 2160p.mkv", "video")
	write("Films/Example Film (2024)/Example Film (2024) 2160p.nfo", validNFO)
	write("Films/loose_movie_1999.avi", "video")
	write("Shows/Episode.One.mov", "video")
	write(".hidden/secret.mkv", "video")
	write("_staging/temp.mp4", "video")
	write("notes.txt", "not a video")
	write("Broken (2020).mkv", "video")
	write("Broken (2020).nfo", "<movie><title>Broken</plot>")

	items, problems := Scan(root)
	if len(items) != 4 {
		t.Fatalf("Scan returned %d items, want 4: %+v", len(items), items)
	}
	byName := map[string]LocalItem{}
	for _, item := range items {
		byName[filepath.Base(item.Path)] = item
		if strings.Contains(item.Path, ".hidden") || strings.Contains(item.Path, "_staging") {
			t.Errorf("ignored folder leaked into scan: %s", item.Path)
		}
	}
	for i := 1; i < len(items); i++ {
		if items[i-1].Path > items[i].Path {
			t.Errorf("items are not sorted by path: %q before %q", items[i-1].Path, items[i].Path)
		}
	}
	film := byName["Example Film (2024) 2160p.mkv"]
	if film.Title != "Example Film" || film.Year != 2024 || film.Source != "nfo" {
		t.Errorf("nfo item = %+v", film)
	}
	if film.Overview != "A test plot." || len(film.Genres) != 2 || film.RuntimeMinutes != 121 || film.Rating != 7.8 {
		t.Errorf("nfo facts not applied: %+v", film)
	}
	loose := byName["loose_movie_1999.avi"]
	if loose.Title != "loose movie" || loose.Year != 1999 || loose.Source != "filename" {
		t.Errorf("filename item = %+v", loose)
	}
	if broken := byName["Broken (2020).mkv"]; broken.Source != "filename" || broken.Title != "Broken" || broken.Year != 2020 {
		t.Errorf("malformed-nfo item did not fall back to the filename: %+v", broken)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "Broken (2020).nfo") {
		t.Errorf("problems = %v", problems)
	}
}

func TestScanUnsetAndMissingFolder(t *testing.T) {
	if items, problems := Scan(""); items != nil || len(problems) != 1 {
		t.Errorf("Scan(empty) = %v, %v", items, problems)
	}
	if items, problems := Scan(filepath.Join(t.TempDir(), "missing")); items != nil || len(problems) != 1 {
		t.Errorf("Scan(missing) = %v, %v", items, problems)
	}
}

func TestProbeMissingFFprobe(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // nothing named ffprobe exists here
	path := filepath.Join(t.TempDir(), "Film (2020).mkv")
	item, err := Probe(context.Background(), path)
	if !errors.Is(err, ErrFFprobeMissing) {
		t.Fatalf("Probe error = %v, want ErrFFprobeMissing", err)
	}
	if item.Title != "Film" || item.Year != 2020 || item.Source != "filename" {
		t.Errorf("fallback item = %+v", item)
	}
	if !filepath.IsAbs(item.Path) {
		t.Errorf("path is not absolute: %q", item.Path)
	}
}

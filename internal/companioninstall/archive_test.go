package companioninstall

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func zipFixture(t *testing.T, entries []struct {
	name string
	mode os.FileMode
	body string
}) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, entry := range entries {
		h := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		h.SetMode(entry.mode)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestZipRejectsTraversalAbsoluteAndBackslash(t *testing.T) {
	cases := []string{"../escape.txt", "/tmp/absolute.txt", `..\windows.txt`, "Sonarr.app/../../escape.txt"}
	for _, name := range cases {
		zipPath := zipFixture(t, []struct {
			name string
			mode os.FileMode
			body string
		}{{name, 0o644, "escape"}})
		stage := t.TempDir()
		_, err := extractZipBundle(zipPath, stage, 1<<20)
		if err == nil {
			t.Errorf("entry %q was extracted", name)
		}
		outside := filepath.Join(filepath.Dir(stage), "escape.txt")
		if _, statErr := os.Stat(outside); statErr == nil {
			t.Errorf("entry %q wrote outside the stage", name)
		}
	}
}

func TestZipRejectsSymlink(t *testing.T) {
	zipPath := zipFixture(t, []struct {
		name string
		mode os.FileMode
		body string
	}{{"Sonarr.app/Contents/MacOS/link", os.ModeSymlink | 0o777, "../../../../tmp/escape"}})
	_, err := extractZipBundle(zipPath, t.TempDir(), 1<<20)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("got %v, want a symlink refusal", err)
	}
}

func TestZipRejectsOversizedExpansion(t *testing.T) {
	zipPath := zipFixture(t, []struct {
		name string
		mode os.FileMode
		body string
	}{{"Sonarr.app/Contents/Resources/data", 0o644, strings.Repeat("x", 2048)}})
	_, err := extractZipBundle(zipPath, t.TempDir(), 1024)
	if err == nil || !strings.Contains(err.Error(), "cap") {
		t.Fatalf("got %v, want a size cap refusal", err)
	}
}

func TestZipExpansionCapCoversVendorCompression(t *testing.T) {
	if got := zipExpansionCap(64 << 20); got < 167<<20 {
		t.Fatalf("expansion cap %d is too small for a published Servarr zip", got)
	}
}

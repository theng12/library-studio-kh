package companioninstall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// realTarRunner calls /usr/bin/tar so extractBinaryArchive is proved against
// the actual bsdtar on this Mac, not a mock that assumes its behaviour.
type realTarRunner struct{}

func (realTarRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}

// craftTarXz builds a .tar.xz at dest containing exactly the entries listed.
// It uses /usr/bin/tar directly so the fixture is what bsdtar actually reads.
func craftTarXz(t *testing.T, dest string, entries []struct {
	name    string
	body    []byte
	mode    os.FileMode
	symlink string
}) {
	t.Helper()
	dir := t.TempDir()
	for _, e := range entries {
		full := filepath.Join(dir, e.name)
		if e.symlink != "" {
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(e.symlink, full); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, e.body, e.mode); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command("/usr/bin/tar", "-cJf", dest, "-C", dir, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("craft tar.xz: %v\n%s", err, out)
	}
}

func binaryInstaller(t *testing.T) *Installer {
	t.Helper()
	return New(Options{
		Tar:      "/usr/bin/tar",
		Commands: realTarRunner{},
		Now:      func() time.Time { return time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC) },
	})
}

func recyclarrSpec(t *testing.T) appSpec {
	t.Helper()
	s, err := shared.spec(Recyclarr)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestExtractBinaryArchiveValidExecutable(t *testing.T) {
	spec := recyclarrSpec(t)
	stage := t.TempDir()
	asset := filepath.Join(stage, "recyclarr.tar.xz")
	craftTarXz(t, asset, []struct {
		name    string
		body    []byte
		mode    os.FileMode
		symlink string
	}{{spec.bundleName, thinMachO(cpuArm64), 0o755, ""}})

	i := binaryInstaller(t)
	got, err := i.extractBinaryArchive(context.Background(), asset, stage, spec, 1<<20)
	if err != nil {
		t.Fatalf("valid archive: %v", err)
	}
	if got != filepath.Join(stage, "extract", spec.bundleName) {
		t.Errorf("path = %q", got)
	}
	info, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Error("extracted binary lost execute permission")
	}
}

func TestExtractBinaryArchiveRefusesSymlink(t *testing.T) {
	spec := recyclarrSpec(t)
	stage := t.TempDir()
	sentinel := filepath.Join(stage, "sentinel")
	if err := os.WriteFile(sentinel, []byte("safe"), 0o644); err != nil {
		t.Fatal(err)
	}
	asset := filepath.Join(stage, "bad.tar.xz")
	craftTarXz(t, asset, []struct {
		name    string
		body    []byte
		mode    os.FileMode
		symlink string
	}{
		{spec.bundleName, thinMachO(cpuArm64), 0o755, ""},
		{"evil-link", nil, 0o777, "../../../sentinel"},
	})

	i := binaryInstaller(t)
	_, err := i.extractBinaryArchive(context.Background(), asset, stage, spec, 1<<20)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("got %v, want symlink refusal", err)
	}
	data, _ := os.ReadFile(sentinel)
	if string(data) != "safe" {
		t.Errorf("sentinel was modified: %q", data)
	}
}

func TestExtractBinaryArchiveBsdtarStripsEscapePath(t *testing.T) {
	spec := recyclarrSpec(t)
	stage := t.TempDir()
	asset := filepath.Join(stage, "escape.tar.xz")
	// bsdtar strips leading ".." components, so the entry lands safely inside
	// root as "escape.txt". This proves bsdtar itself is the first line of
	// defence; our walk is the second. Either way nothing escapes.
	craftTarXz(t, asset, []struct {
		name    string
		body    []byte
		mode    os.FileMode
		symlink string
	}{
		{spec.bundleName, thinMachO(cpuArm64), 0o755, ""},
		{"sub/../../../escape.txt", []byte("escaped"), 0o644, ""},
	})

	i := binaryInstaller(t)
	got, err := i.extractBinaryArchive(context.Background(), asset, stage, spec, 1<<20)
	if err != nil {
		t.Fatalf("bsdtar should strip escape paths safely, got %v", err)
	}
	if got != filepath.Join(stage, "extract", spec.bundleName) {
		t.Errorf("path = %q", got)
	}
	outside := filepath.Join(stage, "escape.txt")
	if _, statErr := os.Stat(outside); statErr == nil {
		t.Error("entry escaped the extract root despite bsdtar stripping")
	}
}

func TestExtractBinaryArchiveMissingTarget(t *testing.T) {
	spec := recyclarrSpec(t)
	stage := t.TempDir()
	asset := filepath.Join(stage, "wrong.tar.xz")
	craftTarXz(t, asset, []struct {
		name    string
		body    []byte
		mode    os.FileMode
		symlink string
	}{{"not-recyclarr", thinMachO(cpuArm64), 0o755, ""}})

	i := binaryInstaller(t)
	_, err := i.extractBinaryArchive(context.Background(), asset, stage, spec, 1<<20)
	if err == nil || !strings.Contains(err.Error(), "no top-level executable") {
		t.Fatalf("got %v, want missing target refusal", err)
	}
}

func TestExtractBinaryArchiveNonExecutableTarget(t *testing.T) {
	spec := recyclarrSpec(t)
	stage := t.TempDir()
	asset := filepath.Join(stage, "noexec.tar.xz")
	craftTarXz(t, asset, []struct {
		name    string
		body    []byte
		mode    os.FileMode
		symlink string
	}{{spec.bundleName, thinMachO(cpuArm64), 0o644, ""}})

	i := binaryInstaller(t)
	_, err := i.extractBinaryArchive(context.Background(), asset, stage, spec, 1<<20)
	if err == nil || !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("got %v, want non-executable refusal", err)
	}
}

func TestExtractBinaryArchiveSizeCap(t *testing.T) {
	spec := recyclarrSpec(t)
	stage := t.TempDir()
	asset := filepath.Join(stage, "big.tar.xz")
	// The cap is zipExpansionCap(downloadCap). With downloadCap=64 the cap
	// is ~168 bytes, far below a real Mach-O header + padding.
	craftTarXz(t, asset, []struct {
		name    string
		body    []byte
		mode    os.FileMode
		symlink string
	}{
		{spec.bundleName, thinMachO(cpuArm64), 0o755, ""},
		{"extra.bin", make([]byte, 4096), 0o644, ""},
	})

	i := binaryInstaller(t)
	_, err := i.extractBinaryArchive(context.Background(), asset, stage, spec, 64)
	if err == nil || !strings.Contains(err.Error(), "cap") {
		t.Fatalf("got %v, want size cap refusal", err)
	}
}

func TestDetectBinaryAdoptsArm64WithReceipt(t *testing.T) {
	spec := recyclarrSpec(t)
	appDir := t.TempDir()
	binPath := filepath.Join(appDir, spec.bundleName)
	if err := os.WriteFile(binPath, thinMachO(cpuArm64), 0o755); err != nil {
		t.Fatal(err)
	}
	receiptDir := filepath.Join(appDir, receiptDirName)
	if err := os.MkdirAll(receiptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := Receipt{
		InstalledPath:    binPath,
		InstalledVersion: "7.2.1",
		InstalledAt:      time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC),
	}
	data, _ := json.Marshal(r)
	if err := os.WriteFile(filepath.Join(receiptDir, "recyclarr-1.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	i := binaryInstaller(t)
	got, err := i.detectBinary(spec, appDir)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != DetectAdopt || got.Version != "7.2.1" || got.Path != binPath {
		t.Errorf("detect = %+v", got)
	}
}

func TestDetectBinaryKeepsSymlink(t *testing.T) {
	spec := recyclarrSpec(t)
	appDir := t.TempDir()
	target := filepath.Join(appDir, "real-binary")
	if err := os.WriteFile(target, thinMachO(cpuArm64), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(appDir, spec.bundleName)); err != nil {
		t.Fatal(err)
	}

	i := binaryInstaller(t)
	got, err := i.detectBinary(spec, appDir)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != DetectKeep {
		t.Errorf("symlink should be kept, got %+v", got)
	}
}

func TestDetectBinaryKeepsWrongArchitecture(t *testing.T) {
	spec := recyclarrSpec(t)
	appDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(appDir, spec.bundleName), thinMachO(cpuAMD64), 0o755); err != nil {
		t.Fatal(err)
	}

	i := binaryInstaller(t)
	got, err := i.detectBinary(spec, appDir)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != DetectKeep {
		t.Errorf("amd64 binary should be kept, got %+v", got)
	}
}

func TestResolveGitHubLatestDuplicateAssets(t *testing.T) {
	d := doerFunc(func(req *http.Request) (*http.Response, error) {
		body := fmt.Sprintf(`{"tag_name":"v9.1.0","assets":[`+
			`{"name":"recyclarr-osx-arm64.tar.xz","size":100,"digest":"sha256:%s","browser_download_url":"https://github.com/recyclarr/recyclarr/releases/download/v9.1.0/recyclarr-osx-arm64.tar.xz"},`+
			`{"name":"recyclarr-osx-arm64.tar.xz","size":200,"digest":"sha256:%s","browser_download_url":"https://github.com/recyclarr/recyclarr/releases/download/v9.1.0/recyclarr-osx-arm64.tar.xz"}]}`,
			strings.Repeat("ab", 32), strings.Repeat("cd", 32))
		return response(req, http.StatusOK, body, ""), nil
	})
	i := New(Options{HTTPClient: d})
	_, err := i.Resolve(context.Background(), Recyclarr)
	if err == nil || !strings.Contains(err.Error(), "2 matching assets") {
		t.Fatalf("duplicate assets should fail, got %v", err)
	}
}

func TestResolveGitHubLatestMalformedDigest(t *testing.T) {
	d := doerFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"tag_name":"v9.1.0","assets":[` +
			`{"name":"recyclarr-osx-arm64.tar.xz","size":100,"digest":"sha256:not-hex-at-all","browser_download_url":"https://github.com/recyclarr/recyclarr/releases/download/v9.1.0/recyclarr-osx-arm64.tar.xz"}]}`
		return response(req, http.StatusOK, body, ""), nil
	})
	i := New(Options{HTTPClient: d})
	_, err := i.Resolve(context.Background(), Recyclarr)
	if err == nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("malformed digest should be ErrUnavailable, got %v", err)
	}
}

func TestResolveGitHubLatestZeroSize(t *testing.T) {
	d := doerFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"tag_name":"v9.1.0","assets":[` +
			`{"name":"recyclarr-osx-arm64.tar.xz","size":0,"digest":"sha256:` + strings.Repeat("ab", 32) + `","browser_download_url":"https://github.com/recyclarr/recyclarr/releases/download/v9.1.0/recyclarr-osx-arm64.tar.xz"}]}`
		return response(req, http.StatusOK, body, ""), nil
	})
	i := New(Options{HTTPClient: d})
	_, err := i.Resolve(context.Background(), Recyclarr)
	if err == nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("zero size should be ErrUnavailable, got %v", err)
	}
}
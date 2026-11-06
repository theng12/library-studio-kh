package companioninstall

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

func response(req *http.Request, status int, body, location string) *http.Response {
	h := http.Header{}
	if location != "" {
		h.Set("Location", location)
	}
	return &http.Response{
		StatusCode:    status,
		Header:        h,
		Body:          io.NopCloser(strings.NewReader(body)),
		Request:       req,
		ContentLength: int64(len(body)),
	}
}

func sha(bs []byte) string {
	sum := sha256.Sum256(bs)
	return hex.EncodeToString(sum[:])
}

func writeApp(t *testing.T, dir, bundleID, version, execName string) {
	t.Helper()
	contents := filepath.Join(dir, "Contents")
	macOS := filepath.Join(contents, "MacOS")
	if err := os.MkdirAll(macOS, 0o755); err != nil {
		t.Fatal(err)
	}
	plist, err := json.Marshal(map[string]string{
		"CFBundleIdentifier":         bundleID,
		"CFBundleShortVersionString": version,
		"CFBundleExecutable":         execName,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), plist, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(macOS, execName), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func buildAppZip(t *testing.T, appName, bundleID, version, execName string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	plist, err := json.Marshal(map[string]string{
		"CFBundleIdentifier":         bundleID,
		"CFBundleShortVersionString": version,
		"CFBundleExecutable":         execName,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct {
		name string
		perm os.FileMode
		body []byte
	}{
		{appName + "/Contents/Info.plist", 0o644, plist},
		{appName + "/Contents/MacOS/" + execName, 0o755, []byte("#!/bin/sh\n")},
	} {
		h := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		h.SetMode(entry.perm)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(entry.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type fakeRunner struct {
	t           *testing.T
	attachName  string
	attachID    string
	attachVer   string
	attachExec  string
	plutilCalls int
	attachCalls int
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	switch name {
	case "plutil":
		f.plutilCalls++
		return os.ReadFile(args[len(args)-1])
	case "hdiutil":
		switch {
		case len(args) > 0 && args[0] == "attach":
			f.attachCalls++
			mount := ""
			for n, arg := range args {
				if arg == "-mountpoint" && n+1 < len(args) {
					mount = args[n+1]
				}
			}
			if mount == "" {
				return nil, errors.New("fake hdiutil: no mountpoint")
			}
			writeApp(f.t, filepath.Join(mount, f.attachName), f.attachID, f.attachVer, f.attachExec)
			return []byte("attached"), nil
		case len(args) > 0 && args[0] == "detach":
			return []byte("detached"), nil
		}
		return nil, fmt.Errorf("fake hdiutil: unexpected args %v", args)
	case "ditto":
		if len(args) != 2 {
			return nil, fmt.Errorf("fake ditto: unexpected args %v", args)
		}
		return nil, copyTree(args[0], args[1])
	}
	return nil, fmt.Errorf("fake runner: unexpected tool %q", name)
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

func servarrPackage(t *testing.T, appName, bundleID, version, asset string, body []byte) Package {
	t.Helper()
	return Package{
		AppID:              Sonarr,
		Name:               "Sonarr",
		BundleID:           bundleID,
		NativeArchitecture: "arm64",
		ReleaseVersion:     version,
		OfficialAssetURL:   asset,
		ExpectedSHA256:     sha(body),
		ExpectedBytes:      int64(len(body)),
		SizeCap:            int64(len(body)) + (1 << 20),
		Provenance: Provenance{
			ResolverURL:  "https://services.sonarr.tv/test",
			MetadataURL:  "https://api.github.com/repos/Sonarr/Sonarr/releases/tags/v" + version,
			SHA256Source: "GitHub release asset digest (sha256)",
			ReleaseTag:   "v" + version,
			ResolvedAt:   time.Unix(1_700_000_000, 0),
		},
	}
}

func newTestInstaller(d Doer, r CommandRunner) *Installer {
	return New(Options{
		HTTPClient: d,
		Commands:   r,
		RetryDelay: func(int) time.Duration { return 0 },
		Now:        func() time.Time { return time.Unix(1_700_000_000, 0) },
	})
}

func TestRegistryHasOfficialApps(t *testing.T) {
	apps := Registry()
	if len(apps) != 5 {
		t.Fatalf("got %d apps, want 5", len(apps))
	}
	want := map[AppID]string{
		QBittorrent: "org.qbittorrent.qBittorrent",
		Sonarr:      "com.osx.sonarr.tv",
		Radarr:      "com.osx.radarr.video",
		Lidarr:      "com.osx.lidarr.audio",
		Prowlarr:    "com.osx.prowlarr.com",
	}
	for _, app := range apps {
		if app.BundleID != want[app.ID] {
			t.Errorf("%s bundle = %q, want %q", app.ID, app.BundleID, want[app.ID])
		}
	}
}

func TestResolveServarrUsesLiveAssetDigest(t *testing.T) {
	asset := "Sonarr.main.4.0.20.3014.osx-arm64-app.zip"
	assetURL := "https://github.com/Sonarr/Sonarr/releases/download/v4.0.20.3014/" + asset
	apiURL := "https://api.github.com/repos/Sonarr/Sonarr/releases/tags/v4.0.20.3014"
	release := fmt.Sprintf(`{"tag_name":"v4.0.20.3014","assets":[{"name":%q,"size":64661024,"digest":"sha256:%s","browser_download_url":%q}]}`,
		asset, strings.Repeat("ab", 32), assetURL)
	d := doerFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case specs[1].endpoint:
			return response(req, http.StatusFound, "", assetURL), nil
		case apiURL:
			return response(req, http.StatusOK, release, ""), nil
		}
		return nil, fmt.Errorf("unexpected URL %s", req.URL)
	})

	pkg, err := newTestInstaller(d, &fakeRunner{t: t}).Resolve(context.Background(), Sonarr)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.ReleaseVersion != "4.0.20.3014" || pkg.ExpectedSHA256 != strings.Repeat("ab", 32) ||
		pkg.ExpectedBytes != 64661024 || pkg.OfficialAssetURL != assetURL {
		t.Fatalf("unexpected package: %+v", pkg)
	}
	if pkg.Provenance.MetadataURL != apiURL || pkg.Provenance.SHA256Source == "" {
		t.Fatalf("missing provenance: %+v", pkg.Provenance)
	}
	if pkg.SizeCap <= pkg.ExpectedBytes {
		t.Fatalf("size cap %d does not cover expected %d", pkg.SizeCap, pkg.ExpectedBytes)
	}
}

func TestResolveServarrRefusesMissingDigest(t *testing.T) {
	asset := "Sonarr.main.4.0.20.3014.osx-arm64-app.zip"
	assetURL := "https://github.com/Sonarr/Sonarr/releases/download/v4.0.20.3014/" + asset
	apiURL := "https://api.github.com/repos/Sonarr/Sonarr/releases/tags/v4.0.20.3014"
	release := fmt.Sprintf(`{"tag_name":"v4.0.20.3014","assets":[{"name":%q,"size":10,"digest":null,"browser_download_url":%q}]}`, asset, assetURL)
	d := doerFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case specs[1].endpoint:
			return response(req, http.StatusFound, "", assetURL), nil
		case apiURL:
			return response(req, http.StatusOK, release, ""), nil
		}
		return nil, fmt.Errorf("unexpected URL %s", req.URL)
	})

	_, err := newTestInstaller(d, &fakeRunner{t: t}).Resolve(context.Background(), Sonarr)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v, want ErrUnavailable", err)
	}
}

func TestResolveServarrRefusesX64Asset(t *testing.T) {
	assetURL := "https://github.com/Radarr/Radarr/releases/download/v6.4.4.10685/Radarr.master.6.4.4.10685.osx-x64-app.zip"
	d := doerFunc(func(req *http.Request) (*http.Response, error) {
		return response(req, http.StatusFound, "", assetURL), nil
	})
	_, err := newTestInstaller(d, &fakeRunner{t: t}).Resolve(context.Background(), Radarr)
	if err == nil || !strings.Contains(err.Error(), "arm64") {
		t.Fatalf("got %v, want an arm64-only refusal", err)
	}
}

func TestResolveQBittorrentFromOfficialPage(t *testing.T) {
	const standardSHA = "9e37f6c7ff848c7bdd3c10167614c0cb78c00e2ddcc323f1ad3ac6c008a0481f"
	page := `
<a href="https://sourceforge.net/projects/qbittorrent/files/qbittorrent-mac/qbittorrent-5.2.3/qbittorrent-5.2.3.dmg/download">SourceForge</a>
<a href="https://sourceforge.net/projects/qbittorrent/files/qbittorrent-mac/qbittorrent-5.2.3/qbittorrent-5.2.3_lt20.dmg/download">SourceForge</a>
<td>5.2.3</td>
<td class="wordBreak"><code>` + standardSHA + `</code></td>
<td>5.2.3 (qt6 lt20)</td>
<td class="wordBreak"><code>4dde4f357cf8d7271f7e25140d2986d076126566edf2d2ad441cdd382b2a333d</code></td>`
	d := doerFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != specs[0].endpoint {
			return nil, fmt.Errorf("unexpected URL %s", req.URL)
		}
		return response(req, http.StatusOK, page, ""), nil
	})

	pkg, err := newTestInstaller(d, &fakeRunner{t: t}).Resolve(context.Background(), QBittorrent)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.ReleaseVersion != "5.2.3" || pkg.ExpectedSHA256 != standardSHA {
		t.Fatalf("unexpected package: %+v", pkg)
	}
	if strings.Contains(pkg.OfficialAssetURL, "_lt20") {
		t.Fatalf("picked the lt20 build: %s", pkg.OfficialAssetURL)
	}
	if pkg.ExpectedBytes != 0 || pkg.SizeCap <= 0 {
		t.Fatalf("unexpected size fields: %+v", pkg)
	}
	if pkg.Provenance.SHA256Source == "" {
		t.Fatal("missing checksum provenance")
	}
}

func TestResolveQBittorrentRefusesPageWithoutChecksum(t *testing.T) {
	page := `<a href="https://sourceforge.net/projects/qbittorrent/files/qbittorrent-mac/qbittorrent-5.2.3/qbittorrent-5.2.3.dmg/download">SourceForge</a>`
	d := doerFunc(func(req *http.Request) (*http.Response, error) {
		return response(req, http.StatusOK, page, ""), nil
	})
	_, err := newTestInstaller(d, &fakeRunner{t: t}).Resolve(context.Background(), QBittorrent)
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "self-hash") {
		t.Fatalf("got %v, want an explicit refusal to self-hash", err)
	}
}

func TestInstallNewBundleDetectAdoptAndReceipt(t *testing.T) {
	zipBytes := buildAppZip(t, "Sonarr.app", "com.osx.sonarr.tv", "4.0", "Sonarr")
	const asset = "https://github.com/Sonarr/Sonarr/releases/download/v4.0.20.3014/Sonarr.main.4.0.20.3014.osx-arm64-app.zip"
	pkg := servarrPackage(t, "Sonarr", "com.osx.sonarr.tv", "4.0.20.3014", asset, zipBytes)
	d := doerFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != asset {
			return nil, fmt.Errorf("unexpected URL %s", req.URL)
		}
		return response(req, http.StatusOK, string(zipBytes), ""), nil
	})
	appDir := t.TempDir()
	installer := newTestInstaller(d, &fakeRunner{t: t})

	result, err := installer.Install(context.Background(), pkg, appDir)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != Installed || result.Existing || !result.OwnerApprovalNeeded {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Path != filepath.Join(appDir, "Sonarr.app") || result.Version != "4.0" {
		t.Fatalf("unexpected installed bundle: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(result.Path, "Contents", "MacOS", "Sonarr")); err != nil {
		t.Fatal(err)
	}
	receipt, err := os.ReadFile(result.ReceiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(receipt, []byte(pkg.ExpectedSHA256)) || !bytes.Contains(receipt, []byte("GitHub")) {
		t.Fatalf("receipt does not retain provenance: %s", receipt)
	}
	created, err := time.Parse(time.RFC3339Nano, "2023-11-14T22:13:20Z")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(receipt, []byte(created.Format(time.RFC3339))) {
		t.Fatalf("receipt did not use the injected clock: %s", receipt)
	}

	detected, err := installer.Detect(context.Background(), Sonarr, appDir)
	if err != nil {
		t.Fatal(err)
	}
	if detected.State != DetectAdopt || detected.Version != "4.0" || detected.BundleID != "com.osx.sonarr.tv" {
		t.Fatalf("unexpected detection: %+v", detected)
	}

	again, err := installer.Install(context.Background(), pkg, appDir)
	if err != nil {
		t.Fatal(err)
	}
	if again.State != Adopted || !again.Existing || again.Path != result.Path {
		t.Fatalf("second install did not adopt: %+v", again)
	}
}

func TestInstallRefusesChecksumMismatchAndKeepsPartialInTrash(t *testing.T) {
	zipBytes := buildAppZip(t, "Sonarr.app", "com.osx.sonarr.tv", "4.0", "Sonarr")
	const asset = "https://github.com/Sonarr/Sonarr/releases/download/v4.0.20.3014/Sonarr.main.4.0.20.3014.osx-arm64-app.zip"
	pkg := servarrPackage(t, "Sonarr", "com.osx.sonarr.tv", "4.0.20.3014", asset, zipBytes)
	pkg.ExpectedSHA256 = strings.Repeat("0", 64)
	d := doerFunc(func(req *http.Request) (*http.Response, error) {
		return response(req, http.StatusOK, string(zipBytes), ""), nil
	})
	appDir := t.TempDir()

	_, err := newTestInstaller(d, &fakeRunner{t: t}).Install(context.Background(), pkg, appDir)
	if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("got %v, want a checksum refusal", err)
	}
	if _, statErr := os.Stat(filepath.Join(appDir, "Sonarr.app")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("a mismatched bundle was installed: %v", statErr)
	}
	trash := filepath.Join(appDir, trashDirName, string(Sonarr))
	entries, readErr := os.ReadDir(trash)
	if readErr != nil || len(entries) == 0 {
		t.Fatalf("stage was not kept in recoverable trash: %v", readErr)
	}
}

func TestInstallRetriesTransientDownloadFailure(t *testing.T) {
	zipBytes := buildAppZip(t, "Sonarr.app", "com.osx.sonarr.tv", "4.0", "Sonarr")
	const asset = "https://github.com/Sonarr/Sonarr/releases/download/v4.0.20.3014/Sonarr.main.4.0.20.3014.osx-arm64-app.zip"
	pkg := servarrPackage(t, "Sonarr", "com.osx.sonarr.tv", "4.0.20.3014", asset, zipBytes)
	calls := 0
	d := doerFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return response(req, http.StatusInternalServerError, "try again", ""), nil
		}
		return response(req, http.StatusOK, string(zipBytes), ""), nil
	})

	result, err := newTestInstaller(d, &fakeRunner{t: t}).Install(context.Background(), pkg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || result.State != Installed {
		t.Fatalf("calls=%d result=%+v", calls, result)
	}
}

func TestInstallKeepsForeignBundleAtExpectedName(t *testing.T) {
	appDir := t.TempDir()
	foreign := filepath.Join(appDir, "Sonarr.app")
	writeApp(t, foreign, "com.example.not-sonarr", "9.9", "Other")
	marker := filepath.Join(foreign, "marker.txt")
	if err := os.WriteFile(marker, []byte("untouched"), 0o644); err != nil {
		t.Fatal(err)
	}
	zipBytes := buildAppZip(t, "Sonarr.app", "com.osx.sonarr.tv", "4.0", "Sonarr")
	pkg := servarrPackage(t, "Sonarr", "com.osx.sonarr.tv", "4.0.20.3014",
		"https://github.com/Sonarr/Sonarr/releases/download/v4.0.20.3014/Sonarr.main.4.0.20.3014.osx-arm64-app.zip", zipBytes)
	called := false
	d := doerFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("download must not be attempted")
	})

	result, err := newTestInstaller(d, &fakeRunner{t: t}).Install(context.Background(), pkg, appDir)
	if !errors.Is(err, ErrBundleConflict) || result.State != Kept || !result.Existing {
		t.Fatalf("got result=%+v err=%v", result, err)
	}
	if called {
		t.Fatal("downloaded despite an existing conflicting bundle")
	}
	body, err := os.ReadFile(marker)
	if err != nil || string(body) != "untouched" {
		t.Fatalf("foreign bundle was disturbed: %v %q", err, body)
	}
	detected, err := newTestInstaller(d, &fakeRunner{t: t}).Detect(context.Background(), Sonarr, appDir)
	if err != nil {
		t.Fatal(err)
	}
	if detected.State != DetectKeep || detected.BundleID != "com.example.not-sonarr" || detected.Version != "9.9" {
		t.Fatalf("unexpected detection: %+v", detected)
	}
}

func TestInstallQBittorrentFromDMG(t *testing.T) {
	image := []byte("fake dmg bytes")
	const asset = "https://sourceforge.net/projects/qbittorrent/files/qbittorrent-mac/qbittorrent-5.2.3/qbittorrent-5.2.3.dmg/download"
	pkg := Package{
		AppID:              QBittorrent,
		Name:               "qBittorrent",
		BundleID:           "org.qbittorrent.qBittorrent",
		NativeArchitecture: "arm64",
		ReleaseVersion:     "5.2.3",
		OfficialAssetURL:   asset,
		ExpectedSHA256:     sha(image),
		ExpectedBytes:      int64(len(image)),
		SizeCap:            int64(len(image)) + (1 << 20),
	}
	d := doerFunc(func(req *http.Request) (*http.Response, error) {
		return response(req, http.StatusOK, string(image), ""), nil
	})
	runner := &fakeRunner{t: t, attachName: "qbittorrent.app", attachID: "org.qbittorrent.qBittorrent", attachVer: "5.2.3", attachExec: "qbittorrent"}

	result, err := newTestInstaller(d, runner).Install(context.Background(), pkg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != filepath.Join(filepath.Dir(result.Path), "qbittorrent.app") || result.Version != "5.2.3" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if runner.attachCalls != 1 {
		t.Fatalf("attach calls = %d", runner.attachCalls)
	}
}

func TestRedirectEscapeIsRejected(t *testing.T) {
	for _, raw := range []string{
		"http://github.com/Sonarr/Sonarr/releases/download/v1/x.zip",
		"https://evil.example/Sonarr.zip",
		"https://github.com:8443/Sonarr.zip",
		"https://user:secret@github.com/Sonarr.zip",
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := checkOfficialURL(u); err == nil {
			t.Errorf("%s was allowed", raw)
		}
	}
	if err := checkOfficialURL(mustURL("https://onboardcloud.dl.sourceforge.net/project/x.dmg")); err != nil {
		t.Errorf("official SourceForge mirror was refused: %v", err)
	}
}

func mustURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}

func TestDownloadRejectsFinalURLOutsideAllowlist(t *testing.T) {
	pkg := Package{
		AppID:            Sonarr,
		Name:             "Sonarr",
		BundleID:         "com.osx.sonarr.tv",
		OfficialAssetURL: "https://github.com/Sonarr/Sonarr/releases/download/v1/x.zip",
		ExpectedSHA256:   strings.Repeat("a", 64),
		SizeCap:          1 << 20,
	}
	evil := mustURL("https://evil.example/redirected.zip")
	d := doerFunc(func(req *http.Request) (*http.Response, error) {
		resp := response(req, http.StatusOK, "data", "")
		resp.Request = &http.Request{URL: evil}
		return resp, nil
	})
	_, _, err := newTestInstaller(d, &fakeRunner{t: t}).downloadOnce(context.Background(), pkg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("got %v, want an allowlist refusal", err)
	}
}

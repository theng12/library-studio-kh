package companioninstall

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
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

const (
	cpuArm64 = uint32(0x0100000c)
	cpuAMD64 = uint32(0x01000007)
)

func thinMachO(cpu uint32) []byte {
	b := make([]byte, 32)
	binary.LittleEndian.PutUint32(b[0:], 0xfeedfacf)
	binary.LittleEndian.PutUint32(b[4:], cpu)
	binary.LittleEndian.PutUint32(b[12:], 2) // MH_EXECUTE
	return b
}

func fatMachO(cpus ...uint32) []byte {
	const offset = 4096
	b := make([]byte, offset+32*len(cpus))
	binary.BigEndian.PutUint32(b[0:], 0xcafebabe)
	binary.BigEndian.PutUint32(b[4:], uint32(len(cpus)))
	off := uint32(offset)
	for n, cpu := range cpus {
		base := 8 + 20*n
		binary.BigEndian.PutUint32(b[base:], cpu)
		binary.BigEndian.PutUint32(b[base+8:], off)
		binary.BigEndian.PutUint32(b[base+12:], 32)
		binary.BigEndian.PutUint32(b[base+16:], 12)
		copy(b[off:], thinMachO(cpu))
		off += 32
	}
	return b
}

func writeApp(t *testing.T, dir, bundleID, version, execName string) {
	t.Helper()
	writeAppWithExec(t, dir, bundleID, version, execName, thinMachO(cpuArm64))
}

func writeAppWithExec(t *testing.T, dir, bundleID, version, execName string, execBody []byte) {
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
	if err := os.WriteFile(filepath.Join(macOS, execName), execBody, 0o755); err != nil {
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
		{appName + "/Contents/MacOS/" + execName, 0o755, thinMachO(cpuArm64)},
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
	t              *testing.T
	attachName     string
	attachID       string
	attachVer      string
	attachExec     string
	attachExecBody []byte
	plutilCalls    int
	attachCalls    int
	xattrCalls     [][]string
	xattrErr       error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	switch name {
	case "plutil":
		f.plutilCalls++
		return os.ReadFile(args[len(args)-1])
	case "xattr":
		copied := append([]string(nil), args...)
		f.xattrCalls = append(f.xattrCalls, copied)
		if f.xattrErr != nil {
			return nil, f.xattrErr
		}
		return nil, nil
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
			body := f.attachExecBody
			if body == nil {
				body = thinMachO(cpuArm64)
			}
			writeAppWithExec(f.t, filepath.Join(mount, f.attachName), f.attachID, f.attachVer, f.attachExec, body)
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

func specFor(id AppID) appSpec {
	for _, spec := range specs {
		if spec.app.ID == id {
			return spec
		}
	}
	panic("unknown app " + string(id))
}

func servarrAssetURL(id AppID, tag, assetName string) string {
	spec := specFor(id)
	return fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s", spec.owner, spec.repo, tag, assetName)
}

// servarrDoer serves the official endpoint, release metadata and asset. The
// metadata digest and size describe published; the asset body is served so a
// corrupted-download test can differ without changing official metadata.
func servarrDoer(t *testing.T, id AppID, tag, assetName string, published, served []byte) doerFunc {
	t.Helper()
	spec := specFor(id)
	downloadURL := servarrAssetURL(id, tag, assetName)
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s", spec.owner, spec.repo, tag)
	release := fmt.Sprintf(`{"tag_name":%q,"assets":[{"name":%q,"size":%d,"digest":"sha256:%s","browser_download_url":%q}]}`,
		tag, assetName, len(published), sha(published), downloadURL)
	if served == nil {
		served = published
	}
	return func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case spec.endpoint:
			return response(req, http.StatusFound, "", downloadURL), nil
		case apiURL:
			return response(req, http.StatusOK, release, ""), nil
		case downloadURL:
			return response(req, http.StatusOK, string(served), ""), nil
		}
		return nil, fmt.Errorf("unexpected URL %s", req.URL)
	}
}

func qbitDoer(t *testing.T, version, digest string, image []byte) doerFunc {
	t.Helper()
	spec := specFor(QBittorrent)
	downloadURL := fmt.Sprintf("https://sourceforge.net/projects/qbittorrent/files/qbittorrent-mac/qbittorrent-%s/qbittorrent-%s.dmg/download", version, version)
	page := fmt.Sprintf(`<h3 id="macos"><a href="%s">SourceForge</a>
<td>%s</td><td class="wordBreak"><code>%s</code></td>
<h3 id="linux">`, downloadURL, version, digest)
	return func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case spec.endpoint:
			return response(req, http.StatusOK, page, ""), nil
		case downloadURL:
			return response(req, http.StatusOK, string(image), ""), nil
		}
		return nil, fmt.Errorf("unexpected URL %s", req.URL)
	}
}

func TestRegistryHasOfficialApps(t *testing.T) {
	apps := Registry()
	if len(apps) != 8 {
		t.Fatalf("got %d apps, want 8", len(apps))
	}
	want := map[AppID]string{
		QBittorrent: "org.qbittorrent.qBittorrent",
		Sonarr:      "com.osx.sonarr.tv",
		Radarr:      "com.osx.radarr.video",
		Lidarr:      "com.osx.lidarr.audio",
		Prowlarr:    "com.osx.prowlarr.com",
		Recyclarr:   "",
		Unpackerr:   "io.golift.unpackerr",
		Whisparr:    "com.osx.whisparr.video",
	}
	wantOptional := map[AppID]bool{Recyclarr: true, Unpackerr: true, Whisparr: true}
	for _, app := range apps {
		if app.BundleID != want[app.ID] {
			t.Errorf("%s bundle = %q, want %q", app.ID, app.BundleID, want[app.ID])
		}
		if app.Optional != wantOptional[app.ID] {
			t.Errorf("%s optional = %v, want %v", app.ID, app.Optional, wantOptional[app.ID])
		}
		if Optional(app.ID) != app.Optional {
			t.Errorf("Optional(%s) disagrees with the registry", app.ID)
		}
	}
	if !Optional(Recyclarr) || !Optional(Unpackerr) || !Optional(Whisparr) || Optional(Sonarr) || Optional(AppID("readarr")) {
		t.Fatal("optional helpers are not marked correctly; Readarr is not part of this app at all")
	}
}

func TestResolveGitHubLatestPicksTheOfficialMacAsset(t *testing.T) {
	specs := map[AppID]string{Recyclarr: "recyclarr-osx-arm64.tar.xz", Unpackerr: "Unpackerr.dmg"}
	for id, asset := range specs {
		spec, err := shared.spec(id)
		if err != nil {
			t.Fatal(err)
		}
		assetURL := fmt.Sprintf("https://github.com/%s/%s/releases/download/v9.1.0/%s", spec.owner, spec.repo, asset)
		release := fmt.Sprintf(`{"tag_name":"v9.1.0","assets":[`+
			`{"name":%q,"size":12345,"digest":"sha256:%s","browser_download_url":%q},`+
			`{"name":"other-linux-x64.tar.gz","size":99,"digest":"sha256:%s","browser_download_url":%q}]}`,
			asset, strings.Repeat("ab", 32), assetURL,
			strings.Repeat("cd", 32), fmt.Sprintf("https://github.com/%s/%s/releases/download/v9.1.0/other-linux-x64.tar.gz", spec.owner, spec.repo))
		d := doerFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() == spec.endpoint {
				return response(req, http.StatusOK, release, ""), nil
			}
			return nil, fmt.Errorf("unexpected URL %s", req.URL)
		})
		i := New(Options{HTTPClient: d})
		pkg, err := i.Resolve(context.Background(), id)
		if err != nil {
			t.Fatalf("%s resolve: %v", id, err)
		}
		if pkg.OfficialAssetURL != assetURL || pkg.ReleaseVersion != "9.1.0" || pkg.ExpectedBytes != 12345 {
			t.Errorf("%s package = %+v", id, pkg)
		}
		if pkg.ExpectedSHA256 != strings.Repeat("ab", 32) || pkg.SizeCap != 12345+(1<<20) {
			t.Errorf("%s digest/cap not from live metadata: %+v", id, pkg)
		}
		if pkg.AppID != spec.app.ID || pkg.BundleID != spec.app.BundleID || pkg.NativeArchitecture != "arm64" {
			t.Errorf("%s identity mismatch: %+v", id, pkg)
		}
	}

	// The vendor dropping the macOS asset must fail closed, never substitute.
	spec, _ := shared.spec(Recyclarr)
	d := doerFunc(func(req *http.Request) (*http.Response, error) {
		return response(req, http.StatusOK, `{"tag_name":"v9.1.0","assets":[{"name":"recyclarr-linux-x64.tar.xz","size":5,"digest":"sha256:`+strings.Repeat("ab", 32)+`","browser_download_url":"https://github.com/recyclarr/recyclarr/releases/download/v9.1.0/recyclarr-linux-x64.tar.xz"}]}`, ""), nil
	})
	if _, err := New(Options{HTTPClient: d}).Resolve(context.Background(), Recyclarr); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing macOS asset must be ErrUnavailable, got %v", err)
	}
	_ = spec
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
<h3 id="macos">
<a href="https://sourceforge.net/projects/qbittorrent/files/qbittorrent-mac/qbittorrent-5.2.3/qbittorrent-5.2.3.dmg/download">SourceForge</a>
<a href="https://sourceforge.net/projects/qbittorrent/files/qbittorrent-mac/qbittorrent-5.2.3/qbittorrent-5.2.3_lt20.dmg/download">SourceForge</a>
<td>5.2.3</td>
<td class="wordBreak"><code>` + standardSHA + `</code></td>
<td>5.2.3 (qt6 lt20)</td>
<td class="wordBreak"><code>4dde4f357cf8d7271f7e25140d2986d076126566edf2d2ad441cdd382b2a333d</code></td>
<h3 id="linux">`
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
	page := `<h3 id="macos"><a href="https://sourceforge.net/projects/qbittorrent/files/qbittorrent-mac/qbittorrent-5.2.3/qbittorrent-5.2.3.dmg/download">SourceForge</a><h3 id="linux">`
	d := doerFunc(func(req *http.Request) (*http.Response, error) {
		return response(req, http.StatusOK, page, ""), nil
	})
	_, err := newTestInstaller(d, &fakeRunner{t: t}).Resolve(context.Background(), QBittorrent)
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "self-hash") {
		t.Fatalf("got %v, want an explicit refusal to self-hash", err)
	}
}

func TestResolveQBittorrentScopesChecksumToMacOSSection(t *testing.T) {
	const macSHA = "9e37f6c7ff848c7bdd3c10167614c0cb78c00e2ddcc323f1ad3ac6c008a0481f"
	const winSHA = "aff5b3bdc00d6e5773d6099ba30f9696dd3dfd1669ab0f64ce5c4e7168486436"
	page := `
<h3 id="windows">
<a href="https://sourceforge.net/projects/qbittorrent/files/qbittorrent-mac/qbittorrent-9.9.9/qbittorrent-9.9.9.dmg/download">wrong section</a>
<td>5.2.3</td><td><code>` + winSHA + `</code></td>
<h3 id="macos">
<a href="https://sourceforge.net/projects/qbittorrent/files/qbittorrent-mac/qbittorrent-5.2.3/qbittorrent-5.2.3.dmg/download">SourceForge</a>
<td>5.2.3</td><td><code>` + macSHA + `</code></td>
<h3 id="linux">`
	d := doerFunc(func(req *http.Request) (*http.Response, error) {
		return response(req, http.StatusOK, page, ""), nil
	})
	pkg, err := newTestInstaller(d, &fakeRunner{t: t}).Resolve(context.Background(), QBittorrent)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.ReleaseVersion != "5.2.3" || pkg.ExpectedSHA256 != macSHA {
		t.Fatalf("used data outside the macOS section: %+v", pkg)
	}
}

func TestResolveQBittorrentRefusesMissingMacOSSection(t *testing.T) {
	page := `<h3 id="windows"><td>5.2.3</td><td><code>` + strings.Repeat("a", 64) + `</code></td>`
	d := doerFunc(func(req *http.Request) (*http.Response, error) {
		return response(req, http.StatusOK, page, ""), nil
	})
	_, err := newTestInstaller(d, &fakeRunner{t: t}).Resolve(context.Background(), QBittorrent)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v, want ErrUnavailable", err)
	}
}

func TestInstallNewBundleDetectAdoptAndReceipt(t *testing.T) {
	zipBytes := buildAppZip(t, "Sonarr.app", "com.osx.sonarr.tv", "4.0", "Sonarr")
	const assetName = "Sonarr.main.4.0.20.3014.osx-arm64-app.zip"
	asset := servarrAssetURL(Sonarr, "v4.0.20.3014", assetName)
	pkg := servarrPackage(t, "Sonarr", "com.osx.sonarr.tv", "4.0.20.3014", asset, zipBytes)
	runner := &fakeRunner{t: t}
	appDir := t.TempDir()
	installer := newTestInstaller(servarrDoer(t, Sonarr, "v4.0.20.3014", assetName, zipBytes, nil), runner)

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
	if len(runner.xattrCalls) != 1 {
		t.Fatalf("quarantine calls = %d", len(runner.xattrCalls))
	}
	quarantine := runner.xattrCalls[0]
	if len(quarantine) != 4 || quarantine[0] != "-w" || quarantine[1] != "com.apple.quarantine" {
		t.Fatalf("unexpected quarantine call: %v", quarantine)
	}
	if !strings.HasPrefix(quarantine[2], quarantineFlags+";") || !strings.Contains(quarantine[2], quarantineAgent) {
		t.Fatalf("unexpected quarantine value: %q", quarantine[2])
	}
	if !strings.Contains(quarantine[3], ".library-studio-stage-") {
		t.Fatalf("quarantine was not applied to the staged bundle before promotion: %s", quarantine[3])
	}

	receiptBytes, err := os.ReadFile(result.ReceiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(receiptBytes, []byte(pkg.ExpectedSHA256)) || !bytes.Contains(receiptBytes, []byte("GitHub")) {
		t.Fatalf("receipt does not retain provenance: %s", receiptBytes)
	}
	if filepath.Base(result.ReceiptPath) == "sonarr.json" {
		t.Fatalf("receipt still uses a stale per-app name: %s", result.ReceiptPath)
	}
	var receipt Receipt
	if err := json.Unmarshal(receiptBytes, &receipt); err != nil {
		t.Fatal(err)
	}
	if !receipt.QuarantineAdded || receipt.QuarantineAgent != quarantineAgent ||
		receipt.AppLaunched || receipt.CodeSignatureModified || !receipt.OwnerApprovalNeeded {
		t.Fatalf("dishonest receipt semantics: %+v", receipt)
	}
	created, err := time.Parse(time.RFC3339Nano, "2023-11-14T22:13:20Z")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(receiptBytes, []byte(created.Format(time.RFC3339))) {
		t.Fatalf("receipt did not use the injected clock: %s", receiptBytes)
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
	if len(runner.xattrCalls) != 1 {
		t.Fatalf("adoption changed quarantine: %d calls", len(runner.xattrCalls))
	}
}

func TestInstallRefusesChecksumMismatchAndKeepsPartialInTrash(t *testing.T) {
	zipBytes := buildAppZip(t, "Sonarr.app", "com.osx.sonarr.tv", "4.0", "Sonarr")
	const assetName = "Sonarr.main.4.0.20.3014.osx-arm64-app.zip"
	asset := servarrAssetURL(Sonarr, "v4.0.20.3014", assetName)
	pkg := servarrPackage(t, "Sonarr", "com.osx.sonarr.tv", "4.0.20.3014", asset, zipBytes)
	tampered := append([]byte(nil), zipBytes...)
	tampered[len(tampered)/2] ^= 0xff
	appDir := t.TempDir()

	// Official metadata stays intact; the served bytes are corrupted.
	d := servarrDoer(t, Sonarr, "v4.0.20.3014", assetName, zipBytes, tampered)
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
	const assetName = "Sonarr.main.4.0.20.3014.osx-arm64-app.zip"
	asset := servarrAssetURL(Sonarr, "v4.0.20.3014", assetName)
	pkg := servarrPackage(t, "Sonarr", "com.osx.sonarr.tv", "4.0.20.3014", asset, zipBytes)
	base := servarrDoer(t, Sonarr, "v4.0.20.3014", assetName, zipBytes, nil)
	calls := 0
	d := doerFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == asset {
			calls++
			if calls == 1 {
				return response(req, http.StatusInternalServerError, "try again", ""), nil
			}
		}
		return base(req)
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
		// The official page publishes no size; Resolve leaves this zero.
		ExpectedBytes: 0,
		SizeCap:       int64(len(image)) + (1 << 20),
	}
	appDir := t.TempDir()
	runner := &fakeRunner{
		t: t, attachName: "qbittorrent.app", attachID: "org.qbittorrent.qBittorrent",
		attachVer: "5.2.3", attachExec: "qbittorrent", attachExecBody: fatMachO(cpuAMD64, cpuArm64),
	}

	result, err := newTestInstaller(qbitDoer(t, "5.2.3", sha(image), image), runner).Install(context.Background(), pkg, appDir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(result.Path) != appDir || filepath.Base(result.Path) != "qbittorrent.app" || result.Version != "5.2.3" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if runner.attachCalls != 1 {
		t.Fatalf("attach calls = %d", runner.attachCalls)
	}
	if len(runner.xattrCalls) != 1 {
		t.Fatalf("quarantine calls = %d", len(runner.xattrCalls))
	}
}

func TestInstallRejectsForgedPackage(t *testing.T) {
	zipBytes := buildAppZip(t, "Sonarr.app", "com.osx.sonarr.tv", "4.0", "Sonarr")
	const assetName = "Sonarr.main.4.0.20.3014.osx-arm64-app.zip"
	officialURL := servarrAssetURL(Sonarr, "v4.0.20.3014", assetName)
	pkg := servarrPackage(t, "Sonarr", "com.osx.sonarr.tv", "4.0.20.3014", officialURL, zipBytes)
	// Caller supplies an allowed host but a repository and digest of its own.
	pkg.OfficialAssetURL = "https://github.com/attacker/Sonarr/releases/download/v4.0.20.3014/Sonarr.main.4.0.20.3014.osx-arm64-app.zip"
	pkg.ExpectedSHA256 = strings.Repeat("f", 64)

	base := servarrDoer(t, Sonarr, "v4.0.20.3014", assetName, zipBytes, nil)
	attackerRequested := false
	d := doerFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == pkg.OfficialAssetURL {
			attackerRequested = true
		}
		return base(req)
	})
	appDir := t.TempDir()
	result, err := newTestInstaller(d, &fakeRunner{t: t}).Install(context.Background(), pkg, appDir)
	if !errors.Is(err, ErrStalePackage) {
		t.Fatalf("got result=%+v err=%v, want ErrStalePackage", result, err)
	}
	if attackerRequested {
		t.Fatal("downloaded from the caller-supplied URL")
	}
	if _, statErr := os.Stat(filepath.Join(appDir, "Sonarr.app")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("a forged package wrote a bundle: %v", statErr)
	}
}

func TestInstallStopsWhenOfficialVersionChanged(t *testing.T) {
	zipBytes := buildAppZip(t, "Sonarr.app", "com.osx.sonarr.tv", "4.0", "Sonarr")
	const assetName = "Sonarr.main.4.0.20.3014.osx-arm64-app.zip"
	oldURL := servarrAssetURL(Sonarr, "v4.0.19.1", assetName)
	pkg := servarrPackage(t, "Sonarr", "com.osx.sonarr.tv", "4.0.19.1", oldURL, zipBytes)
	appDir := t.TempDir()
	result, err := newTestInstaller(servarrDoer(t, Sonarr, "v4.0.20.3014", assetName, zipBytes, nil), &fakeRunner{t: t}).
		Install(context.Background(), pkg, appDir)
	if !errors.Is(err, ErrStalePackage) {
		t.Fatalf("got result=%+v err=%v, want ErrStalePackage", result, err)
	}
	if !strings.Contains(err.Error(), "4.0.19.1") || !strings.Contains(err.Error(), "4.0.20.3014") {
		t.Fatalf("stale error does not show both versions: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(appDir, "Sonarr.app")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("a stale plan wrote a bundle: %v", statErr)
	}
}

func TestCheckArchitectureFixtures(t *testing.T) {
	cases := []struct {
		name    string
		body    []byte
		wantErr bool
	}{
		{"thin arm64", thinMachO(cpuArm64), false},
		{"thin amd64", thinMachO(cpuAMD64), true},
		{"universal with arm64", fatMachO(cpuAMD64, cpuArm64), false},
		{"universal without arm64", fatMachO(cpuAMD64), true},
		{"malformed", []byte("#!/bin/sh\n"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "executable")
			if err := os.WriteFile(path, tc.body, 0o755); err != nil {
				t.Fatal(err)
			}
			err := checkArchitecture(path, "arm64")
			if tc.wantErr && err == nil {
				t.Fatal("architecture check passed")
			}
			if !tc.wantErr && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestQuarantineFailureStopsPromotion(t *testing.T) {
	zipBytes := buildAppZip(t, "Sonarr.app", "com.osx.sonarr.tv", "4.0", "Sonarr")
	const assetName = "Sonarr.main.4.0.20.3014.osx-arm64-app.zip"
	asset := servarrAssetURL(Sonarr, "v4.0.20.3014", assetName)
	pkg := servarrPackage(t, "Sonarr", "com.osx.sonarr.tv", "4.0.20.3014", asset, zipBytes)
	appDir := t.TempDir()
	runner := &fakeRunner{t: t, xattrErr: errors.New("xattr denied")}

	_, err := newTestInstaller(servarrDoer(t, Sonarr, "v4.0.20.3014", assetName, zipBytes, nil), runner).
		Install(context.Background(), pkg, appDir)
	if err == nil || !strings.Contains(err.Error(), "quarantine") {
		t.Fatalf("got %v, want a quarantine refusal", err)
	}
	if _, statErr := os.Stat(filepath.Join(appDir, "Sonarr.app")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("promoted a bundle without quarantine: %v", statErr)
	}
	entries, readErr := os.ReadDir(filepath.Join(appDir, trashDirName, string(Sonarr)))
	if readErr != nil || len(entries) == 0 {
		t.Fatalf("stage was not kept in recoverable trash: %v", readErr)
	}
}

func TestStageRetirementFailureIsReported(t *testing.T) {
	zipBytes := buildAppZip(t, "Sonarr.app", "com.osx.sonarr.tv", "4.0", "Sonarr")
	const assetName = "Sonarr.main.4.0.20.3014.osx-arm64-app.zip"
	asset := servarrAssetURL(Sonarr, "v4.0.20.3014", assetName)
	pkg := servarrPackage(t, "Sonarr", "com.osx.sonarr.tv", "4.0.20.3014", asset, zipBytes)
	tampered := append([]byte(nil), zipBytes...)
	tampered[len(tampered)/2] ^= 0xff
	appDir := t.TempDir()
	// Make the per-app trash path a file so retirement cannot create its folder.
	trashParent := filepath.Join(appDir, trashDirName)
	if err := os.MkdirAll(trashParent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trashParent, string(Sonarr)), []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := newTestInstaller(servarrDoer(t, Sonarr, "v4.0.20.3014", assetName, zipBytes, tampered), &fakeRunner{t: t}).
		Install(context.Background(), pkg, appDir)
	if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") ||
		!strings.Contains(err.Error(), "could not retire staging folder") {
		t.Fatalf("got %v, want checksum plus stage retirement failure", err)
	}
}

func TestReceiptIsPerInstallHistory(t *testing.T) {
	installer := newTestInstaller(doerFunc(func(req *http.Request) (*http.Response, error) {
		return response(req, http.StatusOK, "", ""), nil
	}), &fakeRunner{t: t})
	appDir := t.TempDir()
	base := servarrPackage(t, "Sonarr", "com.osx.sonarr.tv", "4.0.20.3014",
		servarrAssetURL(Sonarr, "v4.0.20.3014", "Sonarr.main.4.0.20.3014.osx-arm64-app.zip"), []byte("one"))
	second := base
	second.ReleaseVersion = "4.0.21.1"
	second.ExpectedSHA256 = strings.Repeat("b", 64)

	first, err := installer.writeReceipt(appDir, base, filepath.Join(appDir, "Sonarr.app"), bundleInfo{Identifier: "com.osx.sonarr.tv", Version: "4.0"})
	if err != nil {
		t.Fatal(err)
	}
	next, err := installer.writeReceipt(appDir, second, filepath.Join(appDir, "Sonarr.app"), bundleInfo{Identifier: "com.osx.sonarr.tv", Version: "4.1"})
	if err != nil {
		t.Fatal(err)
	}
	if first == next {
		t.Fatalf("receipt path was reused: %s", first)
	}
	for path, want := range map[string]string{first: "4.0.20.3014", next: "4.0.21.1"} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(body, []byte(want)) {
			t.Fatalf("receipt %s does not retain %s", path, want)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o444 {
			t.Fatalf("receipt %s mode = %v", path, info.Mode().Perm())
		}
	}
	entries, err := os.ReadDir(filepath.Join(appDir, receiptDirName))
	if err != nil || len(entries) != 2 {
		t.Fatalf("receipt history = %d, err=%v", len(entries), err)
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

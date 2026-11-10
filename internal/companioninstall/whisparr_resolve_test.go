package companioninstall

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestResolveWhisparrOfficialMetadata(t *testing.T) {
	// Selected public fields from the saved official response; never fetched by tests.
	fixture, err := os.ReadFile("testdata/whisparr-release.json")
	if err != nil {
		t.Fatal(err)
	}
	const endpoint = "https://api.github.com/repos/Whisparr/Whisparr-Eros/releases/latest"
	for _, tc := range []struct {
		name   string
		change func(*githubRelease)
		refuse bool
	}{
		{"published release", func(*githubRelease) {}, false},
		{"future release", func(r *githubRelease) {
			r.TagName = "v99.12.3-release.456"
			r.Assets = r.Assets[:1]
			r.Assets[0].Name = "Whisparr.eros.99.12.3-release.456.osx-arm64-app.zip"
			r.Assets[0].BrowserDownloadURL = "https://github.com/Whisparr/Whisparr-Eros/releases/download/" + r.TagName + "/" + r.Assets[0].Name
		}, false},
		{"unrelated repository", func(r *githubRelease) {
			r.Assets[0].BrowserDownloadURL = strings.Replace(r.Assets[0].BrowserDownloadURL, "/Whisparr/Whisparr-Eros/", "/Other/Whisparr-Eros/", 1)
		}, true},
		{"wrong release tag", func(r *githubRelease) {
			r.Assets[0].BrowserDownloadURL = strings.Replace(r.Assets[0].BrowserDownloadURL, "/"+r.TagName+"/", "/v0.0.1/", 1)
		}, true},
		{"different asset URL", func(r *githubRelease) {
			r.Assets[0].BrowserDownloadURL = strings.TrimSuffix(r.Assets[0].BrowserDownloadURL, r.Assets[0].Name) + "other.zip"
		}, true},
		{"foreign origin", func(r *githubRelease) {
			r.Assets[0].BrowserDownloadURL = strings.Replace(r.Assets[0].BrowserDownloadURL, "github.com", "foreign.example", 1)
		}, true},
		{"non HTTPS", func(r *githubRelease) {
			r.Assets[0].BrowserDownloadURL = strings.Replace(r.Assets[0].BrowserDownloadURL, "https:", "http:", 1)
		}, true},
		{"x64 only", func(r *githubRelease) { r.Assets = r.Assets[2:] }, true},
		{"bare arm64 archive", func(r *githubRelease) { r.Assets = r.Assets[1:2] }, true},
		{"unrelated product", func(r *githubRelease) {
			r.Assets[0].Name = strings.Replace(r.Assets[0].Name, "Whisparr.eros.", "Other.eros.", 1)
		}, true},
		{"unprefixed old assumption", func(r *githubRelease) { r.Assets[0].Name = "osx-arm64-app.zip" }, true},
		{"extra filename prefix", func(r *githubRelease) { r.Assets[0].Name = "other-" + r.Assets[0].Name }, true},
		{"extra filename suffix", func(r *githubRelease) { r.Assets[0].Name += ".exe" }, true},
		{"ambiguous matches", func(r *githubRelease) { r.Assets = append(r.Assets, r.Assets[0]) }, true},
		{"missing digest", func(r *githubRelease) { r.Assets[0].Digest = nil }, true},
		{"malformed digest", func(r *githubRelease) { s := "sha256:not-a-digest"; r.Assets[0].Digest = &s }, true},
		{"missing size", func(r *githubRelease) { r.Assets[0].Size = 0 }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var release githubRelease
			if err := json.Unmarshal(fixture, &release); err != nil {
				t.Fatal(err)
			}
			tc.change(&release)
			body, err := json.Marshal(release)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			d := doerFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != http.MethodGet || req.URL.String() != endpoint {
					return nil, fmt.Errorf("unexpected mock metadata request: %s", req.URL)
				}
				return response(req, http.StatusOK, string(body), ""), nil
			})
			pkg, err := newTestInstaller(d, &fakeRunner{t: t}).Resolve(context.Background(), Whisparr)
			if calls != 1 {
				t.Fatalf("metadata requests = %d", calls)
			}
			if tc.refuse {
				if err == nil {
					t.Fatalf("unsafe metadata accepted: %+v", pkg)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := release.Assets[0]
			if pkg.OfficialAssetURL != want.BrowserDownloadURL || pkg.ReleaseVersion != strings.TrimPrefix(release.TagName, "v") || pkg.ExpectedBytes != want.Size || pkg.ExpectedSHA256 != strings.TrimPrefix(*want.Digest, "sha256:") || pkg.SizeCap != want.Size+(1<<20) || pkg.NativeArchitecture != "arm64" || pkg.Provenance.MetadataURL != endpoint {
				t.Fatalf("metadata values were replaced or wrong asset selected: %+v", pkg)
			}
		})
	}
}

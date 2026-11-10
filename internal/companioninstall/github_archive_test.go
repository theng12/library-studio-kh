package companioninstall

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
)

func TestInstallGitHubArchiveRoutes(t *testing.T) {
	for _, tc := range []struct {
		name, asset string
		id          AppID
		foreign     bool
	}{
		{"Whisparr ZIP", "Whisparr.eros.9.1.0.osx-arm64-app.zip", Whisparr, false},
		{"Whisparr foreign bundle", "Whisparr.eros.9.1.0.osx-arm64-app.zip", Whisparr, true},
		{"Unpackerr DMG", "Unpackerr.dmg", Unpackerr, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := specFor(tc.id)
			identity := spec.app.BundleID
			if tc.foreign {
				identity = "fixture.foreign"
			}
			body := buildAppZip(t, spec.bundleName, identity, "9.1.0", spec.app.Name)
			assetURL := servarrAssetURL(tc.id, "v9.1.0", tc.asset)
			release := fmt.Sprintf(`{"tag_name":"v9.1.0","assets":[{"name":%q,"size":%d,"digest":"sha256:%s","browser_download_url":%q}]}`, tc.asset, len(body), sha(body), assetURL)
			d := doerFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.String() {
				case spec.endpoint:
					return response(req, http.StatusOK, release, ""), nil
				case assetURL:
					return response(req, http.StatusOK, string(body), ""), nil
				default:
					return nil, fmt.Errorf("unexpected mock request: %s", req.URL)
				}
			})
			runner := &fakeRunner{t: t, attachName: spec.bundleName, attachID: identity, attachVer: "9.1.0", attachExec: spec.app.Name}
			i := newTestInstaller(d, runner)
			pkg, err := i.Resolve(context.Background(), tc.id)
			if err != nil {
				t.Fatal(err)
			}
			result, err := i.Install(context.Background(), pkg, t.TempDir())
			wantAttach := 0
			if tc.id == Unpackerr {
				wantAttach = 1
			}
			if runner.attachCalls != wantAttach {
				t.Fatalf("hdiutil calls=%d, want %d", runner.attachCalls, wantAttach)
			}
			if tc.foreign {
				if !errors.Is(err, ErrBundleConflict) || len(runner.xattrCalls) != 0 {
					t.Fatalf("foreign ZIP accepted: %+v, %v", result, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.State != Installed || result.BundleID != spec.app.BundleID || filepath.Base(result.Path) != spec.bundleName || len(runner.xattrCalls) != 1 || !result.OwnerApprovalNeeded {
				t.Fatalf("archive routing bypassed installation checks: %+v", result)
			}
		})
	}
}

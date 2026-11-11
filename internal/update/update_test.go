package update

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func publisherFile(t *testing.T, home, feedURL string, pub ed25519.PublicKey) {
	t.Helper()
	dir := filepath.Join(home, "Library", "KHUpdater")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{
		"feedURL": feedURL, "publicKey": base64.StdEncoding.EncodeToString(pub),
	})
	if err := os.WriteFile(filepath.Join(dir, "publisher.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func feedServer(t *testing.T, key ed25519.PrivateKey, apps map[string]any, schema int) *httptest.Server {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"schema": schema, "published_at": "2026-11-12T00:00:00Z", "apps": apps})
	sig := ed25519.Sign(key, body)
	answer, _ := json.Marshal(map[string]string{
		"feed": base64.StdEncoding.EncodeToString(body), "signature": base64.StdEncoding.EncodeToString(sig),
	})
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(answer)
	}))
}

func entry(version string) map[string]any {
	return map[string]any{"version": version, "name": "Library Studio KH", "bundleID": BundleID, "kind": "app"}
}

func TestCheckStates(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	apps := func(v string) map[string]any { return map[string]any{"library": entry(v)} }
	for _, tc := range []struct {
		name      string
		installed string
		feed      map[string]any
		schema    int
		want      string
		version   string
	}{
		{"current is up to date", "0.7.8", apps("0.7.8"), 1, "up_to_date", ""},
		{"a newer offer is available", "0.7.8", apps("0.7.9"), 1, "available", "0.7.9"},
		{"numeric parts compare as numbers", "0.7.9", apps("0.7.10"), 1, "available", "0.7.10"},
		{"an older feed is not an update", "0.7.10", apps("0.7.9"), 1, "up_to_date", ""},
		{"another app's entry is not ours", "0.7.8", map[string]any{"hub": map[string]any{"version": "9.0", "bundleID": "app.kh.hub"}}, 1, "not_offered", ""},
		{"an unparsable feed version fails", "0.7.8", apps("latest"), 1, "failed", ""},
		{"schema 2 is refused", "0.7.8", apps("0.7.9"), 2, "failed", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := feedServer(t, key, tc.feed, tc.schema)
			defer srv.Close()
			home := t.TempDir()
			publisherFile(t, home, srv.URL, pub)
			got := Check(home, tc.installed, nil)
			if got.State != tc.want || got.Version != tc.version {
				t.Fatalf("got %+v, want state %q version %q", got, tc.want, tc.version)
			}
		})
	}
}

func TestCheckFailuresAreNeverUpToDate(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	body, _ := json.Marshal(map[string]any{"schema": 1, "apps": map[string]any{"library": entry("0.7.9")}})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		answer, _ := json.Marshal(map[string]string{
			"feed":      base64.StdEncoding.EncodeToString(body),
			"signature": base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize)),
		})
		w.Write(answer)
	}))
	defer srv.Close()

	home := t.TempDir()
	publisherFile(t, home, srv.URL, pub)
	badSignature := Check(home, "0.7.8", nil)
	if badSignature.State != "failed" || !strings.Contains(badSignature.Reason, "signature") {
		t.Fatalf("bad signature = %+v", badSignature)
	}

	unconnected := Check(t.TempDir(), "0.7.8", nil)
	if unconnected.State != "not_checked" || unconnected.Reason == "" {
		t.Fatalf("missing publisher.json = %+v", unconnected)
	}

	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	publisherFile(t, home, closed.URL, pub)
	if got := Check(home, "0.7.8", nil); got.State != "failed" || got.Reason == "" {
		t.Fatalf("unreachable feed = %+v", got)
	}

	// A feed signed by a different key must not be trusted.
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	forged := feedServer(t, other, map[string]any{"library": entry("0.7.9")}, 1)
	defer forged.Close()
	publisherFile(t, home, forged.URL, pub)
	if got := Check(home, "0.7.8", nil); got.State != "failed" {
		t.Fatalf("forged feed = %+v", got)
	}
}

func TestInstalledVersionReadsTheBundle(t *testing.T) {
	app := filepath.Join(t.TempDir(), "Library Studio KH.app")
	if err := os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>CFBundleName</key><string>Library Studio KH</string>
<key>CFBundleShortVersionString</key><string>1.2.3</string>
<key>CFBundleVersion</key><string>9</string>
</dict></plist>`
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := bundleVersion(filepath.Join(app, "Contents", "MacOS", "library-studio-server"), "0.0.1"); got != "1.2.3" {
		t.Fatalf("bundle version = %q, want 1.2.3", got)
	}
	if got := bundleVersion("/usr/local/bin/library-studio-server", "0.0.1"); got != "0.0.1" {
		t.Fatalf("outside a bundle = %q, want the fallback", got)
	}
}

// TestLiveFeed reads this Mac's real publisher.json and feed; it is opt-in so the
// suite stays offline.
func TestLiveFeed(t *testing.T) {
	if os.Getenv("LIBRARY_LIVE_FEED") == "" {
		t.Skip("set LIBRARY_LIVE_FEED=1 to check this Mac's real update feed")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("installed bundle version: %q", bundleVersion("/Applications/Library Studio KH.app/Contents/MacOS/library-studio-server", "missing"))
	got := Check(home, "0.0.1", nil)
	t.Logf("live feed: %+v", got)
	if got.State != "available" && got.State != "up_to_date" {
		t.Fatalf("live feed check = %+v", got)
	}
}

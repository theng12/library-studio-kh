// Package update reads the shared KH update feed. It only looks: installing stays
// with Studio Launcher KH, never a second updater.
package update

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// BundleID identifies this app in the feed; names and feed ids are not keys.
const BundleID = "app.librarystudiokh.desktop"

// Result is the answer behind the footer's one compact status line.
type Result struct {
	State   string `json:"state"` // not_checked, up_to_date, available, not_offered, failed
	Version string `json:"version,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

type publisher struct {
	FeedURL   string `json:"feedURL"`
	PublicKey string `json:"publicKey"`
}

type release struct {
	Feed      string `json:"feed"`
	Signature string `json:"signature"`
}

type feed struct {
	Schema int `json:"schema"`
	Apps   map[string]struct {
		Version  string `json:"version"`
		BundleID string `json:"bundleID"`
	} `json:"apps"`
}

var versionPattern = regexp.MustCompile(`^\d{1,9}(\.\d{1,9}){0,5}$`)

// Check reads this Mac's enrolled client config, fetches the signed feed and compares the
// offered version with installed. A nil client uses a short-timeout default.
func Check(home, installed string, client *http.Client) Result {
	pub, result := readConnection(home)
	if result != nil {
		return *result
	}
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	f, reason := fetchFeed(client, pub)
	if f == nil {
		return Result{State: "failed", Reason: reason}
	}
	var entry struct {
		Version  string `json:"version"`
		BundleID string `json:"bundleID"`
	}
	found := false
	for _, a := range f.Apps {
		if a.BundleID == BundleID {
			entry, found = a, true
			break
		}
	}
	if !found {
		return Result{State: "not_offered"}
	}
	if !versionPattern.MatchString(entry.Version) || !versionPattern.MatchString(installed) {
		return Result{State: "failed", Reason: "the feed version could not be compared"}
	}
	if newer(entry.Version, installed) {
		return Result{State: "available", Version: entry.Version}
	}
	return Result{State: "up_to_date"}
}

func readConnection(home string) (publisher, *Result) {
	b, err := os.ReadFile(filepath.Join(home, "Library", "KHUpdater", "base", "config.json"))
	if err != nil {
		return publisher{}, &Result{State: "not_checked", Reason: "this Mac is not connected to Update Studio KH"}
	}
	var pub publisher
	if json.Unmarshal(b, &pub) != nil || pub.FeedURL == "" || pub.PublicKey == "" {
		return publisher{}, &Result{State: "not_checked", Reason: "this Mac's Update Studio connection could not be read"}
	}
	return pub, nil
}

func fetchFeed(client *http.Client, pub publisher) (*feed, string) {
	resp, err := client.Get(pub.FeedURL)
	if err != nil {
		return nil, "the update feed could not be reached"
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "the update feed answered " + resp.Status
	}
	var rel release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return nil, "the update feed could not be read"
	}
	key, err := base64.StdEncoding.DecodeString(pub.PublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, "the update feed key could not be read"
	}
	sig, err := base64.StdEncoding.DecodeString(rel.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, "the update feed signature could not be read"
	}
	body, err := base64.StdEncoding.DecodeString(rel.Feed)
	if err != nil {
		return nil, "the update feed could not be read"
	}
	if !ed25519.Verify(ed25519.PublicKey(key), body, sig) {
		return nil, "the update feed signature did not verify"
	}
	var f feed
	if json.Unmarshal(body, &f) != nil || f.Schema != 1 {
		return nil, "the update feed is not schema 1"
	}
	return &f, ""
}

// newer reports whether a is strictly greater than b; both must already parse.
func newer(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			for _, c := range as[i] {
				x = x*10 + int(c-'0')
			}
		}
		if i < len(bs) {
			for _, c := range bs[i] {
				y = y*10 + int(c-'0')
			}
		}
		if x != y {
			return x > y
		}
	}
	return false
}

// InstalledVersion is the version the updater also reads: CFBundleShortVersionString
// from the running .app. Outside a bundle (dev tree) the generated stamp is used.
func InstalledVersion(fallback string) string {
	exe, err := os.Executable()
	if err != nil {
		return fallback
	}
	return bundleVersion(exe, fallback)
}

func bundleVersion(exe, fallback string) string {
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	for dir := filepath.Dir(exe); ; dir = filepath.Dir(dir) {
		if strings.HasSuffix(dir, ".app") {
			if v := plistVersion(filepath.Join(dir, "Contents", "Info.plist")); v != "" {
				return v
			}
			return fallback
		}
		if parent := filepath.Dir(dir); parent == dir {
			return fallback
		}
	}
}

func plistVersion(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	d := xml.NewDecoder(f)
	for {
		tok, err := d.Token()
		if err != nil {
			return ""
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "key" {
			continue
		}
		var name string
		if d.DecodeElement(&name, &se) != nil || name != "CFBundleShortVersionString" {
			continue
		}
		for {
			tok, err = d.Token()
			if err != nil {
				return ""
			}
			if value, ok := tok.(xml.StartElement); ok {
				var v string
				if d.DecodeElement(&v, &value) != nil {
					return ""
				}
				return strings.TrimSpace(v)
			}
		}
	}
}

// OpenLauncher brings the one updater forward; it never installs anything by itself.
func OpenLauncher() error {
	if runtime.GOOS != "darwin" {
		return errors.New("Studio Launcher KH is only available on macOS")
	}
	return exec.Command("open", "-b", "com.khupdater.launcher").Run()
}

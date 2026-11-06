package companioninstall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// allowedHosts are the official publication and delivery hosts this package
// is willing to contact. SourceForge mirrors are used by qBittorrent's own
// download page.
var allowedHosts = map[string]bool{
	"github.com":                           true,
	"api.github.com":                       true,
	"objects.githubusercontent.com":        true,
	"release-assets.githubusercontent.com": true,
	"services.sonarr.tv":                   true,
	"sonarr.servarr.com":                   true,
	"radarr.servarr.com":                   true,
	"lidarr.servarr.com":                   true,
	"prowlarr.servarr.com":                 true,
	"www.qbittorrent.org":                  true,
	"qbittorrent.org":                      true,
	"sourceforge.net":                      true,
	"downloads.sourceforge.net":            true,
}

func hostAllowed(host string) bool {
	host = strings.ToLower(host)
	if allowedHosts[host] {
		return true
	}
	return strings.HasSuffix(host, ".dl.sourceforge.net")
}

// checkOfficialURL rejects anything but HTTPS on an official host, including
// every redirect hop.
func checkOfficialURL(u *url.URL) error {
	switch {
	case u == nil:
		return errors.New("companioninstall: empty URL")
	case u.Scheme != "https":
		return fmt.Errorf("companioninstall: refusing non-HTTPS URL %q", u.String())
	case u.User != nil:
		return fmt.Errorf("companioninstall: refusing URL with credentials %q", u.Redacted())
	case u.Port() != "" && u.Port() != "443":
		return fmt.Errorf("companioninstall: refusing URL on non-443 port %q", u.String())
	case !hostAllowed(u.Hostname()):
		return fmt.Errorf("companioninstall: refusing host %q outside the official allowlist", u.Hostname())
	}
	return nil
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("companioninstall: stopped after 10 redirects")
	}
	return checkOfficialURL(req.URL)
}

// withoutRedirects resolves the first official hop without following it, so
// the GitHub repository and tag can be checked before any metadata is read.
func withoutRedirects(d Doer) Doer {
	if c, ok := d.(*http.Client); ok {
		cp := *c
		cp.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
		return &cp
	}
	return d
}

func (i *Installer) get(ctx context.Context, client Doer, rawURL, accept string) (*http.Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("companioninstall: bad URL %q: %w", rawURL, err)
	}
	if err := checkOfficialURL(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Library-Studio-KH-companioninstall")
	// Keep byte counts identical to the vendor's published file; no transparent
	// compression may change the size or hash.
	req.Header.Set("Accept-Encoding", "identity")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("companioninstall: HTTP client returned no response")
	}
	if resp.Body == nil {
		resp.Body = http.NoBody
	}
	// A custom Doer may follow redirects itself; check the final URL too.
	if resp.Request != nil && resp.Request.URL != nil {
		if err := checkOfficialURL(resp.Request.URL); err != nil {
			resp.Body.Close()
			return nil, err
		}
	}
	return resp, nil
}

func readMetadata(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, metadataReadLimit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > metadataReadLimit {
		return nil, errors.New("companioninstall: official metadata is unexpectedly large")
	}
	return body, nil
}

func (i *Installer) resolveServarr(ctx context.Context, spec appSpec) (Package, error) {
	pkg := basePackage(spec, i.sizeCap)

	resp, err := i.get(ctx, withoutRedirects(i.http), spec.endpoint, "")
	if err != nil {
		return Package{}, fmt.Errorf("resolve %s: %w", spec.app.ID, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
	default:
		return Package{}, fmt.Errorf("resolve %s: official endpoint returned HTTP %d", spec.app.ID, resp.StatusCode)
	}
	location := resp.Header.Get("Location")
	if location == "" {
		return Package{}, fmt.Errorf("resolve %s: official endpoint sent no asset location", spec.app.ID)
	}
	assetURL, err := url.Parse(location)
	if err != nil {
		return Package{}, fmt.Errorf("resolve %s: bad asset location: %w", spec.app.ID, err)
	}
	if err := checkOfficialURL(assetURL); err != nil {
		return Package{}, err
	}
	parts := strings.Split(strings.Trim(assetURL.Path, "/"), "/")
	// /<owner>/<repo>/releases/download/<tag>/<asset>
	if len(parts) != 6 || parts[2] != "releases" || parts[3] != "download" {
		return Package{}, fmt.Errorf("resolve %s: official endpoint did not point at a GitHub release asset: %s", spec.app.ID, location)
	}
	owner, repo, tag, assetName := parts[0], parts[1], parts[4], parts[5]
	if !strings.EqualFold(owner, spec.owner) || !strings.EqualFold(repo, spec.repo) {
		return Package{}, fmt.Errorf("resolve %s: official endpoint pointed at %s/%s, not %s/%s", spec.app.ID, owner, repo, spec.owner, spec.repo)
	}
	if tag == "" || assetName == "" {
		return Package{}, fmt.Errorf("resolve %s: official endpoint gave an incomplete asset location", spec.app.ID)
	}
	lowerAsset := strings.ToLower(assetName)
	if !strings.HasSuffix(lowerAsset, ".zip") || !strings.Contains(lowerAsset, "app") ||
		!strings.Contains(lowerAsset, "arm64") || strings.Contains(lowerAsset, "x64") ||
		strings.Contains(lowerAsset, "amd64") || strings.Contains(lowerAsset, "x86") {
		return Package{}, fmt.Errorf("resolve %s: official endpoint offered %q, not a macOS arm64 app zip", spec.app.ID, assetName)
	}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s",
		url.PathEscape(spec.owner), url.PathEscape(spec.repo), url.PathEscape(tag))
	metaResp, err := i.get(ctx, i.http, apiURL, "application/vnd.github+json")
	if err != nil {
		return Package{}, fmt.Errorf("resolve %s: %w", spec.app.ID, err)
	}
	if metaResp.StatusCode != http.StatusOK {
		metaResp.Body.Close()
		return Package{}, fmt.Errorf("resolve %s: release metadata returned HTTP %d", spec.app.ID, metaResp.StatusCode)
	}
	body, err := readMetadata(metaResp)
	if err != nil {
		return Package{}, fmt.Errorf("resolve %s: %w", spec.app.ID, err)
	}
	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string  `json:"name"`
			Size               int64   `json:"size"`
			Digest             *string `json:"digest"`
			BrowserDownloadURL string  `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return Package{}, fmt.Errorf("resolve %s: bad release metadata: %w", spec.app.ID, err)
	}
	if release.TagName != tag {
		return Package{}, fmt.Errorf("resolve %s: metadata tag %q does not match asset tag %q", spec.app.ID, release.TagName, tag)
	}

	var found *struct {
		Name               string  `json:"name"`
		Size               int64   `json:"size"`
		Digest             *string `json:"digest"`
		BrowserDownloadURL string  `json:"browser_download_url"`
	}
	for idx := range release.Assets {
		if release.Assets[idx].Name == assetName {
			if found != nil {
				return Package{}, fmt.Errorf("resolve %s: release has more than one %q asset", spec.app.ID, assetName)
			}
			found = &release.Assets[idx]
		}
	}
	if found == nil {
		return Package{}, fmt.Errorf("resolve %s: release metadata has no asset named %q", spec.app.ID, assetName)
	}
	if found.Digest == nil || !strings.HasPrefix(*found.Digest, "sha256:") {
		return Package{}, fmt.Errorf("%w: release asset %q has no official SHA-256 digest", ErrUnavailable, assetName)
	}
	digest := strings.TrimPrefix(*found.Digest, "sha256:")
	if len(digest) != 64 || !isHex(digest) {
		return Package{}, fmt.Errorf("%w: release asset %q has a malformed SHA-256 digest", ErrUnavailable, assetName)
	}
	if found.Size <= 0 {
		return Package{}, fmt.Errorf("%w: release asset %q has no published size", ErrUnavailable, assetName)
	}
	downloadURL, err := url.Parse(found.BrowserDownloadURL)
	if err != nil {
		return Package{}, fmt.Errorf("resolve %s: bad published asset URL: %w", spec.app.ID, err)
	}
	if err := checkOfficialURL(downloadURL); err != nil {
		return Package{}, err
	}
	dparts := strings.Split(strings.Trim(downloadURL.Path, "/"), "/")
	if len(dparts) != 6 || dparts[0] != owner || dparts[1] != repo ||
		dparts[2] != "releases" || dparts[3] != "download" ||
		dparts[4] != tag || dparts[5] != assetName {
		return Package{}, fmt.Errorf("resolve %s: published asset URL does not match the selected asset", spec.app.ID)
	}

	pkg.ReleaseVersion = strings.TrimPrefix(tag, "v")
	pkg.OfficialAssetURL = found.BrowserDownloadURL
	pkg.ExpectedSHA256 = strings.ToLower(digest)
	pkg.ExpectedBytes = found.Size
	if found.Size > int64(^uint64(0)>>1)-(1<<20) {
		return Package{}, errors.New("companioninstall: published asset size is implausible")
	}
	pkg.SizeCap = found.Size + (1 << 20)
	pkg.Provenance = Provenance{
		ResolverURL:  spec.endpoint,
		MetadataURL:  apiURL,
		SHA256Source: "GitHub release asset digest (sha256)",
		ReleaseTag:   tag,
		ResolvedAt:   i.now().UTC(),
	}
	if err := validatePackage(pkg, spec); err != nil {
		return Package{}, err
	}
	return pkg, nil
}

var (
	qbitDMGLink = regexp.MustCompile(`https://sourceforge\.net/projects/qbittorrent/files/qbittorrent-mac/qbittorrent-([0-9]+(?:\.[0-9]+)+)/qbittorrent-([0-9]+(?:\.[0-9]+)+)\.dmg/download`)
	qbitSHA     = regexp.MustCompile(`(?s)<td[^>]*>\s*([^<]+?)\s*</td>\s*<td[^>]*>\s*<code>\s*([0-9a-fA-F]{64})\s*</code>`)
)

// pageSection returns the heading block for id, from its <h3> to the next <h3>.
func pageSection(page, id string) (string, bool) {
	lower := strings.ToLower(page)
	marker := `id="` + strings.ToLower(id) + `"`
	at := strings.Index(lower, marker)
	if at < 0 {
		return "", false
	}
	start := strings.LastIndex(lower[:at], "<h3")
	if start < 0 {
		start = at
	}
	end := len(page)
	if next := strings.Index(lower[at:], "<h3"); next >= 0 {
		end = at + next
	}
	return page[start:end], true
}

func (i *Installer) resolveQBittorrent(ctx context.Context, spec appSpec) (Package, error) {
	pkg := basePackage(spec, i.sizeCap)

	resp, err := i.get(ctx, i.http, spec.endpoint, "")
	if err != nil {
		return Package{}, fmt.Errorf("resolve %s: %w", spec.app.ID, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return Package{}, fmt.Errorf("resolve %s: official download page returned HTTP %d", spec.app.ID, resp.StatusCode)
	}
	body, err := readMetadata(resp)
	if err != nil {
		return Package{}, fmt.Errorf("resolve %s: %w", spec.app.ID, err)
	}
	page := string(body)
	// Checksums are published per platform. Search only the macOS section so an
	// equal-version Windows or Linux row can never supply the digest.
	macSection, ok := pageSection(page, "macos")
	if !ok {
		return Package{}, fmt.Errorf("%w: qBittorrent's official page has no macOS section", ErrUnavailable)
	}
	page = macSection

	// The standard build is the bare *.dmg; the "lt20" variant links include
	// _lt20 and are deliberately not matched by this pattern.
	bestVersion, bestURL := "", ""
	for _, m := range qbitDMGLink.FindAllStringSubmatch(page, -1) {
		if m[1] != m[2] {
			continue
		}
		if bestVersion == "" || versionGreater(m[1], bestVersion) {
			bestVersion, bestURL = m[1], m[0]
		}
	}
	if bestVersion == "" {
		return Package{}, fmt.Errorf("%w: qBittorrent's official page lists no standard macOS DMG", ErrUnavailable)
	}

	seen := map[string]bool{}
	for _, m := range qbitSHA.FindAllStringSubmatch(page, -1) {
		if strings.TrimSpace(m[1]) == bestVersion {
			seen[strings.ToLower(m[2])] = true
		}
	}
	if len(seen) == 0 {
		return Package{}, fmt.Errorf("%w: qBittorrent's official page publishes no SHA-256 for macOS %s; refusing to self-hash", ErrUnavailable, bestVersion)
	}
	if len(seen) > 1 {
		return Package{}, fmt.Errorf("%w: qBittorrent's official page publishes more than one SHA-256 for macOS %s", ErrUnavailable, bestVersion)
	}
	var sha string
	for digest := range seen {
		sha = digest
	}

	assetURL, err := url.Parse(bestURL)
	if err != nil {
		return Package{}, fmt.Errorf("resolve %s: bad asset URL: %w", spec.app.ID, err)
	}
	if err := checkOfficialURL(assetURL); err != nil {
		return Package{}, err
	}

	pkg.ReleaseVersion = bestVersion
	pkg.OfficialAssetURL = bestURL
	pkg.ExpectedSHA256 = sha
	// The official page publishes no byte size for the DMG.
	pkg.ExpectedBytes = 0
	pkg.Provenance = Provenance{
		ResolverURL:  spec.endpoint,
		SHA256Source: "qbittorrent.org download page checksum table",
		ReleaseTag:   bestVersion,
		ResolvedAt:   i.now().UTC(),
	}
	if err := validatePackage(pkg, spec); err != nil {
		return Package{}, err
	}
	return pkg, nil
}

func basePackage(spec appSpec, sizeCap int64) Package {
	return Package{
		AppID:              spec.app.ID,
		Name:               spec.app.Name,
		BundleID:           spec.app.BundleID,
		NativeArchitecture: defaultArm64Label,
		SizeCap:            sizeCap,
	}
}

func versionGreater(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for n := 0; n < len(as) || n < len(bs); n++ {
		var av, bv int
		if n < len(as) {
			av, _ = strconv.Atoi(as[n])
		}
		if n < len(bs) {
			bv, _ = strconv.Atoi(bs[n])
		}
		if av != bv {
			return av > bv
		}
	}
	return false
}

// Resolve fetches the current official macOS package for id. All version, URL
// and digest fields come from the live vendor publication.
func (i *Installer) Resolve(ctx context.Context, id AppID) (Package, error) {
	spec, err := i.spec(id)
	if err != nil {
		return Package{}, err
	}
	ctx, cancel := i.ttl(ctx, i.resolveTimeout)
	defer cancel()
	switch spec.kind {
	case servarrSource:
		return i.resolveServarr(ctx, spec)
	case qbittorrentSource:
		return i.resolveQBittorrent(ctx, spec)
	default:
		return Package{}, fmt.Errorf("companioninstall: unknown source for %q", id)
	}
}

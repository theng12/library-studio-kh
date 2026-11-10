// Package companioninstall resolves, downloads, verifies and installs the
// official macOS builds of the companion apps Library Studio drives.
//
// It is intentionally not a downloader for media and it never launches an app.
// Package metadata (version, asset URL, SHA-256, size) always comes from the
// vendor's live official publication; nothing is pinned in this package.
//
// Apps published by the vendors are unsigned or self-signed. Verification here
// proves where a file came from and that it matches the vendor's published
// digest. It says nothing about Apple's Developer ID and never removes the
// quarantine attribute or otherwise bypasses the OS first-launch approval.
// Install reports OwnerApprovalNeeded so the caller can ask the owner before
// opening the new bundle through the normal OS flow.
package companioninstall

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// AppID identifies a companion app.
type AppID string

const (
	QBittorrent AppID = "qbittorrent"
	Sonarr      AppID = "sonarr"
	Radarr      AppID = "radarr"
	Lidarr      AppID = "lidarr"
	Prowlarr    AppID = "prowlarr"
	// Optional helpers. They are never in the default plan; the owner must
	// select them explicitly, and nothing runs them on its own.
	Recyclarr AppID = "recyclarr"
	Unpackerr AppID = "unpackerr"
	// Whisparr is an optional native Arr app (v3, Radarr-derived). It is off
	// by default and never part of the stock five. The owner must enable it
	// explicitly; nothing queries or starts it without that choice.
	Whisparr AppID = "whisparr"
)

// App is one entry in the official companion registry.
type App struct {
	ID       AppID  `json:"id"`
	Name     string `json:"name"`
	BundleID string `json:"bundleId"`
	// Optional marks helpers that are off by default in every plan and are
	// never part of the stock five.
	Optional bool `json:"optional"`
}

// Optional reports whether id is an optional helper that must be chosen
// explicitly and is never enabled by a default.
func Optional(id AppID) bool {
	for _, s := range specs {
		if s.app.ID == id {
			return s.app.Optional
		}
	}
	return false
}

// Package is a resolved, vendor-published macOS build. Every field is data
// from the live official publication, not a constant chosen by this package.
type Package struct {
	AppID              AppID
	Name               string
	BundleID           string
	NativeArchitecture string
	ReleaseVersion     string
	OfficialAssetURL   string
	ExpectedSHA256     string
	// ExpectedBytes is the vendor-published asset size. Zero means the
	// official source does not publish a size for this asset.
	ExpectedBytes int64
	// SizeCap is the hard download limit enforced while reading the body.
	SizeCap    int64
	Provenance Provenance
}

// Provenance records where the resolution came from so callers and receipts
// can show it without trusting the installer.
type Provenance struct {
	ResolverURL  string    `json:"resolverURL"`
	MetadataURL  string    `json:"metadataURL,omitempty"`
	SHA256Source string    `json:"sha256Source"`
	ReleaseTag   string    `json:"releaseTag,omitempty"`
	ResolvedAt   time.Time `json:"resolvedAt"`
}

// DetectState says what Detect found in the destination folder.
type DetectState string

const (
	// DetectMissing means no valid companion bundle is present.
	DetectMissing DetectState = "missing"
	// DetectAdopt means a valid bundle for the app is already installed and
	// will be kept as-is, with its actual version reported.
	DetectAdopt DetectState = "adopt"
	// DetectKeep means something is present that this package will not
	// overwrite, such as a foreign bundle at the expected name or an invalid
	// bundle for the expected identity.
	DetectKeep DetectState = "keep"
)

// DetectResult is the observed state of an app in a destination folder.
type DetectResult struct {
	AppID    AppID
	Path     string
	Version  string
	BundleID string
	State    DetectState
}

// InstallState describes what Install did.
type InstallState string

const (
	// Installed means a new bundle was copied into the destination.
	Installed InstallState = "installed"
	// Adopted means a valid existing bundle was kept, with no write.
	Adopted InstallState = "adopted"
	// Kept means an existing bundle was kept and nothing was replaced.
	Kept InstallState = "kept"
)

// InstallResult reports the actual bundle on disk after Install.
type InstallResult struct {
	AppID    AppID
	Path     string
	Version  string
	BundleID string
	// Existing is true when the bundle was already present and was not
	// overwritten.
	Existing bool
	State    InstallState
	// OwnerApprovalNeeded is true for a freshly installed vendor build. The
	// caller should ask the owner before opening it; nothing here opens it or
	// clears the OS first-launch approval.
	OwnerApprovalNeeded bool
	ReceiptPath         string
}

// Receipt is the immutable record written for a new bundle. It is written
// once with read-only permissions and never replaced.
type Receipt struct {
	Package             Package   `json:"package"`
	InstalledPath       string    `json:"installedPath"`
	InstalledVersion    string    `json:"installedVersion"`
	InstalledBundleID   string    `json:"installedBundleID"`
	InstalledAt         time.Time `json:"installedAt"`
	Installer           string    `json:"installer"`
	OwnerApprovalNeeded bool      `json:"ownerApprovalNeeded"`
	// QuarantineAdded is true only when this installer wrote the standard
	// com.apple.quarantine attribute on the promoted bundle. macOS, not the
	// installer, then decides first launch.
	QuarantineAdded bool   `json:"quarantineAdded"`
	QuarantineAgent string `json:"quarantineAgent,omitempty"`
	// AppLaunched and CodeSignatureModified are always false; they are recorded
	// so a receipt never implies work this package did not do.
	AppLaunched           bool `json:"appLaunched"`
	CodeSignatureModified bool `json:"codeSignatureModified"`
}

// ErrBundleConflict means a bundle this package must not replace is already at
// the destination. The existing bundle is left untouched.
var ErrBundleConflict = errors.New("a different or invalid app bundle already occupies the install location")

// ErrUnavailable means the official source did not publish metadata that can be
// verified. The resolver refuses to guess or self-hash in that case.
var ErrUnavailable = errors.New("official package metadata unavailable")

// ErrStalePackage means the official publication changed after the caller
// resolved the package. Install stops before downloading or writing anything;
// the caller must resolve again and renew the owner's approval.
var ErrStalePackage = errors.New("official package changed since it was resolved; resolve again and renew owner approval")

// Doer is the HTTP client dependency. A custom Doer must follow or reject
// redirects itself; the default client enforces the HTTPS host allowlist on
// every redirect.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// CommandRunner runs the macOS built-in tools used for installation. Tests
// inject a fake so no real command is ever run.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// Options configures an Installer. Zero values select safe defaults.
type Options struct {
	HTTPClient Doer
	Commands   CommandRunner
	// Tool names, normally the macOS built-ins.
	HDIUtil string
	Ditto   string
	Plutil  string
	Xattr   string
	Tar     string

	ResolveTimeout  time.Duration
	DownloadTimeout time.Duration
	// SizeCap bounds a download when the official source publishes no size.
	SizeCap    int64
	RetryDelay func(attempt int) time.Duration
	Now        func() time.Time
}

const (
	defaultSizeCap     = int64(2 << 30)
	defaultResolve     = 30 * time.Second
	defaultDownload    = 30 * time.Minute
	defaultRetryDelay  = time.Second
	metadataReadLimit  = int64(4 << 20)
	installerName      = "library-studio-kh companioninstall"
	trashDirName       = ".library-studio-trash"
	receiptDirName     = ".library-studio-receipts"
	defaultArm64Label  = "arm64"
	maxDownloadAttempt = 3
	// quarantineFlags is the standard "downloaded" flag set. The agent name is
	// ours, never a browser's, and macOS remains the authority that prompts.
	quarantineFlags = "0083"
	quarantineAgent = "Library Studio KH"
)

// Installer resolves and installs packages. Create one with New.
type Installer struct {
	http            Doer
	run             CommandRunner
	hdiutil         string
	ditto           string
	plutil          string
	xattr           string
	tar             string
	resolveTimeout  time.Duration
	downloadTimeout time.Duration
	sizeCap         int64
	retryDelay      func(int) time.Duration
	now             func() time.Time
}

// New returns an Installer. The default client follows redirects only between
// HTTPS hosts on the official publication allowlist.
func New(opts Options) *Installer {
	i := &Installer{
		http:            opts.HTTPClient,
		run:             opts.Commands,
		hdiutil:         opts.HDIUtil,
		ditto:           opts.Ditto,
		plutil:          opts.Plutil,
		xattr:           opts.Xattr,
		tar:             opts.Tar,
		resolveTimeout:  opts.ResolveTimeout,
		downloadTimeout: opts.DownloadTimeout,
		sizeCap:         opts.SizeCap,
		retryDelay:      opts.RetryDelay,
		now:             opts.Now,
	}
	if i.http == nil {
		i.http = &http.Client{CheckRedirect: checkRedirect}
	}
	if i.run == nil {
		i.run = execRunner{}
	}
	if i.hdiutil == "" {
		i.hdiutil = "hdiutil"
	}
	if i.ditto == "" {
		i.ditto = "ditto"
	}
	if i.plutil == "" {
		i.plutil = "plutil"
	}
	if i.xattr == "" {
		i.xattr = "xattr"
	}
	if i.tar == "" {
		i.tar = "tar"
	}
	if i.resolveTimeout <= 0 {
		i.resolveTimeout = defaultResolve
	}
	if i.downloadTimeout <= 0 {
		i.downloadTimeout = defaultDownload
	}
	if i.sizeCap <= 0 {
		i.sizeCap = defaultSizeCap
	}
	if i.retryDelay == nil {
		i.retryDelay = func(attempt int) time.Duration {
			return time.Duration(attempt) * defaultRetryDelay
		}
	}
	if i.now == nil {
		i.now = time.Now
	}
	return i
}

var shared = New(Options{})

// Registry lists the companion apps this package knows how to install.
func Registry() []App {
	out := make([]App, 0, len(specs))
	for _, s := range specs {
		out = append(out, s.app)
	}
	return out
}

// Resolve fetches the current official macOS package for id.
func Resolve(ctx context.Context, id AppID) (Package, error) {
	return shared.Resolve(ctx, id)
}

// Detect reports what is already installed for id under appDir.
func Detect(ctx context.Context, id AppID, appDir string) (DetectResult, error) {
	return shared.Detect(ctx, id, appDir)
}

// Install installs pkg under appDir, which the caller normally sets to
// ~/Applications. It never replaces an existing bundle.
func Install(ctx context.Context, pkg Package, appDir string) (InstallResult, error) {
	return shared.Install(ctx, pkg, appDir)
}

type sourceKind int

const (
	servarrSource sourceKind = iota
	qbittorrentSource
	// githubSource reads the vendor's own GitHub "latest" release and picks
	// the single macOS asset matching assetPattern.
	githubSource
	// binarySource is an official archive containing a bare command-line
	// binary instead of an .app bundle (Recyclarr).
	binarySource
)

// Endpoints are the vendors' live "latest" addresses. The returned asset is
// inspected, never assumed.
type appSpec struct {
	app        App
	bundleName string
	kind       sourceKind
	owner      string
	repo       string
	endpoint   string
	// assetPattern names the one supported macOS arm64 asset in a GitHub
	// release; the pattern is fixed here, the version, URL, digest and size
	// always come from the live release.
	assetPattern string
}

var specs = []appSpec{
	{
		app:        App{ID: QBittorrent, Name: "qBittorrent", BundleID: "org.qbittorrent.qBittorrent"},
		bundleName: "qbittorrent.app",
		kind:       qbittorrentSource,
		endpoint:   "https://www.qbittorrent.org/download",
	},
	{
		app:        App{ID: Sonarr, Name: "Sonarr", BundleID: "com.osx.sonarr.tv"},
		bundleName: "Sonarr.app",
		kind:       servarrSource,
		owner:      "Sonarr",
		repo:       "Sonarr",
		endpoint:   "https://services.sonarr.tv/v1/download/main/latest?version=4&os=macos&arch=arm64&installer=true",
	},
	{
		app:        App{ID: Radarr, Name: "Radarr", BundleID: "com.osx.radarr.video"},
		bundleName: "Radarr.app",
		kind:       servarrSource,
		owner:      "Radarr",
		repo:       "Radarr",
		endpoint:   "https://radarr.servarr.com/v1/update/master/updatefile?os=osx&runtime=netcore&arch=arm64&installer=true",
	},
	{
		app:        App{ID: Lidarr, Name: "Lidarr", BundleID: "com.osx.lidarr.audio"},
		bundleName: "Lidarr.app",
		kind:       servarrSource,
		owner:      "Lidarr",
		repo:       "Lidarr",
		endpoint:   "https://lidarr.servarr.com/v1/update/master/updatefile?os=osx&runtime=netcore&arch=arm64&installer=true",
	},
	{
		app:        App{ID: Prowlarr, Name: "Prowlarr", BundleID: "com.osx.prowlarr.com"},
		bundleName: "Prowlarr.app",
		kind:       servarrSource,
		owner:      "Prowlarr",
		repo:       "Prowlarr",
		endpoint:   "https://prowlarr.servarr.com/v1/update/master/updatefile?os=osx&runtime=netcore&arch=arm64&installer=true",
	},
	// Recyclarr ships a bare macOS arm64 command-line binary, not an app
	// bundle. It never runs by itself; Library only ever runs it for an
	// owner-requested preview or apply.
	{
		app:          App{ID: Recyclarr, Name: "Recyclarr", Optional: true},
		bundleName:   "recyclarr",
		kind:         binarySource,
		owner:        "recyclarr",
		repo:         "recyclarr",
		endpoint:     "https://api.github.com/repos/recyclarr/recyclarr/releases/latest",
		assetPattern: `^recyclarr-osx-arm64\.tar\.xz$`,
	},
	// Unpackerr ships a macOS disk image with a real app bundle.
	{
		app:          App{ID: Unpackerr, Name: "Unpackerr", BundleID: "io.golift.unpackerr", Optional: true},
		bundleName:   "Unpackerr.app",
		kind:         githubSource,
		owner:        "Unpackerr",
		repo:         "unpackerr",
		endpoint:     "https://api.github.com/repos/Unpackerr/unpackerr/releases/latest",
		assetPattern: `^Unpackerr\.dmg$`,
	},
	// Whisparr v3 (Radarr-derived) ships a macOS arm64 app zip from its own
	// GitHub repo. The official source is whisparr/whisparr-eros, not the
	// v2-develop whisparr/whisparr. Version, URL, digest and size always
	// come from the live release; nothing here is pinned.
	{
		app:          App{ID: Whisparr, Name: "Whisparr", BundleID: "com.osx.whisparr.video", Optional: true},
		bundleName:   "Whisparr.app",
		kind:         githubSource,
		owner:        "Whisparr",
		repo:         "Whisparr-Eros",
		endpoint:     "https://api.github.com/repos/Whisparr/Whisparr-Eros/releases/latest",
		assetPattern: `^Whisparr\.eros\.[0-9]+(?:\.[0-9]+)+(?:-[0-9A-Za-z.-]+)?\.osx-arm64-app\.zip$`,
	},
}

func (i *Installer) spec(id AppID) (appSpec, error) {
	for _, s := range specs {
		if s.app.ID == id {
			return s, nil
		}
	}
	return appSpec{}, fmt.Errorf("companioninstall: unknown app %q", id)
}

func validatePackage(pkg Package, spec appSpec) error {
	switch {
	case pkg.AppID != spec.app.ID:
		return fmt.Errorf("companioninstall: package app %q does not match %q", pkg.AppID, spec.app.ID)
	case pkg.Name != spec.app.Name:
		return fmt.Errorf("companioninstall: package name %q does not match %q", pkg.Name, spec.app.Name)
	case pkg.BundleID != spec.app.BundleID:
		return fmt.Errorf("companioninstall: package bundle %q does not match %q", pkg.BundleID, spec.app.BundleID)
	case pkg.NativeArchitecture != defaultArm64Label:
		return fmt.Errorf("companioninstall: unsupported architecture %q", pkg.NativeArchitecture)
	case strings.TrimSpace(pkg.OfficialAssetURL) == "":
		return errors.New("companioninstall: package has no official asset URL; resolve it first")
	case len(pkg.ExpectedSHA256) != 64 || !isHex(pkg.ExpectedSHA256):
		return fmt.Errorf("companioninstall: package has no valid official SHA-256 (got %q)", pkg.ExpectedSHA256)
	case pkg.ExpectedBytes < 0:
		return errors.New("companioninstall: package has a negative expected size")
	case pkg.SizeCap <= 0:
		return errors.New("companioninstall: package has no download size cap")
	case pkg.ExpectedBytes > 0 && pkg.ExpectedBytes > pkg.SizeCap:
		return errors.New("companioninstall: expected size exceeds the download size cap")
	}
	return nil
}

func isHex(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

func (i *Installer) ttl(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, d)
}

// executeCommand refuses anything but the configured macOS built-ins, then
// delegates to the injectable runner. tar is the system bsdtar: it reads the
// vendor xz archive and fails closed on escaping or symlinked entries.
func (i *Installer) executeCommand(ctx context.Context, tool string, args ...string) ([]byte, error) {
	switch tool {
	case i.hdiutil, i.ditto, i.plutil, i.xattr, i.tar:
	default:
		return nil, fmt.Errorf("companioninstall: refusing to run %q", tool)
	}
	return i.run.Run(ctx, tool, args...)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func cleanDestination(appDir string) (string, error) {
	if strings.TrimSpace(appDir) == "" {
		return "", errors.New("companioninstall: destination folder is empty")
	}
	if !filepath.IsAbs(appDir) {
		return "", fmt.Errorf("companioninstall: destination folder must be absolute, got %q", appDir)
	}
	return filepath.Clean(appDir), nil
}

package platform

import (
	"path/filepath"

	"librarystudio/internal/companioninstall"
)

// CompanionID is one of the five stock apps Library already manages. No other
// ID is accepted, so the launch label and flags can never be caller-chosen.
type CompanionID string

const (
	CompanionQBittorrent CompanionID = "qbittorrent"
	CompanionSonarr      CompanionID = "sonarr"
	CompanionRadarr      CompanionID = "radarr"
	CompanionLidarr      CompanionID = "lidarr"
	CompanionProwlarr    CompanionID = "prowlarr"
)

const companionLabelPrefix = "kh.librarystudio.companion."

type companionApp struct {
	bundle string
	args   func(profileDir string) []string
}

func arrArgs(profileDir string) []string {
	return []string{"-data=" + profileDir, "-nobrowser"}
}

// companionApps is the allowlist. The manager only ever launches these exact
// bundle names through /usr/bin/open; it never runs an app's own executable.
var companionApps = map[CompanionID]companionApp{
	CompanionQBittorrent: {bundle: "qBittorrent.app", args: func(dir string) []string { return []string{"--profile=" + dir} }},
	CompanionSonarr:      {bundle: "Sonarr.app", args: arrArgs},
	CompanionRadarr:      {bundle: "Radarr.app", args: arrArgs},
	CompanionLidarr:      {bundle: "Lidarr.app", args: arrArgs},
	CompanionProwlarr:    {bundle: "Prowlarr.app", args: arrArgs},
}

// CompanionServiceSpec is the only input to the companion at-login manager.
// AppPath is an existing .app bundle with the expected name; ProfileDir must be
// the Library-managed profile under Home/companions/<id>. AtLogin is the
// owner's explicit switch and must be true for install.
type CompanionServiceSpec struct {
	ID         CompanionID `json:"id"`
	AppPath    string      `json:"appPath"`
	ProfileDir string      `json:"profileDir"`
	Home       string      `json:"home"`
	AtLogin    bool        `json:"atLogin"`
}

// CompanionServiceStatus is the public contract for the guided-setup API.
// It shares the host state names and never contains credentials.
type CompanionServiceStatus struct {
	Supported bool             `json:"supported"`
	State     HostServiceState `json:"state"`
	ID        CompanionID      `json:"id"`
	Label     string           `json:"label"`
	Installed bool             `json:"installed"`
	Loaded    bool             `json:"loaded"`
	Detail    string           `json:"detail,omitempty"`
}

// CompanionServiceLabel returns the one Library-owned label for a known ID,
// or "" for anything else. Callers cannot supply a label.
func CompanionServiceLabel(id CompanionID) string {
	if _, ok := companionApps[id]; !ok {
		return ""
	}
	return companionLabelPrefix + string(id)
}

// CompanionServiceProfileDir is the only profile path accepted for a known ID.
func CompanionServiceProfileDir(home string, id CompanionID) string {
	return filepath.Join(home, "companions", string(id))
}

// companionBundleID comes from the verified installer registry, not a second
// list kept here.
func companionBundleID(id CompanionID) string {
	for _, app := range companioninstall.Registry() {
		if CompanionID(app.ID) == id {
			return app.BundleID
		}
	}
	return ""
}

// CompanionService installs optional at-login jobs for new Library-managed
// companion profiles. It never adopts a stock or external job.
type CompanionService struct {
	runner   CommandRunner
	agentDir string // tests override; empty uses the real LaunchAgents folder
}

func NewCompanionService(runner CommandRunner) *CompanionService {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &CompanionService{runner: runner}
}

package platform

import "os/exec"

// HostServiceState is the reported state of Library's own macOS login job.
// The states are deliberately distinct so a setup screen can say what is true.
type HostServiceState string

const (
	HostServiceUnsupported  HostServiceState = "unsupported"   // this OS is not verified
	HostServiceNotInstalled HostServiceState = "not_installed" // desktop-only is in use
	HostServiceInstalled    HostServiceState = "installed"     // login job saved, not loaded
	HostServiceLoaded       HostServiceState = "loaded"        // login job loaded, health unproven
	HostServiceRunning      HostServiceState = "running"       // descriptor proved healthy
	HostServiceNeedsRestart HostServiceState = "needs_restart" // loaded but blocked; reopen the app
	HostServiceBlocked      HostServiceState = "blocked"       // unsafe or foreign job; nothing changed
)

// HostServiceLabel is the only launch job this manager ever writes.
const HostServiceLabel = "kh.librarystudio.host"

// HostServiceSpec is everything the manager needs. Paths are absolute; the
// configuration home is the same folder the desktop and background host use.
type HostServiceSpec struct {
	// Exe is a trusted local packaging value, never a server RPC input. The
	// desktop/server must pass os.Executable(); only the local CLI may override it.
	Exe  string `json:"exe"`
	Home string `json:"home"`
	Port int    `json:"port"`
}

// HostServiceStatus is the public contract for the server API and job UI.
// It never contains the bootstrap token or a listen address.
type HostServiceStatus struct {
	Supported    bool             `json:"supported"`
	State        HostServiceState `json:"state"`
	Label        string           `json:"label"`
	Installed    bool             `json:"installed"`
	Loaded       bool             `json:"loaded"`
	Running      bool             `json:"running"`
	NeedsRestart bool             `json:"needsRestart"`
	Detail       string           `json:"detail,omitempty"`
}

// CommandRunner is injected so tests never call a real launchctl.
type CommandRunner interface {
	Run(name string, args ...string) ([]byte, error)
}

// ExecRunner runs the real command, used by the desktop entry point.
type ExecRunner struct{}

func (ExecRunner) Run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// HostService manages Library's own login job. verify, when set, proves the
// running host owns the private descriptor in home; nil means health is not
// proven and the manager reports "loaded", never "running".
type HostService struct {
	runner   CommandRunner
	verify   func(home string) error
	agentDir string // tests override; empty uses the real LaunchAgents folder
}

func NewHostService(runner CommandRunner, verify func(home string) error) *HostService {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &HostService{runner: runner, verify: verify}
}

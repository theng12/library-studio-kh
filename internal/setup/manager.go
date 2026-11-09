package setup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"librarystudio/internal/companioninstall"
	"librarystudio/internal/config"
	"librarystudio/internal/platform"
	"librarystudio/internal/stock"
)

// Request contains owner choices, never package URLs, hashes or native credentials.
type Request struct {
	Apps                   []companioninstall.AppID `json:"apps"`
	AppDir                 string                   `json:"appDir"`
	DownloadPath           string                   `json:"downloadPath"`
	Roots                  map[string]string        `json:"roots"`
	Ports                  map[string]int           `json:"ports"`
	Categories             map[string]string        `json:"categories"`
	RenameTV               bool                     `json:"renameTV"`
	StartAtLogin           bool                     `json:"startAtLogin"`
	MusicQualityProfileID  int                      `json:"musicQualityProfileId"`
	MusicMetadataProfileID int                      `json:"musicMetadataProfileId"`
}

type AppStep struct {
	ID                 companioninstall.AppID `json:"id"`
	Name               string                 `json:"name"`
	State              string                 `json:"state"`
	Detail             string                 `json:"detail"`
	Version            string                 `json:"version,omitempty"`
	ExpectedBytes      int64                  `json:"expectedBytes"`
	SizeCap            int64                  `json:"sizeCap"`
	Source             string                 `json:"source,omitempty"`
	Path               string                 `json:"path,omitempty"`
	Managed            bool                   `json:"managed"`
	ConnectionDetected bool                   `json:"connectionDetected"`
}

type Check struct {
	App    string `json:"app"`
	Name   string `json:"name"`
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
}

type Job struct {
	ID            string         `json:"id"`
	State         string         `json:"state"`
	Detail        string         `json:"detail"`
	Request       Request        `json:"request"`
	Apps          []AppStep      `json:"apps"`
	Checks        []Check        `json:"checks"`
	MusicProfiles *stock.Options `json:"musicProfiles,omitempty"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

type savedJob struct {
	Version   int                                                 `json:"version"`
	View      Job                                                 `json:"view"`
	Packages  map[companioninstall.AppID]companioninstall.Package `json:"packages"`
	Baseline  config.Config                                       `json:"baseline"`
	Candidate config.Config                                       `json:"candidate"`
}

type packageInstaller interface {
	Resolve(context.Context, companioninstall.AppID) (companioninstall.Package, error)
	Detect(context.Context, companioninstall.AppID, string) (companioninstall.DetectResult, error)
	Install(context.Context, companioninstall.Package, string) (companioninstall.InstallResult, error)
}

// Manager owns one resumable setup. Native credentials stay in the private file.
type Manager struct {
	mu               sync.Mutex
	home             string
	store            *config.Store
	installer        packageInstaller
	job              *savedJob
	busy             bool
	loadErr          error
	cancel           context.CancelFunc
	openApp          func(context.Context, string, []string) error
	applicationDirs  []string
	hostPort         int
	installHost      func(platform.HostServiceSpec) (platform.HostServiceStatus, error)
	installCompanion func(platform.CompanionServiceSpec) (platform.CompanionServiceStatus, error)
	// runTool executes a vendor command-line tool for an explicit owner
	// action (Recyclarr preview/apply). Tests inject a fake; the setup flow
	// itself never calls it.
	runTool          func(ctx context.Context, name string, args ...string) ([]byte, error)
	recyclarrCfgPath func() (string, error)
}

func NewManager(home string, store *config.Store) *Manager {
	m := &Manager{home: home, store: store, installer: companioninstall.New(companioninstall.Options{}), openApp: openNativeApp, runTool: defaultRunTool, recyclarrCfgPath: recyclarrConfigPath}
	m.installHost = platform.NewHostService(nil, nil).Install
	m.installCompanion = platform.NewCompanionService(nil).Install
	m.applicationDirs = []string{"/Applications"}
	if userHome, err := os.UserHomeDir(); err == nil {
		m.applicationDirs = append(m.applicationDirs, filepath.Join(userHome, "Applications"))
	}
	path := filepath.Join(home, "setup-job.json")
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return m
	}
	if err != nil {
		m.loadErr = err
		return m
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0o077 != 0 || fi.Size() > 1<<20 {
		m.loadErr = errors.New("saved setup must be a private regular file within the 1 MiB limit")
		return m
	}
	data, err := os.ReadFile(path)
	if err != nil {
		m.loadErr = err
		return m
	}
	var job savedJob
	if json.Unmarshal(data, &job) != nil || job.Version != 1 || job.View.ID == "" {
		m.loadErr = errors.New("saved setup is incomplete; retained for repair")
		return m
	}
	m.job = &job
	if activeState(job.View.State) {
		m.job.View.State = "interrupted"
		m.job.View.Detail = "Library restarted during setup. Review the saved steps and retry; existing installations and settings will be checked before continuing."
	}
	return m
}

func activeState(state string) bool {
	return state == "planning" || state == "installing" || state == "checking" || state == "configuring"
}

func (m *Manager) Status() (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loadErr != nil {
		return Job{}, m.loadErr
	}
	if m.job == nil {
		return Job{State: "not_started", Apps: []AppStep{}, Checks: []Check{}}, nil
	}
	return cloneJob(m.job.View), nil
}

func cloneJob(job Job) Job {
	data, _ := json.Marshal(job)
	var copy Job
	_ = json.Unmarshal(data, &copy)
	return copy
}

func (m *Manager) saveLocked() error {
	m.job.View.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(m.job, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.home, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(m.home, ".setup-job-*")
	if err != nil {
		return err
	}
	// Failed writes are retained for repair rather than deleting evidence.
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(m.home, "setup-job.json"))
}

func (m *Manager) update(state, detail string, index int, step *AppStep) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.job.View.State = state
	m.job.View.Detail = detail
	if step != nil {
		m.job.View.Apps[index] = *step
	}
	if err := m.saveLocked(); err != nil {
		m.job.View.State = "failed"
		m.job.View.Detail = "Setup state could not be saved. No further setup actions will run."
		return err
	}
	return nil
}

func (m *Manager) Plan(req Request) (Job, error) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return Job{}, errors.New("automatic companion installation currently supports Apple Silicon macOS; use an existing host on this device")
	}
	if err := validateRequest(req); err != nil {
		return Job{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loadErr != nil {
		return Job{}, m.loadErr
	}
	if m.busy {
		return Job{}, errors.New("setup is already working; wait for the current step")
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return Job{}, err
	}
	baseline := m.store.Get()
	job := &savedJob{Version: 1, View: Job{ID: hex.EncodeToString(id), State: "planning", Detail: "Checking installed apps and current official packages…", Request: req, Apps: []AppStep{}, Checks: []Check{}}, Packages: map[companioninstall.AppID]companioninstall.Package{}, Baseline: baseline, Candidate: baseline}
	for _, id := range req.Apps {
		for _, app := range companioninstall.Registry() {
			if app.ID == id {
				job.View.Apps = append(job.View.Apps, AppStep{ID: id, Name: app.Name, State: "not_checked", Detail: "Waiting for inspection"})
			}
		}
	}
	// Preserve the previous setup record before replacing the active plan.
	if m.job != nil {
		old, _ := json.MarshalIndent(m.job, "", "  ")
		archive := filepath.Join(m.home, "setup-history")
		if err := os.MkdirAll(archive, 0o700); err != nil {
			return Job{}, err
		}
		if err := writeNewProfileFile(filepath.Join(archive, m.job.View.ID+"-"+fmt.Sprint(time.Now().UnixNano())+".json"), old); err != nil {
			return Job{}, err
		}
	}
	m.job = job
	if err := m.saveLocked(); err != nil {
		return Job{}, err
	}
	m.busy = true
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	go m.resolve(ctx)
	return cloneJob(job.View), nil
}

func (m *Manager) resolve(ctx context.Context) {
	defer m.finishWork()
	for index, app := range m.job.View.Apps {
		detected, err := m.detectExisting(ctx, app.ID, m.job.View.Request.AppDir)
		if err != nil {
			m.fail("Could not inspect " + app.Name + ": " + safeSetupError(err, m.job.Candidate))
			return
		}
		if detected.State == companioninstall.DetectKeep {
			m.fail(app.Name + " has a conflicting installation. It was kept untouched; choose another destination or connect it explicitly.")
			return
		}
		app.Path = detected.Path
		app.Version = detected.Version
		if detected.State == companioninstall.DetectAdopt {
			app.State = "adopt"
			app.Detail = "Keep the installed app and its native settings"
			m.adoptConnection(&app)
		} else {
			if err := m.update("planning", "Reading the current official "+app.Name+" package…", index, &app); err != nil {
				return
			}
			pkg, err := m.installer.Resolve(ctx, app.ID)
			if err != nil {
				m.fail("Official " + app.Name + " package could not be verified: " + safeSetupError(err, m.job.Candidate))
				return
			}
			m.mu.Lock()
			m.job.Packages[app.ID] = pkg
			m.mu.Unlock()
			app.State = "download"
			app.Version = pkg.ReleaseVersion
			app.ExpectedBytes = pkg.ExpectedBytes
			app.SizeCap = pkg.SizeCap
			app.Source = pkg.OfficialAssetURL
			app.Detail = "Download and verify the official package"
		}
		if err := m.update("planning", "Reviewing selected apps…", index, &app); err != nil {
			return
		}
	}
	_ = m.update("planned", "Review the apps, folders and download limits, then install the selected apps.", -1, nil)
}

func (m *Manager) finishWork() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.busy = false
}
func (m *Manager) fail(detail string) { _ = m.update("failed", detail, -1, nil) }

func validateRequest(req Request) error {
	if len(req.Apps) == 0 {
		return errors.New("select at least one companion app")
	}
	if !filepath.IsAbs(req.AppDir) || !filepath.IsAbs(req.DownloadPath) {
		return errors.New("choose absolute application and download folders")
	}
	known := map[companioninstall.AppID]bool{}
	for _, app := range companioninstall.Registry() {
		known[app.ID] = true
	}
	seen := map[companioninstall.AppID]bool{}
	ports := map[int]bool{}
	for _, id := range req.Apps {
		if !known[id] || seen[id] {
			return errors.New("select each supported companion only once")
		}
		seen[id] = true
		if companioninstall.Optional(id) {
			// Optional helpers have no port or library of their own; they are
			// configured from the instances they assist.
			continue
		}
		port := req.Ports[string(id)]
		if port < 1024 || port > 65535 || ports[port] {
			return errors.New("choose a different port from 1024 to 65535 for each selected app")
		}
		ports[port] = true
		if id == companioninstall.Sonarr || id == companioninstall.Radarr || id == companioninstall.Lidarr {
			if !filepath.IsAbs(req.Roots[string(id)]) {
				return errors.New("choose an absolute library root for each selected media app")
			}
			if req.Categories[string(id)] == "" {
				return errors.New("choose a download category for each selected media app")
			}
		}
	}
	return nil
}

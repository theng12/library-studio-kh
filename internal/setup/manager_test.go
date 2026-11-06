package setup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"librarystudio/internal/companioninstall"
	"librarystudio/internal/config"
)

type fakePackages struct {
	installed    bool
	installErr   error
	existingDir  string
	resolveCalls int
}

func (f *fakePackages) Resolve(context.Context, companioninstall.AppID) (companioninstall.Package, error) {
	f.resolveCalls++
	return companioninstall.Package{}, errors.New("no live resolution in tests")
}
func (f *fakePackages) Detect(_ context.Context, id companioninstall.AppID, dir string) (companioninstall.DetectResult, error) {
	state := companioninstall.DetectMissing
	if f.installed || dir == f.existingDir {
		state = companioninstall.DetectAdopt
	}
	return companioninstall.DetectResult{AppID: id, State: state, Path: filepath.Join(dir, "qbittorrent.app"), Version: "fixture"}, nil
}
func (f *fakePackages) Install(_ context.Context, p companioninstall.Package, dir string) (companioninstall.InstallResult, error) {
	if f.installErr != nil {
		return companioninstall.InstallResult{}, f.installErr
	}
	f.installed = true
	return companioninstall.InstallResult{AppID: p.AppID, Path: filepath.Join(dir, "qbittorrent.app"), Version: "fixture", State: companioninstall.Installed, OwnerApprovalNeeded: true}, nil
}
func managerFixture(t *testing.T) (*Manager, *fakePackages) {
	t.Helper()
	home := t.TempDir()
	store, err := config.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(home, store)
	fake := &fakePackages{}
	m.installer = fake
	m.applicationDirs = nil
	c := store.Get()
	m.job = &savedJob{Version: 1, View: Job{ID: strings.Repeat("a", 32), State: "installing", Request: Request{Apps: []companioninstall.AppID{companioninstall.QBittorrent}, AppDir: filepath.Join(home, "Applications"), DownloadPath: filepath.Join(home, "Downloads"), Ports: map[string]int{"qbittorrent": 8080}}, Apps: []AppStep{{ID: companioninstall.QBittorrent, Name: "qBittorrent", State: "download"}}, Checks: []Check{}}, Packages: map[companioninstall.AppID]companioninstall.Package{companioninstall.QBittorrent: {AppID: companioninstall.QBittorrent}}, Baseline: c, Candidate: c}
	return m, fake
}

func TestInstallRequiresSeparateOwnerOpenAndPreservesLibraryAccount(t *testing.T) {
	m, _ := managerFixture(t)
	opened := false
	m.openApp = func(context.Context, string, []string) error { opened = true; return nil }
	before := m.store.Get()
	m.install(context.Background())
	job, err := m.Status()
	if err != nil {
		t.Fatal(err)
	}
	if job.State != "needs_approval" || !job.Apps[0].Managed || job.Apps[0].State != "needs_open" || opened {
		t.Fatal("install auto-launched or claimed readiness")
	}
	if m.store.Get() != before {
		t.Fatal("unverified account entered Library settings")
	}
	secret := m.job.Candidate.QBit.Password
	data, _ := json.Marshal(job)
	if secret == "" || strings.Contains(string(data), secret) {
		t.Fatal("private login escaped into job response")
	}
	loaded := NewManager(m.home, m.store)
	if loaded.loadErr != nil || loaded.job.Candidate.QBit.Password != secret {
		t.Fatal("private resumable credential was lost")
	}
	openedJob, err := m.Open(context.Background(), job.ID, companioninstall.QBittorrent)
	if err != nil || !opened || openedJob.Apps[0].State != "opened" {
		t.Fatal("explicit owner opening failed")
	}
}

func TestFailedInstallRetainsExplicitFailureWithoutLaunching(t *testing.T) {
	m, fake := managerFixture(t)
	fake.installErr = errors.New("fixture checksum failure")
	m.install(context.Background())
	job, _ := m.Status()
	if job.State != "failed" {
		t.Fatal("failed install looked successful")
	}
	if _, err := os.Stat(filepath.Join(m.home, "companions")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed package created a native profile")
	}
	if _, err := m.Open(context.Background(), job.ID, companioninstall.QBittorrent); err == nil {
		t.Fatal("failed package could be opened")
	}
}

func TestExistingGlobalAppIsAdoptedBeforeResolvingAnotherCopy(t *testing.T) {
	m, fake := managerFixture(t)
	fake.existingDir = filepath.Join(t.TempDir(), "ExistingApps")
	m.applicationDirs = []string{fake.existingDir}
	m.resolve(context.Background())
	job, _ := m.Status()
	if job.State != "planned" || job.Apps[0].State != "adopt" || job.Apps[0].Path != filepath.Join(fake.existingDir, "qbittorrent.app") || fake.resolveCalls != 0 {
		t.Fatal("existing global app was ignored or a duplicate package resolved")
	}
}

func TestInterruptedSetupAndPrivateFolderRules(t *testing.T) {
	m, _ := managerFixture(t)
	if err := m.saveLocked(); err != nil {
		t.Fatal(err)
	}
	loaded := NewManager(m.home, m.store)
	job, err := loaded.Status()
	if err != nil || job.State != "interrupted" {
		t.Fatal("interrupted setup was not resumable")
	}
	path := filepath.Join(t.TempDir(), "new", "media")
	if err := ensureSetupFolder(path); err != nil {
		t.Fatal(err)
	}
	if err := ensureSetupFolder(filepath.Join(path, "file")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "not-a-folder"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureSetupFolder(filepath.Join(path, "not-a-folder")); err == nil {
		t.Fatal("file accepted as a destination")
	}
}

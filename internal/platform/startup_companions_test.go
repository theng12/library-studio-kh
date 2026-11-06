//go:build darwin

package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func companionFixture(t *testing.T, id CompanionID) (*CompanionService, CompanionServiceSpec, *fakeRunner) {
	t.Helper()
	dir := t.TempDir()
	home := filepath.Join(dir, "Library Studio KH")
	profile := CompanionServiceProfileDir(home, id)
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	if id == CompanionQBittorrent {
		cfg := filepath.Join(profile, "qBittorrent", "config")
		if err := os.MkdirAll(cfg, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cfg, "qBittorrent.ini"), []byte("[Preferences]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := os.WriteFile(filepath.Join(profile, "config.xml"), []byte("<Config></Config>"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	app := filepath.Join(dir, companionApps[id].bundle)
	if err := os.MkdirAll(filepath.Join(app, "Contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte("plist"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{}
	svc := NewCompanionService(r)
	svc.agentDir = filepath.Join(dir, "LaunchAgents")
	return svc, CompanionServiceSpec{ID: id, AppPath: app, ProfileDir: profile, Home: home, AtLogin: true}, r
}

func TestCompanionInstallWritesLibraryOnlyJob(t *testing.T) {
	svc, spec, r := companionFixture(t, CompanionRadarr)
	st, err := svc.Install(spec)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != HostServiceLoaded || !st.Installed || !st.Loaded {
		t.Fatalf("status = %+v", st)
	}
	path, _ := svc.plistPath(st.Label)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, want := range []string{
		"kh.librarystudio.companion.radarr",
		"<string>/usr/bin/open</string>",
		"<string>-a</string>",
		"<string>" + spec.AppPath + "</string>",
		"<string>--args</string>",
		"<string>-data=" + spec.ProfileDir + "</string>",
		"<string>-nobrowser</string>",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plist is missing %q:\n%s", want, text)
		}
	}
	for _, banned := range []string{"KeepAlive", "/Contents/MacOS/", "token"} {
		if strings.Contains(text, banned) {
			t.Fatalf("plist must not contain %q:\n%s", banned, text)
		}
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("plist mode is %o, want 600", fi.Mode().Perm())
	}
	if !containsCall(r.calls, "bootstrap") {
		t.Fatalf("install did not load the job: %v", r.calls)
	}
}

func TestCompanionQBittorrentUsesProfileFlag(t *testing.T) {
	svc, spec, _ := companionFixture(t, CompanionQBittorrent)
	st, err := svc.Install(spec)
	if err != nil {
		t.Fatal(err)
	}
	path, _ := svc.plistPath(st.Label)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, "<string>--profile="+spec.ProfileDir+"</string>") {
		t.Fatalf("qBittorrent profile flag missing:\n%s", text)
	}
	if strings.Contains(text, "-nobrowser") {
		t.Fatalf("qBittorrent does not take -nobrowser:\n%s", text)
	}
}

func TestCompanionInstallIsIdempotent(t *testing.T) {
	svc, spec, r := companionFixture(t, CompanionSonarr)
	if _, err := svc.Install(spec); err != nil {
		t.Fatal(err)
	}
	r.calls = nil
	st, err := svc.Install(spec)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != HostServiceLoaded {
		t.Fatalf("status = %+v", st)
	}
	for _, call := range r.calls {
		if call != "print" {
			t.Fatalf("unchanged install changed the job: %v", r.calls)
		}
	}
}

func TestCompanionInstallRetiresChangedJob(t *testing.T) {
	svc, spec, _ := companionFixture(t, CompanionLidarr)
	st, err := svc.Install(spec)
	if err != nil {
		t.Fatal(err)
	}
	path, _ := svc.plistPath(st.Label)
	old := `<key>Label</key><string>` + st.Label + `</string><string>old settings</string>`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Install(spec); err != nil {
		t.Fatal(err)
	}
	retired, err := filepath.Glob(filepath.Join(spec.Home, "retired-launchjobs", "*.plist"))
	if err != nil || len(retired) != 1 {
		t.Fatalf("retired job missing: %v %v", retired, err)
	}
	b, err := os.ReadFile(retired[0])
	if err != nil || !strings.Contains(string(b), "old settings") {
		t.Fatalf("retired job does not hold the previous settings: %v\n%s", err, b)
	}
}

func TestCompanionRejectsUnknownID(t *testing.T) {
	svc, spec, r := companionFixture(t, CompanionProwlarr)
	spec.ID = "vscode"
	if st := svc.Status(spec); st.State != HostServiceBlocked {
		t.Fatalf("unknown ID status = %+v", st)
	}
	if _, err := svc.Install(spec); err == nil {
		t.Fatal("unknown ID was installed")
	}
	if len(r.calls) != 0 {
		t.Fatalf("unknown ID ran commands: %v", r.calls)
	}
	if CompanionServiceLabel("vscode") != "" {
		t.Fatal("unknown ID produced a label")
	}
}

func TestCompanionRequiresExplicitAtLogin(t *testing.T) {
	svc, spec, _ := companionFixture(t, CompanionRadarr)
	spec.AtLogin = false
	if _, err := svc.Install(spec); err == nil || !strings.Contains(err.Error(), "not selected") {
		t.Fatalf("install without the owner switch: %v", err)
	}
}

func TestCompanionRejectsProfileOutsideLibraryRoot(t *testing.T) {
	svc, spec, _ := companionFixture(t, CompanionRadarr)
	other := filepath.Join(t.TempDir(), "radarr")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	spec.ProfileDir = other
	if _, err := svc.Install(spec); err == nil || !strings.Contains(err.Error(), "Library-managed folder") {
		t.Fatalf("foreign profile accepted: %v", err)
	}
}

func TestCompanionRejectsUnsafeProfiles(t *testing.T) {
	svc, spec, _ := companionFixture(t, CompanionRadarr)
	if err := os.Chmod(spec.ProfileDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Install(spec); err == nil || !strings.Contains(err.Error(), "private") {
		t.Fatalf("public profile accepted: %v", err)
	}
	if err := os.Chmod(spec.ProfileDir, 0o700); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	real := filepath.Join(dir, "real")
	if err := os.MkdirAll(filepath.Join(home, "companions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatal(err)
	}
	linked := CompanionServiceProfileDir(home, CompanionRadarr)
	if err := os.Symlink(real, linked); err != nil {
		t.Fatal(err)
	}
	linkedSpec := CompanionServiceSpec{ID: CompanionRadarr, Home: home, AppPath: spec.AppPath, ProfileDir: linked, AtLogin: true}
	if _, err := svc.Install(linkedSpec); err == nil {
		t.Fatal("symlinked profile accepted")
	}
}

func TestCompanionRequiresCompletedFirstRun(t *testing.T) {
	svc, spec, _ := companionFixture(t, CompanionSonarr)
	cfg := filepath.Join(spec.ProfileDir, "config.xml")
	if err := os.Rename(cfg, cfg+".moved"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Install(spec); err == nil || !strings.Contains(err.Error(), "first run") {
		t.Fatalf("profile without first run accepted: %v", err)
	}
}

func TestCompanionRejectsWrongAppBundle(t *testing.T) {
	svc, spec, _ := companionFixture(t, CompanionRadarr)
	wrong := filepath.Join(filepath.Dir(spec.AppPath), "Sonarr.app")
	if err := os.MkdirAll(filepath.Join(wrong, "Contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wrong, "Contents", "Info.plist"), []byte("plist"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec.AppPath = wrong
	if _, err := svc.Install(spec); err == nil || !strings.Contains(err.Error(), "Radarr.app") {
		t.Fatalf("wrong bundle accepted: %v", err)
	}
}

func TestCompanionRejectsForeignOrSymlinkedPlist(t *testing.T) {
	svc, spec, r := companionFixture(t, CompanionProwlarr)
	label := CompanionServiceLabel(spec.ID)
	path, _ := svc.plistPath(label)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := []byte(`<key>Label</key><string>com.example.other</string>`)
	if err := os.WriteFile(path, foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Install(spec); err == nil {
		t.Fatal("foreign plist accepted")
	}
	if b, _ := os.ReadFile(path); string(b) != string(foreign) {
		t.Fatal("foreign plist changed")
	}
	if len(r.calls) != 0 {
		t.Fatalf("foreign plist ran commands: %v", r.calls)
	}
	if err := os.Rename(path, path+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path+".moved", path); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Install(spec); err == nil {
		t.Fatal("symlinked plist accepted")
	}
}

func TestCompanionRemoveRetiresJobRecoverably(t *testing.T) {
	svc, spec, r := companionFixture(t, CompanionRadarr)
	st, err := svc.Install(spec)
	if err != nil {
		t.Fatal(err)
	}
	path, _ := svc.plistPath(st.Label)
	r.calls = nil
	st, err = svc.Remove(spec)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != HostServiceNotInstalled || st.Installed || st.Loaded {
		t.Fatalf("remove status = %+v", st)
	}
	if !containsCall(r.calls, "bootout") {
		t.Fatalf("remove did not unload the job: %v", r.calls)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("remove left the active plist in place")
	}
	retired, err := filepath.Glob(filepath.Join(spec.Home, "retired-launchjobs", "*.plist"))
	if err != nil || len(retired) != 1 {
		t.Fatalf("retired job missing: %v %v", retired, err)
	}
	b, err := os.ReadFile(retired[0])
	if err != nil || !strings.Contains(string(b), st.Label) {
		t.Fatalf("retired job is not recoverable: %v\n%s", err, b)
	}
	if _, err := os.Stat(filepath.Join(spec.Home, "logs", st.Label+".log")); err != nil {
		t.Fatal("remove discarded the private log")
	}
	st, err = svc.Remove(spec)
	if err != nil || st.State != HostServiceNotInstalled {
		t.Fatalf("second remove = %+v, %v", st, err)
	}
}

func TestCompanionStatusStates(t *testing.T) {
	svc, spec, r := companionFixture(t, CompanionSonarr)
	if st := svc.Status(spec); st.State != HostServiceNotInstalled {
		t.Fatalf("not installed status = %+v", st)
	}
	if _, err := svc.Install(spec); err != nil {
		t.Fatal(err)
	}
	if st := svc.Status(spec); st.State != HostServiceLoaded {
		t.Fatalf("loaded status = %+v", st)
	}
	r.loaded = false
	if st := svc.Status(spec); st.State != HostServiceInstalled || !st.Installed {
		t.Fatalf("installed status = %+v", st)
	}
}

func containsCall(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}

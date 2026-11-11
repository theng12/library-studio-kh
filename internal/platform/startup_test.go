//go:build darwin

package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	loaded     bool
	calls      []string
	fail       string
	bundleID   string
	executable string
}

func (f *fakeRunner) Run(name string, args ...string) ([]byte, error) {
	if name == "plutil" {
		f.calls = append(f.calls, name)
		if len(args) < 2 {
			return nil, errors.New("bad plutil call")
		}
		switch args[1] {
		case "CFBundleIdentifier":
			if f.bundleID == "" {
				return nil, errors.New("missing bundle identifier")
			}
			return []byte(f.bundleID + "\n"), nil
		case "CFBundleExecutable":
			if f.executable == "" {
				return nil, errors.New("missing executable")
			}
			return []byte(f.executable + "\n"), nil
		}
		return nil, errors.New("unexpected plutil key")
	}
	if name != "launchctl" || len(args) == 0 {
		return nil, errors.New("unexpected command")
	}
	f.calls = append(f.calls, args[0])
	switch args[0] {
	case "print":
		if !f.loaded {
			return []byte("Could not find service"), errors.New("exit 113")
		}
	case "bootstrap":
		if f.fail == "bootstrap" {
			return []byte("bad plist"), errors.New("exit 1")
		}
		f.loaded = true
	case "bootout":
		if f.fail == "bootout" {
			return []byte("still running"), errors.New("exit 1")
		}
		f.loaded = false
	case "kickstart":
		if f.fail == "kickstart" {
			return []byte("broken"), errors.New("exit 1")
		}
		f.loaded = true
	}
	return nil, nil
}

func newTestService(t *testing.T, runner CommandRunner, verify func(string) error) (*HostService, HostServiceSpec) {
	t.Helper()
	dir := t.TempDir()
	exe := filepath.Join(dir, "library-studio-server")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	svc := NewHostService(runner, verify)
	svc.agentDir = filepath.Join(dir, "LaunchAgents")
	return svc, HostServiceSpec{Exe: exe, Home: filepath.Join(dir, "Library Studio KH"), Port: 7881}
}

func TestInstallWritesPrivateLibraryOnlyPlistAndIsIdempotent(t *testing.T) {
	r := &fakeRunner{}
	svc, spec := newTestService(t, r, nil)
	st, err := svc.Install(spec)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != HostServiceLoaded || !st.Installed || !st.Loaded || st.Running {
		t.Fatalf("install status = %+v", st)
	}
	path, err := svc.plistPath()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, want := range []string{HostServiceLabel, "<string>-service</string>", "<string>7881</string>", "LIBRARY_STUDIO_HOME"} {
		if !strings.Contains(text, want) {
			t.Fatalf("plist is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(strings.ToLower(text), "token") {
		t.Fatal("plist contains a credential")
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("plist mode is %o, want 600", fi.Mode().Perm())
	}
	before := len(r.calls)
	st, err = svc.Install(spec)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != HostServiceLoaded {
		t.Fatalf("unchanged install status = %+v", st)
	}
	for _, call := range r.calls[before:] {
		if call != "print" {
			t.Fatalf("unchanged install changed the job: %v", r.calls[before:])
		}
	}
}

func TestInstallKeepsRecoverableBackupOnChange(t *testing.T) {
	r := &fakeRunner{}
	svc, spec := newTestService(t, r, nil)
	if _, err := svc.Install(spec); err != nil {
		t.Fatal(err)
	}
	spec.Port = 7882
	if _, err := svc.Install(spec); err != nil {
		t.Fatal(err)
	}
	path, _ := svc.plistPath()
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), "<string>7882</string>") {
		t.Fatalf("new plist not written: %v\n%s", err, b)
	}
	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bak), "<string>7881</string>") {
		t.Fatalf("backup does not hold the previous settings:\n%s", bak)
	}
	fi, err := os.Stat(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode is %o, want 600", fi.Mode().Perm())
	}
}

func TestInstallRefusesForeignJob(t *testing.T) {
	r := &fakeRunner{}
	svc, spec := newTestService(t, r, nil)
	path, _ := svc.plistPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := []byte(`<plist version="1.0"><dict><key>Label</key><string>com.example.other</string><key>ProgramArguments</key><array><string>kh.librarystudio.host</string></array></dict></plist>`)
	if err := os.WriteFile(path, foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Install(spec); err == nil || !strings.Contains(err.Error(), "another login job") {
		t.Fatalf("foreign job accepted: %v", err)
	}
	if st := svc.Status(spec); st.State != HostServiceBlocked {
		t.Fatalf("foreign job status = %+v", st)
	}
	if len(r.calls) != 0 {
		t.Fatalf("foreign job ran launchctl: %v", r.calls)
	}
	if b, _ := os.ReadFile(path); string(b) != string(foreign) {
		t.Fatal("foreign job was changed")
	}
}

func TestInstallRefusesSymlinkedPlist(t *testing.T) {
	r := &fakeRunner{}
	svc, spec := newTestService(t, r, nil)
	path, _ := svc.plistPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(filepath.Dir(path), "elsewhere.plist")
	if err := os.WriteFile(target, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Install(spec); err == nil {
		t.Fatal("symlinked plist was accepted")
	}
	if st := svc.Status(spec); st.State != HostServiceBlocked {
		t.Fatalf("symlinked plist status = %+v", st)
	}
	if b, _ := os.ReadFile(target); string(b) != "keep me" {
		t.Fatal("symlink target was changed")
	}
}

func TestInstallAndStartRequireRestartWhileDesktopServerOwnsHome(t *testing.T) {
	r := &fakeRunner{}
	svc, spec := newTestService(t, r, nil)
	if err := os.MkdirAll(spec.Home, 0o700); err != nil {
		t.Fatal(err)
	}
	release, err := LockConfigHome(spec.Home)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	st, err := svc.Install(spec)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != HostServiceNeedsRestart || !st.NeedsRestart || !st.Installed {
		t.Fatalf("install while locked = %+v", st)
	}
	for _, call := range r.calls {
		if call == "bootstrap" || call == "kickstart" {
			t.Fatalf("install started a second server while the lock was held: %v", r.calls)
		}
	}
	r.calls = nil
	st, err = svc.Start(spec)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != HostServiceNeedsRestart || !st.NeedsRestart {
		t.Fatalf("start while locked = %+v", st)
	}
	for _, call := range r.calls {
		if call == "bootstrap" || call == "kickstart" {
			t.Fatalf("start launched a second server while the lock was held: %v", r.calls)
		}
	}
}

func TestStatusDistinguishesStates(t *testing.T) {
	r := &fakeRunner{}
	svc, spec := newTestService(t, r, nil)
	if st := svc.Status(spec); st.State != HostServiceNotInstalled || st.Installed {
		t.Fatalf("not installed status = %+v", st)
	}
	if _, err := svc.Install(spec); err != nil {
		t.Fatal(err)
	}
	if st := svc.Status(spec); st.State != HostServiceLoaded || !st.Loaded || st.Running || st.NeedsRestart {
		t.Fatalf("loaded status = %+v", st)
	}
	failing := NewHostService(r, func(string) error { return errors.New("not answering") })
	failing.agentDir = svc.agentDir
	if st := failing.Status(spec); st.State != HostServiceNeedsRestart || !st.NeedsRestart {
		t.Fatalf("unhealthy status = %+v", st)
	}
	running := NewHostService(r, func(string) error { return nil })
	running.agentDir = svc.agentDir
	if st := running.Status(spec); st.State != HostServiceRunning || !st.Running {
		t.Fatalf("running status = %+v", st)
	}
}

func TestStartReloadsTheJobDefinitionFromDisk(t *testing.T) {
	r := &fakeRunner{}
	svc, spec := newTestService(t, r, nil)
	if _, err := svc.Install(spec); err != nil {
		t.Fatal(err)
	}
	path, _ := svc.plistPath()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	r.calls = nil
	if _, err := svc.Start(spec); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(r.calls, ",")
	if !strings.Contains(joined, "bootout") || !strings.Contains(joined, "bootstrap") {
		t.Fatalf("start did not reload the saved job: %v", r.calls)
	}
	if !loadedConfigMatches(path) {
		t.Fatal("start did not record the reloaded definition")
	}
}

func TestStopKeepsPlistAndOnlyTouchesOwnJob(t *testing.T) {
	r := &fakeRunner{}
	svc, spec := newTestService(t, r, nil)
	if _, err := svc.Install(spec); err != nil {
		t.Fatal(err)
	}
	st, err := svc.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if st.State != HostServiceInstalled || !st.Installed || st.Loaded {
		t.Fatalf("stop status = %+v", st)
	}
	path, _ := svc.plistPath()
	if _, err := os.Stat(path); err != nil {
		t.Fatal("stop removed the login job")
	}
	if r.loaded {
		t.Fatal("job is still loaded after stop")
	}
}

func TestPlistEscapesPathsAndRejectsControlCharacters(t *testing.T) {
	plist := hostPlist(HostServiceSpec{Exe: "/tmp/a & b", Home: "/tmp/<home>", Port: 7881})
	if strings.Contains(plist, "/tmp/a & b") || !strings.Contains(plist, "&amp;") || !strings.Contains(plist, "&lt;home&gt;") {
		t.Fatalf("paths are not escaped:\n%s", plist)
	}
	r := &fakeRunner{}
	svc, spec := newTestService(t, r, nil)
	spec.Port = 80
	if _, err := svc.Install(spec); err == nil {
		t.Fatal("port 80 was accepted")
	}
	spec = HostServiceSpec{Exe: spec.Exe, Home: "relative", Port: 7881}
	if _, err := svc.Install(spec); err == nil {
		t.Fatal("relative home was accepted")
	}
	spec = HostServiceSpec{Exe: spec.Exe, Home: "/tmp/home\n", Port: 7881}
	if _, err := svc.Install(spec); err == nil {
		t.Fatal("control character was accepted")
	}
}

func TestInspectLabeledPlistRequiresTheTopLevelLabel(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	ours, exists, err := inspectLabeledPlist(write("correct.plist", `<plist version="1.0"><dict><key>Label</key><string>kh.librarystudio.host</string><key>ProgramArguments</key><array><string>kh.librarystudio.host</string></array></dict></plist>`), HostServiceLabel)
	if err != nil || !exists || !ours {
		t.Fatalf("correct label = ours %v exists %v err %v", ours, exists, err)
	}
	ours, exists, err = inspectLabeledPlist(write("foreign.plist", `<plist version="1.0"><dict><key>Label</key><string>com.example.other</string><key>ProgramArguments</key><array><string>kh.librarystudio.host</string></array></dict></plist>`), HostServiceLabel)
	if err != nil || !exists || ours {
		t.Fatalf("argument-only label accepted: ours %v exists %v err %v", ours, exists, err)
	}
	ours, exists, err = inspectLabeledPlist(write("nested.plist", `<plist version="1.0"><dict><key>EnvironmentVariables</key><dict><key>Label</key><string>kh.librarystudio.host</string></dict></dict></plist>`), HostServiceLabel)
	if err != nil || !exists || ours {
		t.Fatalf("nested label accepted: ours %v exists %v err %v", ours, exists, err)
	}
	if _, _, err := inspectLabeledPlist(write("duplicate.plist", `<plist version="1.0"><dict><key>Label</key><string>kh.librarystudio.host</string><key>Label</key><string>kh.librarystudio.host</string></dict></plist>`), HostServiceLabel); err == nil {
		t.Fatal("duplicate Label was accepted")
	}
	if _, _, err := inspectLabeledPlist(write("array.plist", `<plist version="1.0"><dict><key>Label</key><array><string>kh.librarystudio.host</string></array></dict></plist>`), HostServiceLabel); err == nil {
		t.Fatal("non-string Label was accepted")
	}
}

func TestHostBootoutFailureKeepsOldJobAndPlist(t *testing.T) {
	r := &fakeRunner{}
	svc, spec := newTestService(t, r, nil)
	if _, err := svc.Install(spec); err != nil {
		t.Fatal(err)
	}
	path, _ := svc.plistPath()
	old, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	oldSpec := spec
	spec.Port = 7882
	r.fail = "bootout"
	if _, err := svc.Install(spec); err == nil {
		t.Fatal("bootout failure reported success")
	}
	now, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(now) != string(old) || !strings.Contains(string(now), "<string>7881</string>") {
		t.Fatal("failed bootout changed the saved plist")
	}
	if !loadedConfigMatches(path) {
		t.Fatal("marker no longer matches the still-loaded old job")
	}
	if !r.loaded {
		t.Fatal("fake job was incorrectly unloaded")
	}
	if st := svc.Status(spec); st.State != HostServiceNeedsRestart || !st.NeedsRestart {
		t.Fatalf("unsaved desired settings status = %+v", st)
	}
	if st := svc.Status(oldSpec); st.State != HostServiceLoaded {
		t.Fatalf("saved old settings status = %+v", st)
	}
}

func TestHostStatusNeedsRestartWhenLoadedDefinitionDiffers(t *testing.T) {
	r := &fakeRunner{}
	svc, spec := newTestService(t, r, nil)
	if _, err := svc.Install(spec); err != nil {
		t.Fatal(err)
	}
	path, _ := svc.plistPath()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if st := svc.Status(spec); st.State != HostServiceNeedsRestart || !st.NeedsRestart {
		t.Fatalf("stale loaded definition status = %+v", st)
	}
}

func TestHostPlistRunsThePackagedHelperApp(t *testing.T) {
	app := filepath.Join(t.TempDir(), "Library Studio KH.app")
	server := filepath.Join(app, "Contents", "MacOS", "library-studio-server")
	helper := filepath.Join(app, "Contents", "Resources", "helper",
		"Library Studio KH Helper.app", "Contents", "MacOS", "Library Studio KH Helper")
	for _, path := range []string{server, helper} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	plist := hostPlist(HostServiceSpec{Exe: server, Home: "/tmp/home", Port: 7881})
	if !strings.Contains(plist, "<string>"+helper+"</string>") {
		t.Fatalf("plist does not run the helper: %s", plist)
	}
	if strings.Contains(plist, "<string>"+server+"</string>") {
		t.Fatalf("plist still runs the bare server: %s", plist)
	}
	if got := helperExecutable(server); got != helper {
		t.Fatalf("helperExecutable = %q, want %q", got, helper)
	}
	if got := helperExecutable("/usr/local/bin/library-studio-server"); got != "/usr/local/bin/library-studio-server" {
		t.Fatalf("helperExecutable changed a bare path: %q", got)
	}
}

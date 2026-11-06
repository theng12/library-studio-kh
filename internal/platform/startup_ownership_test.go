//go:build darwin

package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnotherConfigHomeCannotRestartOrReplaceHost(t *testing.T) {
	r := &fakeRunner{}
	svc, spec := newTestService(t, r, nil)
	if _, err := svc.Install(spec); err != nil {
		t.Fatal(err)
	}
	path, _ := svc.plistPath()
	before, _ := os.ReadFile(path)
	r.calls = nil
	other := spec
	other.Home = filepath.Join(t.TempDir(), "Other Library")
	if st := svc.Status(other); st.State != HostServiceBlocked || st.Installed {
		t.Fatal("another home's job was adopted")
	}
	if _, err := svc.Start(HostServiceSpec{Home: other.Home}); err == nil {
		t.Fatal("another home's job was started")
	}
	if _, err := svc.Install(other); err == nil {
		t.Fatal("another home's job was replaced")
	}
	after, _ := os.ReadFile(path)
	if len(r.calls) != 0 || !r.loaded || string(after) != string(before) {
		t.Fatal("cross-home operations touched the existing job")
	}
}

func TestAnotherConfigHomeCannotRemoveOrReplaceCompanion(t *testing.T) {
	svc, spec, r := companionFixture(t, CompanionSonarr)
	if _, err := svc.Install(spec); err != nil {
		t.Fatal(err)
	}
	path, _ := svc.plistPath(CompanionServiceLabel(spec.ID))
	before, _ := os.ReadFile(path)
	r.calls = nil
	other := spec
	other.Home = filepath.Join(t.TempDir(), "Other Library")
	other.ProfileDir = CompanionServiceProfileDir(other.Home, other.ID)
	if err := os.MkdirAll(other.ProfileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other.ProfileDir, "config.xml"), []byte("<Config/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if st := svc.Status(other); st.State != HostServiceBlocked {
		t.Fatal("another home's companion job was adopted")
	}
	if _, err := svc.Install(other); err == nil {
		t.Fatal("another home's companion job was replaced")
	}
	if _, err := svc.Remove(other); err == nil {
		t.Fatal("another home's companion job was removed")
	}
	for _, call := range r.calls {
		if call != "plutil" {
			t.Fatal("cross-home operation called launchctl")
		}
	}
	after, _ := os.ReadFile(path)
	if !r.loaded || string(after) != string(before) {
		t.Fatal("cross-home operation changed existing companion job")
	}
}

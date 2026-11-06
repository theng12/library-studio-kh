//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"testing"

	"librarystudio/internal/platform"
)

func TestStatusUsesSavedCustomPortAndStopRespectsConfigHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LIBRARY_STUDIO_LAUNCHAGENT_DIR", filepath.Join(dir, "Agents"))
	exe := filepath.Join(dir, "server")
	if err := os.WriteFile(exe, []byte("fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := &fakeHostRunner{}
	svc := platform.NewHostService(r, func(string) error { return nil })
	spec := platform.HostServiceSpec{Exe: exe, Home: filepath.Join(dir, "home"), Port: 18882}
	if _, code := runHostControl("install", svc, spec); code != 0 {
		t.Fatal("fixture install failed")
	}
	defaults := spec
	defaults.Port = 7881
	out, code := runHostControl("status", svc, defaults)
	if code != 0 || out.State != platform.HostServiceRunning || out.NeedsRestart {
		t.Fatal("CLI default port hid a healthy custom installation")
	}
	other := defaults
	other.Home = filepath.Join(dir, "other")
	out, code = runHostControl("stop", svc, other)
	if code != 1 || out.OK || !r.loaded {
		t.Fatal("another configuration could stop the existing host")
	}
}

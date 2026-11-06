//go:build darwin

package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"librarystudio/internal/platform"
)

type fakeHostRunner struct {
	loaded bool
	calls  []string
}

func (f *fakeHostRunner) Run(name string, args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("missing launchctl verb")
	}
	f.calls = append(f.calls, args[0])
	switch args[0] {
	case "print":
		if !f.loaded {
			return nil, errors.New("not loaded")
		}
	case "bootstrap":
		f.loaded = true
	case "bootout":
		f.loaded = false
	case "kickstart":
		f.loaded = true
	}
	return nil, nil
}

func TestHostControlInstallJSONHasNoCredentials(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LIBRARY_STUDIO_LAUNCHAGENT_DIR", filepath.Join(dir, "LaunchAgents"))
	exe := filepath.Join(dir, "library-studio-server")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	svc := platform.NewHostService(&fakeHostRunner{}, func(string) error { return nil })
	out, code := runHostControl("install", svc, platform.HostServiceSpec{Exe: exe, Home: filepath.Join(dir, "home"), Port: 7881})
	if code != 0 || !out.OK || out.State != platform.HostServiceRunning {
		t.Fatalf("code=%d output=%+v", code, out)
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"token", "127.0.0.1", "::1"} {
		if strings.Contains(strings.ToLower(string(b)), secret) {
			t.Fatalf("host-control JSON leaks %q: %s", secret, b)
		}
	}
}

func TestHostControlRejectsUnknownCommand(t *testing.T) {
	out, code := runHostControl("dance", platform.NewHostService(nil, nil), platform.HostServiceSpec{Home: t.TempDir()})
	if code != 1 || out.OK || out.State != platform.HostServiceBlocked {
		t.Fatalf("code=%d output=%+v", code, out)
	}
}

//go:build darwin

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestStartRecoversAJob exercises HostService.Start against the real launchctl
// on this machine. It overrides HostServiceLabel to a per-process disposable
// label so the owner's production job is never touched. The normal gate skips
// this test; only LIBRARY_STUDIO_REAL_LAUNCHD=1 enables it.
func TestStartRecoversAJob(t *testing.T) {
	if os.Getenv("LIBRARY_STUDIO_REAL_LAUNCHD") != "1" {
		t.Skip("skipping real-launchd test: set LIBRARY_STUDIO_REAL_LAUNCHD=1 to run against real launchctl")
	}

	origLabel := HostServiceLabel
	testLabel := fmt.Sprintf("kh.librarystudio.host.test.%d", os.Getpid())
	HostServiceLabel = testLabel

	uid := os.Getuid()
	domain := fmt.Sprintf("gui/%d", uid)

	t.Cleanup(func() {
		// Best-effort: boot out the test job if anything is still loaded.
		_ = exec.Command("launchctl", "bootout", domain+"/"+testLabel).Run()
		HostServiceLabel = origLabel
	})

	// Hard safety: if the override did not take effect, abort before any launchctl call.
	if HostServiceLabel == "kh.librarystudio.host" {
		t.Fatal("label override not in effect; aborting to protect production job")
	}

	tmpDir := t.TempDir()
	agentDir := filepath.Join(tmpDir, "LaunchAgents")
	home := filepath.Join(tmpDir, "LibraryStudioKH")
	readyFile := filepath.Join(tmpDir, "ready")
	for _, dir := range []string{agentDir, home} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Helper script: write readiness file and exit 0.
	// KeepAlive SuccessfulExit=false means launchd will NOT restart it on exit 0,
	// so the job stays loaded with the process dead — exactly the state we need.
	helperPath := filepath.Join(tmpDir, "helper.sh")
	helperScript := fmt.Sprintf("#!/bin/sh\necho $$ > %q\nexit 0\n", readyFile)
	if err := os.WriteFile(helperPath, []byte(helperScript), 0o755); err != nil {
		t.Fatal(err)
	}

	// Pre-create log files so launchd does not fail to open StandardOutPath/StandardErrorPath.
	for _, name := range []string{"host.log", "host.err.log"} {
		if err := os.WriteFile(filepath.Join(home, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	verify := func(_ string) error {
		if _, err := os.Stat(readyFile); err != nil {
			return err
		}
		return nil
	}

	svc := NewHostService(ExecRunner{}, verify)
	svc.agentDir = agentDir

	spec := HostServiceSpec{Exe: helperPath, Home: home, Port: 19876}
	plistPath := filepath.Join(agentDir, testLabel+".plist")

	// --- State (a): job loaded with a matching definition but NOT running ---
	t.Run("KickstartWhenLoadedAndMatching", func(t *testing.T) {
		_ = os.Remove(readyFile)

		// Write plist and bootstrap manually to simulate an installer's state.
		if err := os.WriteFile(plistPath, []byte(hostPlist(spec)), 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command("launchctl", "bootstrap", domain, plistPath).CombinedOutput()
		if err != nil {
			t.Fatalf("manual bootstrap failed: %s: %v", out, err)
		}
		if !waitForReady(t, readyFile, 5*time.Second) {
			t.Fatal("helper did not write readiness file after bootstrap")
		}
		// Record the loaded digest so loadedConfigMatches returns true inside Start.
		if err := markLoaded(plistPath); err != nil {
			t.Fatalf("markLoaded failed: %v", err)
		}
		// The process already exited 0 (KeepAlive won't restart it). Delete the
		// readiness file so verify reports "not running" — Start must kickstart.
		_ = os.Remove(readyFile)

		// Confirm the job is still loaded.
		if out, err := exec.Command("launchctl", "print", domain+"/"+testLabel).CombinedOutput(); err != nil {
			t.Fatalf("job should still be loaded: %s: %v", out, err)
		}

		status, err := svc.Start(spec)
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}

		if !waitForReady(t, readyFile, 5*time.Second) {
			t.Fatal("kickstart did not re-run the helper; readiness file missing")
		}
		if out, err := exec.Command("launchctl", "print", domain+"/"+testLabel).CombinedOutput(); err != nil {
			t.Fatalf("job should still be loaded after kickstart: %s: %v", out, err)
		}
		t.Logf("kickstart path proven: state=%s running=%v loaded=%v", status.State, status.Running, status.Loaded)

		// Clean up for the next sub-test.
		_ = exec.Command("launchctl", "bootout", domain+"/"+testLabel).Run()
		_ = os.Remove(readyFile)
		_ = os.Remove(plistPath)
		_ = os.Remove(plistPath + ".loaded")
	})

	// --- State (b): job not loaded at all (installer-unloaded state) ---
	t.Run("BootstrapWhenNotLoaded", func(t *testing.T) {
		_ = os.Remove(readyFile)

		// Write the plist but do NOT bootstrap — Start must bootstrap it.
		if err := os.WriteFile(plistPath, []byte(hostPlist(spec)), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := exec.Command("launchctl", "print", domain+"/"+testLabel).Run(); err == nil {
			t.Fatal("job should not be loaded before Start")
		}

		status, err := svc.Start(spec)
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}

		if !waitForReady(t, readyFile, 5*time.Second) {
			t.Fatal("bootstrap did not run the helper; readiness file missing")
		}
		if out, err := exec.Command("launchctl", "print", domain+"/"+testLabel).CombinedOutput(); err != nil {
			t.Fatalf("job should be loaded after bootstrap: %s: %v", out, err)
		}
		t.Logf("bootstrap path proven: state=%s running=%v loaded=%v", status.State, status.Running, status.Loaded)
	})
}

func waitForReady(t *testing.T, path string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
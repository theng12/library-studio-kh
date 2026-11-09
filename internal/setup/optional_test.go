package setup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"librarystudio/internal/companioninstall"
	"librarystudio/internal/config"
)

// The written configuration is the safety surface: originals kept, nothing
// deleted, no server opened, and no guessing at profile choices.
func TestOptionalConfigBodiesKeepSafeDefaults(t *testing.T) {
	c := config.Config{}
	c.Sonarr.URL, c.Sonarr.APIKey = "http://127.0.0.1:8989", "sonarr-key"
	c.Radarr.URL, c.Radarr.APIKey = "http://127.0.0.1:7878", "radarr-key"
	req := Request{DownloadPath: "/tmp/downloads", Apps: []companioninstall.AppID{companioninstall.Sonarr, companioninstall.Radarr}}

	unpacker := unpackerrConfigBody(c, req)
	for _, want := range []string{optionalFileMarker, "[sonarr", "[radarr", `url = "http://127.0.0.1:8989"`, `api_key = "sonarr-key"`, "delete_orig = false", `delete_delay = "-1s"`} {
		if !strings.Contains(unpacker, want) {
			t.Errorf("Unpackerr config is missing %q:\n%s", want, unpacker)
		}
	}
	for _, banned := range []string{"[webserver", "[folder.", "delete_orig = true"} {
		if strings.Contains(unpacker, banned) {
			t.Errorf("Unpackerr config must not contain %q:\n%s", banned, unpacker)
		}
	}
	if deletes, instances := unpackerrInspection([]byte(unpacker)); deletes || instances != 2 {
		t.Errorf("inspection = deletes %v, instances %d; want false, 2", deletes, instances)
	}
	if deletes, _ := unpackerrInspection([]byte("[sonarr.0]\ndelete_orig = true\n")); !deletes {
		t.Error("a foreign delete_orig = true must be reported as deleting")
	}

	recyclarr := recyclarrConfigBody(c, req)
	for _, want := range []string{optionalFileMarker, "sonarr:", "series:", "radarr:", "movies:", `base_url: "http://127.0.0.1:8989"`, `api_key: "sonarr-key"`} {
		if !strings.Contains(recyclarr, want) {
			t.Errorf("Recyclarr config is missing %q:\n%s", want, recyclarr)
		}
	}
	if strings.Contains(recyclarr, "quality_profile") {
		t.Error("Recyclarr config must not choose native profiles; apply is explicit")
	}
}

// A configuration this app did not write is never modified, and ours is only
// replaced when the caller allows an update.
func TestWriteOptionalFileNeverOverwritesForeignFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unpackerr.conf")
	if err := os.WriteFile(path, []byte("# the owner's own file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := writeOptionalFile(path, optionalFileMarker+"\nupdated\n", true); err != nil || got != configKept {
		t.Fatalf("foreign file: got %v, %v; want kept", got, err)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), "the owner's own file") {
		t.Fatal("foreign file was changed")
	}

	if got, err := writeOptionalFile(path, optionalFileMarker+"\nfresh\n", false); err != nil || got != configKept {
		t.Fatalf("marked file without update: got %v, %v; want kept", got, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got, err := writeOptionalFile(path, optionalFileMarker+"\nfresh\n", false); err != nil || got != configCreated {
		t.Fatalf("new file: got %v, %v; want created", got, err)
	}
	// After creating with "fresh", writing the same body again with
	// allowUpdate is an idempotent re-write (content matches exactly).
	if got, err := writeOptionalFile(path, optionalFileMarker+"\nfresh\n", true); err != nil || got != configUpdated {
		t.Fatalf("idempotent re-write: got %v, %v; want updated", got, err)
	}
	// A different body against the now-"fresh" file is owner-modified: kept.
	if got, err := writeOptionalFile(path, optionalFileMarker+"\nrefreshed\n", true); err != nil || got != configKept {
		t.Fatalf("owner-modified marked file: got %v, %v; want kept", got, err)
	}
	if err := verifyOptionalFile(path, optionalFileMarker+"\nfresh\n"); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := verifyOptionalFile(path, "something else"); err == nil {
		t.Fatal("verify accepted a file that does not match")
	}
}

// Owner-added profiles in a marked Recyclarr config must survive every
// subsequent Check: writeOptionalFile keeps any marked file whose content
// differs from what Setup would write, regardless of allowUpdate.
func TestWriteOptionalFilePreservesOwnerEditsInMarkedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recyclarr.yml")
	ownerBody := optionalFileMarker + "\n# owner added custom profiles\nsonarr:\n  series:\n    quality_profiles:\n      - name: Custom\n"
	if err := os.WriteFile(path, []byte(ownerBody), 0o600); err != nil {
		t.Fatal(err)
	}
	setupBody := optionalFileMarker + "\nsonarr:\n  series:\n    base_url: \"http://x\"\n"
	for i := 0; i < 3; i++ {
		got, err := writeOptionalFile(path, setupBody, true)
		if err != nil || got != configKept {
			t.Fatalf("iteration %d: got %v, %v; want kept", i, got, err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != ownerBody {
		t.Fatalf("owner edits were destroyed:\ngot:  %s\nwant: %s", data, ownerBody)
	}
}

// Foreign Unpackerr configs with unknown safety posture are kept and
// reported honestly: inspection reads delete_orig only, and the detail
// distinguishes file-level inspection from runtime verification.
func TestUnpackerrInspectionDistinguishesConfigFromRuntime(t *testing.T) {
	safe := "# foreign\ndelete_orig = false\n[sonarr.0]\nurl = \"x\"\n"
	deletes, instances := unpackerrInspection([]byte(safe))
	if deletes || instances != 1 {
		t.Errorf("safe foreign: deletes=%v instances=%d; want false,1", deletes, instances)
	}
	dangerous := "[radarr.0]\ndelete_orig = true\n[lidarr.0]\n"
	deletes, instances = unpackerrInspection([]byte(dangerous))
	if !deletes || instances != 2 {
		t.Errorf("dangerous foreign: deletes=%v instances=%d; want true,2", deletes, instances)
	}
	empty := "# just comments\n"
	deletes, instances = unpackerrInspection([]byte(empty))
	if deletes || instances != 0 {
		t.Errorf("empty foreign: deletes=%v instances=%d; want false,0", deletes, instances)
	}
}

// Private files (mode 0600) are written by writeOptionalFile and verified
// by verifyOptionalFile; group/world-readable files fail verification.
func TestVerifyOptionalFileRejectsNonPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.conf")
	body := optionalFileMarker + "\ntest\n"
	if _, err := writeOptionalFile(path, body, false); err != nil {
		t.Fatal(err)
	}
	if err := verifyOptionalFile(path, body); err != nil {
		t.Fatalf("private file should verify: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyOptionalFile(path, body); err == nil {
		t.Fatal("non-private file must fail verification")
	}
}

// OptionalAction refuses apply without explicit confirmation, rejects
// unknown actions, and returns errors without marking busy or corrupting
// state. Uses a fake runTool so no real binary is executed.
func TestOptionalActionConfirmGateAndUnknownAction(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "recyclarr.yml")
	if err := os.WriteFile(cfgPath, []byte(optionalFileMarker+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &Manager{
		runTool: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("fake output"), nil
		},
		recyclarrCfgPath: func() (string, error) { return cfgPath, nil },
	}
	m.job = &savedJob{
		Version: 1,
		View: Job{
			ID:    "test-job",
			State: "ready",
			Apps: []AppStep{
				{ID: companioninstall.Recyclarr, Name: "Recyclarr", Path: "/usr/local/bin/recyclarr"},
			},
		},
	}

	_, err := m.OptionalAction(nil, "test-job", companioninstall.Unpackerr, "preview", false)
	if err == nil || !strings.Contains(err.Error(), "only Recyclarr") {
		t.Fatalf("non-recyclarr: got %v; want rejection", err)
	}
	_, err = m.OptionalAction(nil, "test-job", companioninstall.Recyclarr, "delete", false)
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("unknown action: got %v; want rejection", err)
	}
	_, err = m.OptionalAction(nil, "test-job", companioninstall.Recyclarr, "apply", false)
	if err == nil || !strings.Contains(err.Error(), "confirm") {
		t.Fatalf("unconfirmed apply: got %v; want rejection", err)
	}
	if m.busy {
		t.Fatal("busy must not be set after rejected requests")
	}
}

// Concurrent OptionalAction calls serialize through busy: the second
// caller gets a clear error instead of racing the first. Fake runTool
// blocks until released so the race window is deterministic.
func TestOptionalActionConcurrentRequestsSerialize(t *testing.T) {
	release := make(chan struct{})
	cfgPath := filepath.Join(t.TempDir(), "recyclarr.yml")
	if err := os.WriteFile(cfgPath, []byte(optionalFileMarker+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeBin := filepath.Join(t.TempDir(), "recyclarr")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{
		home:  t.TempDir(),
		store: store,
		runTool: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			<-release
			return []byte("done"), nil
		},
		recyclarrCfgPath: func() (string, error) { return cfgPath, nil },
	}

	m.job = &savedJob{
		Version: 1,
		View: Job{
			ID:    "test-job",
			State: "ready",
			Apps: []AppStep{
				{ID: companioninstall.Recyclarr, Name: "Recyclarr", Path: fakeBin},
			},
		},
	}

	errCh := make(chan error, 2)
	go func() {
		_, err := m.OptionalAction(context.Background(), "test-job", companioninstall.Recyclarr, "preview", false)
		errCh <- err
	}()
	// Give the first goroutine time to acquire busy.
	for i := 0; i < 100 && !m.busy; i++ {
		time.Sleep(time.Millisecond)
	}
	_, secondErr := m.OptionalAction(context.Background(), "test-job", companioninstall.Recyclarr, "preview", false)
	close(release)
	firstErr := <-errCh

	if firstErr != nil {
		t.Fatalf("first call: %v", firstErr)
	}
	if secondErr == nil || !strings.Contains(secondErr.Error(), "already working") {
		t.Fatalf("second call: got %v; want busy rejection", secondErr)
	}
	if m.busy {
		t.Fatal("busy must be cleared after completion")
	}
}

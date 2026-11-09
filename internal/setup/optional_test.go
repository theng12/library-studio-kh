package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if got, err := writeOptionalFile(path, optionalFileMarker+"\nrefreshed\n", true); err != nil || got != configUpdated {
		t.Fatalf("marked update: got %v, %v; want updated", got, err)
	}
	if err := verifyOptionalFile(path, optionalFileMarker+"\nrefreshed\n"); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := verifyOptionalFile(path, "something else"); err == nil {
		t.Fatal("verify accepted a file that does not match")
	}
}

package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewManagedProfileIsPrivateLoopbackAndIdempotent(t *testing.T) {
	home := t.TempDir()
	p, err := PrepareArrProfile(home, "sonarr", 8989)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Created || p.URL != "http://127.0.0.1:8989" || len(p.APIKey) != 32 {
		t.Fatal("new profile must have a private credential and the chosen loopback port")
	}
	path := filepath.Join(p.DataDir, "config.xml")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(path)
		if fi.Mode().Perm() != 0o600 {
			t.Fatal("new native configuration is not private")
		}
	}
	data, _ := json.Marshal(p)
	if strings.Contains(string(data), p.APIKey) || strings.Contains(string(data), "APIKey") {
		t.Fatal("private native key escaped into public profile JSON")
	}
	p2, err := PrepareArrProfile(home, "sonarr", 9999)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if p2.Created || p2.URL != p.URL || p2.APIKey != p.APIKey || string(before) != string(after) {
		t.Fatal("retry must adopt actual existing values without overwriting the profile")
	}
}

func TestExistingNativeSettingsAndUnknownFieldsAreNeverRewritten(t *testing.T) {
	home := t.TempDir()
	p, err := PrepareArrProfile(home, "radarr", 7878)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(p.DataDir, "config.xml")
	data, _ := os.ReadFile(path)
	data = []byte(strings.Replace(string(data), "</Config>", "<OwnerSetting>keep</OwnerSetting></Config>", 1))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareArrProfile(home, "radarr", 7879); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(data) {
		t.Fatal("existing native values or unknown fields were overwritten")
	}
}

func TestInvalidPathsAndUnsafeProfilesFailClosed(t *testing.T) {
	home := t.TempDir()
	for _, app := range []string{"", "..", "../sonarr", "Sonarr", "a/b", "a\\b"} {
		if _, err := PrepareArrProfile(home, app, 8989); err == nil {
			t.Fatal("unsafe companion ID accepted")
		}
	}
	for _, port := range []int{0, 1023, 65536} {
		if _, err := PrepareArrProfile(home, "sonarr", port); err == nil {
			t.Fatal("invalid port accepted")
		}
	}
	p, err := PrepareArrProfile(home, "lidarr", 8686)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(p.DataDir, "config.xml")
	before, _ := os.ReadFile(path)
	unsafe := strings.Replace(string(before), "127.0.0.1", "0.0.0.0", 1)
	if err := os.WriteFile(path, []byte(unsafe), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadArrProfile("lidarr", p.DataDir); err == nil {
		t.Fatal("wildcard bind was presented as a managed connection")
	}
	after, _ := os.ReadFile(path)
	if string(after) != unsafe {
		t.Fatal("unsafe existing profile was changed without approval")
	}
	if err := os.WriteFile(path, []byte("<Config>incomplete"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareArrProfile(home, "lidarr", 8686); err == nil {
		t.Fatal("corrupt existing configuration was overwritten or accepted")
	}
}

func TestProfileSymlinksAndOversizedConfigurationAreRefused(t *testing.T) {
	home := t.TempDir()
	p, err := PrepareArrProfile(home, "prowlarr", 9696)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(p.DataDir, "config.xml")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", maxProfileConfigBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadArrProfile("prowlarr", p.DataDir); err == nil {
		t.Fatal("oversized native file accepted")
	}
	if runtime.GOOS == "windows" {
		return
	}
	outside := t.TempDir()
	linked := filepath.Join(home, "companions", "linked")
	if err := os.Symlink(outside, linked); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareArrProfile(home, "linked", 9898); err == nil {
		t.Fatal("managed profile followed a symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "config.xml")); !os.IsNotExist(err) {
		t.Fatal("symlink destination was changed")
	}
}

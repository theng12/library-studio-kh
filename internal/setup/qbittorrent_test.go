package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQBitProfileIsPrivateAndRetryPreservesNativeSettings(t *testing.T) {
	home := t.TempDir()
	p, err := PrepareQBitProfile(home, 8080)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Created || p.URL != "http://127.0.0.1:8080" || len(p.Password) < 32 {
		t.Fatal("new profile must have private authentication and loopback binding")
	}
	path := filepath.Join(p.DataDir, "qBittorrent", "config", "qBittorrent.ini")
	before, _ := os.ReadFile(path)
	if strings.Contains(string(before), p.Password) || !strings.Contains(string(before), `WebUI\Password_PBKDF2="@ByteArray(`) {
		t.Fatal("native profile must contain only the native hash format")
	}
	data, _ := json.Marshal(p)
	if strings.Contains(string(data), p.Password) {
		t.Fatal("login escaped into public JSON")
	}
	before = append(before, []byte("OwnerSetting=keep\n")...)
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	p2, err := PrepareQBitProfile(home, 8090)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if p2.Created || p2.URL != p.URL || p2.Password != p.Password || string(after) != string(before) {
		t.Fatal("retry replaced native settings or login")
	}
}

func TestQBitChangedOrIncompleteProfilesAreRetainedForRepair(t *testing.T) {
	for _, change := range []struct{ old, new string }{
		{"127.0.0.1", "*"}, {`WebUI\LocalHostAuth=true`, `WebUI\LocalHostAuth=false`},
		{`WebUI\AuthSubnetWhitelistEnabled=false`, `WebUI\AuthSubnetWhitelistEnabled=true`},
		{"@ByteArray(", "invalid("},
	} {
		home := t.TempDir()
		p, err := PrepareQBitProfile(home, 8080)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(p.DataDir, "qBittorrent", "config", "qBittorrent.ini")
		data, _ := os.ReadFile(path)
		changed := strings.Replace(string(data), change.old, change.new, 1)
		if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := PrepareQBitProfile(home, 8080); err == nil {
			t.Fatal("unsafe or incomplete native settings accepted")
		}
		after, _ := os.ReadFile(path)
		if string(after) != changed {
			t.Fatal("existing profile overwritten")
		}
	}
}

func TestQBitUnownedProfileAndSymlinkAreNotAdopted(t *testing.T) {
	home := t.TempDir()
	dir, err := managedProfileDir(home, "qbittorrent")
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "qBittorrent")); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareQBitProfile(home, 8080); err == nil {
		t.Fatal("followed a profile symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "config")); !os.IsNotExist(err) {
		t.Fatal("changed linked directory")
	}
	home = t.TempDir()
	dir = filepath.Join(home, "companions", "qbittorrent", "qBittorrent", "config")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "qBittorrent.ini")
	if err := os.WriteFile(path, []byte("owner profile"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareQBitProfile(home, 8080); err == nil {
		t.Fatal("invented credentials for unowned profile")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "owner profile" {
		t.Fatal("unowned profile overwritten")
	}
}

package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"librarystudio/internal/companioninstall"
	"librarystudio/internal/config"
)

func TestManagedFirstLaunchMustStillHavePrivateNativeBinding(t *testing.T) {
	m, _ := managerFixture(t)
	p, err := PrepareArrProfile(m.home, "sonarr", 18989)
	if err != nil {
		t.Fatal(err)
	}
	c := config.Default()
	c.Sonarr = config.App{URL: p.URL, APIKey: p.APIKey}
	app := AppStep{ID: companioninstall.Sonarr, Name: "Sonarr", Managed: true}
	if err := m.checkManagedProfile(app, &c); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(p.DataDir, "config.xml")
	data, _ := os.ReadFile(path)
	changed := strings.Replace(string(data), "127.0.0.1", "0.0.0.0", 1)
	if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.checkManagedProfile(app, &c); err == nil {
		t.Fatal("wildcard binding became a private-ready connection")
	}
	after, _ := os.ReadFile(path)
	if string(after) != changed {
		t.Fatal("native settings were silently rewritten")
	}
}

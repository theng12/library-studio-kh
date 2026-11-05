package librarystudio

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"
)

func TestChangelogAndVersionsAgree(t *testing.T) {
	r := Releases()
	if len(r) == 0 || len(r[0].Notes) == 0 || r[0].Date == "" {
		t.Fatalf("CHANGELOG.md needs a dated top version with notes, got %+v", r)
	}
	v := Version()
	for _, f := range []string{"package.json", "src-tauri/tauri.conf.json"} {
		var m struct{ Version string }
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &m); err != nil || m.Version != v {
			t.Errorf("%s version %q, CHANGELOG.md says %q (%v)", f, m.Version, v, err)
		}
	}
	cargo, _ := os.ReadFile("src-tauri/Cargo.toml")
	if m := regexp.MustCompile(`(?m)^version = "([^"]+)"`).FindSubmatch(cargo); m == nil || string(m[1]) != v {
		t.Errorf("src-tauri/Cargo.toml version does not match %q", v)
	}
}

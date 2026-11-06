package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"librarystudio/internal/config"
)

func TestClientHostCLIStoresNormalURLAndClears(t *testing.T) {
	home := t.TempDir()
	raw := "https://library.example.com/"
	want, err := config.ClientHostURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	out, code := runClientHost(raw, home)
	if code != 0 || !out.OK || out.ClientHost != want {
		t.Fatalf("save = code %d, %+v, want %q", code, out, want)
	}
	path := filepath.Join(home, "client.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(data)), "token") {
		t.Fatal("saved client host contains a credential")
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("saved client host mode is %o, want private", fi.Mode().Perm())
	}
	out, code = runClientHost("clear", home)
	if code != 0 || !out.OK || out.ClientHost != "" {
		t.Fatalf("clear = code %d, %+v", code, out)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("clear left the active client setting in place")
	}
	retired, err := filepath.Glob(filepath.Join(home, "retired-client-settings", "*-cleared.json"))
	if err != nil || len(retired) != 1 {
		t.Fatalf("clear did not keep a recoverable record: %v %v", retired, err)
	}
}

func TestClientHostCLIRejectsUnsafeURLs(t *testing.T) {
	home := t.TempDir()
	for _, raw := range []string{
		"http://user:pass@example.com",
		"https://example.com/?token=1",
		"https://example.com/#token",
		"https://example.com/api",
		"ftp://example.com",
		"not a url",
	} {
		out, code := runClientHost(raw, home)
		if code == 0 || out.OK || out.Error == "" {
			t.Fatalf("%q was accepted: %+v", raw, out)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "client.json")); !os.IsNotExist(err) {
		t.Fatal("an unsafe URL was saved")
	}
}

func TestClientHostJSONContract(t *testing.T) {
	out, _ := runClientHost("https://example.com", t.TempDir())
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"ok":true,"clientHost":"https://example.com"}` {
		t.Fatalf("JSON = %s", b)
	}
}

func TestRemoteShellPrintsNormalURLAndWaitsForShell(t *testing.T) {
	home := t.TempDir()
	host := "https://library.example.com"
	if _, err := config.SaveClientHost(home, host); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	handled, err := remoteShell(home, strings.NewReader(""), &out)
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if got, want := out.String(), remoteListeningLine(host)+"\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	if strings.Contains(out.String(), "#") || strings.Contains(strings.ToLower(out.String()), "token") {
		t.Fatalf("remote line leaks a local credential: %q", out.String())
	}
}

func TestRemoteShellLeavesLocalModeWithoutSavedHost(t *testing.T) {
	var out bytes.Buffer
	handled, err := remoteShell(t.TempDir(), strings.NewReader(""), &out)
	if err != nil || handled || out.Len() != 0 {
		t.Fatalf("handled=%v err=%v output=%q", handled, err, out.String())
	}
}

func TestRemoteShellRefusesDamagedSavedHost(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "client.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	handled, err := remoteShell(home, strings.NewReader(""), &out)
	if err == nil || handled {
		t.Fatalf("damaged setting accepted: handled=%v err=%v", handled, err)
	}
}

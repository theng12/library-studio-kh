package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	librarystudio "librarystudio"
	"librarystudio/internal/server"
)

func TestRuntimeDescriptorRoundTrip(t *testing.T) {
	home := t.TempDir()
	want := runtimeInfo{PID: 42, Address: "127.0.0.1:7881", Token: "secret-token", Version: "0.1.0"}
	if err := writeRuntime(home, want); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(runtimePath(home))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("descriptor mode is %o, want 600", fi.Mode().Perm())
	}
	got, err := readRuntime(home)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("read %+v, want %+v", got, want)
	}
}

func TestRuntimeDescriptorRefusesUnsafeFiles(t *testing.T) {
	home := t.TempDir()
	if _, err := readRuntime(home); !errors.Is(err, errNoHost) {
		t.Fatalf("missing descriptor must report no host, got %v", err)
	}
	info := runtimeInfo{PID: 42, Address: "127.0.0.1:7881", Token: "secret", Version: "0.1.0"}
	if err := writeRuntime(home, info); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(runtimePath(home), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readRuntime(home); err == nil || !strings.Contains(err.Error(), "readable by other users") {
		t.Fatalf("world-readable descriptor must be refused, got %v", err)
	}

	linkHome := t.TempDir()
	target := filepath.Join(linkHome, "elsewhere.json")
	if err := os.WriteFile(target, []byte(`{"pid":1,"address":"127.0.0.1:1","token":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, runtimePath(linkHome)); err != nil {
		t.Fatal(err)
	}
	if _, err := readRuntime(linkHome); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("symlinked descriptor must be refused, got %v", err)
	}

	badHome := t.TempDir()
	if err := writeRuntime(badHome, runtimeInfo{PID: 1, Address: "example.com:7881", Token: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := readRuntime(badHome); err == nil || !strings.Contains(err.Error(), "not this computer") {
		t.Fatalf("foreign host must be refused, got %v", err)
	}

	damaged := t.TempDir()
	if err := os.WriteFile(runtimePath(damaged), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readRuntime(damaged); err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Fatalf("damaged descriptor must be refused, got %v", err)
	}
}

func TestCheckLoopbackAddress(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:7881", "127.0.0.9:1024", "[::1]:65535"} {
		if err := checkLoopbackAddress(addr); err != nil {
			t.Errorf("%s should be allowed: %v", addr, err)
		}
	}
	for _, addr := range []string{"localhost:7881", "10.0.0.2:7881", "example.com:7881", "127.0.0.1:0", "127.0.0.1:http", "127.0.0.1"} {
		if err := checkLoopbackAddress(addr); err == nil {
			t.Errorf("%s should be refused", addr)
		}
	}
}

func proofServer(token, version string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/host-proof" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		challenge := r.URL.Query().Get("challenge")
		io.WriteString(w, `{"ok":true,"version":"`+version+`","proof":"`+server.HostProof(token, challenge)+`"}`)
	}))
}

func TestAttachProvesOwnershipWithoutSendingToken(t *testing.T) {
	token := strings.Repeat("a", 64)
	ts := proofServer(token, librarystudio.Version())
	defer ts.Close()
	home := t.TempDir()
	info := runtimeInfo{PID: os.Getpid(), Address: strings.TrimPrefix(ts.URL, "http://"), Token: token, Version: librarystudio.Version()}
	if err := writeRuntime(home, info); err != nil {
		t.Fatal(err)
	}
	got, err := attach(home)
	if err != nil {
		t.Fatal(err)
	}
	if got != info {
		t.Fatalf("attach returned %+v, want %+v", got, info)
	}
}

func TestAttachRefusesImpersonatorWithoutLeakingToken(t *testing.T) {
	token := strings.Repeat("a", 64)
	var leaked atomic.Bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Library-Token") != "" {
			leaked.Store(true)
		}
		io.WriteString(w, `{"ok":true,"version":"`+librarystudio.Version()+`","proof":"`+strings.Repeat("0", 64)+`"}`)
	}))
	defer ts.Close()
	home := t.TempDir()
	if err := writeRuntime(home, runtimeInfo{PID: 1, Address: strings.TrimPrefix(ts.URL, "http://"), Token: token, Version: librarystudio.Version()}); err != nil {
		t.Fatal(err)
	}
	if _, err := attach(home); err == nil || !strings.Contains(err.Error(), "prove") {
		t.Fatalf("impersonator accepted: %v", err)
	}
	if leaked.Load() {
		t.Fatal("attach sent the bootstrap token")
	}
}

func TestAttachRefusesVersionMismatch(t *testing.T) {
	token := strings.Repeat("a", 64)
	ts := proofServer(token, "0.0.0-old")
	defer ts.Close()
	home := t.TempDir()
	if err := writeRuntime(home, runtimeInfo{PID: 1, Address: strings.TrimPrefix(ts.URL, "http://"), Token: token, Version: librarystudio.Version()}); err != nil {
		t.Fatal(err)
	}
	if _, err := attach(home); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("version mismatch accepted: %v", err)
	}
}

func TestAttachRefusesRedirects(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"ok":true,"version":"0.1.0"}`)
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/api/health", http.StatusFound)
	}))
	defer redirector.Close()
	home := t.TempDir()
	if err := writeRuntime(home, runtimeInfo{PID: 1, Address: strings.TrimPrefix(redirector.URL, "http://"), Token: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := attach(home); err == nil || !strings.Contains(err.Error(), "redirects are not allowed") {
		t.Fatalf("redirect must not be followed, got %v", err)
	}
}

func TestRemoveRuntimeOnlyRemovesOwnDescriptor(t *testing.T) {
	home := t.TempDir()
	info := runtimeInfo{PID: 100, Address: "127.0.0.1:7881", Token: "x"}
	if err := writeRuntime(home, info); err != nil {
		t.Fatal(err)
	}
	removeRuntime(home, runtimeInfo{PID: 101, Address: info.Address, Token: "x"})
	if _, err := readRuntime(home); err != nil {
		t.Fatalf("another process's descriptor must stay: %v", err)
	}
	removeRuntime(home, info)
	if _, err := readRuntime(home); !errors.Is(err, errNoHost) {
		t.Fatalf("own descriptor must be removed, got %v", err)
	}
}

func TestNewServerRefusesUnusableSignInStore(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "access.json"), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newServer(home, "127.0.0.1:7881"); err == nil {
		t.Fatal("server started with unusable sign-in storage")
	}
}

func TestWaitShellReturnsWhenStdinCloses(t *testing.T) {
	done := make(chan struct{})
	r, w := io.Pipe()
	go func() { waitShell(r); close(done) }()
	w.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("waitShell did not return after stdin closed")
	}
}

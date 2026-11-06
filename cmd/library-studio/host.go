package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	librarystudio "librarystudio"
	"librarystudio/internal/server"
)

const defaultServicePort = 7881

var (
	errNoHost      = errors.New("no background host is configured")
	errHostUnsafe  = errors.New("the background host descriptor is not safe to use")
	errHostVersion = errors.New("the background host version does not match")
)

// runtimeInfo is the private handshake between the background host and a desktop shell.
// It lives only in this configuration home and is never printed or logged.
type runtimeInfo struct {
	PID     int    `json:"pid"`
	Address string `json:"address"`
	Token   string `json:"token"`
	Version string `json:"version"`
}

func runtimePath(home string) string { return filepath.Join(home, "host.json") }

func writeRuntime(home string, info runtimeInfo) error {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(info)
	if err != nil {
		return err
	}
	tmp := runtimePath(home) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, runtimePath(home))
}

// readRuntime accepts only a private regular file in this configuration home that
// names a loopback address. Anything else is refused, never guessed at.
func readRuntime(home string) (runtimeInfo, error) {
	path := runtimePath(home)
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return runtimeInfo{}, errNoHost
	}
	if err != nil {
		return runtimeInfo{}, err
	}
	if !fi.Mode().IsRegular() {
		return runtimeInfo{}, fmt.Errorf("%w: %s is not a regular file; the background host descriptor must be private", errHostUnsafe, path)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return runtimeInfo{}, fmt.Errorf("%w: %s is readable by other users; run chmod 600 on it or reinstall the background host", errHostUnsafe, path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return runtimeInfo{}, err
	}
	var info runtimeInfo
	if err := json.Unmarshal(b, &info); err != nil {
		return runtimeInfo{}, fmt.Errorf("%w: %s is damaged: %v", errHostUnsafe, path, err)
	}
	if info.PID <= 0 || info.Token == "" {
		return runtimeInfo{}, fmt.Errorf("%w: %s is incomplete; restart the background host", errHostUnsafe, path)
	}
	if err := checkLoopbackAddress(info.Address); err != nil {
		return runtimeInfo{}, fmt.Errorf("%w: %s is not safe to attach to: %v", errHostUnsafe, path, err)
	}
	return info, nil
}

func checkLoopbackAddress(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("address %q is not host:port", addr)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("address %q is not this computer", addr)
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return fmt.Errorf("address %q has no usable port", addr)
	}
	return nil
}

// attach proves that the live service named by this home's descriptor owns the
// private bootstrap token and runs this app's version. The token is never sent.
func attach(home string) (runtimeInfo, error) {
	info, err := readRuntime(home)
	if err != nil {
		return runtimeInfo{}, err
	}
	got, err := probe(info)
	if err != nil {
		return runtimeInfo{}, fmt.Errorf("Library Studio KH background host at http://%s is configured but not reachable: %v. Start it with the host manager, or remove %s to use a desktop-only session", info.Address, err, runtimePath(home))
	}
	current := librarystudio.Version()
	if got != current || info.Version != current {
		return runtimeInfo{}, fmt.Errorf("%w: the host is %s but this app is %s. Quit the background host, then reopen Library Studio KH", errHostVersion, got, current)
	}
	return info, nil
}

// waitForHost waits a bounded time for a just-started service to publish and
// prove its private descriptor. It never falls back to a second server.
func waitForHost(home string, timeout time.Duration) (runtimeInfo, error) {
	deadline := time.Now().Add(timeout)
	var last error
	for {
		info, err := attach(home)
		if err == nil {
			return info, nil
		}
		last = err
		if errors.Is(err, errHostVersion) || errors.Is(err, errHostUnsafe) {
			return runtimeInfo{}, err
		}
		if time.Now().After(deadline) {
			return runtimeInfo{}, last
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func probe(info runtimeInfo) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	challenge := hex.EncodeToString(b)
	client := &http.Client{
		Timeout: 2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("redirects are not allowed")
		},
	}
	req, err := http.NewRequest("GET", "http://"+info.Address+"/api/host-proof?challenge="+challenge, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("it answered %d", resp.StatusCode)
	}
	var p struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
		Proof   string `json:"proof"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&p); err != nil {
		return "", err
	}
	if !p.OK {
		return "", errors.New("it did not report healthy")
	}
	if subtle.ConstantTimeCompare([]byte(p.Proof), []byte(server.HostProof(info.Token, challenge))) != 1 {
		return "", errors.New("it could not prove it owns this configuration folder")
	}
	return p.Version, nil
}

// waitShell blocks until the desktop shell closes our stdin. It closes on any exit,
// crash or kill included, so a helper process never outlives its window.
func waitShell(r io.Reader) {
	io.Copy(io.Discard, r)
}

// removeRuntime removes the descriptor only while it still belongs to this process,
// so a stopping host cannot erase a newer host's handshake.
func removeRuntime(home string, info runtimeInfo) {
	cur, err := readRuntime(home)
	if err != nil || cur.PID != info.PID || cur.Address != info.Address {
		return
	}
	_ = os.Remove(runtimePath(home))
}

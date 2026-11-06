package config

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ClientHostURL accepts a normal host root URL, never a bootstrap shortcut.
func ClientHostURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Opaque != "" || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("use the host's normal HTTP or HTTPS webpage address, without a path, login, token or query")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && (ip.IsUnspecified() || ip.IsMulticast()) {
		return "", errors.New("choose a reachable host address")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", errors.New("host port must be from 1 to 65535")
		}
	}
	u.Path = ""
	u.RawPath = ""
	return u.String(), nil
}

func LoadClientHost(home string) (string, error) {
	path := filepath.Join(home, "client.json")
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() || (runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0) || fi.Size() > 16<<10 {
		return "", errors.New("saved client host must be a private regular file within the 16 KiB limit")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (16<<10)+1))
	if err != nil || len(data) > 16<<10 {
		return "", errors.New("saved client host exceeds the read limit")
	}
	var saved struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(data, &saved) != nil {
		return "", errors.New("saved client host is incomplete; retained for repair")
	}
	return ClientHostURL(saved.URL)
}

func SaveClientHost(home, raw string) (string, error) {
	host, err := ClientHostURL(raw)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(home, "client.json")
	if fi, err := os.Lstat(path); err == nil {
		if !fi.Mode().IsRegular() {
			return "", errors.New("client settings must not be a symlink")
		}
		if _, err := LoadClientHost(home); err != nil {
			return "", err
		}
		if err := archiveClientHost(home, path); err != nil {
			return "", err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	data, _ := json.Marshal(struct {
		URL string `json:"url"`
	}{host})
	file, err := os.CreateTemp(home, ".client-*")
	if err != nil {
		return "", err
	}
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return "", err
	}
	if err = file.Close(); err != nil {
		return "", err
	}
	if err = os.Rename(file.Name(), path); err != nil {
		return "", err
	}
	return host, nil
}

func archiveClientHost(home, path string) error {
	dir := filepath.Join(home, "retired-client-settings")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// Hard-link the old record before atomic replacement; no credentials are stored.
	return os.Link(path, filepath.Join(dir, strconv.FormatInt(time.Now().UnixNano(), 10)+".json"))
}

func ClearClientHost(home string) error {
	path := filepath.Join(home, "client.json")
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return errors.New("client settings must not be a symlink")
	}
	dir := filepath.Join(home, "retired-client-settings")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Rename(path, filepath.Join(dir, strconv.FormatInt(time.Now().UnixNano(), 10)+"-cleared.json"))
}

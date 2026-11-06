// Package setup coordinates guided installation over the official companion apps.
package setup

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const maxProfileConfigBytes = 64 << 10

// Profile holds the private first-run connection. APIKey never enters job JSON.
type Profile struct {
	App     string `json:"app"`
	DataDir string `json:"dataDir"`
	URL     string `json:"url"`
	Created bool   `json:"created"`
	APIKey  string `json:"-"`
}

type arrConfig struct {
	XMLName                xml.Name `xml:"Config"`
	BindAddress            string   `xml:"BindAddress"`
	Port                   int      `xml:"Port"`
	EnableSSL              bool     `xml:"EnableSsl"`
	LaunchBrowser          bool     `xml:"LaunchBrowser"`
	APIKey                 string   `xml:"ApiKey"`
	AuthenticationMethod   string   `xml:"AuthenticationMethod"`
	AuthenticationRequired string   `xml:"AuthenticationRequired"`
}

func managedProfileDir(home, app string) (string, error) {
	if !filepath.IsAbs(home) || app == "" || app == "." || app == ".." || strings.ContainsAny(app, "/\\\x00") {
		return "", errors.New("choose an absolute Library configuration folder and a valid companion ID")
	}
	for _, ch := range app {
		if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
			return "", errors.New("companion ID must contain only lowercase letters, numbers and hyphens")
		}
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", err
	}
	for _, dir := range []string{home, filepath.Join(home, "companions"), filepath.Join(home, "companions", app)} {
		if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", err
		}
		fi, err := os.Lstat(dir)
		if err != nil {
			return "", err
		}
		if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("the managed companion profile must be a directory, not a symlink")
		}
	}
	return filepath.Join(home, "companions", app), nil
}

// PrepareArrProfile creates only a new Library-managed profile. It never edits
// an existing stock profile or replaces a previous setup's native settings.
func PrepareArrProfile(home, app string, port int) (Profile, error) {
	if port < 1024 || port > 65535 {
		return Profile{}, errors.New("companion port must be between 1024 and 65535")
	}
	dir, err := managedProfileDir(home, app)
	if err != nil {
		return Profile{}, err
	}
	path := filepath.Join(dir, "config.xml")
	if _, err := os.Lstat(path); err == nil {
		return ReadArrProfile(app, dir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Profile{}, err
	}
	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		return Profile{}, err
	}
	cfg := arrConfig{BindAddress: "127.0.0.1", Port: port, APIKey: hex.EncodeToString(secret), AuthenticationMethod: "None", AuthenticationRequired: "Enabled"}
	data, err := xml.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return Profile{}, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return ReadArrProfile(app, dir)
	}
	if err != nil {
		return Profile{}, err
	}
	_, writeErr := io.WriteString(file, xml.Header+string(data)+"\n")
	closeErr := file.Close()
	if writeErr != nil {
		return Profile{}, fmt.Errorf("new profile could not be written; retained for repair: %w", writeErr)
	}
	if closeErr != nil {
		return Profile{}, closeErr
	}
	p, err := ReadArrProfile(app, dir)
	p.Created = err == nil
	return p, err
}

// ReadArrProfile reads back the actual native values after launch. An unknown
// binding or incomplete file never becomes a successful connection report.
func ReadArrProfile(app, dir string) (Profile, error) {
	path := filepath.Join(dir, "config.xml")
	fi, err := os.Lstat(path)
	if err != nil {
		return Profile{}, err
	}
	if !fi.Mode().IsRegular() || fi.Mode()&os.ModeSymlink != 0 {
		return Profile{}, errors.New("native configuration must be a regular file, not a symlink")
	}
	if fi.Size() > maxProfileConfigBytes {
		return Profile{}, errors.New("native configuration exceeds the 64 KiB read limit")
	}
	file, err := os.Open(path)
	if err != nil {
		return Profile{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxProfileConfigBytes+1))
	if err != nil || len(data) > maxProfileConfigBytes {
		return Profile{}, errors.New("native configuration could not be read within the 64 KiB limit")
	}
	var cfg arrConfig
	if err := xml.Unmarshal(data, &cfg); err != nil {
		return Profile{}, errors.New("native configuration is incomplete or unreadable")
	}
	if cfg.Port < 1024 || cfg.Port > 65535 || len(strings.TrimSpace(cfg.APIKey)) < 16 {
		return Profile{}, errors.New("native configuration has no valid port or API credential yet")
	}
	bind := strings.TrimSpace(cfg.BindAddress)
	if bind != "127.0.0.1" && bind != "localhost" && bind != "::1" {
		return Profile{}, errors.New("managed companion is not bound to loopback; review its native configuration")
	}
	if cfg.EnableSSL {
		return Profile{}, errors.New("managed companion changed to HTTPS; review its native connection settings")
	}
	return Profile{App: app, DataDir: dir, URL: "http://" + net.JoinHostPort(bind, strconv.Itoa(cfg.Port)), APIKey: cfg.APIKey}, nil
}

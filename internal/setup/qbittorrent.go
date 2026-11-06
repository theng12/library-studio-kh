package setup

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// QBitProfile keeps the generated login private while the app is awaiting approval.
type QBitProfile struct {
	Profile
	Username string `json:"username"`
	Password string `json:"-"`
}

type qbitCredential struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// PrepareQBitProfile writes only a new custom profile; stock profiles are untouched.
// qBittorrent's macOS custom profile uses qBittorrent/config/qBittorrent.ini.
func PrepareQBitProfile(home string, port int) (QBitProfile, error) {
	if port < 1024 || port > 65535 {
		return QBitProfile{}, errors.New("companion port must be between 1024 and 65535")
	}
	dir, err := managedProfileDir(home, "qbittorrent")
	if err != nil {
		return QBitProfile{}, err
	}
	cfgDir := filepath.Join(dir, "qBittorrent", "config")
	for _, path := range []string{filepath.Dir(cfgDir), cfgDir} {
		if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return QBitProfile{}, err
		}
		fi, err := os.Lstat(path)
		if err != nil {
			return QBitProfile{}, err
		}
		if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return QBitProfile{}, errors.New("qBittorrent profile directories must not be symlinks")
		}
	}
	path := filepath.Join(cfgDir, "qBittorrent.ini")
	_, configErr := os.Lstat(path)
	if configErr != nil && !errors.Is(configErr, os.ErrNotExist) {
		return QBitProfile{}, configErr
	}
	vault := filepath.Join(dir, "webui-credentials.json")
	var login qbitCredential
	data, err := readPrivateProfileFile(vault)
	if errors.Is(err, os.ErrNotExist) {
		if configErr == nil {
			return QBitProfile{}, errors.New("existing qBittorrent profile has no managed login; connect it explicitly without replacing its settings")
		}
		secret := make([]byte, 24)
		if _, err := rand.Read(secret); err != nil {
			return QBitProfile{}, err
		}
		login = qbitCredential{Username: "admin", Password: hex.EncodeToString(secret)}
		data, _ = json.Marshal(login)
		if err := writeNewProfileFile(vault, data); err != nil {
			return QBitProfile{}, err
		}
	} else if err != nil {
		return QBitProfile{}, err
	} else if json.Unmarshal(data, &login) != nil || login.Username == "" || len(login.Password) < 32 {
		return QBitProfile{}, errors.New("managed qBittorrent login is incomplete; retained for repair")
	}
	created := errors.Is(configErr, os.ErrNotExist)
	if created {
		salt := make([]byte, 16)
		if _, err := rand.Read(salt); err != nil {
			return QBitProfile{}, err
		}
		hash, err := pbkdf2.Key(sha512.New, login.Password, salt, 100000, 64)
		if err != nil {
			return QBitProfile{}, err
		}
		encoded := base64.StdEncoding.EncodeToString(salt) + ":" + base64.StdEncoding.EncodeToString(hash)
		// Native qBittorrent format: QByteArray(saltBase64:PBKDF2-HMAC-SHA512Base64).
		ini := fmt.Sprintf("[Preferences]\nWebUI\\Enabled=true\nWebUI\\Address=127.0.0.1\nWebUI\\Port=%d\nWebUI\\Username=%s\nWebUI\\Password_PBKDF2=\"@ByteArray(%s)\"\nWebUI\\LocalHostAuth=true\nWebUI\\AuthSubnetWhitelistEnabled=false\nWebUI\\CSRFProtection=true\nWebUI\\HostHeaderValidation=true\n", port, login.Username, encoded)
		if err := writeNewProfileFile(path, []byte(ini)); err != nil {
			return QBitProfile{}, err
		}
	}
	data, err = readPrivateProfileFile(path)
	if err != nil {
		return QBitProfile{}, err
	}
	values := map[string]string{}
	section := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			section = line
			continue
		}
		if section != "[Preferences]" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), "\"")
		}
	}
	actualPort, err := strconv.Atoi(values[`WebUI\Port`])
	if err != nil || actualPort < 1024 || actualPort > 65535 || values[`WebUI\Address`] != "127.0.0.1" || values[`WebUI\Username`] != login.Username || values[`WebUI\Enabled`] != "true" || values[`WebUI\LocalHostAuth`] != "true" || values[`WebUI\AuthSubnetWhitelistEnabled`] != "false" {
		return QBitProfile{}, errors.New("managed qBittorrent connection or authentication settings changed; review them without overwriting")
	}
	encoded := values[`WebUI\Password_PBKDF2`]
	if !strings.HasPrefix(encoded, "@ByteArray(") || !strings.HasSuffix(encoded, ")") {
		return QBitProfile{}, errors.New("managed qBittorrent password format is not recognised")
	}
	saltText, hashText, ok := strings.Cut(strings.TrimSuffix(strings.TrimPrefix(encoded, "@ByteArray("), ")"), ":")
	salt, saltErr := base64.StdEncoding.DecodeString(saltText)
	want, hashErr := base64.StdEncoding.DecodeString(hashText)
	if !ok || saltErr != nil || hashErr != nil || len(salt) != 16 || len(want) != 64 {
		return QBitProfile{}, errors.New("managed qBittorrent password hash is incomplete")
	}
	got, err := pbkdf2.Key(sha512.New, login.Password, salt, 100000, 64)
	if err != nil || subtle.ConstantTimeCompare(got, want) != 1 {
		return QBitProfile{}, errors.New("qBittorrent login changed; connect the saved native account without replacing it")
	}
	return QBitProfile{Profile: Profile{App: "qbittorrent", DataDir: dir, URL: fmt.Sprintf("http://127.0.0.1:%d", actualPort), Created: created}, Username: login.Username, Password: login.Password}, nil
}

func readPrivateProfileFile(path string) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0o077 != 0 || fi.Size() > maxProfileConfigBytes {
		return nil, errors.New("managed profile files must be private regular files within the 64 KiB read limit")
	}
	data, err := os.ReadFile(path)
	if len(data) > maxProfileConfigBytes {
		return nil, errors.New("managed profile exceeds the 64 KiB read limit")
	}
	return data, err
}

func writeNewProfileFile(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".profile-*")
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("profile staging failed; retained for repair: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	// Atomic no-replace publication; the private staging hard link stays recoverable.
	return os.Link(file.Name(), path)
}

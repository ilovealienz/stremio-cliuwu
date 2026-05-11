package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    = 4
	argonMemory  = 128 * 1024
	argonThreads = 2
	argonKeyLen  = 32
)

var blobSep = []byte{0xff, 0x00, 0xff, 0x00}

func configDir() string {
	if runtime.GOOS == "windows" {
		if ap := os.Getenv("APPDATA"); ap != "" {
			return filepath.Join(ap, appName)
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "AppData", "Roaming", appName)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", appName)
}

func authFile() string { return filepath.Join(configDir(), "auth.enc") }
func cfgFile() string  { return filepath.Join(configDir(), "config.json") }
func favsFile() string { return filepath.Join(configDir(), "favourites.json") }
func histFile() string { return filepath.Join(configDir(), "history.json") }

func ensureDir() { os.MkdirAll(configDir(), 0700) }

func machineSecret() []byte {
	var parts []string
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if b, err := os.ReadFile(p); err == nil {
			parts = append(parts, strings.TrimSpace(string(b)))
			break
		}
	}
	if runtime.GOOS == "windows" {
		if v := os.Getenv("COMPUTERNAME"); v != "" {
			parts = append(parts, v)
		}
	}
	if u := os.Getenv("USER"); u != "" {
		parts = append(parts, u)
	} else if u := os.Getenv("USERNAME"); u != "" {
		parts = append(parts, u)
	}
	if h, err := os.Hostname(); err == nil {
		parts = append(parts, h)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return sum[:]
}

func machineKey() []byte {
	sum := sha256.Sum256(machineSecret())
	return sum[:]
}

func deriveVaultKey(pw string) []byte {
	return argon2.IDKey([]byte(pw), machineSecret(),
		argonTime, argonMemory, argonThreads, argonKeyLen)
}

func aesEnc(key, plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return append(nonce, gcm.Seal(nil, nonce, plain, nil)...), nil
}

func aesDec(key, blob []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(blob) < ns {
		return nil, fmt.Errorf("blob too short")
	}
	return gcm.Open(nil, blob[:ns], blob[ns:], nil)
}

func SaveAuth(data AuthData, vaultKey []byte) error {
	ensureDir()
	j, _ := json.Marshal(data)
	b1, err := aesEnc(machineKey(), vaultKey)
	if err != nil {
		return err
	}
	b2, err := aesEnc(vaultKey, j)
	if err != nil {
		return err
	}
	out := append(b1, blobSep...)
	out = append(out, b2...)
	return os.WriteFile(authFile(), out, 0600)
}

func LoadAuth() (*AuthData, error) {
	raw, err := os.ReadFile(authFile())
	if err != nil {
		return nil, err
	}
	sep := -1
	for i := 0; i <= len(raw)-len(blobSep); i++ {
		if string(raw[i:i+len(blobSep)]) == string(blobSep) {
			sep = i
			break
		}
	}
	if sep < 0 {
		return nil, fmt.Errorf("malformed auth file")
	}
	vk, err := aesDec(machineKey(), raw[:sep])
	if err != nil {
		return nil, err
	}
	plain, err := aesDec(vk, raw[sep+len(blobSep):])
	if err != nil {
		return nil, err
	}
	var d AuthData
	json.Unmarshal(plain, &d)
	return &d, nil
}

func ClearAuth() { os.Remove(authFile()) }

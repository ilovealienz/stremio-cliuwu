package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

// configDir returns the per-user config directory, honouring XDG on unix.
func configDir() string {
	if runtime.GOOS == "windows" {
		if ad := os.Getenv("APPDATA"); ad != "" {
			return filepath.Join(ad, appName)
		}
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, appName)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "." + appName
	}
	return filepath.Join(home, ".config", appName)
}

func cfgFile() string    { return filepath.Join(configDir(), "config.json") }
func addonsFile() string { return filepath.Join(configDir(), "addons.json") }
func favsFile() string   { return filepath.Join(configDir(), "favourites.json") }
func histFile() string   { return filepath.Join(configDir(), "history.json") }

func ensureDir() { os.MkdirAll(configDir(), 0700) }

// writeJSON writes v to path as indented JSON with 0600 perms.
//
// 0600 matters more than it looks: addon manifest URLs routinely carry a
// debrid API key in the path (torrentio.strem.fun/torbox=<key>/manifest.json),
// so addons.json is effectively a credentials file.
func writeJSON(path string, v any) error {
	ensureDir()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0600)
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

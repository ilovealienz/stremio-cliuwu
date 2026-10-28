package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

const AppName = "stremio-cliuwu"

// xdgDir is this app's folder under the XDG base directory env names, or
// under ~/fallback when it's unset. The spec says a relative path is invalid
// and to ignore it. Windows keeps everything in %APPDATA%.
func xdgDir(env, fallback string) string {
	if runtime.GOOS == "windows" {
		if ad := os.Getenv("APPDATA"); ad != "" {
			return filepath.Join(ad, AppName)
		}
	}
	if dir := os.Getenv(env); filepath.IsAbs(dir) {
		return filepath.Join(dir, AppName)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "." + AppName
	}
	return filepath.Join(home, fallback, AppName)
}

// ConfigDir holds settings.
func ConfigDir() string { return xdgDir("XDG_CONFIG_HOME", ".config") }

// DataDir holds your addons and favourites. Addon urls carry debrid keys,
// and ~/.config often ends up in a public dotfiles repo.
func DataDir() string { return xdgDir("XDG_DATA_HOME", filepath.Join(".local", "share")) }

// StateDir holds what the app keeps track of: watch history and downloads.
func StateDir() string { return xdgDir("XDG_STATE_HOME", filepath.Join(".local", "state")) }

func CfgFile() string { return filepath.Join(ConfigDir(), "config.json") }

func AddonsFile() string { return filepath.Join(DataDir(), "addons.json") }

func favsFile() string { return filepath.Join(DataDir(), "favourites.json") }

func histFile() string { return filepath.Join(StateDir(), "history.json") }

func DownloadsFile() string { return filepath.Join(StateDir(), "downloads.json") }

// writeJSON writes v to path as indented JSON with 0600 perms.
//
// 0600 matters more than it looks: addon manifest URLs routinely carry a
// debrid API key in the path (torrentio.strem.fun/torbox=<key>/manifest.json),
// so addons.json is effectively a credentials file.
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return WriteAtomic(path, b, 0600)
}

// WriteAtomic writes to a temporary file in the same directory and renames
// over the target.
//
// os.WriteFile truncates in place, so an interrupted write leaves a truncated
// file rather than the previous one. For history that loses what you have
// watched; for addons.json it loses the addon urls, which carry the debrid
// keys and are not recoverable from anywhere else.
func WriteAtomic(path string, b []byte, perm os.FileMode) error {
	os.MkdirAll(filepath.Dir(path), 0700)

	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()

	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	// Synced before the rename: a rename is atomic, but on a crash the
	// directory entry can land before the contents do.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}

	if err := os.Chmod(name, perm); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// DefaultDownloadDir is a sensible starting point for the settings prompt.
func DefaultDownloadDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "downloads"
	}
	return filepath.Join(home, "Downloads", AppName)
}

var winEnvVar = regexp.MustCompile(`%([A-Za-z_][A-Za-z0-9_]*)%`)

// ExpandPath resolves what people actually type into a path field: a leading
// ~, $HOME style variables, and on Windows %USERPROFILE% style ones, which
// os.ExpandEnv doesn't understand.
func ExpandPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}

	if runtime.GOOS == "windows" {
		p = winEnvVar.ReplaceAllStringFunc(p, func(m string) string {
			if v := os.Getenv(strings.Trim(m, "%")); v != "" {
				return v
			}
			return m
		})
	}
	p = os.ExpandEnv(p)

	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			rest := strings.TrimLeft(strings.TrimPrefix(p, "~"), `/\`)
			return filepath.Join(home, rest)
		}
	}
	return filepath.Clean(p)
}

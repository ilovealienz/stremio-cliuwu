package store

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Cfg is the running config, shared with the UI.
var Cfg = &Config{}

func LoadConfig() Config {
	var c Config
	readJSON(CfgFile(), &c) // missing or corrupt file → zero value, defaults below

	if c.SetDefaults() {
		SaveConfig(c) // write the upgrade back so it only happens once
	}
	return c
}

func SaveConfig(c Config) { writeJSON(CfgFile(), c) }

// DetectMpv looks for an mpv binary in the usual places.
func DetectMpv() string {
	if p, err := exec.LookPath("mpv"); err == nil {
		return p
	}

	var candidates []string
	switch runtime.GOOS {
	case "windows":
		for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA")} {
			if base == "" {
				continue
			}
			candidates = append(candidates,
				filepath.Join(base, "mpv", "mpv.exe"),
				filepath.Join(base, "mpv.net", "mpvnet.exe"),
			)
		}
	case "darwin":
		candidates = []string{
			"/opt/homebrew/bin/mpv",
			"/usr/local/bin/mpv",
			"/Applications/mpv.app/Contents/MacOS/mpv",
		}
	default:
		candidates = []string{
			"/usr/bin/mpv",
			"/usr/local/bin/mpv",
			"/var/lib/flatpak/exports/bin/io.mpv.Mpv",
			filepath.Join(os.Getenv("HOME"), ".nix-profile/bin/mpv"),
			"/run/current-system/sw/bin/mpv",
		}
	}

	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	return ""
}

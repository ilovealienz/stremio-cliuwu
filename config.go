package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func LoadConfig() AppConfig {
	b, err := os.ReadFile(cfgFile())
	if err != nil {
		return AppConfig{}
	}
	var c AppConfig
	json.Unmarshal(b, &c)
	c.SetDefaults()
	return c
}

func SaveConfig(c AppConfig) {
	ensureDir()
	b, _ := json.MarshalIndent(c, "", "  ")
	os.WriteFile(cfgFile(), b, 0644)
}

func detectMpv() string {
	if p, err := exec.LookPath("mpv"); err == nil {
		return p
	}
	candidates := map[string][]string{
		"windows": {
			`C:\Program Files\mpv\mpv.exe`,
			`C:\Program Files (x86)\mpv\mpv.exe`,
			filepath.Join(os.Getenv("APPDATA"), `mpv\mpv.exe`),
		},
		"darwin": {"/usr/local/bin/mpv", "/opt/homebrew/bin/mpv"},
		"linux":  {"/usr/bin/mpv", "/usr/local/bin/mpv", "/snap/bin/mpv"},
	}
	for _, p := range candidates[runtime.GOOS] {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func linuxInstallHint() string {
	b, _ := os.ReadFile("/etc/os-release")
	s := strings.ToLower(string(b))
	switch {
	case strings.Contains(s, "fedora") || strings.Contains(s, "rhel"):
		return "sudo dnf install mpv"
	case strings.Contains(s, "arch") || strings.Contains(s, "manjaro"):
		return "sudo pacman -S mpv"
	case strings.Contains(s, "opensuse"):
		return "sudo zypper install mpv"
	case strings.Contains(s, "void"):
		return "sudo xbps-install mpv"
	case strings.Contains(s, "alpine"):
		return "sudo apk add mpv"
	default:
		return "sudo apt install mpv"
	}
}

func setupMpv() (string, error) {
	if p := detectMpv(); p != "" {
		ok(fmt.Sprintf("found mpv  %s", grey(p)))
		return p, nil
	}
	fail("mpv not found")
	blank()
	switch runtime.GOOS {
	case "linux":
		hint(linuxInstallHint())
	case "windows":
		hint("winget install mpv")
	case "darwin":
		hint("brew install mpv")
	}
	blank()
	p := prompt("path to mpv binary")
	if p == "" {
		return "", fmt.Errorf("no path given")
	}
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("not found: %s", p)
	}
	return p, nil
}

// configScreen lets the user edit settings interactively.
func configScreen(cfg *AppConfig) {
	for {
		header("config")
		fmt.Printf("  %s\n\n", grey(cfgFile()))

		type field struct{ key, label, val string }
		fields := []field{
			{"1", "mpv path", cfg.MpvPath},
			{"2", "preferred quality", orDef(cfg.PreferredQuality, "none")},
			{"3", "subtitle language", orDef(cfg.SubtitleLang, "none")},
			{"4", "history max", fmt.Sprintf("%d", cfg.HistoryMax)},
			{"5", "OMDB API key", orDef(cfg.OmdbKey, "trilogy")},
		}
		for _, f := range fields {
			fmt.Printf("  %s  %s  %s\n", accent("["+f.key+"]"), bold(f.label), grey("("+f.val+")"))
		}

		blank()
		fmt.Printf("  %s  %s: ", grey("choose"), grey("b=back"))
		StatusStart()
		raw := readLine()
		StatusStop()

		switch raw {
		case "0", "q", "b", "":
			return
		case "1":
			header("config")
			hint("current: " + cfg.MpvPath)
			if p := detectMpv(); p != "" {
				hint("detected: " + p)
			}
			blank()
			fmt.Printf("  %s: ", grey("new path (empty to keep)"))
			if v := readLine(); v != "" {
				cfg.MpvPath = v
			}
		case "2":
			header("config")
			hint("options: 4K  2K  1080P  720P  480P  (empty = no preference)")
			blank()
			fmt.Printf("  %s: ", grey("quality"))
			cfg.PreferredQuality = strings.ToUpper(strings.TrimSpace(readLine()))
		case "3":
			header("config")
			hint("ISO 639-1 code e.g. en  fr  de  ja  (empty = mpv default)")
			blank()
			fmt.Printf("  %s: ", grey("language code"))
			cfg.SubtitleLang = strings.TrimSpace(readLine())
		case "4":
			header("config")
			fmt.Printf("  %s: ", grey("max entries (1–10000)"))
			n := 0
			fmt.Sscanf(readLine(), "%d", &n)
			if n > 0 {
				cfg.HistoryMax = n
			}
		case "5":
			header("config")
			hint("get a free key at omdbapi.com")
			blank()
			fmt.Printf("  %s: ", grey("key (empty to keep)"))
			if v := strings.TrimSpace(readLine()); v != "" {
				cfg.OmdbKey = v
			}
		}
		SaveConfig(*cfg)
		ok("saved")
	}
}

func orDef(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const appName = "stremio-cliuwu"
const version = "0.1.0"

func firstRun() (AuthData, AppConfig, error) {
	header("first time setup")

	fmt.Printf("  %s  %s\n", bold("step 1"), white("vault password"))
	blank()
	hint("encrypts your login with argon2id + AES-256-GCM")
	hint("machine-bound — useless on any other machine")
	hint("only asked once, never again")
	blank()

	var vaultPw string
	for {
		pw1 := readPassword("vault password")
		pw2 := readPassword("confirm")
		if pw1 == pw2 {
			vaultPw = pw1
			break
		}
		fmt.Printf("  %s\n\n", bad("passwords do not match"))
	}

	blank()
	spin("deriving key via argon2id...")
	vk := deriveVaultKey(vaultPw)
	ok("vault key derived")

	blank()
	fmt.Printf("  %s  %s\n", bold("step 2"), white("locate mpv"))
	mpvPath, err := setupMpv()
	if err != nil {
		return AuthData{}, AppConfig{}, err
	}

	blank()
	fmt.Printf("  %s  %s\n", bold("step 3"), white("stremio login"))
	auth, err := StremioLogin()
	if err != nil {
		return AuthData{}, AppConfig{}, err
	}

	cfg := AppConfig{MpvPath: mpvPath}
	cfg.SetDefaults()

	blank()
	spin("saving...")
	if err := SaveAuth(auth, vk); err != nil {
		return AuthData{}, AppConfig{}, err
	}
	SaveConfig(cfg)
	ok(fmt.Sprintf("saved to %s", grey(configDir())))
	blank()
	hint("vault password will never be asked again on this machine")

	return auth, cfg, nil
}

func main() {
	// --version flag
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-v" {
			fmt.Printf("%s %s\n", appName, version)
			return
		}
	}

	initSignals()
	StatusStart()

	var authKey string
	var cfg AppConfig

	auth, err := LoadAuth()
	cfg = LoadConfig()

	if err != nil {
		if _, statErr := os.Stat(authFile()); statErr == nil {
			header("")
			fail("couldn't decrypt saved session")
			hint("machine identity may have changed or file is corrupt")
			hint("clearing saved session")
			blank()
			ClearAuth()
			time.Sleep(1200 * time.Millisecond)
		}
		a, c, err := firstRun()
		if err != nil {
			fail(err.Error())
			os.Exit(1)
		}
		authKey = a.AuthKey
		cfg = c
	} else {
		authKey = auth.AuthKey
		if cfg.MpvPath == "" {
			cfg.MpvPath = detectMpv()
		}
		if cfg.MpvPath == "" {
			header("mpv not configured")
			mpvPath, err := setupMpv()
			if err != nil {
				fail(err.Error())
				os.Exit(1)
			}
			cfg.MpvPath = mpvPath
			SaveConfig(cfg)
		}

		header("")
		if auth.Email != "" {
			ok(fmt.Sprintf("logged in as  %s", bold(auth.Email)))
		} else {
			ok("loaded saved session")
		}
	}

	blank()
	spin("loading addons...")
	addons, err := GetAddons(authKey)
	if err != nil || len(addons) == 0 {
		fail("no addons found — session may have expired")
		hint("delete " + authFile() + " to re-login")
		os.Exit(1)
	}

	names := make([]string, len(addons))
	for i, a := range addons {
		names[i] = a.Manifest.Name
	}
	ok(fmt.Sprintf("%d addon(s) loaded", len(addons)))
	hint(strings.Join(names, "  ·  "))
	blank()
	time.Sleep(600 * time.Millisecond)

	mainMenu(addons, cfg)

	StatusStop()
	clearScreen()
	fmt.Printf("\n  %s  %s\n\n", accent(appName), grey("bye ♡"))
}

package main

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
)

const appName = "stremio-cliuwu"
var version = "0.2.0"

func main() {
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--version", "-v":
			fmt.Printf("%s %s\n", appName, version)
			return
		case "--config":
			fmt.Println(configDir())
			return
		case "--help", "-h":
			usage()
			return
		}
	}

	cfg := LoadConfig()
	if cfg.MpvPath == "" {
		cfg.MpvPath = detectMpv()
		SaveConfig(cfg)
	}

	// First run: no addons.json yet, so seed the two metadata addons. Stream
	// addons are the user's to add — there's no account to pull them from.
	refs := LoadAddonRefs()
	firstRun := false
	if len(refs.Items) == 0 {
		if _, err := os.Stat(addonsFile()); os.IsNotExist(err) {
			refs = SeedAddons()
			firstRun = true
		}
	}

	player := NewPlayer(cfg)
	ctx = &appCtx{
		cfg:    cfg,
		refs:   refs,
		addons: LoadAddons(refs),
		player: player,
	}

	root := newMenuScreen()
	model := newApp(root)

	prog := tea.NewProgram(model, tea.WithAltScreen())
	ctx.prog = prog
	player.Attach(prog)

	if firstRun {
		go prog.Send(toastMsg{text: "welcome — add your stream addons under 'addons'"})
	}
	if cfg.MpvPath == "" {
		go prog.Send(toastMsg{text: "mpv not found — set its path under 'settings'", isErr: true})
	}

	// cleanup must be idempotent: it runs on the normal exit path, and again
	// from the signal handler if we're killed before Run() returns.
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			if ctx.cfg.CloseMpvOnExit {
				player.Quit()
			} else {
				player.Shutdown()
			}
		})
	}
	defer cleanup()

	// SIGTERM/SIGHUP don't go through Bubble Tea, so flush position and stop
	// mpv ourselves rather than orphaning a player that keeps advancing with
	// nothing recording where it got to.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-sigs
		cleanup()
		os.Exit(130)
	}()

	if _, err := prog.Run(); err != nil {
		cleanup()
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	cleanup()
	fmt.Printf("\n  %s  %s\n\n", accent(appName), grey("bye ♡"))
}

func usage() {
	fmt.Printf(`%s %s

  a terminal stremio client — no account, just addon manifest urls

  usage:
    %s              launch the tui
    %s --version    print version
    %s --config     print the config directory

  config lives in %s
    config.json     mpv path, quality preference, autoplay
    addons.json     your addon manifest urls (mode 0600)
    favourites.json
    history.json

  add addons from the 'addons' screen, e.g.
    https://v3-cinemeta.strem.io/manifest.json
    https://torrentio.strem.fun/torbox=<key>/manifest.json
`, appName, version, appName, appName, appName, configDir())
}

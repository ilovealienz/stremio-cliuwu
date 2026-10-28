package ui

import (
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"stremio-cliuwu/internal/download"
	"stremio-cliuwu/internal/player"
	"stremio-cliuwu/internal/poster"
	"stremio-cliuwu/internal/store"
	"stremio-cliuwu/internal/stremio"
)

// Options is what the command line asked for.
type Options struct {
	Search bool   // open search
	First  bool   // …and jump into the first result
	Kind   string // movie | show | anime, empty for any
	Query  string
}

// startupCmd turns the parsed flags into the screen to open on launch.
func startupCmd(o Options) tea.Cmd {
	label := map[string]string{"movie": "movies", "show": "shows", "anime": "anime"}

	switch {
	case o.Query != "":
		return push(newSearchScreenWith(o.Query, o.Kind, o.First))
	case o.Search:
		return push(searchPrompt())
	case o.Kind != "":
		return browseKind(o.Kind, label[o.Kind])
	}
	return nil
}

// lastInput is when the last key arrived, for returnMemoryWhenIdle.
var lastInput atomic.Int64

// returnMemoryWhenIdle returns freed memory to the OS a few seconds after the last key.
func returnMemoryWhenIdle() {
	freed := true
	for range time.Tick(time.Second) {
		idle := time.Since(time.Unix(0, lastInput.Load())) > 3*time.Second
		switch {
		case idle && !freed:
			debug.FreeOSMemory()
			freed = true
		case !idle:
			freed = false
		}
	}
}

// Run starts the UI and blocks until it exits.
func Run(opts Options) {
	// Collect sooner than the default; the heap is small. GOGC still wins if set.
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(50)
	}
	go returnMemoryWhenIdle()

	cfg := store.LoadConfig()
	applyAccent(cfg.Accent)
	if cfg.MpvPath == "" {
		cfg.MpvPath = store.DetectMpv()
		store.SaveConfig(cfg)
	}

	// First run: no addons.json yet, so seed the two metadata addons. Stream
	// addons are the user's to add — there's no account to pull them from.
	refs := store.LoadAddonRefs()
	firstRun := false
	if len(refs.Items) == 0 {
		if _, err := os.Stat(store.AddonsFile()); os.IsNotExist(err) {
			refs = store.SeedAddons()
			firstRun = true
		}
	}

	pl := player.New(cfg)
	downloader := download.New()
	*store.Cfg = cfg
	ctx = &appCtx{
		cfg:        store.Cfg,
		refs:       refs,
		player:     pl,
		downloader: downloader,
		loading:    true,
	}

	model := newApp(newMenuScreen())
	model.setStartup(startupCmd(opts))

	// 30fps keeps up with key repeat and halves idle wake-ups.
	prog := tea.NewProgram(model, tea.WithAltScreen(), tea.WithFPS(30))
	ctx.prog = prog
	pl.Attach(prog)
	downloader.Attach(prog)

	// Manifests are fetched after the UI is up, not before it.
	//
	// LoadAddons waits for every one of them, and the http client allows
	// fifteen seconds — so a single unresponsive addon held the whole launch.
	// This is a background goroutine, which is what Send is for.
	go prog.Send(addonsReadyMsg{Addons: stremio.LoadAddons(refs)})

	// Index only — no folder walk at startup. R on the downloads screen
	// rescans when the index and the disk have drifted apart.
	downloader.Load()

	store.WarmHistory()

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
			poster.KittyCleanup() // free terminal-side poster images, if any
			if ctx.cfg.CloseMpvOnExit {
				pl.Quit()
			} else {
				pl.Shutdown()
			}
			store.FlushHistory() // debounced writes mean the last few seconds are still in memory
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
	fmt.Printf("\n  %s  %s\n\n", accent(store.AppName), grey("bye ♡"))
}

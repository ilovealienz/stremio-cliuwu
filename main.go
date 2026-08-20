package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
)

const appName = "stremio-cliuwu"
// version is overwritten at build time via
// -ldflags="-X main.version=…". "dev" is what an untagged local build
// reports, which is more honest than a hardcoded number that goes stale.
var version = "dev"

// launchOpts is what the command line asked for.
type launchOpts struct {
	search bool   // open search
	first  bool   // …and jump into the first result
	kind   string // movie | show | anime, empty for any
	query  string
}

// parseArgs is hand-rolled rather than using the flag package so that
// "-sf -a chuunibyou" reads the way you'd expect, and so the query can be
// several words without quoting.
func parseArgs(args []string) (launchOpts, bool) {
	var o launchOpts
	var words []string

	for _, a := range args {
		switch a {
		case "--version", "-v":
			fmt.Printf("%s %s\n", appName, version)
			return o, false
		case "--config":
			fmt.Println(configDir())
			return o, false
		case "--help", "-h":
			usage()
			return o, false

		case "-s", "--search":
			o.search = true
		case "-sf", "--search-first":
			o.search, o.first = true, true
		case "-m", "--movies":
			o.kind = "movie"
		case "-t", "--shows":
			o.kind = "show"
		case "-a", "--anime":
			o.kind = "anime"

		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(os.Stderr, "unknown option %q\n\n", a)
				usage()
				return o, false
			}
			words = append(words, a)
		}
	}

	o.query = strings.Join(words, " ")
	return o, true
}

// startupCmd turns the parsed flags into the screen to open on launch.
func startupCmd(o launchOpts) tea.Cmd {
	label := map[string]string{"movie": "movies", "show": "shows", "anime": "anime"}

	switch {
	case o.query != "":
		return push(newSearchScreenWith(o.query, o.kind, o.first))
	case o.search:
		return push(searchPrompt())
	case o.kind != "":
		return browseKind(o.kind, label[o.kind])
	}
	return nil
}

func main() {
	opts, run := parseArgs(os.Args[1:])
	if !run {
		return
	}

	cfg := LoadConfig()
	applyAccent(cfg.Accent)
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
	downloader := NewDownloader()
	ctx = &appCtx{
		cfg:        cfg,
		refs:       refs,
		player:     player,
		downloader: downloader,
		loading:    true,
	}

	model := newApp(newMenuScreen())
	model.setStartup(startupCmd(opts))

	prog := tea.NewProgram(model, tea.WithAltScreen())
	ctx.prog = prog
	player.Attach(prog)
	downloader.Attach(prog)

	// Manifests are fetched after the UI is up, not before it.
	//
	// LoadAddons waits for every one of them, and the http client allows
	// fifteen seconds — so a single unresponsive addon held the whole launch.
	// This is a background goroutine, which is what Send is for.
	go prog.Send(addonsReadyMsg{Addons: LoadAddons(refs)})

	// Index only — no folder walk at startup. R on the downloads screen
	// rescans when the index and the disk have drifted apart.
	downloader.Load()

	WarmHistory()

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
			FlushHistory() // debounced writes mean the last few seconds are still in memory
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
    %s [options] [search terms]

  options:
    -s,  --search          open search
    -sf, --search-first    search and open the first result
    -m,  --movies          movies
    -t,  --shows           tv shows
    -a,  --anime           anime
    -v,  --version         print version
         --config          print the config directory
    -h,  --help            this

  examples:
    %s                     the main menu
    %s -a                  browse anime
    %s -s                  straight to the search box
    %s chuunibyou          search for it
    %s -sf -a chuunibyou   search anime, open the first hit

  config lives in %s
    config.json     mpv path, quality preference, downloads, appearance
    addons.json     your addon manifest urls (mode 0600)
    favourites.json
    history.json
`, appName, version, appName, appName, appName, appName, appName, appName, configDir())
}

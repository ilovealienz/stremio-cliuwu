package main

import (
	"fmt"
	"os"
	"strings"

	"stremio-cliuwu/internal/store"
	"stremio-cliuwu/internal/ui"
	"stremio-cliuwu/internal/update"
)

// version is overwritten at build time via
// -ldflags="-X main.version=…". "dev" is what an untagged local build
// reports, which is more honest than a hardcoded number that goes stale.
var version = "dev"

func main() {
	update.Version = version
	opts, run := parseArgs(os.Args[1:])
	if !run {
		return
	}
	ui.Run(opts)
}

// parseArgs is hand-rolled rather than using the flag package so that
// "-sf -a chuunibyou" reads the way you'd expect, and so the query can be
// several words without quoting.
func parseArgs(args []string) (ui.Options, bool) {
	var o ui.Options
	var words []string

	for _, a := range args {
		switch a {
		case "--version", "-v":
			fmt.Printf("%s %s\n", store.AppName, update.Version)
			return o, false
		case "--config":
			fmt.Println(store.ConfigDir())
			return o, false
		case "--help", "-h":
			usage()
			return o, false

		case "-s", "--search":
			o.Search = true
		case "-sf", "--search-first":
			o.Search, o.First = true, true
		case "-m", "--movies":
			o.Kind = "movie"
		case "-t", "--shows":
			o.Kind = "show"
		case "-a", "--anime":
			o.Kind = "anime"

		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(os.Stderr, "unknown option %q\n\n", a)
				usage()
				return o, false
			}
			words = append(words, a)
		}
	}

	o.Query = strings.Join(words, " ")
	return o, true
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

  files
    %s
      config.json     mpv path, quality preference, downloads, appearance
    %s
      addons.json     your addon manifest urls (mode 0600)
      favourites.json
    %s
      history.json    positions and watched state
      downloads.json
`, store.AppName, update.Version, store.AppName, store.AppName, store.AppName, store.AppName, store.AppName, store.AppName,
		store.ConfigDir(), store.DataDir(), store.StateDir())
}

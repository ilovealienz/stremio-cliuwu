package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/term"
)

// ── Colours ───────────────────────────────────────────────────────────────────

const (
	cReset   = "\033[0m"
	cBold    = "\033[1m"
	cRed     = "\033[91m"
	cGreen   = "\033[92m"
	cYellow  = "\033[93m"
	cCyan    = "\033[96m"
	cWhite   = "\033[97m"
	cGrey    = "\033[90m"
	cMagenta = "\033[95m"
	cBlue    = "\033[94m"
)

func bold(s string) string   { return cBold + s + cReset }
func grey(s string) string   { return cGrey + s + cReset }
func good(s string) string   { return cGreen + s + cReset }
func bad(s string) string    { return cRed + s + cReset }
func hi(s string) string     { return cCyan + s + cReset }
func accent(s string) string { return cMagenta + s + cReset }
func white(s string) string  { return cWhite + s + cReset }
func yell(s string) string   { return cYellow + s + cReset }
func blue(s string) string   { return cBlue + s + cReset }

// ── Terminal ──────────────────────────────────────────────────────────────────

func tw() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w < 20 {
		return 80
	}
	return w
}

func th() int {
	_, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || h < 5 {
		return 24
	}
	return h
}

// contentRows returns rows available for list content accounting for
// header(4) + optional sub(1) + nav hints(1) + prompt(1) + status bar(2) + padding(1)
func contentRows(hasSub bool) int {
	n := th() - 10
	if hasSub {
		n--
	}
	if n < 3 {
		return 3
	}
	return n
}

// ── Screen ────────────────────────────────────────────────────────────────────

func clearScreen() {
	StatusStop()
	if runtime.GOOS == "windows" {
		cmd := exec.Command("cmd", "/c", "cls")
		cmd.Stdout = os.Stdout
		cmd.Run()
	} else {
		fmt.Print("\033[H\033[2J")
	}
}

func header(section string) {
	clearScreen()
	w := tw()
	fmt.Printf("\r%s\033[K\n", accent(strings.Repeat("▓", w)))
	app := bold(white("  stremio-cli")) + accent("uwu")
	if section != "" {
		fmt.Printf("\r%s  %s  %s\033[K\n", app, grey("›"), bold(white(section)))
	} else {
		fmt.Printf("\r%s\033[K\n", app)
	}
	fmt.Printf("\r%s\033[K\n", accent(strings.Repeat("▓", w)))
	fmt.Println()
}

func blank()        { fmt.Println() }
func hint(s string) { fmt.Printf("  %s %s\n", grey("›"), grey(s)) }
func ok(s string)   { fmt.Printf("  %s %s\n", good("✓"), s) }
func fail(s string) { fmt.Printf("  %s %s\n", bad("✗"), s) }
func spin(s string) { fmt.Printf("  %s %s\n", accent("◆"), s) }

func sourceTag(source string) string {
	switch source {
	case "movie":
		return blue("[movie]")
	case "show":
		return hi("[show]")
	case "anime":
		return yell("[anime]")
	}
	return grey("[?]")
}

// ── List screen ───────────────────────────────────────────────────────────────

// ListOpts configures a list screen.
type ListOpts struct {
	Title string
	Sub   string
	Items []Item
	Page  int

	// Key enables
	CanBack         bool
	CanBackAll      bool
	CanFav          bool
	CanDelete       bool
	CanDeleteAll    bool
	CanToggleWatch  bool
	CanSort         bool
	CanFilter       bool
	CanPrevEp       bool // [ key
	CanNextEp       bool // ] key
}

type ListAct int

const (
	ActPick        ListAct = iota
	ActBack
	ActBackAll
	ActQuit
	ActFav
	ActDelete
	ActDeleteAll
	ActToggleWatch
	ActSort
	ActFilter
	ActNextPage
	ActPrevPage
	ActPrevEp
	ActNextEp
)

type ListResult struct {
	Act  ListAct
	Pick int    // 0-based absolute index when Act==ActPick
	Raw  string // raw input for unrecognised keys
}

// ShowList draws a paginated list screen and returns the user's action.
// The status bar ticker is started before the prompt and stopped after input.
func ShowList(opts ListOpts) ListResult {
	perPage := contentRows(opts.Sub != "")

	total := len(opts.Items)
	totalPages := (total + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}
	page := opts.Page
	if page >= totalPages {
		page = totalPages - 1
	}
	if page < 0 {
		page = 0
	}

	start := page * perPage
	end := start + perPage
	if end > total {
		end = total
	}

	// Draw
	header(opts.Title)
	if opts.Sub != "" {
		fmt.Printf("  %s\n\n", grey(opts.Sub))
	}

	for i := start; i < end; i++ {
		item := opts.Items[i]
		num := accent(fmt.Sprintf("[%2d]", i+1))

		prefix := "   "
		if item.Watched {
			prefix = good("✓") + " "
		}

		label := item.Label
		if item.Dim {
			label = grey(label)
		}

		sub := ""
		if item.Sub != "" {
			sub = "  " + grey(item.Sub)
		}

		badge := ""
		if item.Badge != "" {
			badge = "  " + item.Badge
		}

		fmt.Printf("  %s  %s%s%s%s\n", num, prefix, label, sub, badge)
	}

	blank()

	// Nav hints
	var nav []string
	if opts.CanFilter {
		nav = append(nav, grey("/")+"=search")
	}
	if opts.CanSort {
		nav = append(nav, grey("r")+"=reverse")
	}
	if opts.CanToggleWatch {
		nav = append(nav, grey("w")+"=watched")
	}
	if opts.CanFav {
		nav = append(nav, grey("f")+"=favourite")
	}
	if opts.CanDelete {
		nav = append(nav, grey("d")+"=remove")
	}
	if opts.CanDeleteAll {
		nav = append(nav, grey("D")+"=clear all")
	}
	if opts.CanPrevEp {
		nav = append(nav, grey("[")+"=prev ep")
	}
	if opts.CanNextEp {
		nav = append(nav, grey("]")+"=next ep")
	}
	if page > 0 {
		nav = append(nav, grey("p")+"=prev")
	}
	if page < totalPages-1 {
		nav = append(nav, grey("n")+"=next")
	}
	if opts.CanBackAll {
		nav = append(nav, grey("B")+"=back to list")
	}
	if opts.CanBack {
		nav = append(nav, grey("b")+"=back")
	}
	nav = append(nav, grey("0")+"=quit")
	if totalPages > 1 {
		nav = append(nav, grey(fmt.Sprintf("p%d/%d", page+1, totalPages)))
	}

	fmt.Printf("  %s\n", strings.Join(nav, "  "))
	fmt.Printf("  %s: ", grey("›"))
	StatusStart()
	raw := readLine()
	StatusStop()

	// Handle input
	switch raw {
	case "0", "q", "Q":
		return ListResult{Act: ActQuit}
	case "b":
		if opts.CanBack {
			return ListResult{Act: ActBack}
		}
	case "B":
		if opts.CanBackAll {
			return ListResult{Act: ActBackAll}
		}
	case "f", "F":
		if opts.CanFav {
			return ListResult{Act: ActFav}
		}
	case "d":
		if opts.CanDelete {
			return ListResult{Act: ActDelete}
		}
	case "D":
		if opts.CanDeleteAll {
			return ListResult{Act: ActDeleteAll}
		}
	case "w", "W":
		if opts.CanToggleWatch {
			return ListResult{Act: ActToggleWatch}
		}
	case "r", "R":
		if opts.CanSort {
			return ListResult{Act: ActSort}
		}
	case "/":
		if opts.CanFilter {
			return ListResult{Act: ActFilter}
		}
	case "n":
		if page < totalPages-1 {
			return ListResult{Act: ActNextPage}
		}
	case "p":
		if page > 0 {
			return ListResult{Act: ActPrevPage}
		}
	case "[":
		if opts.CanPrevEp {
			return ListResult{Act: ActPrevEp}
		}
	case "]":
		if opts.CanNextEp {
			return ListResult{Act: ActNextEp}
		}
	}

	n, err := strconv.Atoi(raw)
	if err == nil && n >= 1 && n <= total {
		return ListResult{Act: ActPick, Pick: n - 1}
	}

	return ListResult{Act: ActPick, Pick: -1, Raw: raw}
}

// ── Input ─────────────────────────────────────────────────────────────────────

var lineReader = bufio.NewReader(os.Stdin)

func readLine() string {
	s, _ := lineReader.ReadString('\n')
	return strings.TrimSpace(s)
}

func readPassword(p string) string {
	fmt.Printf("  %s: ", grey(p))
	b, _ := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	return string(b)
}

func prompt(p string) string {
	fmt.Printf("  %s: ", grey(p))
	return readLine()
}

// ── Signals ───────────────────────────────────────────────────────────────────

func initSignals() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		for range c {
			StatusStop()
			clearScreen()
			fmt.Printf("\n  %s  %s\n\n", accent(appName), grey("bye ♡"))
			os.Exit(0)
		}
	}()
}

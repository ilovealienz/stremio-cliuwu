package main

import (
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// ── Raw ANSI helpers ──────────────────────────────────────────────────────────
//
// Kept verbatim from the old ui.go because streams.go / history.go / favs.go
// build their row labels by embedding these directly into strings. lipgloss
// handles layout; these handle inline emphasis inside a label.

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
func accent(s string) string { return stAccentText.Render(s) }
func white(s string) string  { return cWhite + s + cReset }
func yell(s string) string   { return cYellow + s + cReset }
func blue(s string) string   { return cBlue + s + cReset }

// stripANSI removes escape sequences so we can match/measure raw text.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			j := i + 1
			for j < len(s) && !((s[j] >= 'a' && s[j] <= 'z') || (s[j] >= 'A' && s[j] <= 'Z')) {
				j++
			}
			i = j + 1
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// tw is the current terminal width. streams.go uses it to size labels.
func tw() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w < 20 {
		return 80
	}
	return w
}

// ── lipgloss ──────────────────────────────────────────────────────────────────

// ── Theme ─────────────────────────────────────────────────────────────────────

var (
	clAccent = lipgloss.Color("13")
	clGrey   = lipgloss.Color("240")
	clDim    = lipgloss.Color("244")
	clGreen  = lipgloss.Color("10")
	clYellow = lipgloss.Color("11")
	clRed    = lipgloss.Color("9")
	clDarkRed = lipgloss.Color("124") // stopped, not failed — muted rather than alarming
	clCyan   = lipgloss.Color("14")
)

// accentPresets are the named colours offered in settings. Anything else is
// passed through to lipgloss, so "#ff8800" or a 0-255 terminal index works too.
var accentPresets = map[string]string{
	"pink":   "13",
	"purple": "99",
	"blue":   "12",
	"cyan":   "14",
	"teal":   "43",
	"green":  "10",
	"lime":   "118",
	"yellow": "11",
	"orange": "208",
	"red":    "9",
	"grey":   "245",
	"white":  "15",
}

var accentOrder = []string{
	"pink", "purple", "blue", "cyan", "teal",
	"green", "lime", "yellow", "orange", "red", "grey", "white",
}

// resolveAccent turns a config value into a colour. Unknown values fall back
// to pink rather than rendering as unstyled text.
func resolveAccent(name string) lipgloss.Color {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return lipgloss.Color("13")
	}
	if v, ok := accentPresets[name]; ok {
		return lipgloss.Color(v)
	}
	if strings.HasPrefix(name, "#") && (len(name) == 7 || len(name) == 4) {
		return lipgloss.Color(name)
	}
	if n, err := strconv.Atoi(name); err == nil && n >= 0 && n <= 255 {
		return lipgloss.Color(name)
	}
	return lipgloss.Color("13")
}

var (
	stTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	stCrumb = lipgloss.NewStyle().Foreground(clDim)
	stSub   = lipgloss.NewStyle().Foreground(clDim)
	stHint  = lipgloss.NewStyle().Foreground(clGrey)
	stErr   = lipgloss.NewStyle().Foreground(clRed)
	stOK    = lipgloss.NewStyle().Foreground(clGreen)
	stWarn  = lipgloss.NewStyle().Foreground(clYellow)
	stStop  = lipgloss.NewStyle().Foreground(clDarkRed)
	stToast = lipgloss.NewStyle().Foreground(clCyan)
	stFaint = lipgloss.NewStyle().Foreground(clGrey).Faint(true)

	stSelected = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	stBarRest  = lipgloss.NewStyle().Foreground(clGrey)

	// Everything below is rebuilt by applyAccent.
	stBrand      lipgloss.Style
	stRule       lipgloss.Style
	stCursor     lipgloss.Style
	stKey        lipgloss.Style
	stSection    lipgloss.Style
	stBarFill    lipgloss.Style
	stAccentText lipgloss.Style
)

func init() { applyAccent("") }

// applyAccent restyles everything that uses the accent colour. Called at
// startup and again whenever the setting changes, so the change is live
// without a restart.
func applyAccent(name string) {
	clAccent = resolveAccent(name)

	stBrand = lipgloss.NewStyle().Bold(true).Foreground(clAccent)
	stRule = lipgloss.NewStyle().Foreground(clAccent)
	stCursor = lipgloss.NewStyle().Foreground(clAccent).Bold(true)
	stKey = lipgloss.NewStyle().Foreground(clAccent)
	stSection = lipgloss.NewStyle().Foreground(clAccent).Bold(true)
	stBarFill = lipgloss.NewStyle().Foreground(clAccent)
	stAccentText = lipgloss.NewStyle().Foreground(clAccent)
}

// accentSwatch renders a small preview block in the given colour.
func accentSwatch(name string) string {
	return lipgloss.NewStyle().Foreground(resolveAccent(name)).Render("███")
}

// footerKeys is the most recent set of hints a screen asked for.
//
// Captured rather than passed around: keyHint is called once per frame, by
// the top screen's Footer, so this always describes what is on the footer
// right now. It lets ? list the keys the footer had no room for without every
// screen having to declare the same list twice and keep the two in step.
var footerKeys [][2]string

// footerShown is how many hints the footer keeps before deferring to ?.
//
// Five is the usual advice and more than that stops being readable — the
// stream picker had eleven, which is a wall of text rather than a reminder.
const footerShown = 2

// keyHintMore renders extra pairs the app appends to a screen's footer,
// without disturbing what ? will show.
//
// keyHint captures what it renders so the overlay can list the keys the
// footer trimmed. Calling it a second time to append the globals replaced
// that capture with just the globals — which is how ? came to show "S, X"
// followed by the whole global set again.
func keyHintMore(pairs ...[2]string) string {
	var out []string
	for _, p := range pairs {
		if p[0] != "" {
			out = append(out, stKey.Render(p[0])+stHint.Render("="+p[1]))
		}
	}
	return stHint.Render("  ") + strings.Join(out, stHint.Render("  "))
}

// keyHint renders "k=label" pairs for the footer.
//
// Only the first few: screens list their keys in order of use, so the ones
// that matter survive the trim and the rest move behind ?.
func keyHint(pairs ...[2]string) string {
	live := make([][2]string, 0, len(pairs))
	for _, p := range pairs {
		if p[0] != "" {
			live = append(live, p)
		}
	}
	footerKeys = live

	shown := live
	if len(live) > footerShown+1 {
		shown = live[:footerShown]

		// Back survives the trim wherever it is in the list. Screens put it
		// last, being the least interesting thing they offer, which is also
		// what makes it the first casualty — and it is the one key someone
		// stuck on a screen actually needs.
		for _, p := range live[footerShown:] {
			if p[0] == "b/esc" || p[0] == "esc" {
				shown = append(append([][2]string{}, live[:footerShown-1]...), p)
				break
			}
		}
	}

	var out []string
	for _, p := range shown {
		out = append(out, stKey.Render(p[0])+stHint.Render("="+p[1]))
	}
	return stHint.Render("  ") + strings.Join(out, stHint.Render("  "))
}

// withStatus puts the position counter ahead of the key hints.
//
// Trailing it left the count sitting against the global "X=stop mpv" that the
// root model appends, where "3/48" read as part of that binding rather than as
// where you are in the list.
func withStatus(status, hints string) string {
	if status == "" {
		return hints
	}
	return " " + stHint.Render(status) + "  " + hints
}

// clamp truncates an ANSI-containing string to w display columns.
func clamp(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(s)
}

// ellipsize truncates to w columns with a trailing marker, ANSI-aware.
func ellipsize(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	return clamp(s, w-1) + stHint.Render("…")
}

func rule(w int) string { return stRule.Render(strings.Repeat("─", max(0, w))) }

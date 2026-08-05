package main

import (
	"os"
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
func accent(s string) string { return cMagenta + s + cReset }
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

var (
	clAccent = lipgloss.Color("13")
	clGrey   = lipgloss.Color("240")
	clDim    = lipgloss.Color("244")
	clGreen  = lipgloss.Color("10")
	clYellow = lipgloss.Color("11")
	clRed    = lipgloss.Color("9")
	clCyan   = lipgloss.Color("14")
)

var (
	stTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	stBrand = lipgloss.NewStyle().Bold(true).Foreground(clAccent)
	stCrumb   = lipgloss.NewStyle().Foreground(clDim)
	stSection = lipgloss.NewStyle().Foreground(clAccent).Bold(true)
	stRule  = lipgloss.NewStyle().Foreground(clAccent)

	stCursor   = lipgloss.NewStyle().Foreground(clAccent).Bold(true)
	stSelected = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	stSub      = lipgloss.NewStyle().Foreground(clDim)
	stHint     = lipgloss.NewStyle().Foreground(clGrey)
	stKey      = lipgloss.NewStyle().Foreground(clAccent)
	stErr      = lipgloss.NewStyle().Foreground(clRed)
	stOK       = lipgloss.NewStyle().Foreground(clGreen)
	stWarn     = lipgloss.NewStyle().Foreground(clYellow)
	stBarFill  = lipgloss.NewStyle().Foreground(clAccent)
	stBarRest  = lipgloss.NewStyle().Foreground(clGrey)
	stToast    = lipgloss.NewStyle().Foreground(clCyan)
)

// keyHint renders "k=label" pairs for the footer.
func keyHint(pairs ...[2]string) string {
	var out []string
	for _, p := range pairs {
		if p[0] == "" {
			continue
		}
		out = append(out, stKey.Render(p[0])+stHint.Render("="+p[1]))
	}
	return stHint.Render("  ") + strings.Join(out, stHint.Render("  "))
}

// clamp truncates an ANSI-containing string to w display columns.
func clamp(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(s)
}

func rule(w int) string { return stRule.Render(strings.Repeat("─", max(0, w))) }

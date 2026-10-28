package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestFormatHelpers(t *testing.T) {
	var out []string
	add := func(f string, a ...any) { out = append(out, fmt.Sprintf(f, a...)) }

	for _, s := range []string{"7.123", " 8 ", "N/A", ""} {
		add("fmtRating(%q)=%q", s, fmtRating(s))
	}
	pairs := [][2]string{{"S01E02", "S01E10"}, {"S01E10", "S01E02"}, {"Season 2", "season 10"},
		{"a", "a"}, {"a", "ab"}, {"ab", "a"}, {"file007", "file7"}, {"x9y", "x10y"}, {"B", "a"}}
	for _, p := range pairs {
		add("naturalLess(%q,%q)=%v", p[0], p[1], naturalLess(p[0], p[1]))
	}
	for _, p := range [][2]string{{"Show OVA 1", "Show OVA 2"}, {"abc", "xyz"}, {"", "a"}} {
		add("commonPrefix(%q,%q)=%q", p[0], p[1], commonPrefix(p[0], p[1]))
	}
	for _, k := range []string{"dmy", "mdy", "ymd", "long", "?"} {
		add("date(%q)=%s %s %s", k, dateFormatName(k), dateLayout(k), nextDateFormat(k))
	}
	golden(t, "format_helpers", out)
}

func TestFitLine(t *testing.T) {
	line := "  " + stSub.Render("a fairly long header line that needs trimming")
	if fitLine(line, 0) != line {
		t.Error("unsized screen should leave the line alone")
	}
	if fitLine("  short", 40) != "  short" {
		t.Error("a line that fits was changed")
	}
	got := fitLine(line, 20)
	if w := lipgloss.Width(got); w > 19 {
		t.Errorf("trimmed to %d columns, want at most 19: %q", w, stripANSI(got))
	}
	if !strings.HasSuffix(stripANSI(got), "…") {
		t.Errorf("trimmed line should end with …: %q", stripANSI(got))
	}
}

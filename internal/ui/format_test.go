package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

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

func TestUntilRelease(t *testing.T) {
	now := time.Now()
	in := func(days int) string {
		d := time.Date(now.Year(), now.Month(), now.Day()+days, 23, 30, 0, 0, time.UTC)
		return d.Format("2006-01-02T15:04:05.000Z")
	}
	for _, c := range []struct {
		days int
		want string
	}{
		{-1, ""}, {0, "today"}, {1, "tomorrow"}, {2, "in 2 days"}, {7, "in 1 week"},
		{9, "in 1 week, 2 days"}, {30, "in 4 weeks, 2 days"}, {45, "in 1 month, 2 weeks"},
		{60, "in 2 months"}, {400, "in 1 year, 1 month"}, {730, "in 2 years"},
	} {
		if got := untilRelease(in(c.days)); got != c.want {
			t.Errorf("%d days: %q, want %q", c.days, got, c.want)
		}
	}
	if got := untilRelease("not a date"); got != "" {
		t.Errorf("bad date: %q", got)
	}
}

func TestIndexOfSeason(t *testing.T) {
	seasons := []int{0, 1, 2, 5}
	if indexOfSeason(seasons, 5) != 3 || indexOfSeason(seasons, 0) != 0 || indexOfSeason(seasons, 9) != 0 {
		t.Fatal("wrong rows")
	}
}

package store

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// ── Naming ────────────────────────────────────────────────────────────────────

// winReserved are DOS device names that Windows still refuses as filenames.
// A name is reserved on its base alone, so NUL.txt and NUL.tar.gz are both
// the null device. Windows 11 relaxed most of this, but older versions
// haven't, and applying it everywhere keeps a synced download folder portable.
var winReserved = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com0": true, "com1": true, "com2": true, "com3": true, "com4": true,
	"com5": true, "com6": true, "com7": true, "com8": true, "com9": true,
	"lpt0": true, "lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true,
	"lpt5": true, "lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// SafeName strips anything a filesystem would object to. The Windows set is
// the strictest, so use it everywhere and keep filenames portable.
func SafeName(s string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '-'
		}
		if r < 32 {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	s = strings.Trim(s, ".") // leading/trailing dots are stripped by Windows
	if s == "" {
		return "untitled"
	}

	base := s
	if i := strings.IndexByte(base, '.'); i > 0 {
		base = base[:i]
	}
	if winReserved[strings.ToLower(base)] {
		s = "_" + s
	}
	return s
}

// Naming is pattern-driven so it can be changed without a rebuild.
// Placeholders: {title} {year} {show} {season} {episode}
//
// Either slash makes a folder separator. When "organise downloads" is off only the
// last segment is used, so one setting controls depth for both patterns.
const (
	DefaultMoviePattern   = "Movies/({year}) {title}"
	DefaultEpisodePattern = "{show}/Season {season}/{episode} {title}"
)

var spaceRun = regexp.MustCompile(`\s+`)

// tidySegment cleans up after a placeholder resolved to nothing — an absent
// year would otherwise leave "() Inception" behind.
func tidySegment(s string) string {
	for _, empty := range []string{"()", "[]", "{}", "- -"} {
		s = strings.ReplaceAll(s, empty, "")
	}
	s = spaceRun.ReplaceAllString(s, " ")
	return strings.Trim(s, " -_·.")
}

// ExpandPattern fills in a naming pattern and returns its folder parts.
//
// The pattern is split before the values go in, so only a separator you
// wrote makes a folder: a "/" in a title like Fate/Zero stays in the name.
// Either separator counts: a Windows user will reach for a backslash.
func ExpandPattern(pattern string, vals map[string]string) []string {
	// One pass, so a title that happens to contain "{year}" isn't filled in
	// again, and the result doesn't depend on map order.
	pairs := make([]string, 0, 2*len(vals))
	for k, v := range vals {
		pairs = append(pairs, "{"+k+"}", v)
	}
	fill := strings.NewReplacer(pairs...)

	var segs []string
	for _, seg := range strings.FieldsFunc(pattern, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		seg = fill.Replace(seg)
		if seg = tidySegment(seg); seg != "" {
			segs = append(segs, SafeName(seg))
		}
	}
	return segs
}

// MaxNameBytes is the longest file or folder name Linux and macOS accept.
const MaxNameBytes = 255

// FitName shortens a name to max bytes without splitting a character, then
// tidies the end the way SafeName does.
func FitName(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return SafeName(strings.TrimRight(s[:cut], " .-_·"))
}

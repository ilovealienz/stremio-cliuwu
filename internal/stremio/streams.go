package stremio

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

var (
	invisRe  = regexp.MustCompile(`[\x{200b}-\x{200f}\x{2060}-\x{206f}\x{fe0f}\x{00ad}\x{feff}\x{200d}\x{200c}\x{180e}\x{00a0}\x{202a}-\x{202e}\x{2028}\x{2029}]+`)
	ResRe    = regexp.MustCompile(`(?i)\b(4K|2K|2160[Pp]|1440[Pp]|1080[Pp]|720[Pp]|480[Pp]|360[Pp])\b`)
	SourceRe = regexp.MustCompile(`[〈<]([^〉>]+)[〉>]`)
	SizeRe   = regexp.MustCompile(`(?i)(\d+\.?\d*)\s*(GB|MB|GiB|MiB)`)
	StarRe   = regexp.MustCompile(`[★☆✦✧⭐]+`)
)

func StripInvis(s string) string {
	s = invisRe.ReplaceAllString(s, " ")
	var b strings.Builder
	for _, r := range s {
		if unicode.IsPrint(r) || r == ' ' {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func ExtractRes(s string) string {
	m := ResRe.FindString(s)
	if m == "" {
		return ""
	}
	switch strings.ToUpper(m) {
	case "2160P":
		return "4K"
	case "1440P":
		return "2K"
	default:
		return strings.ToUpper(m)
	}
}

func ExtractSrc(s string) string {
	m := SourceRe.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func ExtractSize(s string) string {
	m := SizeRe.FindStringSubmatch(s)
	if len(m) < 3 {
		return ""
	}
	return m[1] + " " + strings.ToUpper(m[2])
}

// StreamBytes is the stream's size in bytes, from behaviorHints or the size
// shown in its text, or 0 when it doesn't say.
func StreamBytes(s Stream) int64 {
	if s.BehaviorHints.VideoSize > 0 {
		return s.BehaviorHints.VideoSize
	}
	for _, hay := range []string{s.Description, s.Title, s.Name} {
		m := SizeRe.FindStringSubmatch(hay)
		if m == nil {
			continue
		}
		f, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			continue
		}
		if strings.HasPrefix(strings.ToUpper(m[2]), "G") {
			return int64(f * (1 << 30))
		}
		return int64(f * (1 << 20))
	}
	return 0
}

// cachedMarkers are how the common debrid addons signal instant availability.
// Torrentio uses ⚡ plus a "[RD+]"-style provider tag; ⏳ means it would have to
// be downloaded first.
// Torrentio marks a debrid result "[TB+]" when it's ready to stream and
// "[TB download]" when it isn't, per provider. Both are matched precisely
// rather than by searching for the word "download" anywhere in the row — a
// release named ...WEB-DL.Download.Edition would otherwise be sorted to the
// bottom and shown with an hourglass it hasn't earned.
const debridProviders = `RD|TB|AD|PM|DL|OC|PR|EC`

var (
	cachedProviderRe  = regexp.MustCompile(`\[(` + debridProviders + `)\+\]`)
	pendingProviderRe = regexp.MustCompile(`(?i)\[(` + debridProviders + `)\s+download\]`)
)

// StreamCached classifies a stream: 1 cached, -1 explicitly not cached,
// 0 unknown (a direct HTTP addon, say, where the question doesn't apply).
func StreamCached(s Stream) int {
	// An addon that states it wins over anything parsed out of the text.
	if s.Cached != nil {
		if *s.Cached {
			return 1
		}
		return -1
	}

	hay := s.Name + " " + s.Title + " " + s.Description

	switch {
	case strings.Contains(hay, "⚡"), cachedProviderRe.MatchString(hay):
		return 1
	case strings.Contains(hay, "⏳"), pendingProviderRe.MatchString(hay):
		return -1
	}
	return 0
}

// SortStreams orders the stream list. Cached-first matters more than
// resolution in practice: picking an uncached debrid result means waiting for
// the provider to fetch the torrent before mpv gets anything at all.
// matchWords flattens a release name so terms can be matched against it.
//
// Release naming has no agreed separator — the same tag turns up as "AI
// Upscale", "AI.Upscale" and "Ai_Upscaled" depending on who packed it. Every
// separator becomes a space, so one term covers all of them, and the result
// is padded so callers can anchor to word boundaries.
func matchWords(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		switch r {
		case '.', '_', '-', '[', ']', '(', ')', '{', '}', ',', '+', '/', ':':
			return ' '
		}
		return r
	}, s)
	return " " + strings.Join(strings.Fields(s), " ") + " "
}

// StreamBlocked reports whether a stream matches any of your blocked terms.
//
// Whole words, not substrings. Blocking "cam" should hide a camrip without
// also hiding Camelot, a film called Cam, or anything from a group with
// "cam" in its name — a block list you can't trust is worse than none. A
// trailing * makes a term match the start of a word instead, so "upscale*"
// covers upscaled and upscaler without listing each.
//
// Matched across the name, title, description and filename together: a
// release is sometimes flagged in the addon's label and sometimes only in
// the filename.
func StreamBlocked(s Stream, terms []string) bool {
	if len(terms) == 0 {
		return false
	}
	hay := matchWords(s.Name + " " + s.Title + " " + s.Description + " " +
		s.BehaviorHints.Filename)

	for _, raw := range terms {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}

		prefix := strings.HasSuffix(t, "*")
		t = matchWords(strings.TrimSuffix(t, "*"))
		if t = strings.TrimSuffix(t, " "); t == "" || t == " " {
			continue
		}

		if prefix {
			if strings.Contains(hay, t) { // t still carries its leading space
				return true
			}
			continue
		}
		if strings.Contains(hay, t+" ") {
			return true
		}
	}
	return false
}

// SortStreams orders by addon priority, then cached, then quality.
//
// Addon order has to come first. With cached-first as the primary key, an
// addon serving direct debrid files — no ⚡ or [RD+] in the title, so it
// scores as unknown — sank below one whose results are detectably cached,
// however high you'd placed it. Reordering the list then looked like it did
// nothing at all for that addon.
func SortStreams(streams []Stream, preferred string, cachedFirst bool) []Stream {
	pref := strings.ToUpper(preferred)

	score := func(s Stream) (int, int) {
		cache := 0
		if cachedFirst {
			cache = StreamCached(s)
		}
		quality := 0
		if pref != "" && strings.Contains(strings.ToUpper(s.Name+s.Title), pref) {
			quality = 1
		}
		return cache, quality
	}

	sort.SliceStable(streams, func(i, j int) bool {
		a, b := streams[i], streams[j]
		if a.Rank != b.Rank {
			return a.Rank < b.Rank
		}

		ca, qa := score(a)
		cb, qb := score(b)
		if ca != cb {
			return ca > cb
		}
		return qa > qb
	})
	return streams
}

// ReleaseName is a stream's file or release name. The filename hint wins;
// otherwise the title and description lines are searched, preferring one
// that names a video file (Jackettio puts the pack first and the file second),
// then one that looks like a release, then the first line. Comet-style emoji
// prefixes are dropped. The stream name is the last resort.
func ReleaseName(s Stream) string {
	if f := StripInvis(s.BehaviorHints.Filename); f != "" {
		return f
	}
	loadRx()
	var lines []string
	for _, block := range []string{s.Title, s.Description} {
		for _, l := range strings.Split(block, "\n") {
			if l = strings.TrimLeftFunc(StripInvis(l), notNameStart); l != "" {
				lines = append(lines, l)
			}
		}
	}
	for _, l := range lines {
		if rx.ext.MatchString(l) {
			return l
		}
	}
	for _, l := range lines {
		if rx.releaseLike.MatchString(l) {
			return l
		}
	}
	if len(lines) > 0 {
		return lines[0]
	}
	return StripInvis(s.Name)
}

// NamesFile reports whether a release name is a video file's name, rather
// than a pack's that several episodes share.
func NamesFile(name string) bool {
	loadRx()
	return rx.ext.MatchString(name)
}

// notNameStart skips leading emoji and symbols, keeping an anime "[Group]".
func notNameStart(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '['
}

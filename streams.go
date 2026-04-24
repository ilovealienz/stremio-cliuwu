package main

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

var (
	invisRe  = regexp.MustCompile(`[\x{200b}-\x{200f}\x{2060}-\x{206f}\x{fe0f}\x{00ad}\x{feff}\x{200d}\x{200c}\x{180e}\x{00a0}\x{202a}-\x{202e}\x{2028}\x{2029}]+`)
	resRe    = regexp.MustCompile(`(?i)\b(4K|2K|2160[Pp]|1440[Pp]|1080[Pp]|720[Pp]|480[Pp]|360[Pp])\b`)
	sourceRe = regexp.MustCompile(`[〈<]([^〉>]+)[〉>]`)
	sizeRe   = regexp.MustCompile(`(?i)(\d+\.?\d*)\s*(GB|MB|GiB|MiB)`)
	starRe   = regexp.MustCompile(`[★☆✦✧⭐]+`)
)

func stripInvis(s string) string {
	s = invisRe.ReplaceAllString(s, " ")
	var b strings.Builder
	for _, r := range s {
		if unicode.IsPrint(r) || r == ' ' {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func extractRes(s string) string {
	m := resRe.FindString(s)
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

func extractSrc(s string) string {
	m := sourceRe.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func extractSize(s string) string {
	m := sizeRe.FindStringSubmatch(s)
	if len(m) < 3 {
		return ""
	}
	return m[1] + " " + strings.ToUpper(m[2])
}

// FmtStream produces a readable label for a stream entry.
func FmtStream(s Stream) string {
	tag := grey("[" + s.Addon + "]")

	cached := ""
	if strings.Contains(s.Name, "⚡") {
		cached = "⚡"
	} else if strings.Contains(s.Name, "⏳") {
		cached = "⏳"
	}

	cleanName  := stripInvis(starRe.ReplaceAllString(s.Name, ""))
	cleanTitle := stripInvis(s.Title)
	cleanDesc  := stripInvis(s.Description)

	res := extractRes(cleanName)
	if res == "" {
		res = extractRes(cleanTitle)
	}
	src  := extractSrc(cleanName)
	size := extractSize(cleanDesc)
	if size == "" {
		size = extractSize(cleanName)
	}

	var parts []string
	if res != ""    { parts = append(parts, bold(res)) }
	if cached != "" { parts = append(parts, cached) }
	if src != ""    { parts = append(parts, hi(src)) }
	if size != ""   { parts = append(parts, grey(size)) }

	filename := cleanTitle
	if filename == "" {
		leftover := resRe.ReplaceAllString(cleanName, "")
		leftover  = sourceRe.ReplaceAllString(leftover, "")
		leftover  = sizeRe.ReplaceAllString(leftover, "")
		leftover  = strings.ReplaceAll(leftover, "⚡", "")
		leftover  = strings.ReplaceAll(leftover, "⏳", "")
		filename  = strings.Join(strings.Fields(leftover), " ")
	}
	if filename != "" {
		runes := []rune(filename)
		if len(runes) > 55 {
			filename = string(runes[:55]) + "…"
		}
		parts = append(parts, bold(filename))
	}

	if len(parts) == 0 {
		return tag + "  " + grey("(no info)")
	}
	return tag + "  " + strings.Join(parts, "  ")
}

// SortStreams moves streams matching the preferred quality to the top.
func SortStreams(streams []Stream, preferred string) []Stream {
	if preferred == "" {
		return streams
	}
	pref := strings.ToUpper(preferred)
	sort.SliceStable(streams, func(i, j int) bool {
		iMatch := strings.Contains(strings.ToUpper(streams[i].Name+streams[i].Title), pref)
		jMatch := strings.Contains(strings.ToUpper(streams[j].Name+streams[j].Title), pref)
		return iMatch && !jMatch
	})
	return streams
}

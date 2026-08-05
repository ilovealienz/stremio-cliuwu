package main

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func fmtSecs(s float64) string {
	if s < 0 {
		s = 0
	}
	t := int(s)
	h, m, sec := t/3600, (t%3600)/60, t%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%02d:%02d", m, sec)
}

func fmtRelease(s string) string {
	if s == "" || s == "N/A" {
		return ""
	}
	for _, layout := range []string{
		"2006-01-02",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		"02 Jan 2006",
		"Jan 2, 2006",
		time.RFC3339,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			// Reject the Unix epoch — means the field was unset
			if t.IsZero() || (t.Year() == 1970 && t.Month() == 1 && t.Day() == 1) {
				return ""
			}
			return t.Format("Mon 02/01/06")
		}
	}
	return ""
}

// fmtVideoID turns "tt1234:2:5" or "kitsu:123:7" into "S02E05".
func fmtVideoID(videoID string) string {
	parts := strings.Split(videoID, ":")
	if len(parts) >= 3 {
		sStr, eStr := parts[len(parts)-2], parts[len(parts)-1]
		if parts[0] == "kitsu" && len(parts) == 3 {
			sStr = "1"
			eStr = parts[2]
		}
		s, se := strconv.Atoi(sStr)
		e, ee := strconv.Atoi(eStr)
		if se == nil && ee == nil {
			return fmt.Sprintf("S%02dE%02d", s, e)
		}
	}
	return videoID
}

// cleanTitle un-escapes and de-extensions an mpv media-title.
func cleanTitle(s string) string {
	if s == "" {
		return ""
	}
	if once, err := url.QueryUnescape(s); err == nil {
		s = once
	}
	if twice, err := url.QueryUnescape(s); err == nil {
		s = twice
	}
	for _, ext := range []string{".mkv", ".mp4", ".avi", ".m4v", ".ts", ".mov", ".wmv"} {
		if strings.HasSuffix(strings.ToLower(s), ext) {
			s = s[:len(s)-len(ext)]
			break
		}
	}
	return strings.TrimSpace(s)
}

func sourceTag(source string) string {
	switch source {
	case "movie":
		return accent("[movie]")
	case "show":
		return hi("[show]")
	case "anime":
		return yell("[anime]")
	}
	return grey("[?]")
}

func metaItem(m Meta) Item {
	if m.Name == "" {
		m.Name = m.ID // some debrid catalogs omit names entirely
	}
	yr := m.Year
	if yr == "" {
		yr = "?"
	}
	return Item{
		Label: bold(m.Name),
		Sub:   "(" + yr + ")",
		Badge: sourceTag(m.Source),
	}
}

func epItem(v Video, watched bool) Item {
	badge := ""
	if r := fmtRelease(v.Released); r != "" {
		badge = grey(r)
	}
	return Item{
		Label:   bold(hi(fmt.Sprintf("E%02d", v.Episode))),
		Sub:     v.Title,
		Badge:   badge,
		Watched: watched,
	}
}

// progressGlyph renders a compact "12:34 / 45:00 (27%)" style readout.
func progressGlyph(pos, dur float64) string {
	if dur <= 0 {
		return fmtSecs(pos)
	}
	return fmt.Sprintf("%s / %s (%.0f%%)", fmtSecs(pos), fmtSecs(dur), pos/dur*100)
}

// progressBar draws a filled bar of the given width.
func progressBar(frac float64, w int) string {
	if w <= 0 {
		return ""
	}
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	fill := int(frac * float64(w))
	return stBarFill.Render(strings.Repeat("━", fill)) +
		stBarRest.Render(strings.Repeat("━", w-fill))
}

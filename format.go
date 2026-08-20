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

// parseRelease reads the assorted date formats the metadata addons emit.
func parseRelease(s string) (time.Time, bool) {
	if s == "" || s == "N/A" {
		return time.Time{}, false
	}
	for _, layout := range []string{
		"2006-01-02",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		"02 Jan 2006",
		"Jan 2, 2006",
		time.RFC3339,
	} {
		t, err := time.Parse(layout, s)
		if err != nil {
			continue
		}
		// Reject the Unix epoch — means the field was unset
		if t.IsZero() || (t.Year() == 1970 && t.Month() == 1 && t.Day() == 1) {
			return time.Time{}, false
		}
		return t, true
	}
	return time.Time{}, false
}

// dateFormats are the layouts offered in settings. 02/01 and 01/02 are
// indistinguishable for the first twelve days of a month, so which one you're
// looking at can't be inferred from the output — it has to be a setting.
var dateFormats = []string{"dmy", "mdy", "ymd", "long"}

// dateFormatName is what settings shows, since "dmy" tells you the field
// order but not which convention it belongs to.
func dateFormatName(kind string) string {
	switch kind {
	case "mdy":
		return "US"
	case "ymd":
		return "ISO"
	case "long":
		return "written"
	}
	return "European"
}

func dateLayout(kind string) string {
	switch kind {
	case "mdy":
		return "Mon 01/02/06"
	case "ymd":
		return "Mon 2006-01-02"
	case "long":
		return "Mon 2 Jan 2006"
	}
	return "Mon 02/01/06"
}

func nextDateFormat(cur string) string {
	for i, f := range dateFormats {
		if f == cur {
			return dateFormats[(i+1)%len(dateFormats)]
		}
	}
	return "dmy"
}

// dateSample renders today in a layout, for the settings preview.
func dateSample(kind string) string { return time.Now().Format(dateLayout(kind)) }

func fmtRelease(s string) string {
	t, ok := parseRelease(s)
	if !ok {
		return ""
	}

	kind := "dmy"
	if ctx != nil {
		kind = ctx.cfg.DateFormat
	}
	return t.Format(dateLayout(kind))
}

// daysUntil is how many whole days away a release date is, or -1 if it's
// already passed or unparseable.
func daysUntil(released string) int {
	t, ok := parseRelease(released)
	if !ok {
		return -1
	}
	d := time.Until(t)
	if d <= 0 {
		return -1
	}
	return int(d.Hours() / 24)
}

// untilParts splits a day count into two units — a month and a bit reads
// badly as either "in a month" (loses two weeks) or "in 43 days" (makes you
// do the arithmetic). Months are approximated at 30 days, which is close
// enough for an air date and avoids calendar edge cases.
func untilParts(days int) (n1 int, u1 string, n2 int, u2 string) {
	switch {
	case days < 7:
		return days, "day", 0, ""
	case days < 31:
		return days / 7, "week", days % 7, "day"
	case days < 365:
		return days / 30, "month", (days % 30) / 7, "week"
	}
	return days / 365, "year", (days % 365) / 30, "month"
}

func plural(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// untilRelease is the long form, for the info panel.
func untilRelease(released string) string {
	days := daysUntil(released)
	switch {
	case days < 0:
		return ""
	case days < 1:
		return "today"
	case days == 1:
		return "tomorrow"
	}

	n1, u1, n2, u2 := untilParts(days)
	out := "in " + plural(n1, u1)
	if n2 > 0 {
		out += ", " + plural(n2, u2)
	}
	return out
}

// videoAired reports whether an episode's release date has passed. An
// unparseable or missing date counts as aired — plenty of catalogs leave it
// blank, and refusing to play those would be worse than the occasional
// unaired entry slipping through.
func videoAired(v Video) bool {
	t, ok := parseRelease(v.Released)
	if !ok {
		return true
	}
	return !t.After(time.Now())
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

// naturalLess compares strings with embedded numbers the way a person would:
// "S01E02" before "S01E10", "Season 2" before "Season 10". A plain string
// compare puts E10 before E2, which is why a season pack comes out shuffled.
func naturalLess(a, b string) bool {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	i, j := 0, 0

	for i < len(la) && j < len(lb) {
		ca, cb := la[i], lb[j]
		da, db := ca >= '0' && ca <= '9', cb >= '0' && cb <= '9'

		if da && db {
			// Consume both runs of digits and compare them as numbers.
			si, sj := i, j
			for i < len(la) && la[i] >= '0' && la[i] <= '9' {
				i++
			}
			for j < len(lb) && lb[j] >= '0' && lb[j] <= '9' {
				j++
			}
			na := strings.TrimLeft(la[si:i], "0")
			nb := strings.TrimLeft(lb[sj:j], "0")
			if len(na) != len(nb) {
				return len(na) < len(nb) // fewer digits = smaller number
			}
			if na != nb {
				return na < nb
			}
			continue
		}

		if ca != cb {
			return ca < cb
		}
		i++
		j++
	}
	return len(la)-i < len(lb)-j
}

// ── Completion thresholds ─────────────────────────────────────────────────────
//
// A flat percentage treats every runtime the same, which breaks badly at the
// extremes: 70% of a two and a half hour film leaves 45 minutes unwatched, and
// 5% of a ten minute cartoon is half a minute.
//
// The approach the established players take is a percentage bounded by
// absolute limits — Kodi pairs playcountminimumpercent with a flat
// ignoresecondsatstart of 180, Jellyfin pairs MinResumePct with
// MinResumeDurationSeconds. Same idea here.
//
// Deliberately generous at the end: film credits regularly run ten minutes,
// and stopping when they start should count as having watched the thing.

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// watchedAllowance is how much runtime may remain while still counting as
// watched — 14%, bounded to 25s…18min. That puts most things at 86% and a
// feature-length film nearer 88%, since the cap only starts to bite past
// about two hours.
func watchedAllowance(dur float64) float64 {
	return clampF(0.14*dur, 25, 18*60)
}

// resumeFloor is how far in you must be before a resume point is worth
// keeping — 5%, bounded to 15s…3min. Below it, you've barely started.
func resumeFloor(dur float64) float64 {
	return clampF(0.05*dur, 15, 3*60)
}

// prefetchLead is how long before the end to start resolving the next episode.
// Enough time for a debrid addon to answer, without jumping the gun on a film.
func prefetchLead(dur float64) float64 {
	return clampF(0.15*dur, 90, 6*60)
}

// IsWatchedAt reports whether a position counts as having finished the video.
func IsWatchedAt(pos, dur float64) bool {
	if dur <= 0 {
		return false
	}
	return dur-pos <= watchedAllowance(dur)
}

// HasResumePoint reports whether a position is worth resuming from: far enough
// in to be meaningful, not so far in that it's effectively finished.
func HasResumePoint(pos, dur float64) bool {
	if dur <= 0 || pos <= 0 {
		return false
	}
	return pos >= resumeFloor(dur) && !IsWatchedAt(pos, dur)
}

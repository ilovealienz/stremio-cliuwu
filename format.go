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

func fmtRelease(s string) string {
	t, ok := parseRelease(s)
	if !ok {
		return ""
	}
	return t.Format("Mon 02/01/06")
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

	it := Item{
		Label:   bold(hi(fmt.Sprintf("E%02d", v.Episode))),
		Sub:     v.Title,
		Badge:   badge,
		Watched: watched,
	}

	// Catalogs list episodes well before they air. Without a marker they look
	// identical to everything else, and you only find out when the stream
	// search comes back empty.
	if !videoAired(v) {
		it.Badge = stWarn.Render("○ airs " + fmtRelease(v.Released))
		it.Dim = true
	}
	return it
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

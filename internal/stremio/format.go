package stremio

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ParseRelease reads the assorted date formats the metadata addons emit.
func ParseRelease(s string) (time.Time, bool) {
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

// VideoAired reports whether an episode's release date has passed. An
// unparseable or missing date counts as aired — plenty of catalogs leave it
// blank, and refusing to play those would be worse than the occasional
// unaired entry slipping through.
func VideoAired(v Video) bool {
	t, ok := ParseRelease(v.Released)
	if !ok {
		return true
	}
	return !t.After(time.Now())
}

// FmtVideoID turns "tt1234:2:5" or "kitsu:123:7" into "S02E05".
// KitsuIDOf pulls the numeric id out of a kitsu meta id, or returns "".
func KitsuIDOf(id string) string {
	if !strings.HasPrefix(id, "kitsu:") {
		return ""
	}
	return strings.TrimPrefix(id, "kitsu:")
}

// FmtEp labels an episode from its stored numbers, falling back to parsing
// the id.
//
// Parsing is the unreliable path: a kitsu id carries no season at all, so
// FmtVideoID has to assume 1 — which is right for a first cour and wrong for
// every one after it. Anywhere the season and episode are already known, use
// them.
func FmtEp(season, episode int, videoID string) string {
	// season 0 is legitimate — it's where specials live — so the episode
	// number is what decides whether this is an episode at all.
	if episode > 0 && season >= 0 {
		return fmt.Sprintf("S%02dE%02d", season, episode)
	}
	return FmtVideoID(videoID)
}

func FmtVideoID(videoID string) string {
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

// CleanTitle un-escapes and de-extensions an mpv media-title.
func CleanTitle(s string) string {
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

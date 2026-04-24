package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

func LoadHistory() HistoryList {
	b, err := os.ReadFile(histFile())
	if err != nil {
		return HistoryList{}
	}
	var h HistoryList
	json.Unmarshal(b, &h)
	return h
}

func saveHistory(h HistoryList) {
	ensureDir()
	b, _ := json.MarshalIndent(h, "", "  ")
	os.WriteFile(histFile(), b, 0644)
}

func AddHistory(e HistoryEntry, maxEntries int) {
	h := LoadHistory()
	e.WatchedAt = time.Now()
	out := h.Items[:0]
	for _, ex := range h.Items {
		if ex.ID == e.ID && ex.Season == e.Season && ex.Episode == e.Episode {
			// Preserve existing watch state and position — don't reset on re-open
			if e.Position == 0 {
				e.Position = ex.Position
				e.Duration = ex.Duration
			}
			if !e.Watched {
				e.Watched = ex.Watched
			}
			continue
		}
		out = append(out, ex)
	}
	h.Items = append([]HistoryEntry{e}, out...)
	if maxEntries > 0 && len(h.Items) > maxEntries {
		h.Items = h.Items[:maxEntries]
	}
	saveHistory(h)
}

// UpdatePosition saves playback position. Marks watched at >= 70%.
func UpdatePosition(videoID string, pos, duration, percent float64) {
	h := LoadHistory()
	for i, e := range h.Items {
		if e.VideoID == videoID {
			h.Items[i].Position = pos
			h.Items[i].Duration = duration
			if percent >= 70 {
				h.Items[i].Watched = true
			}
			saveHistory(h)
			return
		}
	}
}

// ToggleWatchedByEpisode flips the watched state for a show+season+episode.
func ToggleWatchedByEpisode(showID string, season, episode int) bool {
	h := LoadHistory()
	for i, e := range h.Items {
		if e.ID == showID && e.Season == season && e.Episode == episode {
			h.Items[i].Watched = !h.Items[i].Watched
			saveHistory(h)
			return h.Items[i].Watched
		}
	}
	// Not in history yet — add as watched
	AddHistory(HistoryEntry{
		ID: showID, Season: season, Episode: episode, Watched: true,
	}, 0)
	return true
}

// GetPosition returns saved position for a videoID.
func GetPosition(videoID string) (pos, duration float64, watched bool) {
	for _, e := range LoadHistory().Items {
		if e.VideoID == videoID {
			return e.Position, e.Duration, e.Watched
		}
	}
	return 0, 0, false
}

func ClearHistoryEntry(idx int) {
	h := LoadHistory()
	if idx < 0 || idx >= len(h.Items) {
		return
	}
	h.Items = append(h.Items[:idx], h.Items[idx+1:]...)
	saveHistory(h)
}

func ClearAllHistory() {
	saveHistory(HistoryList{})
}

// WatchedEpisodes returns (season,episode) pairs marked watched for a show.
func WatchedEpisodes(showID string) map[[2]int]bool {
	set := map[[2]int]bool{}
	for _, e := range LoadHistory().Items {
		if e.ID == showID && e.Season > 0 && e.Episode > 0 && e.Watched {
			set[[2]int{e.Season, e.Episode}] = true
		}
	}
	return set
}

// HistoryItem builds a list Item for a history entry.
func HistoryItem(e HistoryEntry) Item {
	yr := e.Year
	if yr == "" {
		yr = "?"
	}
	ep := ""
	if e.Season > 0 && e.Episode > 0 {
		ep = fmt.Sprintf("  S%02dE%02d", e.Season, e.Episode)
		if e.EpTitle != "" {
			ep += "  " + e.EpTitle
		}
	}
	label := bold(e.Name) + grey("  ("+yr+")") + ep

	badge := sourceTag(e.Source)
	if e.Watched {
		badge += "  " + good("✓")
	} else if e.Position > 0 && e.Duration > 0 {
		badge += "  " + yell("▶ "+fmtSecs(e.Position))
	}
	badge += grey("  " + e.WatchedAt.Format("Mon 02/01/06"))

	return Item{Label: label, Badge: badge, Watched: e.Watched}
}

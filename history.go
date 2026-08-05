package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// histMu guards the history file and the in-progress cache.
//
// UpdatePosition runs on its own goroutine roughly once a second while
// something is playing, at the same time as the UI is reading history to
// redraw. Without this they race on both the file and the cache globals.
var histMu sync.Mutex

// inProgressCache caches the last in-progress entry so the main menu
// doesn't hit disk on every render. Invalidated whenever history changes.
var (
	inProgressCache *HistoryEntry
	inProgressValid bool
)

// invalidateInProgress must be called with histMu held.
func invalidateInProgress() { inProgressValid = false }

func loadLocked() HistoryList {
	b, err := os.ReadFile(histFile())
	if err != nil {
		return HistoryList{}
	}
	var h HistoryList
	json.Unmarshal(b, &h)
	return h
}

func saveLocked(h HistoryList) {
	ensureDir()
	b, _ := json.MarshalIndent(h, "", "  ")
	os.WriteFile(histFile(), b, 0644)
}

func LoadHistory() HistoryList {
	histMu.Lock()
	defer histMu.Unlock()
	return loadLocked()
}

// EpState is one episode's recorded state.
type EpState struct {
	Position float64
	Duration float64
	Watched  bool
}

// EpisodeStates returns every recorded episode for a show in a single read.
//
// The episode list used to call one helper for watched state and another for
// position, per row — 25 file reads and JSON parses to draw a 24-episode
// season, once a second while playing.
func EpisodeStates(showID string) map[[2]int]EpState {
	histMu.Lock()
	defer histMu.Unlock()

	out := map[[2]int]EpState{}
	for _, e := range loadLocked().Items {
		if e.ID != showID || e.Season <= 0 || e.Episode <= 0 {
			continue
		}
		out[[2]int{e.Season, e.Episode}] = EpState{
			Position: e.Position, Duration: e.Duration, Watched: e.Watched,
		}
	}
	return out
}

// LastInProgress returns the most recent partially-watched entry.
func LastInProgress() *HistoryEntry {
	histMu.Lock()
	defer histMu.Unlock()

	if inProgressValid {
		return inProgressCache
	}
	inProgressCache = nil
	items := loadLocked().Items
	for i := range items {
		e := items[i]
		if e.Position > 0 && e.Duration > 0 && !e.Watched {
			if pct := e.Position / e.Duration * 100; pct >= 5 && pct <= 90 {
				inProgressCache = &e
				break
			}
		}
	}
	inProgressValid = true
	return inProgressCache
}

func AddHistory(e HistoryEntry, maxEntries int) {
	histMu.Lock()
	defer histMu.Unlock()

	h := loadLocked()
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
	saveLocked(h)
}

// UpdatePosition saves playback position. Marks watched at >= 70%.
func UpdatePosition(videoID string, pos, duration, percent float64) {
	histMu.Lock()
	defer histMu.Unlock()

	h := loadLocked()
	for i, e := range h.Items {
		if e.VideoID == videoID {
			h.Items[i].Position = pos
			h.Items[i].Duration = duration
			if percent >= 70 {
				h.Items[i].Watched = true
			}
			saveLocked(h)
			invalidateInProgress()
			return
		}
	}
}

// SetWatchedByEpisode marks an episode watched or unwatched, creating the
// history entry if there isn't one. Previously this could only toggle episodes
// you'd already played, so there was no way to mark a back catalogue as seen.
func SetWatchedByEpisode(e HistoryEntry, watched bool) {
	histMu.Lock()
	defer histMu.Unlock()

	h := loadLocked()
	for i := range h.Items {
		it := &h.Items[i]
		if it.ID == e.ID && it.Season == e.Season && it.Episode == e.Episode {
			it.Watched = watched
			if watched {
				it.Position = it.Duration
			} else {
				it.Position = 0
			}
			it.WatchedAt = time.Now()
			saveLocked(h)
			invalidateInProgress()
			return
		}
	}
	if !watched {
		return // nothing recorded, nothing to unmark
	}
	e.Watched = true
	e.WatchedAt = time.Now()
	h.Items = append([]HistoryEntry{e}, h.Items...)
	saveLocked(h)
	invalidateInProgress()
}

// ToggleWatchedByEpisode flips state, creating the entry when needed.
func ToggleWatchedByEpisode(e HistoryEntry) bool {
	_, _, watched := GetPositionByEpisode(e.ID, e.Season, e.Episode)
	SetWatchedByEpisode(e, !watched)
	return !watched
}

// SetSeasonWatched applies a state to every episode in one pass, so marking a
// whole season doesn't rewrite the file once per episode.
func SetSeasonWatched(show Meta, season int, eps []Video, watched bool) {
	histMu.Lock()
	defer histMu.Unlock()

	h := loadLocked()

	index := map[int]int{} // episode number -> position in h.Items
	for i, e := range h.Items {
		if e.ID == show.ID && e.Season == season {
			index[e.Episode] = i
		}
	}

	now := time.Now()
	var added []HistoryEntry
	for _, v := range eps {
		if i, ok := index[v.Episode]; ok {
			h.Items[i].Watched = watched
			if watched {
				h.Items[i].Position = h.Items[i].Duration
			} else {
				h.Items[i].Position = 0
			}
			h.Items[i].WatchedAt = now
			continue
		}
		if !watched {
			continue
		}
		added = append(added, HistoryEntry{
			Name: show.Name, ID: show.ID, Type: "series",
			Source: show.Source, Year: show.Year,
			Season: season, Episode: v.Episode,
			VideoID: v.ID, EpTitle: v.Title,
			Watched: true, WatchedAt: now,
		})
	}

	h.Items = append(added, h.Items...)
	saveLocked(h)
	invalidateInProgress()
}

// GetPosition returns saved position for a videoID.
func GetPosition(videoID string) (pos, duration float64, watched bool) {
	histMu.Lock()
	defer histMu.Unlock()

	for _, e := range loadLocked().Items {
		if e.VideoID == videoID {
			return e.Position, e.Duration, e.Watched
		}
	}
	return 0, 0, false
}

// GetPositionByEpisode returns saved position by show ID + season + episode.
// Used when video_id may not be set on older history entries.
func GetPositionByEpisode(showID string, season, episode int) (pos, duration float64, watched bool) {
	histMu.Lock()
	defer histMu.Unlock()

	for _, e := range loadLocked().Items {
		if e.ID == showID && e.Season == season && e.Episode == episode {
			return e.Position, e.Duration, e.Watched
		}
	}
	return 0, 0, false
}

func ClearHistoryEntry(idx int) {
	histMu.Lock()
	defer histMu.Unlock()

	h := loadLocked()
	if idx < 0 || idx >= len(h.Items) {
		return
	}
	h.Items = append(h.Items[:idx], h.Items[idx+1:]...)
	saveLocked(h)
}

func ClearAllHistory() {
	histMu.Lock()
	defer histMu.Unlock()

	saveLocked(HistoryList{})
	invalidateInProgress()
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

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// History is held in memory and flushed on a timer.
//
// The old shape was a flat array of fully-populated entries, rewritten whole
// on every position update — once a second during playback — and re-parsed by
// every reader. Marking a 300-episode season watched wrote 300 records that
// each repeated the show's name, id, type, source and year plus the next-up
// fields, to express one bit per episode, and then evicted the rest of the
// history under the entry cap.
//
// Now a show is stored once and an episode carries only what differs from the
// default: 19 bytes for a watched one. Reads never touch disk, and writes are
// debounced, turning ~1300 KB/s into about 6 KB every fifteen seconds.

const (
	flushDelay      = 15 * time.Second
	defaultRecent   = 300 // fallback when the setting is unreadable
	historyVer      = 2
)

// recentLimit is how many entries the history screen keeps. Watched state is
// unbounded and lives separately — this only caps the list you browse.
func recentLimit() int {
	if ctx != nil && ctx.cfg.HistoryMax > 0 {
		return ctx.cfg.HistoryMax
	}
	return defaultRecent
}

// ── On-disk shape ─────────────────────────────────────────────────────────────

// epState is one episode. Short keys and omitempty throughout: this is the
// part that repeats thousands of times.
type epState struct {
	W bool    `json:"w,omitempty"` // watched
	P float64 `json:"p,omitempty"` // position, seconds
	D float64 `json:"d,omitempty"` // duration, seconds
	T int64   `json:"t,omitempty"` // last touched, unix seconds
}

// showState holds a title's details once, with its episodes beneath.
type showState struct {
	Name   string `json:"name"`
	Type   string `json:"type,omitempty"`
	Source string `json:"source,omitempty"`
	Year   string `json:"year,omitempty"`
	SeenAt int64  `json:"seen_at,omitempty"`

	// Keyed "season:episode". Movies use "0:0".
	Eps map[string]*epState `json:"eps,omitempty"`
}

// recentEntry is one line of the history screen.
//
// Deliberately separate from watched state: bulk-marking a season writes
// hundreds of episode states but nothing here, because marking isn't
// watching, and it used to flush the entire history list off the end.
type recentEntry struct {
	ShowID  string `json:"id"`
	Season  int    `json:"s,omitempty"`
	Episode int    `json:"e,omitempty"`
	VideoID string `json:"v,omitempty"`
	EpTitle string `json:"t,omitempty"`
	Total   int    `json:"n,omitempty"` // episodes in that season
	At      int64  `json:"at"`

	// What follows this episode, recorded at play time.
	//
	// EpQueue has the real video list at that moment, including the next
	// season, so this is exact. Working it out later from Episode+1 can't
	// know a season has five episodes and can't roll over — which is how a
	// finished season came to offer S28E06. An empty NextVideoID means there
	// genuinely is nothing after this one.
	NextVideoID string `json:"nv,omitempty"`
	NextSeason  int    `json:"ns,omitempty"`
	NextEpisode int    `json:"ne,omitempty"`
	NextTitle   string `json:"nt,omitempty"`
}

type historyFile struct {
	Version int                   `json:"version"`
	Shows   map[string]*showState `json:"shows"`
	Recent  []recentEntry         `json:"recent"`
}

// ── In-memory store ───────────────────────────────────────────────────────────

// epRef locates an episode's state without a second lookup.
type epRef struct {
	showID string
	key    string
}

type historyStore struct {
	mu   sync.Mutex
	data historyFile

	byVideo map[string]epRef
	loaded  bool
	dirty   bool
	pending bool // a flush is already scheduled
}

var hist = &historyStore{}

func epKey(season, episode int) string {
	return strconv.Itoa(season) + ":" + strconv.Itoa(episode)
}

func parseEpKey(k string) (season, episode int) {
	if i := strings.IndexByte(k, ':'); i > 0 {
		season, _ = strconv.Atoi(k[:i])
		episode, _ = strconv.Atoi(k[i+1:])
	}
	return
}

// load reads the file once. Callers hold the lock.
func (h *historyStore) load() {
	if h.loaded {
		return
	}
	h.loaded = true
	h.data = historyFile{Version: historyVer, Shows: map[string]*showState{}}
	h.byVideo = map[string]epRef{}

	b, err := os.ReadFile(histFile())
	if err != nil {
		return
	}

	// One file, one format. Nothing here reads the old flat array — if that
	// ever needs moving across, a standalone script can do it once rather
	// than the app carrying a migration path forever.
	var f historyFile
	if json.Unmarshal(b, &f) != nil || f.Version < historyVer || f.Shows == nil {
		return
	}

	h.data = f
	if h.data.Shows == nil {
		h.data.Shows = map[string]*showState{}
	}
	h.reindex()
}

// reindex rebuilds the videoID lookup after loading.
func (h *historyStore) reindex() {
	h.byVideo = map[string]epRef{}
	for _, sh := range h.data.Shows {
		if sh.Eps == nil {
			sh.Eps = map[string]*epState{}
		}
	}
	for _, r := range h.data.Recent {
		if r.VideoID != "" {
			h.byVideo[r.VideoID] = epRef{showID: r.ShowID, key: epKey(r.Season, r.Episode)}
		}
	}
}

func (h *historyStore) show(id string, m Meta) *showState {
	sh, ok := h.data.Shows[id]
	if !ok {
		sh = &showState{Eps: map[string]*epState{}}
		h.data.Shows[id] = sh
	}
	if sh.Eps == nil {
		sh.Eps = map[string]*epState{}
	}
	if m.Name != "" {
		sh.Name = m.Name
	}
	if m.Type != "" {
		sh.Type = m.Type
	}
	if m.Source != "" {
		sh.Source = m.Source
	}
	if m.Year != "" {
		sh.Year = m.Year
	}
	return sh
}

func (h *historyStore) ep(showID, key string) *epState {
	if sh, ok := h.data.Shows[showID]; ok {
		return sh.Eps[key]
	}
	return nil
}

func (h *historyStore) trimRecent() {
	if n := recentLimit(); len(h.data.Recent) > n {
		h.data.Recent = h.data.Recent[:n]
	}
}

// touch marks the store dirty and schedules a flush, if one isn't already due.
func (h *historyStore) touch() {
	h.dirty = true
	invalidateInProgress()

	if h.pending {
		return
	}
	h.pending = true
	go func() {
		time.Sleep(flushDelay)
		hist.Flush()
	}()
}

// Flush writes the store out if anything changed.
func (h *historyStore) Flush() {
	h.mu.Lock()
	h.pending = false
	if !h.dirty || !h.loaded {
		h.mu.Unlock()
		return
	}
	h.dirty = false
	h.data.Version = historyVer
	b, err := json.Marshal(h.data)
	h.mu.Unlock()

	if err == nil {
		writeAtomic(histFile(), b)
	}
}

// WarmHistory parses the file off the critical path. Without it the first
// read happens while the first frame is being drawn, which is the one moment
// it's visible.
func WarmHistory() {
	go func() {
		hist.mu.Lock()
		defer hist.mu.Unlock()
		hist.load()
	}()
}

// FlushHistory is called on the way out so the last few seconds aren't lost.
func FlushHistory() { hist.Flush() }

// writeAtomic writes to a temporary file and renames over the target.
//
// os.WriteFile truncates in place, so an interrupted write leaves a truncated
// file rather than the previous one — losing history outright instead of a
// few seconds of it.
func writeAtomic(path string, b []byte) error {
	ensureDir()

	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()

	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}

	os.Chmod(name, 0644)
	return os.Rename(name, path)
}

// ── In-progress cache ─────────────────────────────────────────────────────────

var (
	inProgressCache *ContinueItem
	inProgressValid bool
)

func invalidateInProgress() { inProgressValid = false }

// ── Episode state ─────────────────────────────────────────────────────────────

// EpState is one episode's recorded state.
type EpState struct {
	Position float64
	Duration float64
	Watched  bool
}

// EpisodeStates returns every recorded episode for a show — a map lookup now,
// where it used to be a file read and a linear scan.
func EpisodeStates(showID string) map[[2]int]EpState {
	hist.mu.Lock()
	defer hist.mu.Unlock()
	hist.load()

	out := map[[2]int]EpState{}
	sh, ok := hist.data.Shows[showID]
	if !ok {
		return out
	}
	for k, e := range sh.Eps {
		season, episode := parseEpKey(k)
		if season <= 0 || episode <= 0 {
			continue
		}
		out[[2]int{season, episode}] = EpState{Position: e.P, Duration: e.D, Watched: e.W}
	}
	return out
}

func GetPosition(videoID string) (pos, duration float64, watched bool) {
	hist.mu.Lock()
	defer hist.mu.Unlock()
	hist.load()

	if ref, ok := hist.byVideo[videoID]; ok {
		if e := hist.ep(ref.showID, ref.key); e != nil {
			return e.P, e.D, e.W
		}
	}
	return 0, 0, false
}

func GetPositionByEpisode(showID string, season, episode int) (pos, duration float64, watched bool) {
	hist.mu.Lock()
	defer hist.mu.Unlock()
	hist.load()

	if e := hist.ep(showID, epKey(season, episode)); e != nil {
		return e.P, e.D, e.W
	}
	return 0, 0, false
}

// ── Mutations ─────────────────────────────────────────────────────────────────

// AddHistory records that something was opened. maxEntries is kept in the
// signature for callers; the recent list has its own bound.
func AddHistory(e HistoryEntry, maxEntries int) {
	hist.mu.Lock()
	defer hist.mu.Unlock()
	hist.load()

	sh := hist.show(e.ID, Meta{Name: e.Name, Type: e.Type, Source: e.Source, Year: e.Year})
	now := time.Now().Unix()
	sh.SeenAt = now

	key := epKey(e.Season, e.Episode)
	if sh.Eps[key] == nil {
		sh.Eps[key] = &epState{}
	}
	sh.Eps[key].T = now

	if e.VideoID != "" {
		hist.byVideo[e.VideoID] = epRef{showID: e.ID, key: key}
	}

	hist.pushRecent(e, now)
	hist.touch()
}

// pushRecent moves an entry to the front of the recent list, dropping any
// earlier row for the same episode. Callers hold the lock.
func (h *historyStore) pushRecent(e HistoryEntry, now int64) {
	out := h.data.Recent[:0]
	for _, r := range h.data.Recent {
		if r.ShowID == e.ID && r.Season == e.Season && r.Episode == e.Episode {
			continue
		}
		out = append(out, r)
	}

	h.data.Recent = append([]recentEntry{{
		ShowID: e.ID, Season: e.Season, Episode: e.Episode,
		VideoID: e.VideoID, EpTitle: e.EpTitle, Total: e.EpisodeTotal, At: now,
		NextVideoID: e.NextVideoID, NextSeason: e.NextSeason,
		NextEpisode: e.NextEpisode, NextTitle: e.NextTitle,
	}}, out...)

	h.trimRecent()
}

// UpdatePosition records playback progress. Called about once a second, so it
// only mutates memory — the write is on a timer.
func UpdatePosition(videoID string, pos, duration float64) {
	if videoID == "" || duration <= 0 {
		return
	}

	hist.mu.Lock()
	defer hist.mu.Unlock()
	hist.load()

	ref, ok := hist.byVideo[videoID]
	if !ok {
		return
	}
	e := hist.ep(ref.showID, ref.key)
	if e == nil {
		return
	}

	e.P, e.D, e.T = pos, duration, time.Now().Unix()
	if IsWatchedAt(pos, duration) {
		e.W = true
	}
	hist.touch()
}

// SetWatchedByEpisode marks an episode watched or unwatched, creating the
// record if there isn't one.
func SetWatchedByEpisode(e HistoryEntry, watched bool) {
	hist.mu.Lock()
	defer hist.mu.Unlock()
	hist.load()

	sh := hist.show(e.ID, Meta{Name: e.Name, Type: e.Type, Source: e.Source, Year: e.Year})
	key := epKey(e.Season, e.Episode)

	st := sh.Eps[key]
	if st == nil {
		if !watched {
			return // nothing recorded, nothing to unmark
		}
		st = &epState{}
		sh.Eps[key] = st
	}

	now := time.Now().Unix()
	st.W = watched
	st.T = now
	if watched {
		st.P = st.D
	} else {
		st.P = 0
	}
	if e.VideoID != "" {
		hist.byVideo[e.VideoID] = epRef{showID: e.ID, key: key}
	}

	// Marking one episode is a deliberate act about that episode, so it
	// belongs in history the same as watching it would. Bulk marking is the
	// case that mustn't flood the list — see SetSeasonWatched.
	if watched {
		sh.SeenAt = now
		hist.pushRecent(e, now)
	}
	hist.touch()
}

func ToggleWatchedByEpisode(e HistoryEntry) bool {
	_, _, watched := GetPositionByEpisode(e.ID, e.Season, e.Episode)
	SetWatchedByEpisode(e, !watched)
	return !watched
}

// SetSeasonWatched applies a state to a whole season in one pass.
//
// This is what the old format handled worst: 300 episodes meant 300 full
// records and 300 lines of history. Now it's 300 × 19 bytes of state and no
// recent entries at all.
func SetSeasonWatched(show Meta, season int, eps []Video, watched bool) {
	hist.mu.Lock()
	defer hist.mu.Unlock()
	hist.load()

	sh := hist.show(show.ID, show)
	now := time.Now().Unix()

	for _, v := range eps {
		key := epKey(season, v.Episode)
		st := sh.Eps[key]
		if st == nil {
			if !watched {
				continue
			}
			st = &epState{}
			sh.Eps[key] = st
		}

		st.W = watched
		st.T = now
		if watched {
			st.P = st.D
		} else {
			st.P = 0
		}
		if v.ID != "" {
			hist.byVideo[v.ID] = epRef{showID: show.ID, key: key}
		}
	}

	// One row for the season, not one per episode: marking twelve episodes
	// shouldn't push eleven other shows off the history screen. The last
	// episode stands for the lot, and carries no next-episode pointer —
	// nothing here knows what follows the season.
	if watched && len(eps) > 0 {
		last := eps[len(eps)-1]
		for _, v := range eps {
			if v.Episode > last.Episode {
				last = v
			}
		}
		sh.SeenAt = now
		hist.pushRecent(HistoryEntry{
			Name: show.Name, ID: show.ID, Type: show.Type,
			Source: show.Source, Year: show.Year,
			Season: season, Episode: last.Episode, VideoID: last.ID,
			EpTitle: last.Title, EpisodeTotal: len(eps),
		}, now)
	}
	hist.touch()
}

// ── History screen ────────────────────────────────────────────────────────────

// LoadHistory renders the recent list as the flat entries the UI expects.
func LoadHistory() HistoryList {
	hist.mu.Lock()
	defer hist.mu.Unlock()
	hist.load()
	return hist.recentEntries()
}

// recentEntries is the unlocked form, so callers already holding the lock
// don't have to drop it and retake it — a pattern that works until someone
// edits it and then deadlocks.
func (h *historyStore) recentEntries() HistoryList {
	out := HistoryList{Items: make([]HistoryEntry, 0, len(h.data.Recent))}
	for _, r := range h.data.Recent {
		sh := h.data.Shows[r.ShowID]
		if sh == nil {
			continue
		}
		e := HistoryEntry{
			Name: sh.Name, ID: r.ShowID, Type: sh.Type, Source: sh.Source, Year: sh.Year,
			Season: r.Season, Episode: r.Episode, VideoID: r.VideoID, EpTitle: r.EpTitle,
			EpisodeTotal: r.Total, WatchedAt: time.Unix(r.At, 0),
			NextVideoID:  r.NextVideoID, NextSeason: r.NextSeason,
			NextEpisode:  r.NextEpisode, NextTitle: r.NextTitle,
		}
		if st := sh.Eps[epKey(r.Season, r.Episode)]; st != nil {
			e.Position, e.Duration, e.Watched = st.P, st.D, st.W
		}
		out.Items = append(out.Items, e)
	}
	return out
}

func ClearHistoryEntry(idx int) {
	hist.mu.Lock()
	defer hist.mu.Unlock()
	hist.load()

	if idx < 0 || idx >= len(hist.data.Recent) {
		return
	}
	hist.data.Recent = append(hist.data.Recent[:idx], hist.data.Recent[idx+1:]...)
	hist.touch()
}

// ClearAllHistory drops everything, watched state included.
func ClearAllHistory() {
	hist.mu.Lock()
	defer hist.mu.Unlock()
	hist.load()

	hist.data = historyFile{Version: historyVer, Shows: map[string]*showState{}}
	hist.byVideo = map[string]epRef{}
	hist.touch()
}

// ── Continue watching ─────────────────────────────────────────────────────────

// ContinueItem is what the main menu offers you next.
type ContinueItem struct {
	Entry    HistoryEntry
	Position float64
	Duration float64

	NextUp    bool
	LastLabel string
	Index     int
	Total     int
}

func inProgress(e HistoryEntry) bool {
	if e.Watched {
		return false
	}
	return HasResumePoint(e.Position, e.Duration)
}

// ContinueTarget decides what "continue watching" should offer: resume the
// most recent thing if it isn't finished, otherwise the episode after it,
// otherwise anything else left part-watched.
func ContinueTarget() *ContinueItem {
	hist.mu.Lock()
	defer hist.mu.Unlock()
	hist.load()

	if inProgressValid {
		return inProgressCache
	}
	items := hist.recentEntries().Items

	inProgressCache = nil
	inProgressValid = true
	if len(items) == 0 {
		return nil
	}

	resume := func(e HistoryEntry) *ContinueItem {
		return &ContinueItem{
			Entry: e, Position: e.Position, Duration: e.Duration,
			Index: e.Episode, Total: e.EpisodeTotal,
		}
	}

	top := items[0]
	if !top.Watched {
		inProgressCache = resume(top)
		return inProgressCache
	}

	// The next episode is derived from what's recorded for the show, rather
	// than stored on the entry: keeping next-up fields per episode meant
	// rewriting them for every episode of a bulk-marked season.
	// The next episode is whatever was recorded when this one played, not
	// Episode+1. An empty pointer means the queue had nothing after it — end
	// of the series, or a season we've no list for — so there's nothing to
	// offer rather than an episode that doesn't exist.
	if top.Type == "series" && top.NextVideoID != "" {
		if sh := hist.data.Shows[top.ID]; sh != nil {
			nextKey := epKey(top.NextSeason, top.NextEpisode)
			st := sh.Eps[nextKey]

			if st == nil || !st.W {
				e := HistoryEntry{
					Name: sh.Name, ID: top.ID, Type: sh.Type,
					Source: sh.Source, Year: sh.Year,
					Season: top.NextSeason, Episode: top.NextEpisode,
					VideoID: top.NextVideoID, EpTitle: top.NextTitle,
					EpisodeTotal: top.EpisodeTotal,
				}
				item := &ContinueItem{
					Entry:     e,
					NextUp:    true,
					LastLabel: fmtVideoID(top.VideoID),
					Index:     top.NextEpisode,
					Total:     top.EpisodeTotal,
				}
				// Already started it — resume rather than announce it.
				if st != nil && st.P > 0 {
					item.NextUp = false
					item.Position, item.Duration = st.P, st.D
					item.Entry.Position, item.Entry.Duration = st.P, st.D
				}
				inProgressCache = item
				return inProgressCache
			}
		}
	}

	for i := range items {
		if inProgress(items[i]) {
			inProgressCache = resume(items[i])
			return inProgressCache
		}
	}
	return nil
}

// ContinueList returns the shows you're partway through, most recent first,
// one entry per show.
//
// ContinueTarget answers "what single thing next"; this answers "what am I in
// the middle of", which is a different question — you're usually three or four
// shows deep and the single most recent one isn't always the one you want.
func ContinueList(n int) []ContinueItem {
	hist.mu.Lock()
	defer hist.mu.Unlock()
	hist.load()

	items := hist.recentEntries().Items
	seen := map[string]bool{}
	out := make([]ContinueItem, 0, n)

	for _, e := range items {
		if len(out) >= n {
			break
		}
		if e.ID == "" || seen[e.ID] {
			continue // one row per show, at whatever the latest point is
		}

		sh := hist.data.Shows[e.ID]
		if sh == nil {
			continue
		}

		switch {
		case !e.Watched:
			seen[e.ID] = true
			out = append(out, ContinueItem{
				Entry: e, Position: e.Position, Duration: e.Duration,
				Index: e.Episode, Total: e.EpisodeTotal,
			})

		case e.Type == "series" && e.NextVideoID != "":
			// Same stored pointer the menu uses. A finished series records no
			// next episode, so it simply drops out of the list.
			seen[e.ID] = true

			next := sh.Eps[epKey(e.NextSeason, e.NextEpisode)]
			if next != nil && next.W {
				continue // already seen it too
			}

			item := ContinueItem{
				Entry: HistoryEntry{
					Name: sh.Name, ID: e.ID, Type: sh.Type,
					Source: sh.Source, Year: sh.Year,
					Season: e.NextSeason, Episode: e.NextEpisode,
					VideoID: e.NextVideoID, EpTitle: e.NextTitle,
					EpisodeTotal: e.EpisodeTotal,
				},
				NextUp:    true,
				LastLabel: fmtVideoID(e.VideoID),
				Index:     e.NextEpisode,
				Total:     e.EpisodeTotal,
			}
			if next != nil && next.P > 0 {
				item.NextUp = false
				item.Position, item.Duration = next.P, next.D
				item.Entry.Position, item.Entry.Duration = next.P, next.D
			}
			out = append(out, item)

		default:
			seen[e.ID] = true // finished film, nothing to continue
		}
	}
	return out
}

// RecentlyWatched returns the last few things you finished, newest first.
func RecentlyWatched(n int) []HistoryEntry {
	hist.mu.Lock()
	defer hist.mu.Unlock()
	hist.load()

	out := make([]HistoryEntry, 0, n)
	for _, e := range hist.recentEntries().Items {
		if !e.Watched {
			continue
		}
		out = append(out, e)
		if len(out) >= n {
			break
		}
	}
	return out
}

// Stats summarises everything watched. Walks the in-memory store, so it costs
// nothing to recompute.
type Stats struct {
	Episodes int
	Films    int
	Shows    int
	Hours    float64
}

func HistoryStats() Stats {
	hist.mu.Lock()
	defer hist.mu.Unlock()
	hist.load()

	var st Stats
	for _, sh := range hist.data.Shows {
		counted := false
		for k, e := range sh.Eps {
			if !e.W {
				continue
			}
			season, _ := parseEpKey(k)
			if season > 0 {
				st.Episodes++
				counted = true
			} else {
				st.Films++
			}
			// Duration is only recorded for things actually played, so this
			// undercounts anything marked watched by hand rather than viewed.
			st.Hours += e.D / 3600
		}
		if counted {
			st.Shows++
		}
	}
	return st
}


// ── Rendering ─────────────────────────────────────────────────────────────────


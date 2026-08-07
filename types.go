package main

import (
	"sort"
	"strings"
	"time"
)

// ── List rendering ────────────────────────────────────────────────────────────

// Item is a single row in any list screen.
type Item struct {
	Label   string // primary text
	Sub     string // dimmed secondary text
	Badge   string // right-aligned tag e.g. "[movie]" "S02 · 4/12"
	Watched bool   // prefix with green ✓
	Dim     bool   // grey out entire row
	Header  bool   // section label or blank spacer — not selectable
}

func (i Item) selectable() bool { return !i.Header }

// ── Addons ────────────────────────────────────────────────────────────────────

// AddonRef is what we persist: just the manifest URL and whether it's on.
type AddonRef struct {
	URL      string `json:"url"`
	Disabled bool   `json:"disabled"`
}

type AddonList struct {
	Items []AddonRef `json:"items"`
}

// CatalogExtra is an `extra` entry on a catalog: "search", "skip", "genre".
type CatalogExtra struct {
	Name       string   `json:"name"`
	IsRequired bool     `json:"isRequired"`
	Options    []string `json:"options"`
}

// AddonCatalog represents a catalog entry in an addon manifest.
type AddonCatalog struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`

	// Modern manifests use `extra`; older ones use these two flat lists.
	Extra          []CatalogExtra `json:"extra"`
	ExtraSupported []string       `json:"extraSupported"`
	ExtraRequired  []string       `json:"extraRequired"`
}

func (c AddonCatalog) Supports(extra string) bool {
	for _, e := range c.Extra {
		if e.Name == extra {
			return true
		}
	}
	for _, e := range c.ExtraSupported {
		if e == extra {
			return true
		}
	}
	return false
}

func (c AddonCatalog) Requires(extra string) bool {
	for _, e := range c.Extra {
		if e.Name == extra && e.IsRequired {
			return true
		}
	}
	for _, e := range c.ExtraRequired {
		if e == extra {
			return true
		}
	}
	return false
}

// Browsable reports whether the catalog can be listed. Genre may be required
// — several addons only serve a catalog once you've picked one — so those are
// browsable as long as we ask for a genre first.
func (c AddonCatalog) Browsable() bool {
	for _, e := range c.Extra {
		if e.IsRequired && e.Name != "skip" && e.Name != "genre" {
			return false
		}
	}
	for _, e := range c.ExtraRequired {
		if e != "skip" && e != "genre" {
			return false
		}
	}
	return true
}

// Genres returns the options declared for the genre extra, if any.
func (c AddonCatalog) Genres() []string {
	for _, e := range c.Extra {
		if e.Name == "genre" {
			return e.Options
		}
	}
	return nil
}

// CatalogRef is a catalog bound to the addon that serves it.
type CatalogRef struct {
	AddonName string
	Base      string
	Type      string // manifest type: movie, series, anime, other…
	ID        string
	Name      string
	Search    bool
	Skip      bool

	Genres     []string // options for the genre extra, if declared
	NeedsGenre bool     // the addon won't serve this catalog without one
}

// Kind buckets a catalog under one of the top-level menu entries.
func (c CatalogRef) Kind() string {
	switch c.Type {
	case "movie":
		return "movie"
	case "series":
		return "show"
	case "anime":
		return "anime"
	}
	return "other"
}

func (c CatalogRef) Key() string { return c.Base + "|" + c.Type + "|" + c.ID }

type Manifest struct {
	ID          string         `json:"id"`
	Version     string         `json:"version"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Types       []string       `json:"types"`
	Resources   []any          `json:"resources"`
	Catalogs    []AddonCatalog `json:"catalogs"`
}

// Addon is a fetched, live addon: its manifest URL plus the parsed manifest.
type Addon struct {
	TransportURL string   `json:"transportUrl"`
	Manifest     Manifest `json:"manifest"`
	Err          error    `json:"-"` // set if the manifest failed to load
}

// streamResource holds the parsed fields of a resource object declaring "stream".
type streamResource struct {
	types      []string
	idPrefixes []string
}

func (a Addon) parseResources(name string) []streamResource {
	var out []streamResource
	for _, r := range a.Manifest.Resources {
		switch v := r.(type) {
		case string:
			if v == name {
				out = append(out, streamResource{}) // no restrictions
			}
		case map[string]any:
			if v["name"] != name {
				continue
			}
			sr := streamResource{}
			if ts, ok := v["types"].([]any); ok {
				for _, t := range ts {
					if s, ok := t.(string); ok {
						sr.types = append(sr.types, s)
					}
				}
			}
			if ps, ok := v["idPrefixes"].([]any); ok {
				for _, p := range ps {
					if s, ok := p.(string); ok {
						sr.idPrefixes = append(sr.idPrefixes, s)
					}
				}
			}
			out = append(out, sr)
		}
	}
	return out
}

// HasStreams reports whether the addon declares any stream resource.
func (a Addon) HasStreams() bool { return len(a.parseResources("stream")) > 0 }

// SupportsStream reports whether this addon's stream resources cover the given
// mediaType (e.g. "movie", "series") and videoID (checked against idPrefixes).
func (a Addon) SupportsStream(mediaType, videoID string) bool {
	return a.SupportsResource("stream", mediaType, videoID)
}

// SupportsResource is the general form: does this addon serve `name` for the
// given type and id? Used to find whichever addon can answer a meta request,
// rather than guessing between Cinemeta and Kitsu by source.
func (a Addon) SupportsResource(name, mediaType, id string) bool {
	for _, sr := range a.parseResources(name) {
		typeOK := len(sr.types) == 0
		for _, t := range sr.types {
			if t == mediaType {
				typeOK = true
				break
			}
		}
		if !typeOK {
			continue
		}
		prefixOK := len(sr.idPrefixes) == 0
		for _, p := range sr.idPrefixes {
			if strings.HasPrefix(id, p) {
				prefixOK = true
				break
			}
		}
		if prefixOK {
			return true
		}
	}
	return false
}

// MetaDetail is the full meta object. Catalog rows only carry enough to draw
// a list; this is what the /meta/ endpoint actually returns, and it's fetched
// lazily for whichever row you're looking at.
type MetaDetail struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`

	ReleaseInfo string `json:"releaseInfo"`
	Released    string `json:"released"`
	Runtime     string `json:"runtime"`
	ImdbRating  string `json:"imdbRating"`
	Country     string `json:"country"`
	Awards      string `json:"awards"`
	Status      string `json:"status"`
	Poster      string `json:"poster"`

	// Addons disagree on singular vs plural here, so accept both.
	Genres   []string `json:"genres"`
	Genre    []string `json:"genre"`
	Cast     []string `json:"cast"`
	Director []string `json:"director"`
	Writer   []string `json:"writer"`
}

func (m MetaDetail) AllGenres() []string {
	if len(m.Genres) > 0 {
		return m.Genres
	}
	return m.Genre
}

// ── Catalog / meta types ──────────────────────────────────────────────────────

type Meta struct {
	ID string `json:"id"`
	// Poster/Description deliberately omitted — nothing renders them.
	Type        string `json:"type"`
	Name        string `json:"name"`
	Year        string `json:"year"`
	ReleaseInfo string `json:"releaseInfo"`
	Released    string `json:"released"`

	Source string // "movie" | "show" | "anime" — injected
	Base   string // addon base that produced this, used as a meta lookup hint
}

// normalize fills Year from releaseInfo. Cinemeta and Kitsu both return
// releaseInfo on catalog rows and `year` only on the full meta object, which
// is why the old code fired an HTTP request per row just to show "(2019)".
func (m *Meta) normalize(source, base string) {
	m.Source = source
	m.Base = base
	if m.Type == "" {
		if source == "movie" {
			m.Type = "movie"
		} else {
			m.Type = "series"
		}
	}
	if m.Year == "" && m.ReleaseInfo != "" {
		y := m.ReleaseInfo
		for _, sep := range []string{"–", "-", "—"} {
			if i := strings.Index(y, sep); i > 0 {
				y = y[:i]
				break
			}
		}
		m.Year = strings.TrimSpace(y)
	}
}

type Video struct {
	ID       string `json:"id"`
	Season   int    `json:"season"`
	Episode  int    `json:"episode"`
	Title    string `json:"title"`
	Released string `json:"released"`
	Overview string `json:"overview"`
}

type SeriesMeta struct {
	Videos []Video `json:"videos"`
}

type Stream struct {
	URL         string `json:"url"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Addon       string // injected
}

// ── Episode queue ─────────────────────────────────────────────────────────────

// EpQueue is the season context handed to the player so it can advance to the
// next episode itself. With loadfile/replace there is no mpv playlist to lean
// on, so autoplay lives here instead.
type EpQueue struct {
	Show     Meta
	Season   int
	Episodes []Video // this season only
	Index    int

	// All is every episode of the series. Carrying it means the end of a
	// season isn't the end of the queue — without it, finishing a finale
	// looked identical to finishing the show.
	All []Video
}

func (q *EpQueue) HasPrev() bool { return q != nil && q.Index > 0 }

func (q *EpQueue) HasNext() bool {
	_, _, ok := q.Next()
	return ok
}

// Next returns the following episode and its season, rolling over into the
// next season when the current one runs out.
func (q *EpQueue) Next() (Video, int, bool) {
	if q == nil {
		return Video{}, 0, false
	}
	if q.Index+1 < len(q.Episodes) {
		v := q.Episodes[q.Index+1]
		if !videoAired(v) {
			return Video{}, 0, false // not out yet — nothing to queue
		}
		return v, q.Season, true
	}

	// Lowest season above this one…
	next := -1
	for _, v := range q.All {
		if v.Season > q.Season && (next == -1 || v.Season < next) {
			next = v.Season
		}
	}
	if next == -1 {
		return Video{}, 0, false
	}

	// …and its earliest episode.
	var first Video
	found := false
	for _, v := range q.All {
		if v.Season == next && (!found || v.Episode < first.Episode) {
			first, found = v, true
		}
	}
	if found && !videoAired(first) {
		return Video{}, 0, false
	}
	return first, next, found
}

// SeasonEpisodes pulls one season's episodes out of All, in order.
func (q *EpQueue) SeasonEpisodes(season int) []Video {
	var eps []Video
	for _, v := range q.All {
		if v.Season == season {
			eps = append(eps, v)
		}
	}
	sort.Slice(eps, func(a, b int) bool { return eps[a].Episode < eps[b].Episode })
	return eps
}

// ── Config ────────────────────────────────────────────────────────────────────

// configVersion is bumped whenever a new field needs a non-zero default.
// Without this, adding a bool to the struct silently gives every existing
// install `false`, because encoding/json just leaves absent fields alone.
const configVersion = 6

type AppConfig struct {
	Version          int    `json:"version"`
	MpvPath          string `json:"mpv_path"`
	PreferredQuality string `json:"preferred_quality"`
	SubtitleLang     string `json:"subtitle_lang"`
	HistoryMax       int    `json:"history_max"`
	OmdbKey          string `json:"omdb_key"`
	AutoNext         bool   `json:"auto_next"`
	AutoResume       bool   `json:"auto_resume"`
	CloseMpvOnExit   bool   `json:"close_mpv_on_exit"`
	CachedFirst      bool   `json:"cached_first"`
	Accent           string `json:"accent"`
	AutoInfo         bool   `json:"auto_info"`
	Posters          bool   `json:"posters"`
}

// SetDefaults fills in anything missing. Returns true if it changed something,
// so the caller can persist the upgrade once rather than on every load.
func (c *AppConfig) SetDefaults() bool {
	changed := false

	if c.Version < 1 {
		// Pre-versioned config (or a fresh one): opt into the behaviour that
		// used to be implicit.
		c.AutoNext = true
		c.AutoResume = true
		c.CloseMpvOnExit = true
		c.Version = 1
		changed = true
	}

	if c.Version < 2 {
		c.CachedFirst = true
		changed = true
	}

	if c.Version < 3 {
		c.Accent = "pink"
		changed = true
	}

	if c.Version < 4 {
		c.AutoInfo = true
		changed = true
	}

	if c.Version < 5 {
		c.Posters = true
		changed = true
	}

	if c.Version < 6 {
		c.Posters = false // opt-in: half-block art is rough at this size
		changed = true
	}

	if c.HistoryMax <= 0 {
		c.HistoryMax = 100
		changed = true
	}
	if c.OmdbKey == "" {
		c.OmdbKey = "trilogy"
		changed = true
	}

	c.Version = configVersion
	return changed
}

// ── Favourites ────────────────────────────────────────────────────────────────

type Favourite struct {
	Name   string `json:"name"`
	ID     string `json:"id"`
	Type   string `json:"type"`
	Source string `json:"source"`
	Year   string `json:"year"`
	Season int    `json:"season"` // 0 = whole show
	Added  string `json:"added"`
}

type FavouriteList struct {
	Items []Favourite `json:"items"`
}

// ── History ───────────────────────────────────────────────────────────────────

type HistoryEntry struct {
	Name      string    `json:"name"`
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Source    string    `json:"source"`
	Year      string    `json:"year"`
	Season    int       `json:"season,omitempty"`
	Episode   int       `json:"episode,omitempty"`
	VideoID   string    `json:"video_id,omitempty"`
	EpTitle   string    `json:"ep_title,omitempty"`
	Position  float64   `json:"position,omitempty"`
	Duration  float64   `json:"duration,omitempty"`
	Watched   bool      `json:"watched"`
	WatchedAt time.Time `json:"watched_at"`

	// Recorded when playback starts so the menu can offer the next episode
	// without a network round trip. Working it out at render time would mean
	// fetching the season's episode list every time the menu draws.
	EpisodeTotal int    `json:"episode_total,omitempty"`
	NextVideoID  string `json:"next_video_id,omitempty"`
	NextSeason   int    `json:"next_season,omitempty"`
	NextEpisode  int    `json:"next_episode,omitempty"`
	NextTitle    string `json:"next_title,omitempty"`
}

type HistoryList struct {
	Items []HistoryEntry `json:"items"`
}

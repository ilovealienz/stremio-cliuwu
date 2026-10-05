package main

import (
	"fmt"
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

	BehaviorHints struct {
		// The addon says it can't work until you've been through its
		// configure page. Worth showing: an unconfigured addon answers
		// every request with nothing, which looks like a broken addon.
		ConfigurationRequired bool `json:"configurationRequired"`

		// Serves peer-to-peer sources directly, so playing one exposes your
		// address to the swarm. Not a problem through a debrid service, very
		// much one without.
		P2P bool `json:"p2p"`
	} `json:"behaviorHints"`
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

	// Sent by cinemeta on films and series alike. Usually the same as ID,
	// but an addon with its own id scheme carries the mapping here — which
	// is the only way to reach imdb for a kitsu-backed title.
	ImdbID string `json:"imdb_id"`

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

	// Addons disagree on these. Kitsu sends title/overview; Cinemeta and the
	// imdb-keyed metas send name/description, leaving the other pair null —
	// which is why regrouped anime and Cinemeta specials came out as bare
	// episode numbers. Both are read, then folded into Title and Overview.
	Name        string `json:"name"`
	Description string `json:"description"`

	// Set when the series was fetched by its imdb id: the anime addon
	// answers with real seasons but keeps a kitsu reference on every episode.
	// Episode still. Shown only on a kitty terminal: at forty cells a 16:9
	// frame is about eleven half-block rows, which is noise rather than a
	// picture.
	Thumbnail string `json:"thumbnail"`

	KitsuID      string `json:"kitsu_id"`
	KitsuEpisode int    `json:"kitsuEpisode"`
}

// fill copies whichever spelling the addon used into the canonical fields.
func (v Video) fill() Video {
	if v.Title == "" {
		v.Title = v.Name
	}
	if v.Overview == "" {
		v.Overview = v.Description
	}
	return v
}

// StreamID is what stream addons should be asked for.
//
// Deliberately not always v.ID. Browsing an anime by its imdb id gives proper
// seasons, but torrentio matches anime far better on kitsu ids — that's why
// the kitsu addon exists. Since every episode carries both, we can browse one
// way and request the other, and give up nothing.
func (v Video) StreamID() string {
	if v.KitsuID != "" && v.KitsuEpisode > 0 {
		return fmt.Sprintf("kitsu:%s:%d", v.KitsuID, v.KitsuEpisode)
	}
	return v.ID
}

type SeriesMeta struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Poster string  `json:"poster"`
	Year   string  `json:"releaseInfo"`
	ImdbID string  `json:"imdb_id"`

	// The show's typical episode length. Cinemeta carries no per-episode
	// duration, so this is the only figure available.
	Runtime string `json:"runtime"`
	Videos []Video `json:"videos"`
}

// SeasonOf finds which season a kitsu entry became once the series was
// regrouped, and whether it's there at all.
//
// The second return matters: an OVA or a specials collection carries the same
// imdb id as the show it belongs to, but isn't among its episodes. Regrouping
// those would swap their content for the main series and leave nothing to
// play — so a miss means don't regroup. Season 0 is a real answer, which is
// why this can't just report zero for absent.
func (sm SeriesMeta) SeasonOf(kitsuID string) (int, bool) {
	if kitsuID == "" {
		return 0, false
	}
	for _, v := range sm.Videos {
		if v.KitsuID == kitsuID {
			return v.Season, true
		}
	}
	return 0, false
}

type Stream struct {
	URL         string `json:"url"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`

	// Subtitles the addon ships with the stream itself, distinct from what
	// the subtitle addons return for the title.
	Subtitles []Subtitle `json:"subtitles"`

	// Some addons say outright whether a result is cached rather than
	// leaving it to be read out of the display text. A pointer so "absent"
	// stays distinct from "explicitly false".
	Cached *bool `json:"cached"`

	// Documented in the addon spec, and better than anything we can parse
	// out of the display text — the addon already knows these exactly.
	//
	// VideoHash is the OpenSubtitles hash and, with VideoSize, is what the
	// spec says to pass to subtitle addons so they can match the actual
	// file rather than just the title. Without it, subtitles are matched on
	// the video id alone and you get every release's timing.
	BehaviorHints struct {
		Filename  string `json:"filename"`
		VideoSize int64  `json:"videoSize"`
		VideoHash string `json:"videoHash"`
	} `json:"behaviorHints"`

	Addon string // injected
	Rank  int    // injected: the addon's position in your list
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
// Next is the episode to play after this one, or nothing when it isn't out
// yet — autoplay and prefetch both depend on that.
func (q *EpQueue) Next() (Video, int, bool) {
	v, season, ok := q.Upcoming()
	if !ok || !videoAired(v) {
		return Video{}, 0, false
	}
	return v, season, true
}

// Upcoming is the episode after this one whether or not it has aired.
//
// Split from Next because the two questions differ: you can't play an episode
// that isn't out, but you still want to be told it's coming. Folding them
// together made a season read as finished the moment you caught up with it.
func (q *EpQueue) Upcoming() (Video, int, bool) {
	if q == nil {
		return Video{}, 0, false
	}
	if q.Index+1 < len(q.Episodes) {
		return q.Episodes[q.Index+1], q.Season, true
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
const configVersion = 13

type AppConfig struct {
	Version          int    `json:"version"`
	MpvPath          string `json:"mpv_path"`
	PreferredQuality string `json:"preferred_quality"`
	SubtitleLang     string `json:"subtitle_lang"`

	// Terms that hide a stream from the picker. Substring, case-insensitive,
	// matched against everything the addon says about it.
	Blocked []string `json:"blocked,omitempty"`
	HistoryMax       int    `json:"history_max"`
	OmdbKey          string `json:"omdb_key"`
	AutoNext         bool   `json:"auto_next"`
	AutoResume       bool   `json:"auto_resume"`
	CloseMpvOnExit   bool   `json:"close_mpv_on_exit"`
	CachedFirst      bool   `json:"cached_first"`
	Accent           string `json:"accent"`
	AutoInfo         bool   `json:"auto_info"`
	Posters          bool   `json:"posters"`
	PosterSize       string `json:"poster_size"`

	// auto follows terminal detection, on and off force it either way.
	KittyMode string `json:"kitty_mode"`

	// Which poster metahub serves for the kitty path. Half-blocks always
	// take the small one, since they downscale to a few dozen cells and
	// cannot show the difference.
	PosterQuality string `json:"poster_quality"`

	// Off by default: an episode still is a frame from an episode you
	// haven't watched, which is a spoiler nobody asked for.
	EpisodeImages bool `json:"episode_images"`

	// Fetch the next page when the cursor reaches the load more row.
	AutoLoadMore bool `json:"auto_load_more"`
	DownloadDir      string `json:"download_dir"`
	DownloadFolders  bool   `json:"download_folders"`
	MoviePattern     string `json:"movie_pattern"`
	EpisodePattern   string `json:"episode_pattern"`

	// What mpv shows as the title. "default" leaves mpv to work it out from
	// the url, "formatted" uses the built-in forms, anything else is a
	// pattern using the same placeholders as the download filenames.
	MpvTitle string `json:"mpv_title"`
	DateFormat       string `json:"date_format"`
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

	if c.Version < 7 || c.PosterSize == "" {
		c.PosterSize = "medium"
		changed = true
	}

	// Posters on at xl by default: they were opt-in because half-block art
	// was rough, and on a kitty terminal it isn't art any more.
	if c.Version < 13 {
		c.Posters = true
		c.PosterSize = "xl"
		changed = true
	}

	if c.KittyMode == "" {
		c.KittyMode = "auto"
		changed = true
	}

	if c.PosterQuality == "" {
		c.PosterQuality = "large"
		changed = true
	}

	if c.Version < 8 {
		c.DownloadFolders = true
		changed = true
	}

	if c.MpvTitle == "" {
		c.MpvTitle = "default"
		changed = true
	}

	if c.MoviePattern == "" {
		c.MoviePattern = DefaultMoviePattern
		changed = true
	}
	if c.EpisodePattern == "" {
		c.EpisodePattern = DefaultEpisodePattern
		changed = true
	}
	if c.DateFormat == "" {
		c.DateFormat = "dmy"
		changed = true
	}

	// A default rather than blank: an unset language leaves the subtitle
	// picker opening on "all", which is the state every edge case shows up
	// in. Clear it deliberately and it stays clear.
	if c.Version < 11 && c.SubtitleLang == "" {
		// Spelled out rather than just "eng": all three resolve to English
		// anyway, but seeing the list in the setting is what tells you it
		// takes a list at all.
		c.SubtitleLang = "eng, en, English"
		changed = true
	}

	if c.HistoryMax <= 0 {
		c.HistoryMax = 300
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

	// Same reason as showState.Base: a favourited library entry is opened
	// by asking its addon for the file list, and nothing else records which
	// addon that was.
	Base string `json:"base,omitempty"`
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

	// The addon that served this, for library entries. Their files live
	// behind a meta lookup rather than a stream search, so resuming one
	// without knowing which addon to ask returns nothing at all.
	Base string `json:"base,omitempty"`
	WatchedAt time.Time `json:"watched_at"`

	// Recorded when playback starts so the menu can offer the next episode
	// without a network round trip. Working it out at render time would mean
	// fetching the season's episode list every time the menu draws.
	EpisodeTotal int    `json:"episode_total,omitempty"`
	NextVideoID  string `json:"next_video_id,omitempty"`
	NextSeason   int    `json:"next_season,omitempty"`
	NextEpisode  int    `json:"next_episode,omitempty"`
	NextTitle    string `json:"next_title,omitempty"`
	NextReleased string `json:"next_released,omitempty"`
}

type HistoryList struct {
	Items []HistoryEntry `json:"items"`
}

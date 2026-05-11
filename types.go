package main

import (
	"strings"
	"time"
)

// ── Navigation ────────────────────────────────────────────────────────────────

type navAct int

const (
	navOK      navAct = iota // stay / continue
	navBack                  // b — go back one level
	navBackAll               // B — go back to top of current flow
	navQuit                  // 0/q
)

// ── List rendering ────────────────────────────────────────────────────────────

// Item is a single row in any list screen.
type Item struct {
	Label   string // primary text
	Sub     string // dimmed secondary text (shown on same line after label)
	Badge   string // right-aligned tag e.g. "[movie]" "S02 · 4/12"
	Watched bool   // prefix with green ✓ if true
	Dim     bool   // grey out entire row (e.g. already watched)
}

// ── API types ─────────────────────────────────────────────────────────────────

type Meta struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Year     string `json:"year"`
	Released string `json:"released"`
	Source   string // "movie" | "show" | "anime" — injected
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

// AddonCatalog represents a catalog entry in an addon manifest.
type AddonCatalog struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
}

type Addon struct {
	TransportURL string `json:"transportUrl"`
	Manifest     struct {
		Name      string         `json:"name"`
		Resources []any          `json:"resources"`
		Catalogs  []AddonCatalog `json:"catalogs"`
	} `json:"manifest"`
}

// streamResource holds the parsed fields of a resource object that declares "stream".
type streamResource struct {
	types      []string
	idPrefixes []string
}

// parseStreamResources returns all stream resources declared by this addon,
// including their supported types and idPrefixes (empty slice = no restriction).
func (a Addon) parseStreamResources() []streamResource {
	var out []streamResource
	for _, r := range a.Manifest.Resources {
		switch v := r.(type) {
		case string:
			if v == "stream" {
				out = append(out, streamResource{}) // no restrictions
			}
		case map[string]any:
			if v["name"] != "stream" {
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

// HasStreams returns true if the addon declares a stream resource that supports
// the given mediaType and videoID. Pass empty strings to skip filtering.
func (a Addon) HasStreams() bool {
	return len(a.parseStreamResources()) > 0
}

// SupportsStream returns true if this addon's stream resources cover the given
// mediaType (e.g. "movie", "series") and videoID (checked against idPrefixes).
func (a Addon) SupportsStream(mediaType, videoID string) bool {
	for _, sr := range a.parseStreamResources() {
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
			if strings.HasPrefix(videoID, p) {
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

// ── Episode context ───────────────────────────────────────────────────────────

// EpCtx carries episode list context into the stream picker so
// [ and ] can navigate to adjacent episodes without going back through menus.
type EpCtx struct {
	Show     Meta
	Season   int
	Episodes []Video // sorted list for this season
	Index    int     // current index into Episodes
}

// ── Auth / config ─────────────────────────────────────────────────────────────

type AuthData struct {
	AuthKey string `json:"authKey"`
	Email   string `json:"email"`
}

type AppConfig struct {
	MpvPath          string `json:"mpv_path"`
	PreferredQuality string `json:"preferred_quality"`
	SubtitleLang     string `json:"subtitle_lang"`
	HistoryMax       int    `json:"history_max"`
	OmdbKey          string `json:"omdb_key"`
}

func (c *AppConfig) SetDefaults() {
	if c.HistoryMax <= 0 {
		c.HistoryMax = 100
	}
	if c.OmdbKey == "" {
		c.OmdbKey = "trilogy"
	}
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
}

type HistoryList struct {
	Items []HistoryEntry `json:"items"`
}

// ── Player ────────────────────────────────────────────────────────────────────

type MpvStatus struct {
	Alive    bool
	Title    string
	Pos      float64
	Duration float64
	Percent  float64
	Cache    float64
	Paused   bool
}

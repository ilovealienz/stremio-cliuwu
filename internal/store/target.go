package store

import (
	"stremio-cliuwu/internal/stremio"
)

// StreamTarget is everything needed to fetch streams for one playable thing.
type StreamTarget struct {
	Meta      stremio.Meta
	MediaType string
	VideoID   string // identity: history, resume, favourites
	StreamID  string // what stream addons are asked for; VideoID when empty
	Label     string
	Resume    float64
	Queue     *stremio.EpQueue // nil for movies
}

// FetchID is the id stream addons are asked for.
//
// Separate from VideoID because anime is browsed by imdb id — which gives
// real seasons — but matched far better by kitsu id. History keys on the
// browsing id so it stays stable; only the fetch differs.
func (t StreamTarget) FetchID() string {
	if t.StreamID != "" {
		return t.StreamID
	}
	return t.VideoID
}

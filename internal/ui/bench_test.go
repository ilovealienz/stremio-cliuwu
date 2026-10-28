package ui

import (
	"testing"

	"stremio-cliuwu/internal/player"
	"stremio-cliuwu/internal/store"
	"stremio-cliuwu/internal/stremio"
)

func BenchmarkPickStream220(b *testing.B) {
	var names []string
	for len(names) < 220 {
		names = append(names, carrieE02...)
	}
	next := torrentio(names[:220])
	q := &stremio.EpQueue{Show: stremio.Meta{Name: "Carrie"}, Season: 1, Episodes: []stremio.Video{
		{ID: "c:1:1", Season: 1, Episode: 1, Title: "First Period", Released: "2020-01-01T00:00:00.000Z"},
		{ID: "c:1:2", Season: 1, Episode: 2, Title: "Foreign Language", Released: "2020-01-01T00:00:00.000Z"},
	}}
	prev := player.PlayRequest{Queue: q, Release: "Carrie S01 E01.mkv"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pickStream(next, prev, store.Config{})
	}
}

package ui

import (
	"testing"

	"stremio-cliuwu/internal/store"
	"stremio-cliuwu/internal/stremio"
)

// A show's details arriving after you've moved to an episode stay out of it.
func TestStaleShowDetailSkipsEpisode(t *testing.T) {
	useCtx(t, &appCtx{cfg: &store.Config{}})
	p := newInfoPane()
	p.on = true
	p.Show(stremio.Meta{ID: "tt1", Type: "series", Name: "Show"})
	old := p.id
	p.ShowEpisode("Show", stremio.Video{ID: "tt1:1:1", Season: 1, Episode: 1})
	p.Update(metaDetailMsg{id: old, metaID: "tt1", detail: stremio.MetaDetail{Description: "the show"}, ok: true})
	if p.detail.Description != "" {
		t.Fatal("show detail landed on the episode")
	}
}

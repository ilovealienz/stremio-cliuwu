package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"stremio-cliuwu/internal/player"
	"stremio-cliuwu/internal/store"
	"stremio-cliuwu/internal/stremio"
)

func newTestStreamScreen(t *testing.T) *streamScreen {
	saved := ctx
	t.Cleanup(func() { ctx = saved })
	ctx = &appCtx{cfg: &store.Config{}, player: player.New(store.Config{})}
	return newStreamScreen(store.StreamTarget{MediaType: "movie", VideoID: "tt1"})
}

func TestStreamRetryGivesUpAfterThree(t *testing.T) {
	s := newTestStreamScreen(t)
	for i := 1; i <= len(streamRetryDelays); i++ {
		_, cmd := s.Update(streamsMsg{id: s.id, failed: 1})
		if !s.retrying || s.tries != i || s.loaded || cmd == nil {
			t.Fatalf("try %d: retrying=%v tries=%d loaded=%v", i, s.retrying, s.tries, s.loaded)
		}
	}
	s.Update(streamsMsg{id: s.id, failed: 1})
	if s.retrying || !s.loaded {
		t.Fatalf("after last try: retrying=%v loaded=%v", s.retrying, s.loaded)
	}
}

func TestStreamRetryOnlyWhenAddonsFailed(t *testing.T) {
	s := newTestStreamScreen(t)
	s.Update(streamsMsg{id: s.id}) // everyone answered, with nothing
	if s.retrying || !s.loaded {
		t.Fatalf("retrying=%v loaded=%v", s.retrying, s.loaded)
	}

	s = newTestStreamScreen(t)
	s.Update(streamsMsg{id: s.id, failed: 1, streams: []stremio.Stream{{URL: "http://x/a"}}})
	if s.retrying || len(s.streams) != 1 {
		t.Fatalf("retrying=%v streams=%d", s.retrying, len(s.streams))
	}
}

func TestStreamRetryBackStops(t *testing.T) {
	s := newTestStreamScreen(t)
	s.Update(streamsMsg{id: s.id, failed: 1})
	old := s.id
	if !s.HandleBack() {
		t.Fatal("back during retry should be consumed")
	}
	if s.retrying || !s.loaded {
		t.Fatalf("retrying=%v loaded=%v", s.retrying, s.loaded)
	}
	if _, cmd := s.Update(streamsRetryMsg{id: old}); cmd != nil {
		t.Fatal("stale retry still fetched")
	}
	if s.HandleBack() {
		t.Fatal("back after stopping should leave the screen")
	}
}

func TestStreamResumeAfterBuried(t *testing.T) {
	s := newTestStreamScreen(t)
	s.Update(streamsMsg{id: s.id, failed: 1})
	old := s.id
	if cmd := s.resume(); cmd == nil || s.id == old || !s.retrying {
		t.Fatalf("resume while retrying: id changed=%v retrying=%v", s.id != old, s.retrying)
	}
	s.Update(streamsMsg{id: s.id, streams: []stremio.Stream{{URL: "http://x/a"}}})
	if cmd := s.resume(); cmd != nil {
		t.Fatal("resume after loading should do nothing")
	}
}

func TestPlayRequest(t *testing.T) {
	newTestStreamScreen(t)
	q := &stremio.EpQueue{
		Show:   stremio.Meta{ID: "tt9", Name: "Show", Type: "series"},
		Season: 1,
		Episodes: []stremio.Video{
			{ID: "tt9:1:1", Season: 1, Episode: 1, Title: "One"},
			{ID: "tt9:1:2", Season: 1, Episode: 2, Title: "Two"},
		},
	}
	target := store.StreamTarget{Meta: q.Show, MediaType: "series", VideoID: "tt9:1:1", Label: "Show · S01E01", Queue: q}
	st := stremio.Stream{URL: "http://x/a"}
	st.BehaviorHints.Filename = "a.mkv"

	req := playRequest(target, st, 42)
	e := req.Entry
	if req.URL != st.URL || req.Resume != 42 || req.Filename != "a.mkv" || req.Queue != q {
		t.Fatalf("request = %+v", req)
	}
	if e.Season != 1 || e.Episode != 1 || e.EpTitle != "One" || e.EpisodeTotal != 2 ||
		e.NextVideoID != "tt9:1:2" || e.NextEpisode != 2 {
		t.Fatalf("entry = %+v", e)
	}
}

func TestStreamListMarksUpNext(t *testing.T) {
	s := newTestStreamScreen(t)
	streams := []stremio.Stream{{URL: "http://x/a", Name: "A"}, {URL: "http://x/b", Name: "B"}}
	s.Update(streamsMsg{id: s.id, streams: streams})
	badges := func() (out []string) {
		for _, it := range s.list.items {
			out = append(out, stripANSI(it.Badge))
		}
		return
	}
	if b := badges(); b[0] != "" || b[1] != "" {
		t.Fatalf("marked before queueing: %q", b)
	}

	ctx.player.Queue(player.PlayRequest{VideoID: "tt1", URL: "http://x/b"})
	s.Update(player.StateMsg{})
	if b := badges(); b[0] != "" || b[1] != "▶ up next" {
		t.Fatalf("after queueing: %q", b)
	}

	ctx.player.Queue(player.PlayRequest{VideoID: "other", URL: "http://x/a"})
	s.Update(player.StateMsg{})
	if b := badges(); b[0] != "" || b[1] != "" {
		t.Fatalf("marked a stream queued for another episode: %q", b)
	}
}

func TestStreamFilterIgnoresWidth(t *testing.T) {
	s := newTestStreamScreen(t)
	st := stremio.Stream{Addon: "AIOStreams", Name: "1080p", URL: "http://x/a"}
	st.BehaviorHints.Filename = "Rick and Morty S09E05 Jer Bud 1080p AMZN WEB-DL DDP5 1 H 264-Kitsune.mkv"
	s.Update(streamsMsg{id: s.id, streams: []stremio.Stream{st}})

	for _, w := range []int{50, 80, 200} {
		s.SetSize(w, 20)
		s.list.SetQuery("kitsune")
		if len(s.list.view) != 1 {
			t.Fatalf("width %d: filter hid the stream", w)
		}
		for _, line := range strings.Split(s.View(), "\n") {
			if lipgloss.Width(line) > w {
				t.Fatalf("width %d: line overflows: %q", w, stripANSI(line))
			}
		}
	}
	s.list.SetQuery("nothere")
	if !strings.Contains(stripANSI(s.list.View()), "nothing matches the filter") {
		t.Fatal("empty filter result should say so")
	}
}

func TestStreamHeaderFitsWidth(t *testing.T) {
	s := newTestStreamScreen(t)
	ctx.cfg.AutoResume = true
	s.target.Label = "Rick and Morty · S09E05"
	s.target.Resume = 1325
	s.target.Queue = &stremio.EpQueue{Episodes: make([]stremio.Video, 10), Index: 4}
	var streams []stremio.Stream
	for _, a := range []string{"Torrentio TB", "AIOStreams", "DMM Cast for TorBox"} {
		for i := 0; i < 3; i++ {
			streams = append(streams, stremio.Stream{Addon: a, Name: "1080p", URL: "http://x/" + a + string(rune('a'+i))})
		}
	}
	s.Update(streamsMsg{id: s.id, streams: streams})

	for _, w := range []int{40, 60, 90} {
		s.SetSize(w, 20)
		lines := strings.Split(s.View(), "\n")
		for _, line := range lines {
			if lipgloss.Width(line) > w {
				t.Fatalf("width %d: line overflows: %q", w, stripANSI(line))
			}
		}
		if len(lines) > 20 {
			t.Fatalf("width %d: %d lines for a 20 line screen", w, len(lines))
		}
	}
}

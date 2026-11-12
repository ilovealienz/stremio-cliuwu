package player

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"stremio-cliuwu/internal/store"
	"stremio-cliuwu/internal/stremio"
)

func TestUpNextSlot(t *testing.T) {
	p := New(store.Config{})
	p.now = &PlayRequest{VideoID: "ep1"}

	if ok, _ := p.QueueAuto(PlayRequest{VideoID: "ep2", URL: "auto"}, "ep1"); !ok || !p.Queued().Auto {
		t.Fatal("auto pick should fill an empty slot")
	}

	p.Queue(PlayRequest{VideoID: "ep2", URL: "mine", Auto: true})
	if q := p.Queued(); q.URL != "mine" || q.Auto {
		t.Fatalf("n should replace the auto pick, got %+v", q)
	}
	if ok, _ := p.QueueAuto(PlayRequest{VideoID: "ep2", URL: "auto"}, "ep1"); ok || p.Queued().URL != "mine" {
		t.Fatal("auto pick replaced a hand-queued stream")
	}

	p.Unqueue()
	if ok, _ := p.QueueAuto(PlayRequest{VideoID: "ep2", URL: "auto"}, "ep1"); ok || p.Queued() != nil {
		t.Fatal("auto pick refilled a slot cleared by hand")
	}

	p.noAuto = false // a new episode started
	if ok, _ := p.QueueAuto(PlayRequest{VideoID: "ep3", URL: "auto"}, "ep0"); ok {
		t.Fatal("auto pick for an episode that is no longer playing")
	}
	if p.State().NextLabel != "" {
		t.Fatal("label set without a queued stream")
	}
}

func TestUpNextAfterEnd(t *testing.T) {
	p := New(store.Config{})
	var started []PlayRequest
	p.start = func(req PlayRequest) { started = append(started, req) }
	p.now = &PlayRequest{VideoID: "ep1"}
	p.ended = true

	ok, now := p.QueueAuto(PlayRequest{VideoID: "ep2", URL: "auto"}, "ep1")
	if !ok || !now || len(started) != 1 || !started[0].Auto || p.Queued() != nil {
		t.Fatalf("ok=%v started=%v plays=%v queued=%v", ok, now, started, p.Queued())
	}
	if ok, _ := p.QueueAuto(PlayRequest{VideoID: "ep2", URL: "again"}, "ep1"); ok && len(started) != 1 {
		t.Fatal("second pick started another play")
	}
}

func TestClearAuto(t *testing.T) {
	p := New(store.Config{})
	p.now = &PlayRequest{VideoID: "ep1"}
	p.QueueAuto(PlayRequest{VideoID: "ep2"}, "ep1")
	p.ClearAuto()
	if p.Queued() != nil || p.State().NextLabel != "" {
		t.Fatal("auto pick not cleared")
	}
	p.Queue(PlayRequest{VideoID: "ep2", Label: "mine"})
	p.ClearAuto()
	if p.Queued() == nil {
		t.Fatal("ClearAuto removed a hand-queued stream")
	}
}

func TestQueueAfterEndPlaysAtOnce(t *testing.T) {
	p := New(store.Config{})
	var started []PlayRequest
	p.start = func(req PlayRequest) { started = append(started, req) }
	p.now = &PlayRequest{VideoID: "ep1"}

	if p.Queue(PlayRequest{VideoID: "ep2"}) || len(started) != 0 || p.Queued() == nil {
		t.Fatal("while playing, n should queue")
	}
	p.Unqueue()
	p.ended = true
	if !p.Queue(PlayRequest{VideoID: "ep2"}) || len(started) != 1 || p.Queued() != nil {
		t.Fatalf("after the end, n should play at once: started=%v queued=%v", started, p.Queued())
	}
}

func TestFinaleLooksAheadOnlyAcrossSeasons(t *testing.T) {
	aired := "2020-01-01T00:00:00.000Z"
	finale := stremio.Video{ID: "fin:1:12", Season: 1, Episode: 12, Released: aired}
	req := PlayRequest{VideoID: finale.ID, MediaType: "series", Label: "fin",
		Entry: store.HistoryEntry{Name: "fin", ID: "fin", Type: "series", Season: 1, Episode: 12, VideoID: finale.ID},
		Queue: &stremio.EpQueue{Season: 1, Episodes: []stremio.Video{finale},
			All: []stremio.Video{finale, {ID: "fin:2:1", Season: 2, Episode: 1, Released: aired}}}}
	for _, cross := range []bool{false, true} {
		p := New(store.Config{AutoNext: true, NextSeason: cross})
		playing(p, req)
		p.onProperty("duration", 1440.0)
		p.onProperty("time-pos", 1430.0)
		if p.prefetched != cross {
			t.Errorf("continue into next season %v: looked ahead %v", cross, p.prefetched)
		}
	}
}

func TestClipBlocksAutoUntilItGrows(t *testing.T) {
	p := New(store.Config{})
	req := episode("clipshow", "clipshow:1:1")
	playing(p, req)
	p.property("duration", 20.0, nil)
	if ok, _ := p.QueueAuto(PlayRequest{VideoID: "clipshow:1:2"}, req.VideoID); ok {
		t.Fatal("an autoplay pick queued behind an error clip")
	}
	p.property("duration", 1400.0, nil) // the length was still coming in
	if ok, _ := p.QueueAuto(PlayRequest{VideoID: "clipshow:1:2"}, req.VideoID); !ok {
		t.Fatal("still blocked once the file proved long enough")
	}
	p.Unqueue()
	p.property("duration", 1440.0, nil)
	if ok, _ := p.QueueAuto(PlayRequest{VideoID: "clipshow:1:2"}, req.VideoID); ok {
		t.Fatal("a slot you cleared was refilled")
	}
}

func TestAutoplayOnLooksAheadAgain(t *testing.T) {
	p := New(store.Config{AutoNext: true})
	p.prefetched = true // open next episode already looked
	p.SetConfig(store.Config{AutoNext: true, Autoplay: true})
	if p.prefetched {
		t.Fatal("turning autoplay on didn't look ahead again")
	}
	p.prefetched = true
	p.queued = &PlayRequest{VideoID: "mine"}
	p.SetConfig(store.Config{AutoNext: true})
	p.SetConfig(store.Config{AutoNext: true, Autoplay: true})
	if !p.prefetched {
		t.Fatal("looked ahead again with something already lined up")
	}
}

func TestQueueAfterFailedLoadPlays(t *testing.T) {
	p := New(store.Config{})
	var started []PlayRequest
	p.start = func(req PlayRequest) { started = append(started, req) }
	playing(p, episode("failshow", "failshow:1:1"))
	p.onEndFile("error")
	if !p.Queue(PlayRequest{VideoID: "failshow:1:1", URL: "another"}) || len(started) != 1 {
		t.Fatal("n after a failed load waited behind nothing")
	}
}

// Skipping the outro jumps to the end from the last quarter, and the file
// ends before the position is read again. A seek earlier on, or no seek,
// followed by the end is still stopping early.
func TestSkipOutroCountsAsWatched(t *testing.T) {
	for _, c := range []struct {
		pos    float64 // the last position read, of 1440
		seeked bool
		early  bool
	}{{1224, true, false}, {600, true, true}, {1224, false, true}} {
		p := New(store.Config{})
		var early bool
		p.sink = func(msg tea.Msg) {
			if e, ok := msg.(ErrMsg); ok && strings.Contains(e.Err.Error(), "stopped early") {
				early = true
			}
		}
		playing(p, episode("outroshow", fmt.Sprintf("outroshow:%v:%v", c.pos, c.seeked)))
		p.state.Duration, p.state.Pos = 1440, c.pos
		if c.seeked {
			p.dispatch(map[string]any{"event": "seek"})
		}
		p.onEndFile("eof")
		if early != c.early {
			t.Errorf("at %v, seeked %v: stopped early %v", c.pos, c.seeked, early)
		}
	}
}

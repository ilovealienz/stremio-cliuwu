package player

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"stremio-cliuwu/internal/stremio"
)

// fakeApp answers the player the way the app does with autoplay on: a
// prefetch or an end with nothing queued gets the next episode auto-queued.
type fakeApp struct {
	mu      sync.Mutex
	p       *Player
	file    string
	asks    []string // the episodes autoplay was asked to follow
	refused []string // picks QueueAuto turned down
	clips   int
}

func (a *fakeApp) handle(msg tea.Msg) {
	var prev PlayRequest
	switch m := msg.(type) {
	case PrefetchNextMsg:
		prev = m.Prev
	case EpisodeEndedMsg:
		prev = m.Prev
	case ErrorClipMsg:
		a.mu.Lock()
		a.clips++
		a.mu.Unlock()
		return
	default:
		return
	}
	if a.p.Queued() != nil {
		return
	}
	next, ok := prev.Queue.Next(false)
	if !ok {
		return
	}
	req := episodeReq(next.Episodes[next.Index].ID, a.file)
	req.Queue, req.Resume = next, 160
	a.mu.Lock()
	a.asks = append(a.asks, prev.VideoID)
	a.mu.Unlock()
	go func() { // the app picks off the player's goroutine
		if ok, _ := a.p.QueueAuto(req, prev.VideoID); !ok {
			a.mu.Lock()
			a.refused = append(a.refused, req.VideoID)
			a.mu.Unlock()
		}
	}()
}

// An addon's error clip, a stream picked by hand, then autoplay carrying on:
// every episode after the pick asks for the next one and takes the pick.
func TestRealMpvAutoplayChainAfterErrorClip(t *testing.T) {
	p := realMpv(t)
	a := &fakeApp{p: p, file: clip(t, 200)}
	p.sink = a.handle
	cfg := p.cfg
	cfg.Autoplay = true
	p.SetConfig(cfg)

	var eps []stremio.Video
	for e := 1; e <= 5; e++ {
		eps = append(eps, stremio.Video{ID: vid(7, e), Season: 1, Episode: e, Released: "2020-01-01T00:00:00.000Z"})
	}
	ep := func(i int, url string) PlayRequest {
		r := episodeReq(eps[i].ID, url)
		r.Queue = &stremio.EpQueue{Season: 1, Episodes: eps, Index: i}
		return r
	}

	p.play(ep(0, clip(t, 20)))
	waitFor(t, "mpv", 10*time.Second, func() bool { return p.State().Duration > 0 })
	p.command("set_property", "speed", 4)
	waitFor(t, "the error clip", 20*time.Second, func() bool { a.mu.Lock(); defer a.mu.Unlock(); return a.clips > 0 })

	first := ep(0, a.file)
	first.Resume = 160
	if msg := p.play(first); msg != nil {
		if e, ok := msg.(ErrMsg); ok {
			t.Fatal(e.Err)
		}
	}
	p.command("set_property", "speed", 4)

	waitFor(t, "the last episode", 90*time.Second, func() bool { return p.State().VideoID == eps[4].ID })
	a.mu.Lock()
	defer a.mu.Unlock()
	want := fmt.Sprint([]string{eps[0].ID, eps[1].ID, eps[2].ID, eps[3].ID})
	if fmt.Sprint(a.asks[:min(4, len(a.asks))]) != want || len(a.refused) > 0 {
		t.Fatalf("asked after %v, refused %v", a.asks, a.refused)
	}
}

// Autoplay's pick fails to load, you open the same episode again and it
// plays: autoplay carries on from there.
func TestRealMpvAutoplayAfterFailedLoad(t *testing.T) {
	p := realMpv(t)
	a := &fakeApp{p: p, file: clip(t, 200)}
	cfg := p.cfg
	cfg.Autoplay = true
	p.SetConfig(cfg)

	var eps []stremio.Video
	for e := 1; e <= 4; e++ {
		eps = append(eps, stremio.Video{ID: vid(8, e), Season: 1, Episode: e, Released: "2020-01-01T00:00:00.000Z"})
	}
	ep := func(i int, url string) PlayRequest {
		r := episodeReq(eps[i].ID, url)
		r.Queue = &stremio.EpQueue{Season: 1, Episodes: eps, Index: i}
		r.Resume = 160
		return r
	}

	// The first pick is a dead link; after that the app picks working ones.
	var errs []string
	var mu sync.Mutex
	bad := true
	p.sink = func(msg tea.Msg) {
		if e, ok := msg.(ErrMsg); ok {
			mu.Lock()
			errs = append(errs, e.Err.Error())
			mu.Unlock()
		}
		if m, ok := msg.(PrefetchNextMsg); ok && bad {
			bad = false
			next, _ := m.Prev.Queue.Next(false)
			req := ep(next.Index, filepath.Join(t.TempDir(), "gone.mkv"))
			go p.QueueAuto(req, m.Prev.VideoID)
			return
		}
		a.handle(msg)
	}

	p.play(ep(0, a.file))
	p.command("set_property", "speed", 4)
	waitFor(t, "the dead link to fail", 30*time.Second, func() bool { mu.Lock(); defer mu.Unlock(); return len(errs) > 0 })
	if p.State().VideoID != eps[1].ID {
		t.Fatalf("on %s after the failed load", p.State().VideoID)
	}

	// You open the same episode again, and this time it loads.
	p.play(ep(1, a.file))
	p.command("set_property", "speed", 4)
	waitFor(t, "episode 4", 60*time.Second, func() bool { return p.State().VideoID == eps[3].ID })
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.refused) > 0 {
		t.Fatalf("refused %v", a.refused)
	}
}

// A stream whose length mpv only learns as it plays starts out looking like
// an error clip. Once it's long enough it's an episode, and autoplay follows it.
func TestRealMpvGrowingDurationStillAutoplays(t *testing.T) {
	p := realMpv(t)
	a := &fakeApp{p: p, file: clip(t, 200)}
	p.sink = a.handle
	cfg := p.cfg
	cfg.Autoplay = true
	p.SetConfig(cfg)

	eps := []stremio.Video{{ID: vid(10, 1), Season: 1, Episode: 1}, {ID: vid(10, 2), Season: 1, Episode: 2}}
	req := episodeReq(eps[0].ID, "av://lavfi:testsrc=size=32x18:rate=5:duration=240")
	req.Queue = &stremio.EpQueue{Season: 1, Episodes: eps}
	p.play(req)
	waitFor(t, "playing", 10*time.Second, func() bool { return p.State().Duration > 0 })
	p.command("set_property", "speed", 20)

	waitFor(t, "episode 2", 40*time.Second, func() bool { return p.State().VideoID == eps[1].ID })
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.refused) > 0 {
		t.Fatalf("refused %v", a.refused)
	}
}

// A skip-outro key sets time-pos to the end of the last chapter, which is the
// end of the file. That's watched, and autoplay follows.
func TestRealMpvSkipOutroToEnd(t *testing.T) {
	p := realMpv(t)
	a := &fakeApp{p: p, file: clip(t, 200)}
	var errs []string
	var mu sync.Mutex
	p.sink = func(msg tea.Msg) {
		if e, ok := msg.(ErrMsg); ok {
			mu.Lock()
			errs = append(errs, e.Err.Error())
			mu.Unlock()
		}
		a.handle(msg)
	}
	cfg := p.cfg
	cfg.Autoplay = true
	p.SetConfig(cfg)
	eps := []stremio.Video{{ID: vid(11, 1), Season: 1, Episode: 1}, {ID: vid(11, 2), Season: 1, Episode: 2}}
	req := episodeReq(eps[0].ID, a.file)
	req.Queue = &stremio.EpQueue{Season: 1, Episodes: eps}
	req.Resume = 160 // 80%, past the watched mark's reach
	p.play(req)
	waitFor(t, "playing", 10*time.Second, func() bool { return p.State().Pos >= 160 })

	p.command("set_property", "time-pos", 200)
	waitFor(t, "episode 2", 15*time.Second, func() bool { return p.State().VideoID == eps[1].ID })
	mu.Lock()
	defer mu.Unlock()
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
}

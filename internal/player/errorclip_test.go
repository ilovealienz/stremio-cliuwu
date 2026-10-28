package player

import (
	"os"
	"testing"
	"time"

	"stremio-cliuwu/internal/store"
	"stremio-cliuwu/internal/stremio"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "player-test")
	if err != nil {
		panic(err)
	}
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"} {
		os.Setenv(v, dir)
	}
	os.Setenv("APPDATA", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// episode builds a request for one episode of a show with a next one after it.
func episode(show, vid string) PlayRequest {
	aired := "2020-01-01T00:00:00.000Z"
	return PlayRequest{
		VideoID: vid, MediaType: "series", Label: vid,
		Entry: store.HistoryEntry{Name: show, ID: show, Type: "series", Season: 1, Episode: 1, VideoID: vid},
		Queue: &stremio.EpQueue{Season: 1, Episodes: []stremio.Video{
			{ID: vid, Season: 1, Episode: 1, Released: aired},
			{ID: vid + "-next", Season: 1, Episode: 2, Released: aired},
		}},
	}
}

// playing sets p up as if mpv had just loaded req.
func playing(p *Player, req PlayRequest) {
	p.now = &req
	p.state = State{Alive: true, VideoID: req.VideoID}
}

// waitPos waits for the throttled save, which runs on its own goroutine.
func waitPos(vid string, want float64) bool {
	for end := time.Now().Add(time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if pos, _, _ := store.GetPosition(vid); pos == want {
			return true
		}
	}
	return false
}

func TestIsErrorClip(t *testing.T) {
	ep, lib := episode("s", "v"), PlayRequest{MediaType: "other"}
	for _, c := range []struct {
		req  *PlayRequest
		dur  float64
		want bool
	}{
		{&ep, 8, true}, {&ep, 30, true}, {&ep, 120, true}, {&ep, 179.9, true},
		{&ep, 180, false}, {&ep, 1440, false},
		{&ep, 0, false}, // not known yet
		{&lib, 30, false},
		{nil, 30, false},
	} {
		if got := isErrorClip(c.req, c.dur); got != c.want {
			t.Errorf("%v s (%v): got %v", c.dur, c.req != nil && c.req.MediaType == "other", got)
		}
	}
}

func TestErrorClipKeepsResumePoint(t *testing.T) {
	ep := episode("show1", "show1:1:1")
	store.AddHistory(ep.Entry, 100)
	store.UpdatePosition(ep.VideoID, 600, 1500)

	p := New(store.Config{Autoplay: true})
	playing(p, ep)
	p.onProperty("duration", 120.0)
	p.onProperty("time-pos", 110.0)
	if !p.State().ErrorClip || p.prefetched {
		t.Fatalf("clip=%v prefetched=%v", p.State().ErrorClip, p.prefetched)
	}

	// Every way out: the save points that used to write the clip's position.
	record(p.now, p.State())
	p.Shutdown()
	p.onEndFile("quit")
	p.gone()

	time.Sleep(20 * time.Millisecond)
	if pos, dur, w := store.GetPosition(ep.VideoID); pos != 600 || dur != 1500 || w {
		t.Fatalf("resume point changed to %v/%v watched=%v", pos, dur, w)
	}
}

func TestErrorClipEndsWithoutMovingOn(t *testing.T) {
	ep := episode("show2", "show2:1:1")
	p := New(store.Config{Autoplay: true})
	var started []PlayRequest
	p.start = func(req PlayRequest) { started = append(started, req) }

	playing(p, ep)
	p.Queue(PlayRequest{VideoID: "show2:1:1-next", Label: "next"})
	p.onProperty("duration", 30.0)
	p.onProperty("time-pos", 30.0)
	p.onEndFile("eof")

	if len(started) != 0 || p.Queued() != nil || p.State().NextLabel != "" || !p.ended {
		t.Fatalf("started=%v queued=%v ended=%v", started, p.Queued(), p.ended)
	}
	if _, _, w := store.GetPosition(ep.VideoID); w {
		t.Fatal("clip marked the episode watched")
	}
	if p.historied {
		t.Fatal("clip written to history")
	}

	// n afterwards plays the stream you picked straight away.
	if !p.Queue(PlayRequest{VideoID: "show2:1:1", URL: "other"}) || len(started) != 1 {
		t.Fatal("n after a clip should play at once")
	}
}

func TestRealFileStillRecorded(t *testing.T) {
	ep := episode("show3", "show3:1:1")
	p := New(store.Config{})
	playing(p, ep)
	p.onProperty("duration", 1440.0)
	if !p.historied {
		t.Fatal("not written to history once the length was known")
	}
	p.onProperty("time-pos", 300.0)
	if !waitPos(ep.VideoID, 300) {
		t.Fatal("position not saved")
	}

	// A short library file is the content itself.
	lib := PlayRequest{VideoID: "lib:1", MediaType: "other", Entry: store.HistoryEntry{Name: "pack", ID: "lib", Type: "other", Episode: 1, VideoID: "lib:1"}}
	p = New(store.Config{})
	playing(p, lib)
	p.onProperty("duration", 45.0)
	p.onProperty("time-pos", 20.0)
	if p.State().ErrorClip || !waitPos("lib:1", 20) {
		t.Fatal("short library file not recorded")
	}
}

func TestHistoryWaitsForTheNewFile(t *testing.T) {
	ep := episode("show4", "show4:1:1")
	p := New(store.Config{})
	playing(p, ep)
	p.replacing = true
	p.onProperty("duration", 1440.0) // could be the outgoing file's
	if p.historied {
		t.Fatal("written to history before the new file loaded")
	}
	p.onFileLoaded()
	if !p.historied {
		t.Fatal("not written once the file loaded")
	}
}

func TestEarlyEndIsNotWatched(t *testing.T) {
	for _, c := range []struct {
		name          string
		dur, pos      float64
		watched, next bool
	}{
		{"dropped mid-episode", 1440, 600, false, false},
		{"no duration known", 0, 0, false, false},
		{"played to the end", 1440, 1438, true, true},
		{"stopped in the credits", 1440, 1300, true, true},
	} {
		ep := episode("early-"+c.name, "early:"+c.name)
		store.AddHistory(ep.Entry, 100)
		p := New(store.Config{})
		var started []PlayRequest
		p.start = func(req PlayRequest) { started = append(started, req) }
		playing(p, ep)
		p.historied = true
		p.state.Duration, p.state.Pos = c.dur, c.pos
		p.Queue(PlayRequest{VideoID: ep.VideoID + "-next"})
		p.onEndFile("eof")

		pos, _, w := store.GetPosition(ep.VideoID)
		if w != c.watched || (len(started) == 1) != c.next {
			t.Errorf("%s: watched=%v started=%d", c.name, w, len(started))
		}
		if !c.watched && c.dur > 0 && pos != c.pos {
			t.Errorf("%s: position %v, want %v kept to resume from", c.name, pos, c.pos)
		}
		if !c.next && (p.Queued() != nil || !p.ended) {
			t.Errorf("%s: queued=%v ended=%v", c.name, p.Queued(), p.ended)
		}
		// The prefetch may already have asked autoplay for a pick.
		if ok, _ := p.QueueAuto(PlayRequest{VideoID: ep.VideoID + "-next"}, ep.VideoID); !c.next && (ok || len(started) != 0) {
			t.Errorf("%s: a late autoplay pick started the next episode", c.name)
		}
	}
}

func TestStaleDurationWhileReplacing(t *testing.T) {
	p := New(store.Config{Autoplay: true})
	playing(p, episode("show5", "show5:1:1"))
	p.replacing = true
	p.onProperty("duration", 30.0) // the outgoing file's, arriving late
	if p.clip || p.noAuto || p.ended {
		t.Fatalf("new file flagged from the old one: clip=%v noAuto=%v ended=%v", p.clip, p.noAuto, p.ended)
	}
	p.onProperty("duration", 1440.0)
	p.onFileLoaded()
	if p.clip || p.State().ErrorClip {
		t.Fatal("real file flagged as a clip")
	}

	// A clip whose length came in before it loaded is still caught.
	p = New(store.Config{})
	playing(p, episode("show6", "show6:1:1"))
	p.replacing = true
	p.onProperty("duration", 30.0)
	p.onFileLoaded()
	if !p.clip {
		t.Fatal("clip not caught at load")
	}
}

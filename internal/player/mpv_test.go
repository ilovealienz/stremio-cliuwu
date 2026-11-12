package player

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"stremio-cliuwu/internal/store"
)

// These drive a real mpv, headless, with clips mpv generates itself. Skipped
// when mpv isn't installed or with -short.

func realMpv(t *testing.T) *Player {
	t.Helper()
	runs++
	bin, err := exec.LookPath("mpv")
	if err != nil || testing.Short() {
		t.Skip("needs mpv")
	}
	run := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", run)
	home := t.TempDir() // keeps the user's mpv.conf and scripts out of it
	os.WriteFile(filepath.Join(home, "mpv.conf"), []byte("vo=null\nao=null\n"), 0644)
	t.Setenv("MPV_HOME", home)

	p := New(store.Config{MpvPath: bin})
	t.Cleanup(func() {
		p.Quit()
		time.Sleep(200 * time.Millisecond)
	})
	return p
}

// clip makes a video of the given length with mpv's own encoder. A lavfi
// source can't stand in for one: mpv sees it as live, its duration grows from
// zero, and that reads as an addon's error clip.
func clip(t *testing.T, secs int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), fmt.Sprintf("clip%d.mkv", secs))
	src := fmt.Sprintf("av://lavfi:testsrc=size=32x18:rate=1:duration=%d", secs)
	out, err := exec.Command("mpv", "--no-config", "--o="+path, "--of=matroska",
		"--ovc=mpeg4", "--no-audio", src).CombinedOutput()
	if err != nil {
		t.Skipf("this mpv can't encode a test clip: %v %s", err, out)
	}
	return path
}

// vid is this run's id for season s episode e of the test show.
func vid(s, e int) string { return fmt.Sprintf("mpvshow%d:%d:%d", runs, s, e) }

func waitFor(t *testing.T, what string, d time.Duration, ok func() bool) {
	t.Helper()
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if ok() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

var runs int

// episodeReq is a request for id, which also names the show so each test's
// history stays its own. A run number keeps -count repeats apart too, since
// history lives for the whole test process.
func episodeReq(id, url string) PlayRequest {
	e := episode(id, id)
	e.URL = url
	e.Entry.Name = "Real mpv"
	return e
}

func TestRealMpvPlaysToTheEnd(t *testing.T) {
	p := realMpv(t)
	req := episodeReq(vid(1, 1), clip(t, 200))
	if msg := p.play(req); msg == nil {
		t.Fatal("play returned nothing")
	} else if e, bad := msg.(ErrMsg); bad {
		t.Fatalf("play: %v", e.Err)
	}

	waitFor(t, "duration", 10*time.Second, func() bool { return p.State().Duration > 0 && !p.State().Loading })
	// 160 of 199 is outside the watched allowance, so only reaching the end
	// can mark it. Played at 4x: much faster and the once-a-second position
	// lags further than the allowance, so the end reads as stopping early.
	p.command("seek", 160, "absolute")
	p.command("set_property", "speed", 4)
	waitFor(t, "a polled position", 5*time.Second, func() bool { return p.State().Pos >= 160 })
	if _, _, watched := store.GetPosition(vid(1, 1)); watched {
		t.Fatal("watched before the end")
	}

	waitFor(t, "the end of the file", 20*time.Second, func() bool {
		_, _, watched := store.GetPosition(vid(1, 1))
		return watched
	})
	if h := store.LoadHistory(); len(h.Items) == 0 || h.Items[0].VideoID != vid(1, 1) {
		t.Fatalf("not in history: %+v", h.Items)
	}
}

// A replace records where the outgoing file got to and starts the new one.
func TestRealMpvReplace(t *testing.T) {
	p := realMpv(t)
	p.play(episodeReq(vid(2, 1), clip(t, 600)))
	waitFor(t, "first file", 10*time.Second, func() bool { return p.State().Duration > 0 && !p.State().Loading })
	p.command("seek", 300, "absolute")
	waitFor(t, "position past 300", 5*time.Second, func() bool { return p.State().Pos >= 300 })

	p.play(episodeReq(vid(2, 2), clip(t, 400)))
	waitFor(t, "second file", 10*time.Second, func() bool {
		st := p.State()
		return st.VideoID == vid(2, 2) && !st.Loading && st.Duration > 390
	})
	if pos, _, _ := store.GetPosition(vid(2, 1)); pos < 300 {
		t.Fatalf("outgoing position not saved: %v", pos)
	}
	time.Sleep(1500 * time.Millisecond) // a poll or two
	if pos := p.State().Pos; pos > 100 {
		t.Fatalf("old file's position carried over: %v", pos)
	}
}

// A link mpv can't open ends loading with an error rather than hanging.
func TestRealMpvDeadLink(t *testing.T) {
	p := realMpv(t)
	p.play(episodeReq(vid(3, 1), clip(t, 300)))
	waitFor(t, "first file", 10*time.Second, func() bool { return p.State().Duration > 0 && !p.State().Loading })

	p.play(episodeReq(vid(3, 2), filepath.Join(t.TempDir(), "gone.mkv")))
	waitFor(t, "loading to stop", 10*time.Second, func() bool { return !p.State().Loading })
	if p.replacing {
		t.Fatal("still marked replacing")
	}
}

func TestRealMpvStop(t *testing.T) {
	p := realMpv(t)
	p.play(episodeReq(vid(4, 1), clip(t, 300)))
	waitFor(t, "playing", 10*time.Second, func() bool { return p.State().Duration > 0 && !p.State().Loading })
	p.command("seek", 120, "absolute")
	waitFor(t, "position past 120", 5*time.Second, func() bool { return p.State().Pos >= 120 })

	p.Stop()()
	waitFor(t, "mpv to exit", 10*time.Second, func() bool { return !p.State().Alive })
	if pos, _, _ := store.GetPosition(vid(4, 1)); pos < 120 {
		t.Fatalf("position not saved on stop: %v", pos)
	}
}

// S in the app: a subtitle added to the playing file is selected, and one
// mpv can't load comes back as an error.
func TestRealMpvAddSubtitle(t *testing.T) {
	p := realMpv(t)
	p.play(episodeReq(vid(5, 1), clip(t, 300)))
	waitFor(t, "playing", 10*time.Second, func() bool { return p.State().Duration > 0 && !p.State().Loading })

	srt := filepath.Join(t.TempDir(), "en.srt")
	os.WriteFile(srt, []byte("1\n00:00:01,000 --> 00:00:05,000\nhello\n"), 0644)
	if msg := p.AddSubtitle(srt, "English", "eng")().(SubtitleAddedMsg); msg.Err != nil {
		t.Fatal(msg.Err)
	}
	resp, err := p.command("get_property", "current-tracks/sub/title")
	if err != nil || resp["data"] != "English" {
		t.Fatalf("selected sub: %v %v", resp["data"], err)
	}
	if msg := p.AddSubtitle(filepath.Join(t.TempDir(), "gone.srt"), "", "")().(SubtitleAddedMsg); msg.Err == nil {
		t.Fatal("a missing subtitle reported as added")
	}
}

// The mpv command settings reach mpv, chained with ; as the help says.
func TestRealMpvKindCommand(t *testing.T) {
	p := realMpv(t)
	cfg := p.cfg
	cfg.MpvCmdShow = "set sub-scale 1.5; set sub-delay 2"
	p.SetConfig(cfg)
	p.play(episodeReq(vid(6, 1), clip(t, 300)))
	waitFor(t, "the show command", 10*time.Second, func() bool {
		scale, _ := p.command("get_property", "sub-scale")
		delay, _ := p.command("get_property", "sub-delay")
		return scale["data"] == 1.5 && delay["data"] == 2.0
	})
}

// Without a runtime dir (always so on macOS) the socket goes in the app's own
// state folder, not a shared /tmp, and that folder is made if it's missing.
func TestRealMpvSocketWithoutRuntimeDir(t *testing.T) {
	p := realMpv(t)
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	if dir := filepath.Dir(socketPath()); dir != store.StateDir() {
		t.Fatalf("socket in %s", dir)
	}
	p.play(episodeReq(vid(40, 1), clip(t, 20)))
	waitFor(t, "playing", 10*time.Second, func() bool { return p.State().Duration > 0 })
	if fi, err := os.Stat(filepath.Dir(socketPath())); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("socket folder: %v %v", fi.Mode(), err)
	}
}

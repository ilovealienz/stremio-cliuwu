package player

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"stremio-cliuwu/internal/store"
)

// watch collects the player's errors and notices. retried is set once the
// player says it's trying again.
func watch(p *Player) (got func() (errs, notes []string), retried *atomic.Bool) {
	cfg := p.cfg
	cfg.RetryLoads = true
	p.SetConfig(cfg)
	var mu sync.Mutex
	var e, n []string
	retried = &atomic.Bool{}
	p.sink = func(msg tea.Msg) {
		mu.Lock()
		defer mu.Unlock()
		switch m := msg.(type) {
		case ErrMsg:
			e = append(e, m.Err.Error())
		case NoticeMsg:
			n = append(n, m.Text)
			retried.Store(true)
		}
	}
	return func() ([]string, []string) {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), e...), append([]string(nil), n...)
	}, retried
}

// flaky answers with first until the player retries, then with the file.
func flaky(t *testing.T, retried *atomic.Bool, first http.HandlerFunc, file string) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !retried.Load() {
			first(w, r)
			return
		}
		http.ServeFile(w, r, file)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/ep.mkv"
}

// A link that fails to open the first time and works the second plays.
func TestRealMpvRetriesAFailedLoad(t *testing.T) {
	p := realMpv(t)
	got, retried := watch(p)
	url := flaky(t, retried, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }, clip(t, 200))

	req := episodeReq(vid(30, 1), url)
	req.Retry = true
	p.play(req)
	waitFor(t, "the episode", 15*time.Second, func() bool { return p.State().Duration > 180 })
	if e, notes := got(); len(e) > 0 || len(notes) != 1 {
		t.Fatalf("errors %v, notices %v", e, notes)
	}
}

// A status clip in place of the file is replaced by the file on the second
// try, before the clip plays out.
func TestRealMpvRetriesAStatusClip(t *testing.T) {
	p := realMpv(t)
	got, retried := watch(p)
	status := clip(t, 30)
	url := flaky(t, retried, func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, status) }, clip(t, 200))

	req := episodeReq(vid(31, 1), url)
	req.Retry = true
	p.play(req)
	waitFor(t, "the episode", 10*time.Second, func() bool { return p.State().Duration > 180 })
	if p.clip || p.ended {
		t.Fatalf("clip %v, ended %v", p.clip, p.ended)
	}
	if e, _ := got(); len(e) > 0 {
		t.Fatalf("errors %v", e)
	}
}

// Only once: a link that keeps failing ends in the error, and one that's not
// to be retried (an uncached one) fails straight away.
func TestRealMpvRetriesOnce(t *testing.T) {
	p := realMpv(t)
	got, _ := watch(p)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	req := episodeReq(vid(32, 1), srv.URL+"/a.mkv")
	req.Retry = true
	p.play(req)
	waitFor(t, "the error", 15*time.Second, func() bool { e, _ := got(); return len(e) > 0 })
	if _, notes := got(); len(notes) != 1 {
		t.Fatalf("notices %v, want one retry", notes)
	}

	p.play(episodeReq(vid(32, 2), srv.URL+"/b.mkv"))
	waitFor(t, "the second error", 15*time.Second, func() bool { e, _ := got(); return len(e) > 1 })
	if _, notes := got(); len(notes) != 1 {
		t.Fatalf("retried a link marked not to: %v", notes)
	}
}

// The retry keeps the resume point and the request, and a second clip is
// left to play.
func TestRetryKeepsTheRequest(t *testing.T) {
	ep := episode("show9", "show9:1:1")
	ep.Resume, ep.Retry, ep.URL = 300, true, "u"
	p := New(store.Config{Autoplay: true, RetryLoads: true})
	var started []PlayRequest
	p.start = func(req PlayRequest) { started = append(started, req) }

	playing(p, ep)
	p.onProperty("duration", 30.0)
	p.onProperty("duration", 30.0) // more of the clip's events before the reload
	p.onEndFile("eof")
	if len(started) != 1 || started[0].Resume != 300 || started[0].URL != "u" || started[0].Retry || p.clip {
		t.Fatalf("started %+v, clip %v", started, p.clip)
	}

	playing(p, started[0])
	p.replacing = false // the reload has loaded
	p.onProperty("duration", 30.0)
	if len(started) != 1 || !p.clip {
		t.Fatalf("retried twice: %d", len(started))
	}
}

// Turned off in settings, nothing is retried.
func TestRetryOff(t *testing.T) {
	ep := episode("show10", "show10:1:1")
	ep.Retry = true
	p := New(store.Config{})
	var started []PlayRequest
	p.start = func(req PlayRequest) { started = append(started, req) }
	playing(p, ep)
	p.onProperty("duration", 30.0)
	if len(started) != 0 || !p.clip {
		t.Fatalf("retried with the setting off: %d", len(started))
	}
}

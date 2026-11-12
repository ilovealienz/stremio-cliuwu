package player

import (
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// A long session in one mpv: episode after episode, with seeks and pauses.
// Goroutines and the live heap should stay where they were, and closing mpv
// should leave nothing running behind it. SOAK=<episodes> runs it longer.
func TestRealMpvSoak(t *testing.T) {
	n := 15
	if v, err := strconv.Atoi(os.Getenv("SOAK")); err == nil {
		n = v
	}
	before := runtime.NumGoroutine()
	p := realMpv(t)
	file := clip(t, 200)
	p.sink = func(tea.Msg) {}

	ep := func(i int) {
		req := episodeReq(vid(20, i), file)
		p.play(req)
		waitFor(t, "playing", 10*time.Second, func() bool { return p.State().VideoID == req.VideoID && p.State().Duration > 0 })
		p.command("set_property", "time-pos", 100)
		p.command("set_property", "pause", true)
		p.command("set_property", "pause", false)
		waitFor(t, "the seek", 10*time.Second, func() bool { return p.State().Pos >= 100 })
	}
	measure := func() (int, uint64) {
		var m runtime.MemStats
		runtime.GC()
		runtime.GC()
		runtime.ReadMemStats(&m)
		return runtime.NumGoroutine(), m.HeapAlloc
	}

	ep(1)
	g0, h0 := measure()
	for i := 2; i <= n; i++ {
		ep(i)
	}
	g1, h1 := measure()
	t.Logf("%d episodes: goroutines %d → %d, heap %d KB → %d KB", n, g0, g1, h0>>10, h1>>10)
	if g1 > g0+3 {
		t.Errorf("goroutines grew from %d to %d", g0, g1)
	}
	if h1 > h0+h0/2+1<<20 {
		t.Errorf("heap grew from %d KB to %d KB", h0>>10, h1>>10)
	}

	p.Quit()
	waitFor(t, "mpv's goroutines to finish", 5*time.Second, func() bool { return runtime.NumGoroutine() <= before+1 })
}

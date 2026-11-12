package ui

import (
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"stremio-cliuwu/internal/stremio"
)

// A long session: the same flows over and over. Goroutines and the live heap
// should level off once the caches are full, not keep climbing.
// SOAK=<rounds> runs it longer.
func TestSoak(t *testing.T) {
	rounds := 12
	if testing.Short() {
		t.Skip("slow")
	}
	if n, err := strconv.Atoi(os.Getenv("SOAK")); err == nil {
		rounds = n
	}
	r := smokeApp(t, 120, 32)

	round := func(i int) {
		if i%10 == 0 {
			stremio.ClearCaches() // fetch again sometimes, as a long session would
		}
		r.keys("m")
		r.waitView("Short")
		r.keys("down", "enter")
		r.waitView("1080P")
		r.keys("esc", "esc", "s")
		r.waitView("A Show")
		r.keys("enter")
		r.waitView("season 1")
		r.keys("enter")
		r.waitView("Episode 3 with a title")
		r.keys("w", "w", "enter")
		r.waitView("1080P")
		r.keys("]", "[", "esc", "esc", "esc", "esc")
		r.keys("/", "f", "i", "l", "m", "enter")
		r.waitView("A Film")
		r.keys("esc", "esc", "h", "tab", "esc", "c", "esc", "?", "esc")
	}
	measure := func() (int, uint64) {
		var m runtime.MemStats
		for range 3 {
			runtime.GC()
		}
		time.Sleep(50 * time.Millisecond) // let finished goroutines exit
		runtime.ReadMemStats(&m)
		return runtime.NumGoroutine(), m.HeapAlloc
	}

	warm := rounds / 4
	for i := range warm {
		round(i)
	}
	g0, h0 := measure()
	start := time.Now()
	for i := warm; i < rounds; i++ {
		round(i)
	}
	g1, h1 := measure()
	t.Logf("%d rounds, %v each: goroutines %d → %d, heap %d KB → %d KB",
		rounds-warm, time.Since(start)/time.Duration(rounds-warm), g0, g1, h0>>10, h1>>10)

	if g1 > g0+5 {
		t.Errorf("goroutines grew from %d to %d", g0, g1)
	}
	if h1 > h0+h0/2+1<<20 {
		t.Errorf("heap grew from %d KB to %d KB", h0>>10, h1>>10)
	}
}

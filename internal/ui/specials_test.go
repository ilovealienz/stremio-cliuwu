package ui

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"stremio-cliuwu/internal/stremio"
)

// counting tracks how many requests are in flight at once.
type counting struct{ now, most, total atomic.Int32 }

func (c *counting) RoundTrip(r *http.Request) (*http.Response, error) {
	n := c.now.Add(1)
	defer c.now.Add(-1)
	c.total.Add(1)
	for m := c.most.Load(); n > m && !c.most.CompareAndSwap(m, n); m = c.most.Load() {
	}
	time.Sleep(20 * time.Millisecond)
	return smokeAddons{}.RoundTrip(r)
}

// drive runs a screen's commands the way the app does: each in its own
// goroutine, its message back into Update, whatever that returns started in
// turn, until nothing is left running.
func drive(s screen, cmd tea.Cmd) {
	msgs := make(chan tea.Msg)
	running := 0
	var start func(tea.Cmd)
	start = func(c tea.Cmd) {
		if c == nil {
			return
		}
		running++
		go func() { msgs <- c() }()
	}
	start(cmd)
	for running > 0 {
		m := <-msgs
		running--
		if b, ok := m.(tea.BatchMsg); ok {
			for _, c := range b {
				start(c)
			}
			continue
		}
		var next tea.Cmd
		s, next = s.Update(m)
		start(next)
	}
}

// specialsFixture is season 0 of a merged anime: n tagged collections, and
// two specials with no kitsu entry, as films usually are.
func specialsFixture(n int) stremio.SeriesMeta {
	var vids []stremio.Video
	for i := range n {
		vids = append(vids, stremio.Video{Season: 0, Episode: i + 1, Title: fmt.Sprint("Special ", i), KitsuID: fmt.Sprint(100 + i)})
		if i == 1 {
			vids = append(vids, stremio.Video{Season: 0, Episode: 50, Title: "The Movie 1"})
		}
	}
	vids = append(vids, stremio.Video{Season: 0, Episode: 51})
	return stremio.SeriesMeta{Videos: vids}
}

func specialsSetup(t *testing.T) *counting {
	smokeApp(t, 80, 24)
	stremio.ClearCaches()
	c := &counting{}
	stremio.HTTPClient.Transport = c
	return c
}

// A long series has dozens of specials. Their names come in a few at a
// time, and every one of them arrives.
func TestSpecialsFetchAFewAtATime(t *testing.T) {
	c := specialsSetup(t)
	s := newSpecialsScreen(stremio.Meta{ID: "kitsu:1", Name: "An Anime"}, specialsFixture(30))
	drive(s, s.Init())

	if c.total.Load() < 30 || c.most.Load() > specialsAtOnce {
		t.Fatalf("%d requests, %d at once", c.total.Load(), c.most.Load())
	}
	for _, g := range s.groups {
		if g.KitsuID != "" && (strings.HasPrefix(g.Title, "Special ") || g.Count == 0) {
			t.Fatalf("not filled in: %+v", g)
		}
	}
}

// Each row fills in as its answer arrives, not once they all have.
func TestSpecialsFillInAsTheyArrive(t *testing.T) {
	specialsSetup(t)
	s := newSpecialsScreen(stremio.Meta{ID: "kitsu:1", Name: "An Anime"}, specialsFixture(10))
	s.SetSize(80, 24)
	batch := s.Init()().(tea.BatchMsg)
	_, next := s.Update(batch[0]())

	filled := 0
	for _, g := range s.groups {
		if g.KitsuID != "" && !strings.HasPrefix(g.Title, "Special ") {
			filled++
		}
	}
	if filled != 1 || next == nil {
		t.Fatalf("%d filled after one answer, next %v", filled, next != nil)
	}
	if !strings.Contains(s.View(), "episodes") {
		t.Fatalf("view not updated:\n%s", s.View())
	}
}

// Answers that arrive while a collection is open are lost. Coming back asks
// again for the names still missing, and an answer from the old round is
// ignored.
func TestSpecialsCatchUpOnReturn(t *testing.T) {
	specialsSetup(t)
	s := newSpecialsScreen(stremio.Meta{ID: "kitsu:1", Name: "An Anime"}, specialsFixture(10))
	old := s.Init()().(tea.BatchMsg) // started, then the screen was buried

	back := s.syncInfo()
	s.Update(old[0]())
	for _, g := range s.groups {
		if g.KitsuID != "" && !strings.HasPrefix(g.Title, "Special ") {
			t.Fatalf("took an answer from the old round: %+v", g)
		}
	}
	drive(s, back)
	for _, g := range s.groups {
		if g.KitsuID != "" && strings.HasPrefix(g.Title, "Special ") {
			t.Fatalf("still missing after coming back: %+v", g)
		}
	}
	if s.syncInfo() != nil {
		t.Fatal("asked again with nothing missing")
	}
}

// Specials with no kitsu entry, films usually, are gathered under one row
// at the end, which opens an episode list of just those.
func TestSpecialsWithoutKitsuEntry(t *testing.T) {
	specialsSetup(t)
	s := newSpecialsScreen(stremio.Meta{ID: "tt9", Name: "An Anime"}, specialsFixture(3))
	s.SetSize(80, 24)
	if len(s.groups) != 3 || len(s.list.items) != 4 || !strings.Contains(s.View(), "other specials") {
		t.Fatalf("%d groups, %d rows:\n%s", len(s.groups), len(s.list.items), s.View())
	}

	s.list.Focus(3)
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	push, ok := cmd().(pushMsg)
	if !ok {
		t.Fatal("enter didn't open anything")
	}
	es, ok := push.s.(*episodeScreen)
	if !ok || es.season != 0 || es.Title() != "other specials" {
		t.Fatalf("opened %T %+v", push.s, push.s)
	}
	var eps []int
	for _, v := range es.sm.Videos {
		eps = append(eps, v.Episode)
	}
	if fmt.Sprint(eps) != "[50 51]" {
		t.Fatalf("episodes %v, want only the two with no kitsu entry", eps)
	}
}

// Only collections decide whether season 0 opens the specials screen: a show
// whose specials have no kitsu entries keeps its plain episode list.
func TestGroupSpecialsOnlyCountsCollections(t *testing.T) {
	plain := []stremio.Video{{Season: 0, Episode: 1, Title: "Pilot"}, {Season: 0, Episode: 2, Title: "Movie"}}
	if g := groupSpecials(plain); len(g) != 0 {
		t.Fatalf("groups %+v", g)
	}
	if l := looseSpecials(plain); len(l) != 2 {
		t.Fatalf("loose %+v", l)
	}
}

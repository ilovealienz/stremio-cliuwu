package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"stremio-cliuwu/internal/player"
	"stremio-cliuwu/internal/store"
	"stremio-cliuwu/internal/stremio"
)

func stream(addon, name, group string) stremio.Stream {
	st := stremio.Stream{Addon: addon, Name: name, URL: "http://x/" + name}
	st.BehaviorHints.BingeGroup = group
	return st
}

func TestPickStream(t *testing.T) {
	streams := []stremio.Stream{
		stream("Torrentio", "720p", "torrentio|720p"),
		stream("Torrentio", "1080p", "torrentio|1080p"),
		stream("Comet", "1080p-comet", "comet|1080p"),
		stream("Comet", "cam", "comet|cam"),
		{Addon: "Torrentio", Name: "nourl"},
	}
	cfg := store.Config{Blocked: []string{"cam"}}

	for _, c := range []struct {
		name string
		prev player.PlayRequest
		want string // "" for no pick
	}{
		{"top of list", player.PlayRequest{}, "720p"},
		{"same bingeGroup", player.PlayRequest{BingeGroup: "torrentio|1080p"}, "1080p"},
		{"group gone, top", player.PlayRequest{BingeGroup: "gone"}, "720p"},
		{"addon tab", player.PlayRequest{AddonTab: "Comet"}, "1080p-comet"},
		{"tab then group", player.PlayRequest{AddonTab: "Comet", BingeGroup: "torrentio|1080p"}, "1080p-comet"},
		{"filter", player.PlayRequest{Filter: "1080"}, "1080p"},
		{"filter and group", player.PlayRequest{Filter: "1080", BingeGroup: "comet|1080p"}, "1080p-comet"},
		{"blocked group not used", player.PlayRequest{BingeGroup: "comet|cam"}, "720p"},
		{"tab with nothing", player.PlayRequest{AddonTab: "Gone"}, ""},
		{"filter with nothing", player.PlayRequest{Filter: "2160"}, ""},
	} {
		st, ok := pickStream(streams, c.prev, cfg)
		got := ""
		if ok {
			got = st.Name
		}
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	if _, ok := pickStream(nil, player.PlayRequest{}, cfg); ok {
		t.Error("picked from an empty list")
	}
}

func TestToggleAutoplay(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("APPDATA", dir)
	s := newTestStreamScreen(t)
	s.target.Queue = &stremio.EpQueue{Episodes: []stremio.Video{{ID: "tt1"}}}

	if s.Footer() == "" || ctx.cfg.Autoplay {
		t.Fatal("autoplay should start off")
	}
	toggleAutoplay()
	if !ctx.cfg.Autoplay || !store.LoadConfig().Autoplay {
		t.Fatal("autoplay not turned on and saved")
	}
	ctx.player.QueueAuto(player.PlayRequest{VideoID: "tt1:2"}, "")
	ctx.player.Queue(player.PlayRequest{VideoID: "x"}) // stand-in now playing
	ctx.player.ClearAuto()

	toggleAutoplay()
	if ctx.cfg.Autoplay {
		t.Fatal("autoplay not turned off")
	}
	if ctx.player.Queued() == nil {
		t.Fatal("turning autoplay off removed a hand-queued stream")
	}
	if cmd := (&app{}).autoPicked(autoPickMsg{streams: []stremio.Stream{{URL: "http://x/a"}}}); cmd != nil {
		t.Fatal("a pick that landed after turning autoplay off still did something")
	}
}

func TestPickStreamPrefersSameRelease(t *testing.T) {
	mk := func(file string) stremio.Stream {
		st := stream("AIOStreams", "1080p", "aio|1080p|kitsune")
		st.URL = "http://x/" + file
		st.BehaviorHints.Filename = file
		return st
	}
	ad := mk("Rick and Morty S09E06 Ricktional Mortpoon with Audio Description 1080p AMZN WEB-DL DDP5 1 H 264-Kitsune.mkv")
	plain := mk("Rick and Morty S09E06 Ricktional Mortpoon 1080p AMZN WEB-DL DDP5 1 H 264-Kitsune.mkv")
	streams := []stremio.Stream{ad, plain}

	prev := player.PlayRequest{
		BingeGroup: "aio|1080p|kitsune",
		Filter:     "kitsune",
		Release:    "Rick and Morty S09E05 Jer Bud 1080p AMZN WEB-DL DDP5 1 H 264-Kitsune.mkv",
	}
	if st, _ := pickStream(streams, prev, store.Config{}); st.URL != plain.URL {
		t.Fatalf("picked %q", st.BehaviorHints.Filename)
	}

	prev.Release = "Rick and Morty S09E05 Jer Bud with Audio Description 1080p AMZN WEB-DL DDP5 1 H 264-Kitsune.mkv"
	if st, _ := pickStream(streams, prev, store.Config{}); st.URL != ad.URL {
		t.Fatalf("watching the AD release, picked %q", st.BehaviorHints.Filename)
	}
}

func TestPickStreamWithoutGroup(t *testing.T) {
	file := func(name, f string) stremio.Stream {
		st := stream("A", name, "")
		st.BehaviorHints.Filename = f
		return st
	}
	prev := player.PlayRequest{Release: "Show S01E01 1080p AMZN WEB-DL DDP5 1 H 264-Kitsune.mkv"}

	// Same cached state: the closer release wins over list order.
	other := file("other", "Show S01E02 1080p WEB-DL-OTHER.mkv")
	same := file("same", "Show S01E02 1080p AMZN WEB-DL DDP5 1 H 264-Kitsune.mkv")
	if st, _ := pickStream([]stremio.Stream{other, same}, prev, store.Config{}); st.Name != "same" {
		t.Fatalf("closer release should win, got %q", st.Name)
	}

	// A cached stream at the top isn't traded for an uncached closer one.
	cached := file("cached ⚡", "Show S01E02 1080p WEB-DL-OTHER.mkv")
	uncached := file("uncached ⏳", "Show S01E02 1080p AMZN WEB-DL DDP5 1 H 264-Kitsune.mkv")
	uncachedOther := file("uncached other ⏳", "Show S01E02 1080p WEB-DL-OTHER.mkv")
	cachedSame := file("cached same ⚡", "Show S01E02 1080p AMZN WEB-DL DDP5 1 H 264-Kitsune.mkv")
	if st, _ := pickStream([]stremio.Stream{cached, uncached}, prev, store.Config{CachedFirst: true}); st.Name != "cached ⚡" {
		t.Fatalf("traded a cached stream for an uncached one: %q", st.Name)
	}

	// An uncached stream at the top doesn't shut out a closer cached one.
	if st, _ := pickStream([]stremio.Stream{uncachedOther, cachedSame}, prev, store.Config{}); st.Name != "cached same ⚡" {
		t.Fatalf("a closer cached stream was left out: %q", st.Name)
	}
}

func TestPickStreamWontAutoplayOtherVariant(t *testing.T) {
	ad := stream("AIOStreams", "1080p", "aio|1080p|kitsune")
	ad.BehaviorHints.Filename = "Show S01E06 with Audio Description 1080p AMZN WEB-DL DDP5 1 H 264-Kitsune.mkv"
	prev := player.PlayRequest{
		BingeGroup: "aio|1080p|kitsune",
		Release:    "Show S01E05 1080p AMZN WEB-DL DDP5 1 H 264-Kitsune.mkv",
	}
	if st, ok := pickStream([]stremio.Stream{ad}, prev, store.Config{}); ok {
		t.Fatalf("autoplayed a different variant: %q", st.BehaviorHints.Filename)
	}

	dub := stream("A", "1080p", "")
	dub.URL = "http://x/dub"
	dub.BehaviorHints.Filename = "[Erai-raws] Show - 06 [1080p][Dual Audio].mkv"
	sub := stream("A", "1080p", "")
	sub.URL = "http://x/sub"
	sub.BehaviorHints.Filename = "[Erai-raws] Show - 06 [1080p].mkv"
	prev = player.PlayRequest{Release: "[Erai-raws] Show - 05 [1080p].mkv"}
	if st, _ := pickStream([]stremio.Stream{dub, sub}, prev, store.Config{}); st.URL != sub.URL {
		t.Fatalf("picked the dual audio release after a sub-only one")
	}
}

func TestAutoplayTag(t *testing.T) {
	newTestStreamScreen(t)
	a := &app{}
	now := time.Now()

	ctx.cfg.Autoplay = true
	if got := stripANSI(a.autoplayTag(now)); got != "[AUTOPLAY ON]" {
		t.Fatalf("on: %q", got)
	}
	ctx.cfg.Autoplay = false
	if got := a.autoplayTag(now); got != "" {
		t.Fatalf("off and never toggled should show nothing: %q", stripANSI(got))
	}
	a.autoOffAt = now
	if got := stripANSI(a.autoplayTag(now.Add(time.Second))); got != "[AUTOPLAY OFF]" {
		t.Fatalf("just turned off: %q", got)
	}
	if got := a.autoplayTag(now.Add(autoOffShown + time.Millisecond)); got != "" {
		t.Fatalf("off tag should go after %v: %q", autoOffShown, stripANSI(got))
	}
}

func TestAlignRight(t *testing.T) {
	tag := good("[AUTOPLAY ON]")
	for _, w := range []int{120, 40, 16} {
		line := alignRight("  enter=play  b/esc=back  S=subtitles  X=stop mpv  ?=keys", tag, w)
		if lw := lipgloss.Width(line); lw != w-1 {
			t.Errorf("width %d: line is %d columns, want %d", w, lw, w-1)
		}
		if !strings.HasSuffix(stripANSI(line), "[AUTOPLAY ON]") {
			t.Errorf("width %d: tag not at the right edge: %q", w, stripANSI(line))
		}
	}
	if alignRight("hints", "", 40) != "hints" {
		t.Error("no tag should leave the line alone")
	}
	if got := alignRight("hints", tag, 10); lipgloss.Width(got) > 10 || strings.Contains(stripANSI(got), "AUTOPLAY") {
		t.Errorf("too narrow for the tag should drop it: %q", stripANSI(got))
	}
}

func TestPickStreamAfterExtendedEpisode(t *testing.T) {
	next := stream("A", "1080p", "")
	next.BehaviorHints.Filename = "Show S01E11 1080p AMZN WEB-DL DDP5 1 H 264-NTb.mkv"
	prev := player.PlayRequest{Release: "Show S01E10 Extended 1080p AMZN WEB-DL DDP5 1 H 264-NTb.mkv"}
	if _, ok := pickStream([]stremio.Stream{next}, prev, store.Config{}); !ok {
		t.Fatal("an extended episode stopped autoplay for a normal one")
	}
}

func TestPickStreamIgnoresEpisodeTitles(t *testing.T) {
	aired := "2020-01-01T00:00:00.000Z"
	q := &stremio.EpQueue{Season: 1, Index: 0, Episodes: []stremio.Video{
		{ID: "tt1:1:1", Season: 1, Episode: 1, Title: "Dubbed", Released: aired},
		{ID: "tt1:1:2", Season: 1, Episode: 2, Title: "The Director's Cut", Released: aired},
	}}
	next := stream("A", "1080p", "")
	next.BehaviorHints.Filename = "Show S01E02 The Directors Cut 1080p AMZN WEB-DL DDP5 1 H 264-NTb.mkv"
	prev := player.PlayRequest{Queue: q, Release: "Show S01E01 Dubbed 1080p AMZN WEB-DL DDP5 1 H 264-NTb.mkv"}
	st, ok := pickStream([]stremio.Stream{next}, prev, store.Config{})
	if !ok || st.BehaviorHints.Filename != next.BehaviorHints.Filename {
		t.Fatal("an episode titled Dubbed stopped autoplay")
	}

	// A real dub of the next episode is still a different variant.
	dub := stream("A", "1080p", "")
	dub.BehaviorHints.Filename = "Show S01E02 The Directors Cut DUBBED 1080p AMZN WEB-DL DDP5 1 H 264-NTb.mkv"
	if _, ok := pickStream([]stremio.Stream{dub}, prev, store.Config{}); ok {
		t.Fatal("autoplayed a dub after a sub")
	}
}

func TestPlayerBarAfterErrorClip(t *testing.T) {
	a := &app{w: 120}
	a.pstate = player.State{Alive: true, Label: "Show · S01E01", Pos: 30, Duration: 30, ErrorClip: true}
	bar := stripANSI(a.playerBar())
	if !strings.Contains(bar, "addon error clip") || strings.Contains(bar, "00:30") {
		t.Fatalf("bar shows the clip's progress: %q", bar)
	}
	a.pstate.ErrorClip = false
	if !strings.Contains(stripANSI(a.playerBar()), "00:30") {
		t.Fatal("normal file lost its progress")
	}
}

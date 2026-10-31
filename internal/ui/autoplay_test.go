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
	if cmd := (&app{}).autoPicked(autoPickMsg{pick: stremio.Stream{URL: "http://x/a"}, ok: true}); cmd != nil {
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

func TestPickStreamKeepsLanguage(t *testing.T) {
	mk := func(name, title string) stremio.Stream {
		st := stream("Torrentio", name, "")
		st.Title = title
		st.URL = "http://x/" + name
		return st
	}
	ita := mk("ita", "Show.S01E06.1080p.WEB.h264-GRP\n👤 40 💾 1.2 GB\n🇮🇹")
	eng := mk("eng", "Show.S01E06.1080p.WEB.h264-GRP\n👤 90 💾 1.2 GB")
	prev := player.PlayRequest{Release: "Show.S01E05.1080p.WEB.h264-GRP"}

	if st, _ := pickStream([]stremio.Stream{ita, eng}, prev, store.Config{}); st.Name != "eng" {
		t.Fatalf("picked %q after an english episode", st.Name)
	}
	if st, ok := pickStream([]stremio.Stream{ita}, prev, store.Config{}); ok {
		t.Fatalf("autoplayed a dub in another language: %q", st.Name)
	}
	prev.Langs = []string{"it"} // watching the italian one
	if st, _ := pickStream([]stremio.Stream{eng, ita}, prev, store.Config{}); st.Name != "ita" {
		t.Fatalf("picked %q after an italian episode", st.Name)
	}
}

func TestPickStreamLooksPastWrongVariantGroup(t *testing.T) {
	ad := stream("A", "1080p", "aio|1080p|kitsune")
	ad.BehaviorHints.Filename = "Show S01E06 with Audio Description 1080p AMZN WEB-DL DDP5 1 H 264-Kitsune.mkv"
	plain := stream("B", "1080p", "other|1080p")
	plain.URL = "http://x/plain"
	plain.BehaviorHints.Filename = "Show S01E06 1080p AMZN WEB-DL DDP5 1 H 264-Kitsune.mkv"
	prev := player.PlayRequest{
		BingeGroup: "aio|1080p|kitsune",
		Release:    "Show S01E05 1080p AMZN WEB-DL DDP5 1 H 264-Kitsune.mkv",
	}
	st, ok := pickStream([]stremio.Stream{ad, plain}, prev, store.Config{})
	if !ok || st.URL != plain.URL {
		t.Fatalf("ok=%v picked %q", ok, st.BehaviorHints.Filename)
	}
}

func TestPickStreamTiebreaks(t *testing.T) {
	mk := func(addon, name, size string) stremio.Stream {
		st := stream(addon, name, "")
		st.URL = "http://x/" + name
		st.Title = "Show.S01E06.1080p.WEB.h264-GRP\n👤 9 💾 " + size
		return st
	}
	prev := player.PlayRequest{Release: "Show.S01E05.1080p.WEB.h264-GRP", Addon: "Torrentio", Size: 1200 << 20}

	if st, _ := pickStream([]stremio.Stream{mk("Comet", "other", "1.2 GB"), mk("Torrentio", "same", "1.2 GB")}, prev, store.Config{}); st.Name != "same" {
		t.Fatalf("same addon should win a tie, got %q", st.Name)
	}
	prev.Addon = ""
	if st, _ := pickStream([]stremio.Stream{mk("A", "big", "4.5 GB"), mk("A", "near", "1.3 GB")}, prev, store.Config{}); st.Name != "near" {
		t.Fatalf("nearer size should win a tie, got %q", st.Name)
	}
	if st, _ := pickStream([]stremio.Stream{mk("A", "first", "1.25 GB"), mk("A", "second", "1.18 GB")}, prev, store.Config{}); st.Name != "first" {
		t.Fatalf("a small size difference shouldn't override list order, got %q", st.Name)
	}
	if st, _ := pickStream([]stremio.Stream{mk("A", "unknown", ""), mk("A", "known", "1.1 GB")}, prev, store.Config{}); st.Name != "known" {
		t.Fatalf("a known nearby size should beat an unknown one, got %q", st.Name)
	}
	// A better match still beats a closer size.
	far := mk("A", "far", "4.5 GB")
	worse := mk("A", "worse", "1.2 GB")
	worse.Title = "Show.S01E06.720p.WEB.h264-GRP\n💾 1.2 GB"
	if st, _ := pickStream([]stremio.Stream{worse, far}, prev, store.Config{}); st.Name != "far" {
		t.Fatalf("size overrode resolution, got %q", st.Name)
	}
}

func TestPickStreamKeepsDualAudio(t *testing.T) {
	file := func(name string) stremio.Stream {
		st := stream("A", "1080p", "")
		st.URL = "http://x/" + name
		st.BehaviorHints.Filename = name
		return st
	}
	subOnly := []stremio.Stream{
		file("[SubsPlease] Show - 06 (1080p) [A1B2C3D4].mkv"),
		file("[Erai-raws] Show - 06 [1080p][Multiple Subtitle][ENG][POR-BR][SPA-LA][FRE][GER][ITA].mkv"),
		file("Show S01E06 1080p CR WEB-DL AAC2.0 H 264-VARYG.mkv"),
		file("Show (English Dub) - 06 [1080p].mkv"),
	}
	duals := []string{
		"[Judas] Show - S01E05 [1080p][HEVC x265 10bit][Dual-Audio][Multi-Subs].mkv",
		"[DB] Show - 05 [Dual Audio 10bit 1080p][HEVC-x265].mkv",
		"[EMBER] Show S01E05 [1080p] [Dual Audio HEVC WEBRip DDP].mkv",
		"Show S01E05 1080p CR WEB-DL DUAL AAC2.0 H 264-VARYG.mkv",
		"Show S01E05 1080p WEB-DL [ENG+JPN] [Multi-Sub].mkv",
		"Show S01E05 1080p WEB-DL [JPN-ENG].mkv",
		"Show.S01E05.MULTi.1080p.WEB.H264-GRP.mkv",
	}
	for _, prevName := range duals {
		for _, next := range duals {
			// The dual release for the next episode, from whichever group, at the bottom.
			want := file(strings.Replace(strings.Replace(next, "S01E05", "S01E06", 1), " - 05", " - 06", 1))
			streams := append(append([]stremio.Stream{}, subOnly...), want)
			st, ok := pickStream(streams, player.PlayRequest{Release: prevName}, store.Config{})
			if !ok || st.URL != want.URL {
				t.Errorf("after %q\n  wanted %q\n  got    %q (ok=%v)", prevName, want.BehaviorHints.Filename, st.BehaviorHints.Filename, ok)
			}
		}
		// With no dual release for the next episode, it stops rather than play a sub-only one.
		if st, ok := pickStream(subOnly, player.PlayRequest{Release: prevName}, store.Config{}); ok {
			t.Errorf("after %q autoplayed sub-only %q", prevName, st.BehaviorHints.Filename)
		}
	}

	// The other way round: after a sub-only episode, dual audio and dubs are skipped.
	sub := file("[SubsPlease] Show - 06 (1080p) [A1B2C3D4].mkv")
	for _, d := range duals {
		dual := file(strings.Replace(strings.Replace(d, "S01E05", "S01E06", 1), " - 05", " - 06", 1))
		streams := []stremio.Stream{dual, file("Show (English Dub) - 06 [1080p].mkv"), sub}
		if st, _ := pickStream(streams, player.PlayRequest{Release: "[SubsPlease] Show - 05 (1080p) [9F8E7D6C].mkv"}, store.Config{}); st.URL != sub.URL {
			t.Errorf("after sub-only, picked %q over the sub-only release", st.BehaviorHints.Filename)
		}
	}
}

func TestPickStreamGroupStaysCached(t *testing.T) {
	aired := "2020-01-01T00:00:00.000Z"
	q := &stremio.EpQueue{Season: 1, Episodes: []stremio.Video{
		{ID: "a", Season: 1, Episode: 1, Released: aired}, {ID: "b", Season: 1, Episode: 2, Released: aired}}}
	mk := func(name, group, f string) stremio.Stream {
		st := stream("A", name, group)
		st.BehaviorHints.Filename = f
		return st
	}
	same := mk("same ⏳", "g", "Show S01E02 1080p AMZN WEB-DL DDP5 1 H 264-NTb.mkv")
	other := mk("other ⚡", "h", "Show S01E02 1080p WEB-DL x264-OTHER.mkv")
	prev := player.PlayRequest{Queue: q, BingeGroup: "g", Release: "Show S01E01 1080p AMZN WEB-DL DDP5 1 H 264-NTb.mkv"}
	if st, _ := pickStream([]stremio.Stream{same, other}, prev, store.Config{CachedFirst: true}); st.Name != "other ⚡" {
		t.Fatalf("picked the uncached binge group stream: %q", st.Name)
	}
	sameCached := mk("same ⚡", "g", "Show S01E02 1080p AMZN WEB-DL DDP5 1 H 264-NTb.mkv")
	if st, _ := pickStream([]stremio.Stream{other, sameCached}, prev, store.Config{CachedFirst: true}); st.Name != "same ⚡" {
		t.Fatalf("binge group lost to list order: %q", st.Name)
	}
}

func TestPickStreamIgnoresNextSeasonTitle(t *testing.T) {
	aired := "2020-01-01T00:00:00.000Z"
	fin := stremio.Video{ID: "a", Season: 1, Episode: 12, Title: "Finale", Released: aired}
	q := &stremio.EpQueue{Season: 1, Episodes: []stremio.Video{fin},
		All: []stremio.Video{fin, {ID: "b", Season: 2, Episode: 1, Title: "Dubbed", Released: aired}}}
	next := stream("A", "1080p", "")
	next.BehaviorHints.Filename = "Show S02E01 Dubbed 1080p AMZN WEB-DL DDP5 1 H 264-NTb.mkv"
	prev := player.PlayRequest{Queue: q, Release: "Show S01E12 Finale 1080p AMZN WEB-DL DDP5 1 H 264-NTb.mkv"}
	if _, ok := pickStream([]stremio.Stream{next}, prev, store.Config{NextSeason: true}); !ok {
		t.Fatal("a season premiere titled Dubbed stopped autoplay")
	}
}

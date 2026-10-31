package player

import (
	"testing"

	"stremio-cliuwu/internal/store"
)

func TestKindCommand(t *testing.T) {
	cfg := store.Config{MpvCmdMovie: "apply-profile movie", MpvCmdShow: " apply-profile show\n", MpvCmdAnime: "script-binding anime-profile"}
	for _, c := range []struct {
		req  *PlayRequest
		want string
	}{
		{&PlayRequest{MediaType: "movie"}, "apply-profile movie"},
		{&PlayRequest{MediaType: "series"}, "apply-profile show"},
		{&PlayRequest{MediaType: "series", Entry: store.HistoryEntry{Source: "anime"}}, "script-binding anime-profile"},
		{&PlayRequest{MediaType: "movie", Entry: store.HistoryEntry{Source: "anime"}}, "script-binding anime-profile"},
		{&PlayRequest{MediaType: "other"}, ""},
		{nil, ""},
	} {
		if got := kindCommand(c.req, cfg); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.req, got, c.want)
		}
	}
	if got := kindCommand(&PlayRequest{MediaType: "movie"}, store.Config{MpvCmdMovie: `{"command":["quit"]}`}); got != "" {
		t.Errorf("json line sent: %q", got)
	}
}

func TestKindCommandOncePerWindow(t *testing.T) {
	p := New(store.Config{MpvCmdShow: "apply-profile show"})
	step := func(name string, f func(), want bool) {
		t.Helper()
		before := p.kindSent
		f()
		if sent := p.kindSent && !before; sent != want {
			t.Fatalf("%s: sent %v, want %v", name, sent, want)
		}
	}

	// An error clip plays first: nothing goes to it.
	playing(p, episode("k", "k:1:1"))
	step("restart before the length is known", func() { p.dispatch(map[string]any{"event": "playback-restart"}) }, false)
	step("clip length", func() { p.onProperty("duration", 30.0) }, false)

	// Then the real file, in the same window.
	playing(p, episode("k", "k:1:1"))
	p.restarted = false
	step("length before playback", func() { p.onProperty("duration", 1440.0) }, false)
	step("first playback", func() { p.dispatch(map[string]any{"event": "playback-restart"}) }, true)
	step("a seek", func() { p.dispatch(map[string]any{"event": "playback-restart"}) }, false)

	// The next episode in the same window gets nothing; a new window does.
	playing(p, episode("k", "k:1:2"))
	p.restarted = false
	p.onProperty("duration", 1440.0)
	step("next episode", func() { p.dispatch(map[string]any{"event": "playback-restart"}) }, false)
	p.kindSent = false // what connect does for a new mpv
	step("new window", func() { p.dispatch(map[string]any{"event": "playback-restart"}) }, true)
}

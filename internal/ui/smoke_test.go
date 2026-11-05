package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"stremio-cliuwu/internal/download"
	"stremio-cliuwu/internal/player"
	"stremio-cliuwu/internal/store"
	"stremio-cliuwu/internal/stremio"
)

// smokeMedia serves the library's files, for downloads.
var smokeMedia string

// smokeAddons is one addon with a film catalog, a show with two seasons,
// metas and streams for everything.
type smokeAddons struct{}

func (smokeAddons) RoundTrip(r *http.Request) (*http.Response, error) {
	p := r.URL.EscapedPath()
	rec := httptest.NewRecorder()
	send := func(v any) { json.NewEncoder(rec).Encode(v) }
	switch {
	case strings.HasPrefix(p, "/catalog/movie/"):
		send(map[string]any{"metas": []any{
			map[string]any{"id": "tt100", "type": "movie", "name": "A Film With A Very Long Title That Will Not Fit Anywhere At All", "releaseInfo": "2020"},
			map[string]any{"id": "tt101", "type": "movie", "name": "Short", "releaseInfo": "1999"},
		}})
	case strings.HasPrefix(p, "/catalog/series/"):
		send(map[string]any{"metas": []any{map[string]any{"id": "tt200", "type": "series", "name": "A Show", "releaseInfo": "2018–"}}})
	case strings.HasPrefix(p, "/catalog/anime/"):
		send(map[string]any{"metas": []any{map[string]any{"id": "kitsu:1", "type": "series", "name": "An Anime", "releaseInfo": "2021"}}})
	case strings.HasPrefix(p, "/meta/series/kitsu") && (strings.Contains(p, "11.json") || strings.Contains(p, "12.json")):
		id, name, n := "kitsu:11", "Recap Collection", 2
		if strings.Contains(p, "12.json") {
			id, name, n = "kitsu:12", "The Movie", 1
		}
		var vids []any
		for e := 1; e <= n; e++ {
			vids = append(vids, map[string]any{"id": fmt.Sprintf("%s:%d", id, e), "season": 1, "episode": e, "title": fmt.Sprintf("%s %d", name, e)})
		}
		send(map[string]any{"meta": map[string]any{"id": id, "name": name, "videos": vids}})
	case strings.HasPrefix(p, "/meta/series/kitsu"):
		var vids []any
		for e := 1; e <= 3; e++ {
			vids = append(vids, map[string]any{"id": fmt.Sprintf("kitsu:1:%d", e), "season": 1, "episode": e, "title": fmt.Sprintf("Anime Ep %d", e)})
		}
		for i, sp := range []struct{ kitsu, title string }{{"11", "Recap Part 1"}, {"11", "Recap Part 2"}, {"12", "The Movie"}} {
			vids = append(vids, map[string]any{"id": fmt.Sprintf("kitsu:%s:%d", sp.kitsu, i+1), "season": 0, "episode": i + 1, "title": sp.title, "kitsu_id": sp.kitsu})
		}
		send(map[string]any{"meta": map[string]any{"id": "kitsu:1", "name": "An Anime", "videos": vids}})
	case strings.HasPrefix(p, "/catalog/other/"):
		send(map[string]any{"metas": []any{map[string]any{"id": "lib1", "type": "other", "name": "A Pack"}}})
	case strings.HasPrefix(p, "/meta/other/"):
		var vids []any
		for e := 1; e <= 3; e++ {
			vids = append(vids, map[string]any{"id": fmt.Sprintf("f%d", e), "title": fmt.Sprintf("/A Pack/Show S01E0%d.mkv", e),
				"streams": []any{map[string]any{"url": fmt.Sprintf("%s/f%d.mkv", smokeMedia, e)}}})
		}
		send(map[string]any{"meta": map[string]any{"id": "lib1", "name": "A Pack", "videos": vids}})
	case strings.HasPrefix(p, "/subtitles/"):
		send(map[string]any{"subtitles": []any{
			map[string]any{"id": "1", "url": "http://x/1.srt", "lang": "eng"},
			map[string]any{"id": "2", "url": "http://x/2.srt", "lang": "spa"},
			map[string]any{"id": "3", "url": "http://x/3.srt", "lang": "eng"},
		}})
	case strings.HasPrefix(p, "/meta/series/"):
		var vids []any
		for s := 1; s <= 2; s++ {
			for e := 1; e <= 12; e++ {
				vids = append(vids, map[string]any{"id": fmt.Sprintf("tt200:%d:%d", s, e), "season": s, "episode": e,
					"title": fmt.Sprintf("Episode %d with a title", e), "released": "2019-01-01T00:00:00.000Z", "overview": "Things happen."})
			}
		}
		send(map[string]any{"meta": map[string]any{"id": "tt200", "name": "A Show", "videos": vids, "description": "A show."}})
	case strings.HasPrefix(p, "/meta/movie/"):
		send(map[string]any{"meta": map[string]any{"id": "tt100", "name": "A Film", "description": "Long synopsis. " + strings.Repeat("Words go here. ", 40),
			"genres": []string{"Drama"}, "cast": []string{"Someone"}, "runtime": "120 min", "imdbRating": "7.0"}})
	case strings.HasPrefix(p, "/stream/"):
		send(map[string]any{"streams": []any{
			map[string]any{"name": "[RD+] Smoke\n1080p", "title": "Release.Name.2020.1080p.WEB-DL.x264-GRP.mkv\n💾 2.1 GB", "url": smokeMedia + "/1.mkv"},
			map[string]any{"name": "[RD download] Smoke\n2160p", "title": "Release.Name.2020.2160p.CAM.mkv", "url": "http://x/2"},
		}})
	default:
		rec.WriteHeader(http.StatusNotFound)
	}
	return rec.Result(), nil
}

// run drives the app model the way Bubble Tea would: each message through
// Update and every command run. A command still going after a moment (a
// timer, a slow fetch) carries on in the background and its message is
// delivered once it arrives, as the real program would.
type run struct {
	t       *testing.T
	a       *app
	w, h    int
	pending []chan tea.Msg
}

func (r *run) send(msg tea.Msg) {
	r.t.Helper()
	r.drain()
	_, cmd := r.a.Update(msg)
	r.do(cmd, 0)
	r.check()
}

// drain delivers whatever background commands have finished.
func (r *run) drain() {
	still := r.pending[:0]
	var done []tea.Msg
	for _, ch := range r.pending {
		select {
		case m := <-ch:
			done = append(done, m)
		default:
			still = append(still, ch)
		}
	}
	r.pending = still
	for _, m := range done {
		r.deliver(m, 0)
	}
}

// settle waits for the background commands to finish, apart from timers
// that keep going.
func (r *run) settle() {
	r.t.Helper()
	for end := time.Now().Add(300 * time.Millisecond); len(r.pending) > 0 && time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		r.drain()
	}
	r.check()
}

// waitView waits for text to appear on screen.
func (r *run) waitView(text string) {
	r.t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		r.drain()
		if strings.Contains(r.view(), text) {
			r.check()
			return
		}
	}
	r.t.Fatalf("%q never appeared:\n%s", text, r.view())
}

func (r *run) do(cmd tea.Cmd, depth int) {
	if cmd == nil || depth > 6 {
		return
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		r.deliver(msg, depth)
	case <-time.After(50 * time.Millisecond):
		r.pending = append(r.pending, ch)
	}
}

func (r *run) deliver(msg tea.Msg, depth int) {
	switch m := msg.(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range m {
			r.do(c, depth+1)
		}
	case tea.QuitMsg:
	default:
		// tea.Sequence's message is unexported: a slice of commands to run
		// in order.
		if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeOf(tea.Cmd(nil)) {
			for i := range v.Len() {
				r.do(v.Index(i).Interface().(tea.Cmd), depth+1)
			}
			return
		}
		_, next := r.a.Update(m)
		r.do(next, depth+1)
	}
}

func (r *run) keys(ks ...string) {
	r.t.Helper()
	for _, k := range ks {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "shift+tab":
			msg = tea.KeyMsg{Type: tea.KeyShiftTab}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		r.send(msg)
	}
}

// check renders and holds the frame to the terminal: no panic, no line
// wider than the window, and no more lines than it has.
func (r *run) check() {
	r.t.Helper()
	var out string
	func() {
		defer func() {
			if p := recover(); p != nil {
				r.t.Fatalf("View panicked at %dx%d: %v", r.w, r.h, p)
			}
		}()
		out = r.a.View()
	}()
	lines := strings.Split(out, "\n")
	if len(lines) > r.h {
		r.t.Fatalf("%dx%d: %d lines\n%s", r.w, r.h, len(lines), out)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w > r.w {
			r.t.Fatalf("%dx%d: line %d is %d wide: %q\nframe:\n%s", r.w, r.h, i, w, stripANSI(l), stripANSI(out))
		}
	}
}

func (r *run) view() string { return stripANSI(r.a.View()) }

func smokeApp(t *testing.T, w, h int) *run {
	t.Setenv("STREMIO_KITTY", "0")
	saved := stremio.HTTPClient.Transport
	stremio.HTTPClient.Transport = smokeAddons{}
	t.Cleanup(func() { stremio.HTTPClient.Transport = saved })

	cfg := store.Config{}
	cfg.SetDefaults()
	cfg.MpvPath = "/nonexistent/mpv"
	cfg.Posters = false
	savedCfg := store.CfgNow()
	t.Cleanup(func() { store.UseConfig(savedCfg) }) // settings toggles save over it
	store.UseConfig(cfg)
	c := cfg
	addon := stremio.Addon{TransportURL: "https://smoke.test/manifest.json"}
	json.Unmarshal([]byte(`{"id":"smoke","name":"Smoke","resources":["catalog","meta","stream","subtitles"],"types":["movie","series","anime","other"],
		"catalogs":[{"type":"movie","id":"top","name":"Films","extra":[{"name":"search"}]},{"type":"series","id":"top","name":"Shows","extra":[{"name":"search"}]},
		{"type":"anime","id":"top","name":"Anime Top"},
		{"type":"anime","id":"genred","name":"Anime By Genre","extra":[{"name":"genre","options":["Action","Comedy"],"isRequired":true}]},
		{"type":"other","id":"lib","name":"Library"}]}`), &addon.Manifest)
	refs := stremio.AddonList{Items: []stremio.AddonRef{{URL: addon.TransportURL}}}
	store.SaveAddonRefs(refs)
	useCtx(t, &appCtx{
		cfg: &c, player: player.New(c), downloader: download.New(),
		refs:   refs,
		addons: []stremio.Addon{addon},
	})
	r := &run{t: t, a: newApp(newMenuScreen()), w: w, h: h}
	r.do(r.a.Init(), 0)
	r.send(tea.WindowSizeMsg{Width: w, Height: h})
	return r
}

// Every screen, at sizes from roomy to cramped: nothing panics and every
// frame fits the terminal.
func TestSmokeEveryScreen(t *testing.T) {
	for _, size := range [][2]int{{200, 50}, {100, 30}, {80, 24}, {50, 14}, {30, 8}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			r := smokeApp(t, size[0], size[1])

			r.keys("?", "esc")                                   // help
			r.keys("m", "down", "i", "i", "enter", "esc", "esc") // films → streams and back
			r.keys("s", "enter", "enter", "down", "w", "w", "]", "[", "enter", "r", "B", "esc", "esc", "esc", "esc")
			r.keys("/", "f", "i", "l", "m", "enter", "esc", "esc") // search
			r.keys("f", "esc", "h", "tab", "esc", "D", "esc", "A", "esc")
			r.keys("c", "down", "down", "enter", "esc", "esc") // settings
			// The prompts with the most help text.
			r.do(push(newPrompt("anime mpv command", "", "", nil, mpvCmdHelp...)), 0)
			r.keys("esc")
			r.do(push(newPrompt("subtitle language", "", "", nil, stremio.LangReference()...)), 0)
			r.keys("esc")

			// Shrink and grow mid-screen.
			r.keys("m")
			for _, s := range [][2]int{{40, 10}, {size[0], size[1]}} {
				r.w, r.h = s[0], s[1]
				r.send(tea.WindowSizeMsg{Width: s[0], Height: s[1]})
			}
			r.keys("esc")
		})
	}
}

// The flows that matter end where they should.
func TestSmokeFlows(t *testing.T) {
	r := smokeApp(t, 160, 40)
	r.keys("m")
	r.waitView("Short")
	r.keys("down", "enter")
	r.waitView("1080P")
	r.waitView("GRP")
	r.keys("esc", "esc", "s")
	r.waitView("A Show")
	r.keys("enter")
	r.waitView("season 1")
	r.keys("enter")
	r.waitView("Episode 3 with a title")
	r.keys("w") // mark E01
	if st := store.EpisodeStates("tt200"); !st[[2]int{1, 1}].Watched {
		t.Fatalf("w didn't mark the episode: %v", st)
	}
	r.keys("w")
	if st := store.EpisodeStates("tt200"); st[[2]int{1, 1}].Watched {
		t.Fatal("w didn't unmark it")
	}
}

// The screens off the main path: a catalog picker, a catalog that needs a
// genre, kitsu specials, a debrid library with downloads, subtitles, resuming
// from history and the confirm dialogs. Text is checked at a roomy size; the
// cramped ones only have to fit.
func TestSmokeMoreScreens(t *testing.T) {
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 4096))
	}))
	defer media.Close()
	smokeMedia = media.URL

	for _, size := range [][2]int{{120, 30}, {50, 14}, {30, 8}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			store.ClearAllHistory()
			r := smokeApp(t, size[0], size[1])
			ctx.cfg.DownloadDir = t.TempDir()
			roomy := size[0] >= 100
			see := func(text string) {
				t.Helper()
				if roomy {
					r.waitView(text)
					return
				}
				r.settle()
			}

			// Anime has two catalogs, so a picker; the show has kitsu specials.
			r.keys("a")
			see("Anime By Genre")
			r.keys("enter")
			see("An Anime")
			r.keys("enter")
			see("specials")
			r.keys("enter")
			see("Recap Collection")
			see("The Movie")
			r.keys("enter")
			see("season 1")
			r.keys("enter")
			see("Recap Collection 2")
			r.keys("esc", "esc", "esc", "esc", "esc", "esc")

			// A catalog that can't be browsed without a genre asks for one.
			r.keys("a", "down", "enter")
			see("Comedy")
			r.keys("down", "enter")
			see("Smoke  ·  Comedy")
			r.keys("g", "esc", "esc", "esc")

			// The library lists files; D downloads one, A the rest.
			r.keys("d")
			see("A Pack")
			r.keys("enter")
			see("Show S01E03.mkv")
			r.keys("down", "D")
			see("queued Show S01E02.mkv")
			r.keys("A")
			see("queued 2 files")
			r.keys("enter")
			see("couldn't start mpv")
			r.keys("esc", "esc")
			if roomy {
				waitDone(t, 3)
			}
			r.keys("D")
			see("4.0 KB")
			r.keys("D")
			see("delete Show")
			r.keys("y")
			if roomy && len(ctx.downloader.Snapshot()) != 2 {
				t.Fatalf("delete left %d downloads", len(ctx.downloader.Snapshot()))
			}
			r.keys("esc")

			// Subtitles, filtered by language with tab.
			r.do(push(newSubsScreen("A Film", stremio.SubsQuery{MediaType: "movie", VideoID: "tt100"}, nil)), 0)
			see("English  3") // your language first
			r.keys("tab")
			see("Spanish  2")
			r.keys("shift+tab", "esc")

			// Resuming from history lands on the episode's streams, with the
			// show and season underneath.
			store.AddHistory(store.HistoryEntry{ID: "tt200", Type: "series", Name: "A Show", VideoID: "tt200:1:2", Season: 1, Episode: 2}, 100)
			store.UpdatePosition("tt200:1:2", 300, 1400)
			r.keys("h")
			see("S01E02")
			r.keys("enter")
			see("A Show › season 1 › S01E02")
			r.keys("esc")
			see("Episode 12 with a title")
			r.keys("esc", "esc", "esc")

			// The confirms: n keeps, y goes.
			r.keys("h", "D")
			see("clear the entire watch history?")
			r.keys("n")
			if roomy && len(store.LoadHistory().Items) == 0 {
				t.Fatal("n cleared the history")
			}
			r.keys("tab", "d")
			see("remove A Show")
			r.keys("y")
			see("nothing watched yet")
			r.keys("esc", "A")
			see("smoke.test")
			r.keys("d")
			see("remove https://smoke.test")
			r.keys("n", "esc")
			if len(store.LoadAddonRefs().Items) != 1 {
				t.Fatal("n removed the addon")
			}
		})
	}
}

// waitDone waits for n finished downloads.
func waitDone(t *testing.T, n int) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		done := 0
		for _, d := range ctx.downloader.Snapshot() {
			if d.State == download.Done {
				done++
			}
		}
		if done == n {
			return
		}
	}
	t.Fatalf("downloads never finished: %+v", ctx.downloader.Snapshot())
}

// What the stream list's keys do, with mpv missing so nothing really plays.
func TestSmokeStreamActions(t *testing.T) {
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 4096))
	}))
	defer media.Close()
	smokeMedia = media.URL
	t.Cleanup(func() { smokeMedia = "" })
	store.ClearAllHistory()

	r := smokeApp(t, 160, 40)
	r.keys("s")
	r.waitView("A Show")
	r.keys("enter")
	r.waitView("season 1")
	r.keys("enter")
	r.waitView("Episode 1 with a title")
	r.keys("enter")
	r.waitView("1080P")

	// [ and ] move between episodes without leaving the list.
	r.keys("]")
	r.waitView("S01E02")
	r.keys("[")
	r.waitView("S01E01")

	r.keys("w")
	r.waitView("marked")
	if !store.EpisodeStates("tt200")[[2]int{1, 1}].Watched {
		t.Fatal("w on the stream list didn't mark the episode")
	}
	r.keys("w")
	if store.EpisodeStates("tt200")[[2]int{1, 1}].Watched {
		t.Fatal("second w didn't unmark it")
	}

	r.keys("n")
	r.waitView("nothing is playing to queue behind")

	// D needs somewhere to save first.
	r.keys("D")
	r.waitView("set a download location")
	ctx.cfg.DownloadDir = t.TempDir()
	r.keys("D")
	r.waitView("queued")
	if n := len(ctx.downloader.Snapshot()); n != 1 {
		t.Fatalf("%d downloads", n)
	}

	// With a saved spot, enter asks before resuming; mpv is missing, so
	// either answer ends in the error rather than a hang.
	store.UpdatePosition("tt200:1:1", 300, 1400)
	ctx.cfg.AutoResume = true
	r.keys("enter")
	r.waitView("from 05:00?")
	r.keys("n")
	r.waitView("couldn't start mpv")
}

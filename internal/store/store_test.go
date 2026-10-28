package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestWatchedMaths(t *testing.T) {
	var out []string
	for _, dur := range []float64{0, 60, 600, 1440, 2700, 7200, 10800} {
		for _, frac := range []float64{0, 0.01, 0.05, 0.5, 0.85, 0.87, 0.9, 1} {
			pos := dur * frac
			out = append(out, fmt.Sprintf("dur=%v pos=%v watched=%v resume=%v lead=%v floor=%v allow=%v",
				dur, pos, IsWatchedAt(pos, dur), HasResumePoint(pos, dur),
				PrefetchLead(dur), resumeFloor(dur), watchedAllowance(dur)))
		}
	}
	golden(t, "watched_maths", out)
}

func TestEpKey(t *testing.T) {
	var out []string
	for _, c := range [][2]int{{0, 0}, {1, 2}, {12, 345}} {
		k := epKey(c[0], c[1])
		s, e := parseEpKey(k)
		out = append(out, fmt.Sprintf("%v -> %q -> %d,%d", c, k, s, e))
	}
	for _, k := range []string{"", ":", "3", "a:b", "2:"} {
		s, e := parseEpKey(k)
		out = append(out, fmt.Sprintf("%q -> %d,%d", k, s, e))
	}
	golden(t, "ep_key", out)
}

func TestPatterns(t *testing.T) {
	var out []string
	for _, s := range []string{"", "  ", "...", "a/b\\c:d*e?f\"g<h>i|j", "con", "CON.mkv", "aux.part.json",
		"normal name", ".hidden.", "tab\there"} {
		out = append(out, fmt.Sprintf("safeName(%q)=%q", s, SafeName(s)))
	}
	for _, s := range []string{"() Inception", "Title [] - - x", "  a   b  ", "-_·.x.·_-"} {
		out = append(out, fmt.Sprintf("tidySegment(%q)=%q", s, tidySegment(s)))
	}
	vals := []map[string]string{
		{"title": "Inception", "year": "2010"},
		{"title": "Inception", "year": ""},
		{"show": "Show: Name", "season": "01", "episode": "02", "title": "Pilot", "year": "2001"},
		{"show": "", "season": "01", "episode": "02", "title": "", "year": ""},
	}
	for _, p := range []string{DefaultMoviePattern, DefaultEpisodePattern, `A\B/{title}`, "{title}", ""} {
		for _, v := range vals {
			out = append(out, fmt.Sprintf("expand(%q,%v)=%q", p, v, ExpandPattern(p, v)))
		}
	}
	golden(t, "patterns", out)
}

func TestConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("APPDATA", dir)

	c := LoadConfig()
	c.MpvPath = "" // depends on the machine
	golden(t, "config_fresh", c)

	b, err := os.ReadFile(filepath.Join(dir, AppName, "config.json"))
	if err != nil {
		t.Fatalf("upgrade not written back: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("empty config written")
	}

	// An old config keeps what it set and only gains the newer defaults.
	old := `{"version":3,"accent":"blue","posters":false,"history_max":50}`
	if err := os.WriteFile(filepath.Join(dir, AppName, "config.json"), []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	c = LoadConfig()
	c.MpvPath = ""
	golden(t, "config_upgraded", c)
}

func TestAddonRefsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("APPDATA", dir)

	if got := LoadAddonRefs(); len(got.Items) != 0 {
		t.Fatalf("expected empty refs, got %v", got)
	}
	al := SeedAddons()
	if err := SaveAddonRefs(al); err != nil {
		t.Fatal(err)
	}
	golden(t, "addon_refs_seed", LoadAddonRefs())
}

func TestHistoryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("APPDATA", dir)
	hist = &historyStore{}

	AddHistory(HistoryEntry{Name: "Film", ID: "tt1", Type: "movie", VideoID: "tt1", Year: "2010"}, 300)
	UpdatePosition("tt1", 600, 6000)
	AddHistory(HistoryEntry{Name: "Show", ID: "tt2", Type: "series", Season: 1, Episode: 3,
		VideoID: "tt2:1:3", EpisodeTotal: 10, NextVideoID: "tt2:1:4", NextSeason: 1, NextEpisode: 4}, 300)
	UpdatePosition("tt2:1:3", 1400, 1440)

	type pos struct {
		Pos, Dur float64
		Watched  bool
	}
	get := func(id string) pos { p, d, w := GetPosition(id); return pos{p, d, w} }
	ep := func(id string, s, e int) pos { p, d, w := GetPositionByEpisode(id, s, e); return pos{p, d, w} }

	var cont []string
	for _, c := range ContinueList(10) {
		cont = append(cont, fmt.Sprintf("%+v", c.Entry.VideoID))
	}
	golden(t, "history_round_trip", map[string]any{
		"film":     get("tt1"),
		"episode":  get("tt2:1:3"),
		"byEp":     ep("tt2", 1, 3),
		"missing":  get("tt9"),
		"states":   fmt.Sprint(EpisodeStates("tt2")),
		"continue": cont,
		"recent":   len(RecentlyWatched(10)),
	})

	FlushHistory()
	if _, err := os.Stat(filepath.Join(dir, AppName, "history.json")); err != nil {
		t.Fatalf("history not flushed: %v", err)
	}
	hist = &historyStore{}
	if p, _, _ := GetPosition("tt1"); p != 600 {
		t.Fatalf("position after reload = %v, want 600", p)
	}
}

func TestPrefetchNotBeforeWatched(t *testing.T) {
	for d := 180.0; d <= 4*3600; d += 30 {
		if lead := PrefetchLead(d); lead > watchedAllowance(d) {
			t.Fatalf("%v s: prefetch %v s before the end, watched only from %v s", d, lead, watchedAllowance(d))
		}
	}
}

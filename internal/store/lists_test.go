package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"stremio-cliuwu/internal/stremio"
)

func tempData(t testing.TB) {
	dir := t.TempDir()
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "APPDATA"} {
		t.Setenv(v, dir)
	}
}

func urls(al stremio.AddonList) []string {
	var out []string
	for _, it := range al.Items {
		s := it.URL
		if it.Disabled {
			s += " (off)"
		}
		out = append(out, s)
	}
	return out
}

func TestAddonRefEditing(t *testing.T) {
	tempData(t)
	a, err := AddAddonRef("stremio://a.example/manifest.json")
	if err != nil || a != "https://a.example/manifest.json" {
		t.Fatalf("add: %q %v", a, err)
	}
	if _, err := AddAddonRef("https://a.example/manifest.json"); err == nil {
		t.Fatal("duplicate added")
	}
	if _, err := AddAddonRef("not a url at all"); err == nil {
		t.Fatal("junk added")
	}
	AddAddonRef("https://b.example/manifest.json")
	AddAddonRef("https://c.example/manifest.json")

	if got := MoveAddonRef(2, -1); got != 1 {
		t.Fatalf("move returned %d", got)
	}
	if got := MoveAddonRef(0, -1); got != 0 {
		t.Fatalf("move past the top returned %d", got)
	}
	ToggleAddonRef(0)
	RemoveAddonRef(2)
	RemoveAddonRef(9) // out of range: no-op
	ToggleAddonRef(-1)

	want := []string{"https://a.example/manifest.json (off)", "https://c.example/manifest.json"}
	got := urls(LoadAddonRefs())
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("refs = %v", got)
	}
	// Windows has no Unix modes to check.
	if fi, err := os.Stat(AddonsFile()); err != nil || runtime.GOOS != "windows" && fi.Mode().Perm() != 0600 {
		t.Fatalf("addons.json should be 0600 (it can hold debrid keys): %v %v", fi.Mode(), err)
	}
}

func TestFavourites(t *testing.T) {
	tempData(t)
	show := Favourite{ID: "tt1", Name: "Show", Type: "series"}
	s3 := Favourite{ID: "tt1", Name: "Show", Type: "series", Season: 3}
	if !ToggleFav(show) || !ToggleFav(s3) {
		t.Fatal("not favourited")
	}
	if fl := LoadFavs(); len(fl.Items) != 2 || fl.Items[0].Added == "" {
		t.Fatalf("favs = %+v", fl.Items)
	}
	// Unfavouriting the season leaves the show.
	if ToggleFav(s3) {
		t.Fatal("toggle didn't remove")
	}
	if fl := LoadFavs(); len(fl.Items) != 1 || fl.Items[0].Season != 0 {
		t.Fatalf("favs = %+v", fl.Items)
	}
	RemoveFav(5)
	RemoveFav(0)
	if len(LoadFavs().Items) != 0 {
		t.Fatal("not removed")
	}
}

func TestExpandPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	t.Setenv("MEDIA", "/srv/media")
	cases := map[string]string{
		"":                "",
		"  ~  ":           home,
		"~/Videos":        filepath.Join(home, "Videos"),
		"$MEDIA/films/":   "/srv/media/films",
		"/a/../b/./c":     "/b/c",
		"relative/dir//x": "relative/dir/x",
	}
	if runtime.GOOS == "windows" {
		cases["%MEDIA%/films"] = "/srv/media/films"
	}
	for in, want := range cases {
		// Compared with forward slashes: Clean gives backslashes on Windows.
		if got := ExpandPath(in); filepath.ToSlash(got) != filepath.ToSlash(want) {
			t.Errorf("ExpandPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSingleEpisodeWatched(t *testing.T) {
	tempData(t)
	old := hist
	hist = &historyStore{}
	defer func() { hist = old }()

	e := HistoryEntry{ID: "tt3", Name: "Show", Type: "series", Season: 1, Episode: 4, VideoID: "tt3:1:4"}
	if !ToggleWatchedByEpisode(e) {
		t.Fatal("toggle didn't mark watched")
	}
	if _, _, w := GetPosition("tt3:1:4"); !w {
		t.Fatal("not watched by video id")
	}
	// Marking one episode by hand is a history row, unlike bulk marking.
	if h := LoadHistory(); len(h.Items) != 1 || h.Items[0].Episode != 4 {
		t.Fatalf("history = %+v", h.Items)
	}
	if ToggleWatchedByEpisode(e) {
		t.Fatal("toggle didn't unmark")
	}
	if p, _, w := GetPositionByEpisode("tt3", 1, 4); w || p != 0 {
		t.Fatalf("unmarked: pos %v watched %v", p, w)
	}
	// Unmarking something never recorded creates nothing.
	SetWatchedByEpisode(HistoryEntry{ID: "tt4", Season: 1, Episode: 1}, false)
	if len(EpisodeStates("tt4")) != 0 {
		t.Fatal("unmark created a record")
	}
}

func TestHistoryShowsAndClear(t *testing.T) {
	tempData(t)
	old := hist
	hist = &historyStore{}
	defer func() { hist = old }()

	a := stremio.Meta{ID: "tta", Name: "Alpha", Type: "series"}
	b := stremio.Meta{ID: "ttb", Name: "Beta", Type: "series"}
	SetSeasonWatched(a, 1, []stremio.Video{{ID: "tta:1:1", Episode: 1}, {ID: "tta:1:2", Episode: 2}}, true)
	hist.data.Shows["tta"].SeenAt = 100
	SetSeasonWatched(b, 1, []stremio.Video{{ID: "ttb:1:1", Episode: 1}}, true)
	hist.data.Shows["ttb"].SeenAt = 200
	AddHistory(HistoryEntry{ID: "ttc", Name: "Opened only", Type: "movie"}, 0) // no progress: not a show you've watched

	shows := HistoryShows()
	if len(shows) != 2 || shows[0].ID != "ttb" || shows[1].Episodes != 2 {
		t.Fatalf("shows = %+v", shows)
	}
	if st := HistoryStats(); st.Episodes != 3 || st.Shows != 2 {
		t.Fatalf("stats = %+v", st)
	}

	ClearShow("tta")
	if len(EpisodeStates("tta")) != 0 || len(HistoryShows()) != 1 {
		t.Fatal("ClearShow left state behind")
	}
	for _, e := range LoadHistory().Items {
		if e.ID == "tta" {
			t.Fatal("ClearShow left a history row")
		}
	}

	ClearAllHistory()
	if len(LoadHistory().Items) != 0 || len(HistoryShows()) != 0 {
		t.Fatal("ClearAllHistory left entries")
	}
	if _, _, w := GetPosition("ttb:1:1"); w {
		t.Fatal("ClearAllHistory left watched state")
	}
}

// The history list stops at the configured length; watched state doesn't.
func TestRecentLimit(t *testing.T) {
	tempData(t)
	old := hist
	hist = &historyStore{}
	defer func() { hist = old }()
	saved := CfgNow()
	defer UseConfig(saved)
	c := saved
	c.HistoryMax = 3
	UseConfig(c)

	for i := 1; i <= 5; i++ {
		SetWatchedByEpisode(HistoryEntry{ID: "tt7", Name: "S", Type: "series", Season: 1, Episode: i}, true)
	}
	if h := LoadHistory(); len(h.Items) != 3 || h.Items[0].Episode != 5 {
		t.Fatalf("history = %+v", h.Items)
	}
	if len(EpisodeStates("tt7")) != 5 {
		t.Fatal("watched state trimmed with the list")
	}
}

func TestContinue(t *testing.T) {
	tempData(t)
	old := hist
	hist = &historyStore{}
	defer func() { hist = old }()
	watched := func(e HistoryEntry) {
		AddHistory(e, 300)
		UpdatePosition(e.VideoID, 1440, 1440)
	}
	show := HistoryEntry{Name: "Show", ID: "tt9", Type: "series", Season: 1, EpisodeTotal: 12}

	// Mid-season: the next episode, with the season's count.
	e := show
	e.Episode, e.VideoID, e.NextVideoID, e.NextSeason, e.NextEpisode = 5, "tt9:1:5", "tt9:1:6", 1, 6
	watched(e)
	InvalidateInProgress()
	c := ContinueTarget()
	if c == nil || !c.NextUp || c.Entry.VideoID != "tt9:1:6" || c.Total != 12 || c.LastLabel != "S01E05" {
		t.Fatalf("next up: %+v", c)
	}
	if l := ContinueList(5); len(l) != 1 || l[0].Entry.VideoID != "tt9:1:6" {
		t.Fatalf("list: %+v", l)
	}

	// Started it, on another day: resume, not announce.
	hist.data.Shows["tt9"].Eps[epKey(1, 6)] = &epState{P: 300, D: 1440}
	InvalidateInProgress()
	if c := ContinueTarget(); c == nil || c.NextUp || c.Position != 300 {
		t.Fatalf("started next: %+v", c)
	}

	// Season finale: season 2 starts, and season 1's count isn't its count.
	e = show
	e.Episode, e.VideoID, e.NextVideoID, e.NextSeason, e.NextEpisode = 12, "tt9:1:12", "tt9:2:1", 2, 1
	watched(e)
	InvalidateInProgress()
	c = ContinueTarget()
	if c == nil || c.Entry.VideoID != "tt9:2:1" || c.Total != 0 || c.Entry.EpisodeTotal != 0 {
		t.Fatalf("next season: %+v", c)
	}
	if l := ContinueList(5); len(l) != 1 || l[0].Total != 0 {
		t.Fatalf("list next season: %+v", l)
	}

	// Seen that too: nothing to continue.
	watched(HistoryEntry{Name: "Show", ID: "tt9", Type: "series", Season: 2, Episode: 1, VideoID: "tt9:2:1"})
	InvalidateInProgress()
	if c := ContinueTarget(); c != nil {
		t.Fatalf("nothing left, got %+v", c)
	}
}

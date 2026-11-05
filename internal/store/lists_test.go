package store

import (
	"os"
	"path/filepath"
	"testing"

	"stremio-cliuwu/internal/stremio"
)

func tempData(t *testing.T) {
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
	if fi, err := os.Stat(AddonsFile()); err != nil || fi.Mode().Perm() != 0600 {
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
	for in, want := range map[string]string{
		"":                "",
		"  ~  ":           home,
		"~/Videos":        filepath.Join(home, "Videos"),
		"$MEDIA/films/":   "/srv/media/films",
		"/a/../b/./c":     "/b/c",
		"relative/dir//x": "relative/dir/x",
	} {
		if got := ExpandPath(in); got != want {
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

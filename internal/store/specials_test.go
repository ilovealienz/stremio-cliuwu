package store

import (
	"os"
	"path/filepath"
	"testing"

	"stremio-cliuwu/internal/stremio"
)

func TestSpecialsHaveWatchedState(t *testing.T) {
	dir := t.TempDir()
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "APPDATA"} {
		t.Setenv(v, dir)
	}
	old := hist
	hist = &historyStore{}
	defer func() { hist = old }()

	show := stremio.Meta{ID: "tt9", Type: "series", Name: "Show"}
	eps := []stremio.Video{{ID: "tt9:0:1", Season: 0, Episode: 1}, {ID: "tt9:0:2", Season: 0, Episode: 2}}
	SetSeasonWatched(show, 0, eps, true)

	got := EpisodeStates("tt9")
	if !got[[2]int{0, 1}].Watched || !got[[2]int{0, 2}].Watched {
		t.Fatalf("specials not reported watched: %v", got)
	}
	if _, ok := got[[2]int{0, 0}]; ok {
		t.Fatal("0:0 is a film, not an episode")
	}
}

func TestFailedFlushIsRetried(t *testing.T) {
	dir := t.TempDir()
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "APPDATA"} {
		t.Setenv(v, dir)
	}
	old := hist
	hist = &historyStore{}
	defer func() { hist = old }()

	// A directory where the file goes makes the rename fail.
	os.MkdirAll(filepath.Join(histFile(), "x"), 0755)
	SetSeasonWatched(stremio.Meta{ID: "tt8", Type: "series"}, 1, []stremio.Video{{ID: "tt8:1:1", Season: 1, Episode: 1}}, true)
	hist.Flush()
	if !hist.dirty {
		t.Fatal("failed write marked as saved")
	}
	os.RemoveAll(histFile())
	hist.Flush()
	if _, err := os.Stat(histFile()); err != nil || hist.dirty {
		t.Fatalf("retry didn't write: %v dirty=%v", err, hist.dirty)
	}
}

// A row whose show is missing isn't listed, so list positions don't match the
// stored rows; delete has to go by which episode it is.
func TestClearHistoryEntryByIdentity(t *testing.T) {
	dir := t.TempDir()
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "APPDATA"} {
		t.Setenv(v, dir)
	}
	old := hist
	hist = &historyStore{loaded: true, byVideo: map[string]epRef{}, data: historyFile{
		Version: historyVer,
		Shows: map[string]*showState{
			"a": {Name: "A", Eps: map[string]*epState{epKey(1, 1): {W: true}}},
			"b": {Name: "B", Eps: map[string]*epState{epKey(1, 2): {W: true}}},
		},
		Recent: []recentEntry{{ShowID: "gone", Season: 1, Episode: 9}, {ShowID: "a", Season: 1, Episode: 1}, {ShowID: "b", Season: 1, Episode: 2}},
	}}
	defer func() { hist = old }()

	list := LoadHistory().Items
	if len(list) != 2 || list[0].ID != "a" {
		t.Fatalf("listed %v", list)
	}
	ClearHistoryEntry(list[0])
	left := LoadHistory().Items
	if len(left) != 1 || left[0].ID != "b" {
		t.Fatalf("wrong row removed, left %v", left)
	}
}

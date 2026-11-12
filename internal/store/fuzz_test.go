package store

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"stremio-cliuwu/internal/stremio"
)

// The files on disk are the user's and outlive any version that wrote them:
// hand-edited, cut short by a crash, or from an older build. Whatever is in
// them, loading must not crash, and a file that won't parse is moved aside
// rather than written over.

func fuzzFile(f *testing.F, path func() string, seeds []string, use func()) {
	tempData(f)
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		p := path()
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		old := hist
		hist = &historyStore{}
		defer func() { hist = old }()
		use()
		if !json.Valid(b) {
			kept(t, p, b)
		}
	})
}

// kept fails unless a .corrupt copy of path holds b.
func kept(t *testing.T, path string, b []byte) {
	t.Helper()
	names, _ := filepath.Glob(path + ".corrupt-*")
	for _, n := range names {
		if got, _ := os.ReadFile(n); bytes.Equal(got, b) {
			return
		}
	}
	t.Fatalf("%s wasn't moved aside", filepath.Base(path))
}

func FuzzHistoryFile(f *testing.F) {
	fuzzFile(f, histFile, []string{
		`{"version":2,"shows":{"tt1":{"name":"A","eps":{"1:2":{"w":true,"p":10,"d":20}}}},"recent":[{"id":"tt1","s":1,"e":2,"v":"tt1:1:2","at":1}]}`,
		`{"version":2,"shows":{"tt1":null},"recent":[{"id":"tt1","s":1,"e":1,"v":"x"}]}`,
		`{"version":2,"shows":{"tt1":{"eps":{"1:1":null}}},"recent":[{"id":"tt1","s":1,"e":1}]}`,
		`{"version":2,"shows":{},"recent":[{"id":"","s":-1,"e":-1,"n":-5}]}`,
		`{"version":"2"}`,
		`[]`,
		``,
	}, func() {
		LoadHistory()
		HistoryShows()
		ContinueTarget()
		ContinueList(10)
		RecentlyWatched(10)
		HistoryStats()
		EpisodeStates("tt1")
		GetPosition("tt1:1:2")
		GetPositionByEpisode("tt1", 1, 1)
		e := HistoryEntry{ID: "tt1", Name: "A", Type: "series", Season: 1, Episode: 1, VideoID: "tt1:1:1"}
		AddHistory(e, 100)
		UpdatePosition("tt1:1:1", 5, 100)
		ToggleWatchedByEpisode(e)
		SetSeasonWatched(stremio.Meta{ID: "tt1"}, 1, []stremio.Video{{ID: "tt1:1:1", Season: 1, Episode: 1}}, true)
		ClearHistoryEntry(e)
		ClearShow("tt1")
		FlushHistory() // a timed flush landing in the next run would hide its file
	})
}

func FuzzConfigFile(f *testing.F) {
	fuzzFile(f, CfgFile, []string{
		`{"autoplay":true,"sub_langs":"eng,jpn","accent":"#ff00ff"}`,
		`{"autoplay":"yes"}`,
		`{"mpv_args":null,"accent":"\u001b[31m"}`,
		`null`,
	}, func() {
		c := LoadConfig()
		SaveConfig(c)
		if got := LoadConfig(); got.SetDefaults() {
			panic("defaults applied twice")
		}
	})
}

func FuzzListFiles(f *testing.F) {
	tempData(f)
	for _, s := range []string{
		`{"items":[{"id":"tt1","name":"A","season":2}]}`,
		`{"items":[{"url":"https://a.test/manifest.json","disabled":true},{"url":""}]}`,
		`{"items":null}`,
		`{"items":[null]}`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		os.MkdirAll(DataDir(), 0o755)
		os.WriteFile(favsFile(), b, 0o644)
		os.WriteFile(AddonsFile(), b, 0o644)

		fl := LoadFavs()
		for i := range fl.Items {
			RemoveFav(i)
		}
		ToggleFav(Favourite{ID: "tt1", Name: "A"})

		al := LoadAddonRefs()
		for i := range al.Items {
			MoveAddonRef(i, 1)
			ToggleAddonRef(i)
		}
		RemoveAddonRef(len(al.Items))
		RemoveAddonRef(-1)
		AddAddonRef("https://b.test/manifest.json")
		if !json.Valid(b) {
			kept(t, favsFile(), b)
			kept(t, AddonsFile(), b)
		}
	})
}

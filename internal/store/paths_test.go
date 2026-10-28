package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestBaseDirs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("everything lives in %APPDATA% on windows")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "rel/data") // relative: the spec says ignore it
	t.Setenv("XDG_STATE_HOME", "/xdg/state")

	for _, c := range []struct{ got, want string }{
		{CfgFile(), filepath.Join(home, ".config", AppName, "config.json")},
		{AddonsFile(), filepath.Join(home, ".local", "share", AppName, "addons.json")},
		{favsFile(), filepath.Join(home, ".local", "share", AppName, "favourites.json")},
		{histFile(), filepath.Join("/xdg/state", AppName, "history.json")},
		{DownloadsFile(), filepath.Join("/xdg/state", AppName, "downloads.json")},
	} {
		if c.got != c.want {
			t.Errorf("got %s, want %s", c.got, c.want)
		}
	}

	// A write makes its own folder, not only the config one.
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	if err := WriteAtomic(histFile(), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Dir(histFile())); err != nil || fi.Mode().Perm() != 0700 {
		t.Fatalf("state dir: %v %v", fi, err)
	}
}

func TestExpandPatternFillsOnce(t *testing.T) {
	vals := map[string]string{"show": "{title}", "title": "{year} Special", "year": "2001", "season": "01", "episode": "02"}
	for i := 0; i < 50; i++ { // map order varies run to run
		got := ExpandPattern(DefaultEpisodePattern, vals)
		if len(got) != 3 || got[0] != "{title}" || got[2] != "02 {year} Special" {
			t.Fatalf("got %q", got)
		}
	}
}

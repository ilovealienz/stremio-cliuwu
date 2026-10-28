package download

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"stremio-cliuwu/internal/store"
	"stremio-cliuwu/internal/stremio"
)

func TestDownloadPaths(t *testing.T) {
	saved := *store.Cfg
	t.Cleanup(func() { *store.Cfg = saved })

	movie := store.StreamTarget{Meta: stremio.Meta{Name: "Inception", Year: "2010"}}
	unnamed := store.StreamTarget{Label: "Some Label"}
	q := &stremio.EpQueue{Show: stremio.Meta{Name: "Show: Name", Year: "2001"}, Season: 2,
		Episodes: []stremio.Video{{Episode: 4, Title: "Ep Four"}, {Episode: 5, Title: "Ep/Five"}}, Index: 1}
	episode := store.StreamTarget{Queue: q}

	var out []string
	for _, folders := range []bool{true, false} {
		*store.Cfg = store.Config{}
		store.Cfg.SetDefaults()
		store.Cfg.DownloadFolders = folders
		for _, c := range []struct {
			name string
			t    store.StreamTarget
			url  string
		}{
			{"movie", movie, "https://x/a.mp4?token=1"},
			{"unnamed", unnamed, "https://x/a"},
			{"episode", episode, "https://x/b.MKV#frag"},
		} {
			p := filepath.ToSlash(Path("/root", c.t, c.url))
			out = append(out, fmt.Sprintf("folders=%v %s %s", folders, c.name, p))
		}
		for _, rel := range []string{"Season 1/E01.mkv", "a//b:c.mkv", "flat.mkv"} {
			for _, pack := range []string{"Pack Name", ""} {
				p := filepath.ToSlash(LibraryPath("/root", pack, rel))
				out = append(out, fmt.Sprintf("folders=%v lib %q %q %s", folders, pack, rel, p))
			}
		}
	}
	golden(t, "download_paths", out)
}

func TestDownloadFormat(t *testing.T) {
	var out []string
	for _, u := range []string{"https://x/a.MP4", "https://x/a.webm?x=1.mkv", "https://x/a.exe", "https://x/a", "a.ts#x"} {
		out = append(out, fmt.Sprintf("ext(%q)=%s", u, extFromURL(u)))
	}
	for _, n := range []int64{0, 1023, 1024, 1536, 1 << 20, 5 << 30, 3 << 40, 1 << 62} {
		out = append(out, fmt.Sprintf("bytes(%d)=%s", n, FmtBytes(n)))
	}
	for _, d := range []Download{{}, {Speed: 1 << 20, Total: 100 << 20, Done: 40 << 20}, {Speed: 10, Total: 5, Done: 5}} {
		out = append(out, fmt.Sprintf("eta=%q", FmtETA(d)))
	}
	golden(t, "download_format", out)
}

func TestFmtSecs(t *testing.T) {
	var out []string
	add := func(f string, a ...any) { out = append(out, fmt.Sprintf(f, a...)) }

	for _, s := range []float64{-5, 0, 59.9, 61, 3599, 3600, 7322} {
		add("fmtSecs(%v)=%s", s, FmtSecs(s))
	}
	golden(t, "fmt_secs", out)
}

func TestPathEdgeCases(t *testing.T) {
	saved := *store.Cfg
	t.Cleanup(func() { *store.Cfg = saved })
	*store.Cfg = store.Config{}
	store.Cfg.SetDefaults()
	store.Cfg.DownloadFolders = true

	ep := func(show, title string) store.StreamTarget {
		return store.StreamTarget{Queue: &stremio.EpQueue{Show: stremio.Meta{Name: show}, Season: 1,
			Episodes: []stremio.Video{{Episode: 3, Title: title}}}}
	}
	// A separator in a value stays in the name; only the pattern's make folders.
	for _, c := range []struct {
		t    store.StreamTarget
		want string
	}{
		{ep("Fate/Zero", "Part 1/2"), "/root/Fate-Zero/Season 01/03 Part 1-2.mkv"},
		{ep(`AC\DC`, "Back/In\\Black"), "/root/AC-DC/Season 01/03 Back-In-Black.mkv"},
		{store.StreamTarget{Meta: stremio.Meta{Name: "Face/Off", Year: "1997"}}, "/root/Movies/(1997) Face-Off.mkv"},
	} {
		if got := filepath.ToSlash(Path("/root", c.t, "https://x/a.mkv")); got != c.want {
			t.Errorf("got %s, want %s", got, c.want)
		}
	}

	// Empty, "." and ".." parts are dropped, so nothing climbs out of root.
	for rel, want := range map[string]string{
		"a//b.mkv":           "/root/Pack/a/b.mkv",
		"./a/./b.mkv":        "/root/Pack/a/b.mkv",
		"../../etc/x.mkv":    "/root/Pack/etc/x.mkv",
		"a/../../b.mkv":      "/root/Pack/a/b.mkv",
		"/abs/start.mkv":     "/root/Pack/abs/start.mkv",
		"a/ /trailing/ .mkv": "/root/Pack/a/trailing/mkv",
	} {
		if got := filepath.ToSlash(LibraryPath("/root", "Pack", rel)); got != want {
			t.Errorf("%q: got %s, want %s", rel, got, want)
		}
	}

	// Long names fit the 255-byte limit with room for the .part.json sidecar,
	// and a multi-byte title is never cut mid-character.
	long := strings.Repeat("ゆ", 120) // 360 bytes
	for _, p := range []string{
		Path("/root", ep(long, long), "https://x/a.mkv"),
		LibraryPath("/root", long, long+"/"+strings.Repeat("a", 300)+".mkv"),
	} {
		parts := strings.Split(filepath.ToSlash(p), "/")
		for i, part := range parts {
			n := len(part)
			if i == len(parts)-1 {
				n += len(".part.json")
			}
			if n > store.MaxNameBytes || !utf8.ValidString(part) {
				t.Errorf("part %q is %d bytes (valid utf-8: %v)", part, n, utf8.ValidString(part))
			}
		}
		if !strings.HasSuffix(p, ".mkv") {
			t.Errorf("lost the extension: %s", p)
		}
	}
}

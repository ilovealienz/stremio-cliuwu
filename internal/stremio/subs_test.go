package stremio

import (
	"strings"
	"testing"
)

func TestSubsPaths(t *testing.T) {
	q := SubsQuery{MediaType: "series", VideoID: "tt1:2:3"}
	if p := q.paths(); len(p) != 1 || p[0] != "/subtitles/series/tt1:2:3.json" {
		t.Fatalf("no hints: %v", p)
	}
	q.Hash, q.Size, q.Filename = "abc123", 1234, "Show S02E03 [1080p].mkv"
	p := q.paths()
	want := "/subtitles/series/tt1:2:3/videoHash=abc123&videoSize=1234&filename=Show+S02E03+%5B1080p%5D.mkv.json"
	if len(p) != 2 || p[0] != want || p[1] != "/subtitles/series/tt1:2:3.json" {
		t.Fatalf("with hints: %v", p)
	}
	if got := (SubsQuery{VideoID: "tt9"}).path(false); got != "/subtitles/movie/tt9.json" {
		t.Errorf("default type: %s", got)
	}
}

func TestGetSubtitles(t *testing.T) {
	os := manifestAddon("os.test", `{"name":"OpenSubtitles","resources":["subtitles"],"types":["movie","series"]}`)
	aio := manifestAddon("aio.test", `{"name":"AIO","resources":[{"name":"subtitles","types":["series"],"idPrefixes":["tt"]}]}`)
	streamsOnly := manifestAddon("tio.test", `{"name":"Torrentio","resources":["stream"]}`)
	w := useWorld(t, map[string]any{
		// hash-matched answer and the plain one, with an overlap
		"os.test/subtitles/series/tt1:1:1/videoHash=h.json": map[string]any{"subtitles": []any{
			map[string]any{"id": "1", "url": "https://s/1.srt", "lang": "eng", "SubtitleFileName": "Exact.Release.srt"},
		}},
		"os.test/subtitles/series/tt1:1:1.json": map[string]any{"subtitles": []any{
			map[string]any{"id": "1", "url": "https://s/1.srt", "lang": "eng"},
			map[string]any{"id": "2", "url": "https://s/2.srt", "lang": "fre"},
			map[string]any{"id": "x", "url": "", "lang": "eng"},
		}},
		"aio.test/subtitles/series/tt1:1:1.json": map[string]any{"subtitles": []any{
			map[string]any{"id": "3", "url": "https://s/3.srt", "lang": "en"},
			map[string]any{"id": "4", "url": "https://s/4.srt", "lang": "pt-BR"},
		}},
	})
	addons := []Addon{os, aio, streamsOnly}
	if n := len(SubtitleAddons(addons, "series", "tt1:1:1")); n != 2 {
		t.Fatalf("subtitle addons = %d", n)
	}

	got := GetSubtitles(addons, SubsQuery{MediaType: "series", VideoID: "tt1:1:1", Hash: "h"}, "fre, eng")
	var order []string
	for _, s := range got {
		order = append(order, s.ID+"/"+LangName(s.Lang)+"/"+s.Addon)
	}
	// French first (first preference), then English by addon order, then the
	// rest; the hash-matched copy of 1 wins over the plain one.
	if strings.Join(order, " ") != "2/French/OpenSubtitles 1/English/OpenSubtitles 3/English/AIO 4/Portuguese/AIO" {
		t.Fatalf("order = %v", order)
	}
	if got[1].Label() != "Exact.Release.srt" {
		t.Errorf("hash-matched result not kept: %q", got[1].Label())
	}

	GetSubtitles(addons, SubsQuery{MediaType: "series", VideoID: "tt1:1:1", Hash: "h"}, "eng")
	if n := w.count("os.test/subtitles/series/tt1:1:1.json"); n != 1 {
		t.Errorf("cached result asked again: %d", n)
	}

	// Nothing found isn't cached.
	GetSubtitles(addons, SubsQuery{MediaType: "series", VideoID: "tt2:1:1"}, "eng")
	GetSubtitles(addons, SubsQuery{MediaType: "series", VideoID: "tt2:1:1"}, "eng")
	if n := w.count("os.test/subtitles/series/tt2:1:1.json"); n != 2 {
		t.Errorf("empty result cached: asked %d times", n)
	}
}

func TestMergeSubtitles(t *testing.T) {
	shipped := []Subtitle{{URL: "u1", Lang: "eng", Rank: 0}}
	found := []Subtitle{{URL: "u1", Lang: "eng", Rank: 3}, {URL: "u2", Lang: "jpn", Rank: 1}, {URL: "u3", Lang: "en", Rank: 2}}
	got := MergeSubtitles(shipped, found, "jpn")
	if len(got) != 3 || got[0].URL != "u2" || got[1].URL != "u1" || got[2].URL != "u3" {
		t.Fatalf("merged = %+v", got)
	}
}

func TestLangName(t *testing.T) {
	for in, want := range map[string]string{
		"eng": "English", " EN ": "English", "English": "English", "en-GB": "English",
		"pt_BR": "Portuguese", "es-419": "Spanish", "": "unknown", "klingon": "klingon",
	} {
		if got := LangName(in); got != want {
			t.Errorf("LangName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := PreferredLangs("eng, en, English, jpn,, "); strings.Join(got, ",") != "English,Japanese" {
		t.Errorf("PreferredLangs = %v", got)
	}
	ref := LangReference()
	if len(ref) == 0 || !strings.HasPrefix(ref[0], "Arabic") || !strings.Contains(strings.Join(ref, "\n"), "English") {
		t.Errorf("reference = %v", ref[:3])
	}
}

func TestSubtitleLabel(t *testing.T) {
	cases := []struct {
		s    Subtitle
		want string
	}{
		{Subtitle{SubtitleFileName: "a.srt", MovieReleaseName: "b", ID: "c"}, "a.srt"},
		{Subtitle{MovieReleaseName: " b ", ID: "c"}, "b"},
		{Subtitle{ID: "c"}, "c"},
		{Subtitle{}, "subtitle"},
	}
	for _, c := range cases {
		if got := c.s.Label(); got != c.want {
			t.Errorf("Label = %q, want %q", got, c.want)
		}
	}
}

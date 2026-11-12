package stremio

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// Release names come from addons, so anything can arrive. These run as
// ordinary tests on the seeds; go test -fuzz=Name explores further.

var nameSeeds = []string{
	"Haikyu S01E16 Winners and Losers 1080p CR WEB-DL DUAL DDP2 0 H 264-Kitsune.mkv",
	"[Judas] Haikyuu!! (Seasons 1-4 + OADs) [BD 1080p][HEVC x265 10bit][Dual-Audio][Eng-Subs]",
	"[SubsPlease] Haikyuu!! - 01 (1080p) [ABCD1234].mkv",
	"The.Daily.Show.2024.03.05.1080p.WEB.h264-GRP.mkv",
	"Show.S01E01-E03.720p", "Show 1x02", "S99E999", "🇬🇧🇯🇵 x", "", "S01E", "[", "(((", "2024.13.45",
}

func FuzzReleaseNames(f *testing.F) {
	for _, s := range nameSeeds {
		f.Add(s, "Haikyuu!!")
	}
	f.Fuzz(func(t *testing.T, name, show string) {
		r := ParseReleaseNameTitled(name, show, "Winners and Losers")
		r.Matches(ParseReleaseName(name))
		ShapeLikeness(NameShape(name, show), NameShape(strings.ToUpper(name), show))
		SameTitle(TitleWords(name), TitleWords(show))
		Extra(name)
		NameDate(name)
		FlagLangs(name)
		ExtractRes(name)
		ExtractSize(name)
		CleanTitle(name)
		ParseRelease(name)
		StreamBlocked(Stream{Name: name, Title: show}, []string{"cam", "upscale*", name})
		if s, first, _, ok := NameEpisode(name); ok && (s < 0 || first < 0) {
			t.Errorf("NameEpisode(%q) = season %d, from %d", name, s, first)
		}
		if c := CleanText(name); CleanText(c) != c || strings.ContainsFunc(c, func(r rune) bool {
			return r < 0x20 && r != '\n' || r >= 0x7f && r <= 0x9f || isBidiControl(r)
		}) {
			t.Errorf("CleanText(%q) = %q: not clean, or changes again", name, c)
		}
		if !utf8.ValidString(CleanTitle(name)) && utf8.ValidString(name) {
			t.Errorf("CleanTitle broke UTF-8: %q", name)
		}
	})
}

// A meta or stream response is addon JSON; decoding and tidying it must not
// panic whatever shape it has.
func FuzzAddonJSON(f *testing.F) {
	f.Add(`{"meta":{"id":"tt1","name":"x","videos":[{"id":"tt1:1:1","season":1,"episode":1}]}}`)
	f.Add(`{"metas":[{"id":"kitsu:1","type":"series","runtime":24,"imdbRating":7.1}]}`)
	f.Add(`{"streams":[{"name":"[TB+]\n1080p","title":"x\n💾 1.4 GB","url":"http://x","behaviorHints":{"filename":"a.mkv","videoSize":1}}]}`)
	f.Fuzz(func(t *testing.T, body string) {
		var m struct {
			Meta  Meta     `json:"meta"`
			Metas []Meta   `json:"metas"`
			Strs  []Stream `json:"streams"`
		}
		if json.Unmarshal([]byte(body), &m) != nil {
			return
		}
		m.Meta.normalize("anime", "https://x.test")
		for i := range m.Metas {
			m.Metas[i].normalize("", "https://x.test")
		}
		SortStreams(m.Strs, "1080p", true)
		for _, st := range m.Strs {
			StreamCached(st)
			StreamBytes(st)
			ReleaseName(st)
		}
	})
}

// Subtitle languages come from addons as codes, names or free text, and the
// setting is typed by hand.
func FuzzSubtitleLangs(f *testing.F) {
	for _, s := range []string{"eng", "en-GB", "pt_BR", "English", "eng, en, English", ",,", " ", "İ", "-en", "es-419", "brazilian portuguese"} {
		f.Add(s, "jpn,eng")
	}
	f.Fuzz(func(t *testing.T, lang, setting string) {
		n := LangName(lang)
		if n == "" || LangName(n) != n {
			t.Fatalf("LangName(%q) = %q, then %q", lang, n, LangName(n))
		}

		prefs := PreferredLangs(setting)
		seen := map[string]bool{}
		for _, p := range prefs {
			if p == "" || seen[p] {
				t.Fatalf("PreferredLangs(%q) = %q", setting, prefs)
			}
			seen[p] = true
		}

		shipped := []Subtitle{{URL: "a", Lang: lang}, {URL: "", Lang: lang}, {URL: "b", Lang: setting}}
		found := []Subtitle{{URL: "a", Lang: "eng"}, {URL: "c", Lang: lang, Rank: 1}, {URL: "d", Lang: setting}}
		out := MergeSubtitles(shipped, found, setting)
		urls := map[string]bool{}
		for i, s := range out {
			if s.URL == "" || urls[s.URL] {
				t.Fatalf("merged %v", out)
			}
			urls[s.URL] = true
			if i > 0 && langRank(prefs, LangName(out[i-1].Lang)) > langRank(prefs, LangName(s.Lang)) {
				t.Fatalf("preferred languages not first: %v with %q", out, prefs)
			}
		}
		if len(out) != 4 {
			t.Fatalf("merged %d, want 4", len(out))
		}
	})
}

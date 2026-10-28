package stremio

import (
	"fmt"
	"testing"
)

var streamTexts = []string{
	"Torrentio\n4K",
	"[RD+] Torrentio 1080p",
	"[TB download] Torrentio 720p",
	"⚡ 2160p HDR <YTS> 1.44 GB",
	"⏳ 480P 700 MiB",
	"Movie.2020.1440p.WEB-DL.Download.Edition",
	"〈Comet〉 12.5 GiB 360p",
	"\u200bzero\u200dwidth\u00a0text\ufeff",
	"★★★ starred ☆ name",
	"AI.Upscale_[x265]-GROUP",
	"",
}

func TestStreamParse(t *testing.T) {
	type row struct {
		In, Invis, Res, Src, Size, Words string
		Cached                           int
	}
	var out []row
	for _, s := range streamTexts {
		out = append(out, row{
			In: s, Invis: StripInvis(s), Res: ExtractRes(s), Src: ExtractSrc(s),
			Size: ExtractSize(s), Words: matchWords(s),
			Cached: StreamCached(Stream{Name: s}),
		})
	}
	yes, no := true, false
	out = append(out,
		row{In: "explicit cached", Cached: StreamCached(Stream{Name: "⏳", Cached: &yes})},
		row{In: "explicit uncached", Cached: StreamCached(Stream{Name: "⚡", Cached: &no})},
		row{In: "cached in description", Cached: StreamCached(Stream{Description: "[PM+]"})},
	)
	golden(t, "stream_parse", out)
}

func TestStreamBlocked(t *testing.T) {
	names := []string{
		"Movie.2020.CAM.x264", "Camelot.1967.1080p", "Cam.2018.720p",
		"Show.S01E01.AI.Upscaled", "Show S01E01 Ai_Upscaler", "clean.release",
	}
	termSets := [][]string{
		nil, {"cam"}, {"upscale*"}, {"ai upscale*"}, {"  ", "*"}, {"CAM", "x264"},
	}
	var out []string
	for _, n := range names {
		for _, ts := range termSets {
			s := Stream{Name: n}
			s.BehaviorHints.Filename = n + ".mkv"
			out = append(out, fmt.Sprintf("%s %q %v", n, ts, StreamBlocked(s, ts)))
		}
	}
	golden(t, "stream_blocked", out)
}

func TestSortStreams(t *testing.T) {
	mk := func() []Stream {
		return []Stream{
			{Name: "a 720p", Rank: 1},
			{Name: "b 1080p [RD+]", Rank: 1},
			{Name: "c 1080p", Rank: 0},
			{Name: "d 4K ⏳", Rank: 0},
			{Name: "e 1080p ⚡", Rank: 0},
			{Name: "f 720p", Rank: 2},
			{Name: "g 1080p [TB download]", Rank: 2},
		}
	}
	names := func(ss []Stream) []string {
		var o []string
		for _, s := range ss {
			o = append(o, s.Name)
		}
		return o
	}
	golden(t, "sort_streams", map[string][]string{
		"none":         names(SortStreams(mk(), "", false)),
		"cached":       names(SortStreams(mk(), "", true)),
		"1080p":        names(SortStreams(mk(), "1080p", false)),
		"1080p+cached": names(SortStreams(mk(), "1080p", true)),
		"4k+cached":    names(SortStreams(mk(), "4k", true)),
	})
}

func TestFormatHelpers(t *testing.T) {
	var out []string
	add := func(f string, a ...any) { out = append(out, fmt.Sprintf(f, a...)) }

	for _, c := range [][3]any{
		{1, 5, ""}, {0, 3, ""}, {2, 0, "tt1:2:5"}, {-1, 0, "kitsu:123:7"},
		{0, 0, "kitsu:9"}, {0, 0, "tt1:x:5"}, {0, 0, "plain"},
	} {
		add("fmtEp(%v,%v,%q)=%s", c[0], c[1], c[2], FmtEp(c[0].(int), c[1].(int), c[2].(string)))
	}
	for _, s := range []string{"", "My%2520Film.mkv", "Show%20S01E01.MP4", "  spaced.ts ", "bad%zz.avi", "keep.srt"} {
		add("cleanTitle(%q)=%q", s, CleanTitle(s))
	}
	for _, s := range []string{"kitsu:123", "kitsu:", "tt123", ""} {
		add("kitsuIDOf(%q)=%q", s, KitsuIDOf(s))
	}
	for _, s := range []string{"2020-05-01", "2020-05-01T10:00:00.000Z", "2020-05-01T10:00:00Z",
		"01 May 2020", "May 1, 2020", "2020-05-01T10:00:00+02:00", "1970-01-01", "N/A", "", "garbage"} {
		tm, ok := ParseRelease(s)
		add("parseRelease(%q)=%s %v", s, tm.UTC().Format("2006-01-02T15:04:05Z"), ok)
	}
	for _, s := range []string{"2000-01-01", "2999-01-01", "", "nonsense"} {
		add("videoAired(%q)=%v", s, VideoAired(Video{Released: s}))
	}
	golden(t, "format_helpers", out)
}

func TestManifestURLs(t *testing.T) {
	var out []string
	for _, s := range []string{
		"", "  torrentio.strem.fun  ", "https://torrentio.strem.fun/manifest.json",
		"stremio://addon.example/torbox=abcdefghijk/manifest.json",
		"[Torrentio](https://torrentio.strem.fun/realdebrid=XYZ123456/)",
		"ftp://weird.example/path", "http://127.0.0.1:11470/", "https:///nohost",
	} {
		u, err := NormalizeManifestURL(s)
		out = append(out, fmt.Sprintf("norm(%q)=%q err=%v", s, u, err))
	}
	for _, s := range []string{
		"https://torrentio.strem.fun/realdebrid=ABCDEFGHIJ/manifest.json",
		"https://x.example/a=short/b=1234567/manifest.json",
		"https://x.example/manifest.json", "::bad",
	} {
		out = append(out, fmt.Sprintf("redact(%q)=%q", s, RedactURL(s)))
	}
	for _, a := range []Addon{
		{TransportURL: "https://a.example/manifest.json", Manifest: Manifest{Name: " Named "}},
		{TransportURL: "https://host.example/x/manifest.json/"},
		{Manifest: Manifest{ID: "org.id"}},
		{},
	} {
		out = append(out, fmt.Sprintf("label=%q base=%q", addonLabel(a), addonBase(a)))
	}
	golden(t, "manifest_urls", out)
}

func TestReleaseName(t *testing.T) {
	st := Stream{Name: "n", Title: "first line\n👤 5 💾 1 GB"}
	if ReleaseName(st) != "first line" {
		t.Errorf("ReleaseName from title = %q", ReleaseName(st))
	}
	st.BehaviorHints.Filename = "file.mkv"
	if ReleaseName(st) != "file.mkv" {
		t.Errorf("ReleaseName from filename = %q", ReleaseName(st))
	}
}

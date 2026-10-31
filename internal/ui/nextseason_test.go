package ui

import (
	"testing"

	"stremio-cliuwu/internal/player"
	"stremio-cliuwu/internal/store"
	"stremio-cliuwu/internal/stremio"
)

// Real Torrentio results for Chuunibyou S02E01, in list order.
var chuuniS2E1 = []string{
	"[Judas] Chuunibyou S2 - 01.mkv",
	"[YURI] Love, Chunibyo & Other Delusions! S02E01 [BD 1080p x264 10bit FLAC].mkv",
	"Love, Chunibyo and Other Delusions! - S02E01v3 [BD Remux 1080p FLAC] [Dual Audio] [uba].mkv",
	"Love.Chunibyo.and.Other.Delusions.S02E01.1080p.BluRay.MULTi.Opus2.0.x264-Fuchs.mkv",
	"Chuunibyou demo Koi ga Shitai! Ren s02e01.mkv",
	"[matheousse] Chuunibyou demo Koi ga Shitai! Ren - 01 VOSTFR v2 [BD 1080p AAC] [83F22EEB].mkv",
	"[Akai] Chuunibyou Demo Koi ga Shitai! Ren - 01 [BD 1080p Hi10p AAC][F5E1D41C].mkv",
	"[Kametsu] Love, Chunibyo & Other Delusions-Heart Throb - 13v2 [BD1080p][Hi10][Dual][FA4B3F1B].mkv",
	"Love Chunibyo Other Delusions 2014 S02E01-[1080p][BDRIP][x265.OPUS].mkv",
	"Love,.Chunibyo.&.Other.Delusions!.S02E01.1080p.Blu-Ray.10-Bit.Dual-Audio.DTS-HD.x265-iAHD.mkv",
	"[HorribleSubs] Chuunibyou demo Koi ga Shitai! Ren - 13 - The Rikka Wars (BD 1920x1080 FLAC) [F3075E98].mkv",
	"[Yousei-raws] Chuunibyou demo Koi ga Shitai! Ren 01 [BDrip 1920x1080 x264 FLAC].mkv",
	"Love, Chunibyo and Other Delusions! - S02E01v2 [BD Remux 1080p FLAC] [Dual Audio] [uba].mkv",
	"Love, Chunibyo and Other Delusions! - S02E01 [BD Remux 1080p FLAC] [Dual Audio] [uba].mkv",
	"[Exiled-Destiny]_Love,_Chunibyo_And_Other_Delusions!_Heart_Throb_Ep01_(032F5ECB).mkv",
	"[FFF] Chuunibyou demo Koi ga Shitai! - 13 (OVA) - Kirameki no... Slapstick Noel.mkv",
	"[Yousei-raws] Chuunibyou demo Koi ga Shitai! Ren (Kotatsu DE 1) [BDrip 1920x1080 x264 FLAC].mkv",
	"[anti-raws]Chuunibyou demo Koi ga Shitai! Ren ep.01[BDRemux].mkv",
	"[HorribleSubs] Chuunibyou demo Koi ga Shitai! Ren - 01 [1080p].mkv",
	"Love Chunibyo Other Delusions 2014 S02E01-[1080p][BDRIP][AV1.OPUS].mkv",
	"[Winter] Chuunibyou demo Koi ga Shitai! Ren 01 [BDrip 1280x720 x264 Vorbis].mkv",
	"Chuunibyou_demo_Koi_ga_Shitai!_Ren_[01]_[720p_x264_aac]_[Zendos_&_Eladiel].mkv",
	"[SilverEye] Chuunibyou Demo Koi Ga Shitai! Ren - 13 [BD 10bit 720p] [F123BDAC].mkv",
	"[SilverEye] Chuunibyou Demo Koi Ga Shitai! Ren - 01 [BD 10bit 720p] [BD81E83B].mkv",
	"[SilverEye] Chuunibyou Demo Koi Ga Shitai! Ren - SP_01 [BD 10bit 720p] [6D562CE3].mkv",
	"[HorribleSubs] Chuunibyou demo Koi ga Shitai! Ren - 01 [720p].mkv",
	"[SHIZA Project] Chuunibyou demo Koi ga Shitai! Ren TV2 [01] [ShadMG & Lizaveta].mkv",
}

func TestNextSeasonPicks(t *testing.T) {
	aired := "2012-12-20T00:00:00.000Z"
	q := &stremio.EpQueue{Show: stremio.Meta{Name: "Love, Chunibyo & Other Delusions!", Source: "anime"}, Season: 1, Index: 0,
		Episodes: []stremio.Video{{ID: "s1e12", Season: 1, Episode: 12, Released: aired}},
		All: []stremio.Video{
			{ID: "s1e12", Season: 1, Episode: 12, Released: aired},
			{ID: "s2e1", Season: 2, Episode: 1, Released: "2014-01-09T00:00:00.000Z"},
			{ID: "s2e2", Season: 2, Episode: 2, Released: "2014-01-16T00:00:00.000Z"},
		}}
	cfg := store.Config{NextSeason: true}
	for prev, want := range map[string]string{
		"[Judas] Chuunibyou S1 - 12.mkv": "[Judas] Chuunibyou S2 - 01.mkv",
		"[YURI] Love, Chunibyo & Other Delusions! S01E12 [BD 1080p x264 10bit FLAC]v2.mkv":         "[YURI] Love, Chunibyo & Other Delusions! S02E01 [BD 1080p x264 10bit FLAC].mkv",
		"Love.Chunibyo.and.Other.Delusions.S01E12.1080p.BluRay.MULTi.Opus2.0.x265-Fuchs.mkv":       "Love.Chunibyo.and.Other.Delusions.S02E01.1080p.BluRay.MULTi.Opus2.0.x264-Fuchs.mkv",
		"Chuunibyou demo Koi ga Shitai! - s01e12.mkv":                                              "Chuunibyou demo Koi ga Shitai! Ren s02e01.mkv",
		"[matheousse] Chuunibyou demo Koi ga Shitai! - 12 VOSTFR v2 [BD 1080p AAC] [ABCDEF12].mkv": "[matheousse] Chuunibyou demo Koi ga Shitai! Ren - 01 VOSTFR v2 [BD 1080p AAC] [83F22EEB].mkv",
		"Love Chunibyo Other Delusions 2012 S01E12-[1080p][BDRIP][x265.OPUS].mkv":                  "Love Chunibyo Other Delusions 2014 S02E01-[1080p][BDRIP][x265.OPUS].mkv",
		"Love Chunibyo Other Delusions 2012 S01E12-[1080p][BDRIP][AV1.OPUS].mkv":                   "Love Chunibyo Other Delusions 2014 S02E01-[1080p][BDRIP][AV1.OPUS].mkv",
		"[Yousei-raws] Chuunibyou demo Koi ga Shitai! 12 [BDrip 1920x1080 x264 FLAC][rev].mkv":     "[Yousei-raws] Chuunibyou demo Koi ga Shitai! Ren 01 [BDrip 1920x1080 x264 FLAC].mkv",
		"[Winter] Chuunibyou demo Koi ga Shitai! 12 [BDrip 1280x720 x264 Vorbis].mkv":              "[Winter] Chuunibyou demo Koi ga Shitai! Ren 01 [BDrip 1280x720 x264 Vorbis].mkv",
		"[anti-raws]Chuunibyou demo Koi ga Shitai! ep.12[BDRemux].mkv":                             "[anti-raws]Chuunibyou demo Koi ga Shitai! Ren ep.01[BDRemux].mkv",
		"[Exiled-Destiny]_Love,_Chunibyo_And_Other_Delusions!_Ep12_(A79E4447).mkv":                 "[Exiled-Destiny]_Love,_Chunibyo_And_Other_Delusions!_Heart_Throb_Ep01_(032F5ECB).mkv",
	} {
		// A pack's wrong file can put the episode just played in the list.
		list := append([]string{prev}, chuuniS2E1...)
		st, ok := pickStream(torrentio(list), player.PlayRequest{Queue: q, Release: prev}, cfg)
		if got := st.BehaviorHints.Filename; !ok || got != want {
			t.Errorf("after %q\n   picked %q (ok=%v)\n   wanted %q", prev, got, ok, want)
		}
	}
}

func TestNextSeasonKeepsContinuedNumbering(t *testing.T) {
	aired := "2020-01-01T00:00:00.000Z"
	q := &stremio.EpQueue{Season: 1, Episodes: []stremio.Video{{ID: "a", Season: 1, Episode: 12, Released: aired}},
		All: []stremio.Video{{ID: "a", Season: 1, Episode: 12, Released: aired}, {ID: "b", Season: 2, Episode: 1, Released: aired}}}
	prev := "Show.S01E12.1080p.WEB-DL.x264-GRP.mkv"
	st, ok := pickStream(torrentio([]string{"Show.S01E12.1080p.WEB-DL.x264-GRP.mkv", "Show.S01E13.1080p.WEB-DL.x264-GRP.mkv"}),
		player.PlayRequest{Queue: q, Release: prev}, store.Config{NextSeason: true})
	if got := st.BehaviorHints.Filename; !ok || got != "Show.S01E13.1080p.WEB-DL.x264-GRP.mkv" {
		t.Fatalf("picked %q", got)
	}
}

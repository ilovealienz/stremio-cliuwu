package ui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"stremio-cliuwu/internal/player"
	"stremio-cliuwu/internal/store"
	"stremio-cliuwu/internal/stremio"
)

// Torrentio results for Carrie S01E01 and S01E02, in list order, as they
// came back from a real search.
var carrieE01 = []string{
	"Carrie.S01E01.FIRST.PERIOD.2160p.AMZN.WEB-DL.DDP5.1.Atmos.DV.HDR.H.265-FLUX.mkv",
	"Carrie.S01E01.First.Period.AMZN.DV.HDR.WEB-DL.2160p.by.AKTEP.mkv",
	"Carrie.S01E01.2160p.WEBRip.DV.HDR.x265-KONTRAST.mkv",
	"Carrie S01E01 FIRST PERIOD 2160p AMZN WEB-DL DDP5 1 Atmos HDR H 265-RAWR.mkv",
	"Carrie.S01E01.MENARCA.2160p.AMZN.WEB-DL.ITA.ENG.DDP5.1.DV.HDR.H.265-G66.mkv",
	"Carrie S01E01 FIRST PERIOD REPACK 2160p AMZN WEB-DL DDP5 1 Atmos DV H 265-RAWR.mkv",
	"Carrie.S01E01.MULTi.2160p.AMZN.WEB-DL.DV.HDR.H265.DDP5.1.Atmos-K83.mkv",
	"Carrie_S01E01 4K CZ dab.mkv",
	"Carrie.2026.S01E01.MULTi.VF2.2160p.WEB.HDR10.H265.E-AC3-AKM.mkv",
	"Carrie S01E01 FIRST PERIOD 2160p AMZN WEB-DL DD 5 1 Atmos HDR H 265-playWEB.mkv",
	"Carrie S01E01 FIRST PERIOD 2160p AMZN WEB-DL DDP5 1 Atmos DV HDR H 265-FLUX.mkv",
	"Carrie.2026.S01E01.2160p.AMZN.WEB-DL.DV.HDR10+[Ben The Men].mp4",
	"Carrie S01E01 2160p AMZN WEB-DL DDP5 1 Atmos DV HDR H 265 DUAL-BiOMA.mkv",
	"Carrie S01E01 FIRST PERIOD 2160p AMZN WEB-DL DDP5 1 Atmos DoVi H 265-playWEB.mkv",
	"Carrie.S01E01.First.Period.AMZN.SDR.WEB-DL.2160p.by.AKTEP.mkv",
	"Carrie S01E01 First Period 2160p AMZN WEB-DL DDP5 1 Atmos H 265-playWEB.mkv",
	"Carrie S01E01 FIRST PERIOD 2160p AMZN WEB-DL DDP5 1 Atmos H 265-RAWR.mkv",
	"Carrie [4k 2160p][Cap.101].mkv",
	"Carrie S01E01 First Period 2160p AMZN WEB-DL DDP5 1 Atmos H 265-FLUX.mkv",
	"Carrie S01 E01.mkv",
	"Carrie.S01E01.1080p.10bit.WEBRip.6CH.x265.HEVC-PSA.mkv",
	"Carrie.S01E01.FIRST.PERIOD.1080p.AMZN.WEB-DL.DDP5.1.ENG.Atmos.ITA.H265-TBK.mkv",
	"carrie.s01e01.1080p.web.h264-cakes[EZTVx.to].mkv",
	"Carrie.S01E01.FIRST.PERIOD.1080p.WEBRip.10Bit.DDP5.1.x265-NeoNoir.mkv",
	"Carrie.S01E01.1080p.HEVC.x265-MeGusta[EZTVx.to].mkv",
	"Carrie (2026) - S01E01 - FIRST PERIOD (1080p AMZN WEB-DL x265 Ghost).mkv",
	"Carrie.A.Estranha.S01E01.WEB-DL.1080p.x264.DUAL.5.1-STARCKFILMES.mkv",
	"Carrie.S01E01.FIRST.PERIOD.mkv",
	"Carrie_S01E01 CZ dab.mkv",
	"Carrie.2026.S01E01.NOUVELLES.REGLES.MULTi.1080p.AMZN.WEB.H264-BYOR.mkv",
	"Carrie.S01E01.FIRST.PERIOD 1080p AMZN WEB-DL DDP5 1 Atmos H 264-FLUX_Morgann23.mkv",
	"carrie.s01e01.1080p.web.h264-RGR.mkv",
	"Carrie.S01E01.FIRST.PERIOD.1080p.ITA-ENG.MULTI.WEBRip.x265.AAC-V3SP4EV3R.mkv",
	"Carrie.1x01.Menarca.ITA.ENG.1080p.AMZN.WEBRip.AAC.x265-Pir8.mkv",
	"Carrie.S01E01.Menarca.ITA.ENG.1080p.AMZN.WEB-DL.DDP5.1.H.264-MeM.GP.mkv",
	"Carrie.S01E01.2026.WEB-DL.1080p-Dual-Lat.mkv",
	"Carrie.S01E01.1080p.WEBRip.x265-KONTRAST.mkv",
	"Carrie.S01E01.MENARCA.1080p.AMZN.WEB-DL.ITA.ENG.DDP5.1.H.264-G66.mkv",
}

var carrieE02 = []string{
	"Carrie.S01E02.FOREIGN.LANGUAGE.2160p.AMZN.WEB-DL.DDP5.1.Atmos.DV.HDR.H.265-FLUX.mkv",
	"Carrie.S01E02.Foreign.Language.AMZN.DV.HDR.WEB-DL.2160p.by.AKTEP.mkv",
	"Carrie.S01E02.2160p.WEBRip.DV.HDR.x265-KONTRAST.mkv",
	"Carrie S01E02 FOREIGN LANGUAGE 2160p AMZN WEB-DL DDP5 1 Atmos DV HDR H 265-FLUX.mkv",
	"Carrie.S01E02.LINGUA.STRANIERA.2160p.AMZN.WEB-DL.ITA.ENG.DDP5.1.DV.HDR.H.265-G66.mkv",
	"Carrie.S01E02.MULTi.2160p.AMZN.WEB-DL.DV.HDR.H265.DDP5.1.Atmos-K83.mkv",
	"Carrie_S01E02 4K CZ dab.mkv",
	"Carrie S01E02 Foreign Language 2160p AMZN WEB-DL DD5 1 Atmos HDR H 265-playWEB.mkv",
	"Carrie.2026.S01E02.MULTi.VF2.2160p.WEB.HDR10.H265.E-AC3-AKM.mkv",
	"Carrie.2026.S01E02.2160p.AMZN.WEB-DL.DV.HDR10+[Ben The Men].mp4",
	"Carrie S01E02 2160p AMZN WEB-DL DDP5 1 Atmos DV HDR H 265 DUAL-BiOMA.mkv",
	"Carrie S01E02 FOREIGN LANGUAGE 2160p AMZN WEB-DL DDP5 1 Atmos HDR H 265-RAWR.mkv",
	"Carrie S01E02 FOREIGN LANGUAGE 2160p AMZN WEB-DL DDP5 1 Atmos DoVi H 265-playWEB.mkv",
	"Carrie S01E02 FOREIGN LANGUAGE 2160p AMZN WEB-DL DD 5 1 Atmos HDR H 265-playWEB.mkv",
	"Carrie.S01E02.Foreign.Language.AMZN.SDR.WEB-DL.2160p.by.AKTEP.mkv",
	"Carrie S01E02 Foreign Language 2160p AMZN WEB-DL DDP5 1 Atmos H 265-playWEB.mkv",
	"Carrie [4k 2160p][Cap.102].mkv",
	"Carrie S01E02 Foreign Language 2160p AMZN WEB-DL DDP5 1 Atmos H 265-FLUX.mkv",
	"Carrie S01 E02.mkv",
	"Carrie.S01E02.1080p.10bit.WEBRip.6CH.x265.HEVC-PSA.mkv",
	"Carrie.S01E02.FOREIGN.LANGUAGE.1080p.AMZN.WEB-DL.DDP5.1.ENG.Atmos.ITA.H265-TBK.mkv",
	"Carrie.S01E02.1080p.HEVC.x265-MeGusta[EZTVx.to].mkv",
	"Carrie.S01E02.FOREIGN.LANGUAGE.1080p.WEBRip.10Bit.DDP5.1.x265-NeoNoir.mkv",
	"Carrie (2026) - S01E02 - FOREIGN LANGUAGE (1080p AMZN WEB-DL x265 Ghost).mkv",
	"carrie.s01e02.1080p.web.h264-cakes[EZTVx.to].mkv",
	"Carrie.A.Estranha.S01E02.WEB-DL.1080p.x264.DUAL.5.1-STARCKFILMES.mkv",
	"Carrie.S01E02.FOREIGN.LANGUAGE.mkv",
	"Carrie S01E02 FOREIGN LANGUAGE.mkv",
	"Carrie_S01E02 CZ dab.mkv",
	"Carrie.2026.S01E02.LANGUE.ETRANGERE.MULTi.1080p.AMZN.WEB.H264-BYOR.mkv",
	"Carrie.S01E02.FOREIGN.LANGUAGE 1080p AMZN WEB-DL DDP5 1 Atmos H 264-FLUX_Morgann23.mkv",
	"carrie.s01e02.1080p.web.h264-RGR.mkv",
	"Carrie.S01E02.FOREIGN.LANGUAGE.1080p.ITA-ENG.MULTI.WEBRip.x265.AAC-V3SP4EV3R.mkv",
	"Carrie.1x02.Lingua.Straniera.ITA.ENG.1080p.AMZN.WEBRip.AAC.x265-Pir8.mkv",
	"Carrie.S01E02.Lingua.Straniera.ITA.ENG.1080p.AMZN.WEB-DL.DDP5.1.H.264-MeM.GP.mkv",
	"carrie.s01e02.1080p.web.h264-cakes.mkv",
	"Carrie.S01E02.2026.WEB-DL.1080p-Dual-Lat.mkv",
	"Carrie.S01E02.1080p.WEBRip.x265-KONTRAST.mkv",
	"Carrie.S01E02.LINGUA.STRANIERA.1080p.AMZN.WEB-DL.ITA.ENG.DDP5.1.H.264-G66.mkv",
	"Carrie, a Estranha S01E02 WEB-DL 1080p x265 DUAL 5.1.mkv",
	"Carrie_S01E02 [1080p.WEB-DL.H265.EAC3.5.1..AS76-FT] [ENG-Lektor PL] [Alusia].mkv",
}

func torrentio(names []string) []stremio.Stream {
	out := make([]stremio.Stream, len(names))
	for i, n := range names {
		st := stream("Torrentio TB", "4K ⚡", "")
		st.URL = "http://x/" + n
		st.BehaviorHints.Filename = n
		out[i] = st
	}
	return out
}

func TestRealCarrieAutoplay(t *testing.T) {
	aired := "2020-01-01T00:00:00.000Z"
	q := &stremio.EpQueue{Show: stremio.Meta{Name: "Carrie", Source: "show"}, Season: 1, Episodes: []stremio.Video{
		{ID: "c:1:1", Season: 1, Episode: 1, Title: "First Period", Released: aired},
		{ID: "c:1:2", Season: 1, Episode: 2, Title: "Foreign Language", Released: aired},
	}}
	next := torrentio(carrieE02)
	info := func(n, title string) stremio.ReleaseInfo { return stremio.ParseReleaseNameTitled(n, "Carrie", title) }
	for _, prevName := range carrieE01 {
		prev := player.PlayRequest{Queue: q, Release: prevName}
		st, ok := pickStream(next, prev, store.Config{})
		got := st.BehaviorHints.Filename
		if !ok {
			got = "(no pick: list opens)"
		}
		if testing.Verbose() {
			fmt.Printf("%-88s -> %s\n", prevName, got)
		}
		if !ok {
			continue
		}
		w, g := info(prevName, "First Period"), info(got, "Foreign Language")
		if m := w.Matches(g); !m[0] || w.Resolution != "" && !m[1] || !m[2] {
			t.Errorf("after %q picked %q: variant/language %v resolution %v hdr %v", prevName, got, m[0], m[1], m[2])
		}
		// The same group when it has a release for the next episode.
		if w.Group != "" && g.Group != w.Group {
			for _, n := range carrieE02 {
				if c := info(n, "Foreign Language"); c.Group == w.Group && w.Matches(c)[0] && w.Matches(c)[1] && w.Matches(c)[2] {
					t.Errorf("after %q picked %q, but %q is the same group", prevName, got, n)
					break
				}
			}
		}
	}
	if strings.Join(info("Carrie.S01E02.FOREIGN.LANGUAGE.2160p.AMZN.WEB-DL.DDP5.1.Atmos.DV.HDR.H.265-FLUX.mkv", "Foreign Language").Langs, ",") != "" {
		t.Error("the episode title Foreign Language read as a language")
	}
}

var reZeroE01 = []string{
	"[Feibanyama] ReZERO Starting Life in Another World S04E01 [IQIYI WebRip 2160p HEVC AAC Multi-Audio Multi-Subs].mkv",
	"[SubsPlease] Re Zero kara Hajimeru Isekai Seikatsu - 67 (1080p) [5BDCA671].mkv",
	"ReZERO.-Starting.Life.in.Another.World-.S04E01.1080p.CR.WEB-DL.DUAL.AAC2.0.H.264.MSubs-ToonsHub.mkv",
	"ReZero.kara.Hajimeru.Isekai.Seikatsu.S04E01.1080p.AMZN.WEB-DL.JPN.DDP2.0.H.264-ToonsHub.mkv",
	"[Judas] Re.Zero - S04E01v2.mkv",
	"[Erai-raws] Re Zero kara Hajimeru Isekai Seikatsu 4th Season - 01 [1080p CR WEBRip HEVC AAC][MultiSub][C86350AC].mkv",
	"[Erai-raws] Re Zero kara Hajimeru Isekai Seikatsu 4th Season - 01 [1080p CR WEB-DL AVC AAC][MultiSub][6DFEC430].mkv",
	"[BudLightSubs] ReZero S4 - 01 [1080p].mkv",
	"Re ZERO, Starting Life In Another World S04E01 MULTi 1080p WEB x264 AAC -Tsundere-Raws (CR).mkv",
	"[FLE] Re ZERO Starting Life in Another World - S04E01 (WEB 1080p HEVC E-AC-3) [Dual Audio] [5898F36C].mkv",
	"Re.ZERO.Starting.Life.in.Another.World.S04E01.MULTi.1080p.WEBRiP.x265-T3KASHi.mkv",
	"[ASW] Re Zero kara Hajimeru Isekai Seikatsu - 67 [1080p HEVC][933A01BC].mkv",
	"[DB]ReZero kara Hajimeru Isekai Seikatsu 4th Season_-_01_(Dual Audio_10bit_1080p_x265).mkv",
	"Re.ZERO.Starting.Life.in.Another.World.2016.S04E01.1080p.CR.WEB-DL.DUAL.DDP2.0.H.264-AnoZu.mkv",
	"[NOOBDLxFortunaTV]ReZero.kara.Hajimeru.Isekai.Seikatsu.S04E01.1080p.WEB-DL.x264.mkv",
	"Re.ZERO.Starting.Life.in.Another.World.S04E01.v2.1080p.WEBRip.Dual.Audio.AV1-Sokudo.mkv",
	"ReZero.Starting.Life.in.Another.World.S04E01.WEB-DL.1080p.AVC.AAC.Dual.Audio-FBI.mkv",
	"[EA]Re_Zero_kara_Hajimeru_Isekai_Seikatsu_4th_01_[1920x1080][HEVC][66BABFDF].mkv",
	"ReZERO.-Starting.Life.in.Another.World-.S04E01.A.Reason.to.Bring.You.1080p.BILI.WEB-DL.JPN.AAC2.0.H.265.MSubs-ToonsHub.mkv",
	"[Yameii] Re ZERO, Starting Life in Another World - S04E01 [English Dub] [CR WEB-DL 1080p H264 AAC] [1F569D92].mkv",
	"ReZERO.-Starting.Life.in.Another.World-.S04E01.1080p.CR.WEB-DL.MULTi.AAC2.0.H.264.MSubs-ToonsHub.mkv",
	"[Erai-raws] Re Zero kara Hajimeru Isekai Seikatsu 4th Season - 01 [1080p CR WEB-DL AVC AAC].mkv",
	"[DKB] ReZero kara Hajimeru Isekai Seikatsu - S04E01 [1080p][HEVC x265 10bit][Multi-Subs][4144BBA6].mkv",
	"[DKB] ReZero kara Hajimeru Isekai Seikatsu - S04E01 [1080p][HEVC x265 10bit][Dual-Audio][Multi-Subs][6B169BA2].mkv",
	"[Z-A] Re Zero kara Hajimeru Isekai Seikatsu - S04E01 (WEB 1080p AAC) [D6CC886B].mkv",
	"[FrixySubs] ReZero kara Hajimeru Isekai Seikatsu - S04E01 [1080p CR WEB-DL H.264 AAC] [Napisy PL].mkv",
	"Re ZERO -Starting Life in Another World- S04E01 1080p CR WEB-DL AAC2 0 H 264 DUAL-BiOMA[EZTVx.to].mkv",
	"[NanakoRaws] Re Zero Kara Hajimeru Isekai Seikatsu S04E01 (AT-X TV 1080p HEVC AAC).mkv",
	"Re.ZERO.Starting.Life.in.Another.World.S04E01.A.Reason.to.Bring.You.1080p.BILI.WEB-DL.AAC2.0.H.264-VARYG.mkv",
	"[Onalrie] Re Zero kara Hajimeru Isekai Seikatsu - S04E01 [1080p WEBRip AV1].mkv",
}

var aotE01 = []string{
	"[Salieri] Attack On Titan S1 - 01 (1080p) (HDR) [Dual Audio].mkv",
	"[Trix] Shingeki no Kyojin S01E01 (01) (BD 1080p AV1) [B77D7B1E].mkv",
	"[Anime Time] Attack on Titan - 01.mkv",
	"[SOFCJ-Raws] Shingeki no Kyojin - S1 - E01 [WEB-DL KP 1080p].mkv",
	"Attack.on.Titan.2013.S01E01.GERMAN.Dubbed.DL.1080p.BD.Opus.2.0.10bit.AV1-PHTMini.mkv",
	"[Judas] Shingeki no Kyojin S1 - 01.mkv",
	"Attack on Titan (2013) - S01E01 - To You, in 2000 Years - The Fall of Shiganshina, Part 1 (1080p BluRay x265 Garshasp).mkv",
	"Attack.on.Titan.S01E01.REPACK.1080p.BluRay.x265.Opus.2.0-ZeroBuild.mkv",
	"[DiabloTripleA] Attack on Titan - S01E01.mkv",
	"[TatakaeFuniSubs] Attack on Titan - S01E01 (BD 1080p AV1) [Dual Audio] [9E1B86EB].mkv",
	"[224] Shingeki no Kyojin - S01 - 01 [BDRip.1080p.x265.FLAC].mkv",
	"[MiniMTBB] Shingeki no Kyojin - 01 (BD 1080p) [25EBA5CB].mkv",
	"Attack.on.Titan.S01E01.1080p.BluRay.MULTi.AV1.OPUS.2.0-GapMoe.mkv",
	"[pseudo] Attack on Titan S01E01 To You, Two Thousand Years Later [1080p] [h.265].mkv",
	"Attack.on.titan.s01e01.1080p.amzn.web-dl.ddp2.0.h.264-Full4Movies.mkv",
	"[TatakaeSubs] Attack on Titan - S01E01 (BD 1080p HEVC OPUS) [21D9AE40].mkv",
	"[SCY] Attack on Titan - 01v2 (BD 1080p Hi10 FLAC) [CBD993E4].mkv",
	"[NewbSubs] Shingeki no Kyojin 01 (1080p Blu-ray x265 Dual Audio)[19D26FFB].mkv",
	"[Erai-raws] Shingeki no Kyojin - 01 [1080p][Multiple Subtitle][C46791CC].mkv",
	"Attack.on.Titan.S01E01.To.You.in.2.000.Years.The.Fall.of.Shiganshina.1.1080p.FLAC.2.0.AVC.REMUX-FraMeSToR.mkv",
	"Attack.on.Titan.2013.S01E01.GERMAN.Dubbed.DL.1080p.BD.Opus.2.0.10bit.x265-PHTM.mkv",
	"[Xspitfire911] Shingeki No Kyojin S01E01 BDRIP 1080p X265 10bit Vostfr.mkv",
	"Attack.on.Titan.S01E01.1080p.BluRay.x265.Dual.Audio-KONTRAST.mp4",
	"Attack on Titan - Episode 01 - To You, in 2000 Years - The Fall of Shiganshina, Part 1 1080p Hybrid ITA BDRip x265 FLAC 2.0 Kira [SEV].mkv",
}

var rickE05 = []string{
	"Rick.and.Morty.S05E05.Amortycan.Grickfitti.2160p.BluRay.AI.AV1.DTS-HD.MA.5.1-Enrav1sh.mkv",
	"Rick and Morty - S05E05 - Amortycan Grickfitti.mkv",
	"Rick.and.Morty.S05E05.1080p.BluRay.x265-RARBG.mp4",
	"Rick.and.Morty.S05E05.1080p.AMZN.WEB-DL.AAC2.0.H.264-EniaHD.mkv",
	"Rick and Morty (2013) - S05E05 - Amortycan Grickfitti [Bluray-1080p][Opus 5.1][x265]-FraMeSToR.mkv",
	"Rick.and.Morty.S05E05.WEB-DL.1080p.RGzsRutracker.mkv",
	"Rick.and.Morty.S05E05.1080p.BluRay.x265-KONTRAST.mkv",
	"Rick.and.Morty.S05E05.Amortycan.Grickfitti.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb.mkv",
	"Rick.and.Morty.S05E05.Amortycan.Grickfitti.1080p.BluRay.DDP.5.1.x265-edge2020.mkv",
	"Rick.and.Morty.S05E05.Amortycan.Grickfitti.1080p.WEBRip.6CH.x265.HEVC-PSA.mkv",
	"Rick.and.Morty.S05E05.MULTi.1080p.BluRay.x264-SODAPOP.mkv",
	"Rick.And.Morty.S05E05.1080p.Bluray.NVENC.x264-SURGE.mkv",
	"Rick.and.Morty.S05E05.Amortycan.Grickfitti.Bluray.AV1.Opus-dAV1nci.mkv",
	"Rick.and.Morty.S05E05.Amortycan.Grickfitti.1080p.BluRay.Remux.DTS-HD.MA5.1.H.264-BTN.mkv",
	"Rick.and.Morty.S05E05.1080p.3D.FULL-SBS.HEVC.SBS-SUB.ENG-HUN.iFA-AI3D.mkv",
	"Rick.and.Morty.S05E05.MULTi.1080p.MAX.WEB-DL.DDP5.1.H.265-KZS.mkv",
	"Rick and Morty (2013) - S05E05 - Amortycan Grickfitti (1080p BluRay x265 Garshasp).mkv",
	"rick.and.morty.s05e05.1080p.webrip.x264-cakes.mkv",
	"Rick.and.Morty.S05E05.MULTi.1080p.WEB-DL.x265-STEGNER.mkv",
	"Rick i Morty_S05E05 [1080p.WEB-DL.H265.AC3-FT] [Napisy ENG-PL] [Dubbing PL] [Alusia].mkv",
	"Rick.and.Morty.S05E05.1080p.HEVC.x265-MeGusta[eztv.re].mkv",
	"[S5.Ep5] Rick and Morty - Amortycan Grickfitti.mkv",
	"Rick.and.Morty.S05E05.1080p.BluRay.x264-BORDURE.mkv",
}

var nextEp = regexp.MustCompile(`(?i)(s\d{1,2}e|s\d - e?|- |_|\be|\bep|\.ep|kyojin |seikatsu |s4 - |episode )(0?1|0?5|67)(v\d|[^0-9]|$)`)

// bump turns an episode's release name into the next one's.
func bump(name string) string {
	return nextEp.ReplaceAllStringFunc(name, func(m string) string {
		sub := nextEp.FindStringSubmatch(m)
		n := map[string]string{"01": "02", "1": "2", "05": "06", "5": "6", "67": "68"}[sub[2]]
		return sub[1] + n + sub[3]
	})
}

func TestRealNamesAutoplay(t *testing.T) {
	aired := "2020-01-01T00:00:00.000Z"
	for _, c := range []struct {
		show, source string
		names        []string
	}{
		{"Re:Zero - Starting Life in Another World", "anime", reZeroE01},
		{"Attack on Titan", "anime", aotE01},
		{"Rick and Morty", "show", rickE05},
	} {
		q := &stremio.EpQueue{Show: stremio.Meta{Name: c.show, Source: c.source}, Season: 1, Episodes: []stremio.Video{
			{ID: "x:1", Episode: 1, Released: aired}, {ID: "x:2", Episode: 2, Released: aired},
		}}
		var nextNames []string
		for _, n := range c.names {
			if b := bump(n); b == n {
				t.Fatalf("couldn't make the next episode's name from %q", n)
			} else {
				nextNames = append(nextNames, b)
			}
		}
		next := torrentio(nextNames)
		misses := 0
		for i, prevName := range c.names {
			st, ok := pickStream(next, player.PlayRequest{Queue: q, Release: prevName}, store.Config{})
			if !ok || st.BehaviorHints.Filename != nextNames[i] {
				misses++
				got := st.BehaviorHints.Filename
				if !ok {
					got = "(no pick)"
				}
				t.Errorf("%s: after %q\n   picked %q\n   wanted %q", c.show, prevName, got, nextNames[i])
			}
		}
		if testing.Verbose() {
			fmt.Printf("%s: %d of %d picked their own next episode\n", c.show, len(c.names)-misses, len(c.names))
		}
	}
}

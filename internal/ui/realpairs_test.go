package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"stremio-cliuwu/internal/player"
	"stremio-cliuwu/internal/store"
	"stremio-cliuwu/internal/stremio"
)

// Real Torrentio, AIOStreams and MediaFusion results for two consecutive
// episodes, in list order.
var realPairs = []struct {
	show, source   string
	season, ep     int // the first episode
	title1, title2 string
	aired1, aired2 string
	first, second  []string
}{
	{"Love, Chunibyo & Other Delusions!", "anime", 1, 1, "", "", "2012-10-04", "2012-10-11", []string{
		"[Judas] Chuunibyou S1 - 01.mkv",
		"Love.Chunibyo.and.Other.Delusions.S01E01.1080p.BluRay.Opus2.0.H.265-LYS1TH3A.mkv",
		"[YURI] Love, Chunibyo & Other Delusions! S01E01 [BD 1080p x264 10bit FLAC]v2.mkv",
		"Love.Chunibyo.and.Other.Delusions.S01E01.1080p.BluRay.MULTi.Opus2.0.x265-Fuchs.mkv",
		"Chuunibyou demo Koi ga Shitai! - s01e01.mkv",
		"[matheousse] Chuunibyou demo Koi ga Shitai! - 01 VOSTFR v2 [BD 1080p AAC] [17A8A4EB].mkv",
		"Love.Chunibyo.and.Other.Delusions.S01E01.1080p.BluRay.Opus2.0.H.265-SNEEDESiUM.mkv",
		"[coldhell]_Chu2byo_Lite_01_[URW][9916EEC8].mkv",
		"[MK] Love, Chunibyo & Other Delusions! - 01v2 [BD 1080p][Hi10][Dual][8A2E0AAC].mkv",
		"[Kametsu] Love, Chunibyo & Other Delusions-Heart Throb - 01v2 [BD1080p][Hi10][Dual][8CD6B0D6].mkv",
		"Love Chunibyo Other Delusions 2012 S01E01-[1080p][BDRIP][x265.OPUS].mkv",
		"[FFF] Chuunibyou demo Koi ga Shitai! - 01 [BD][1080p-FLAC][6BF4B9D1].mkv",
		"[HorribleSubs] Chuunibyou demo Koi ga Shitai! Ren - 01 [1080p].mkv",
		"Love Chunibyo Other Delusions 2012 S01E01-[1080p][BDRIP][AV1.OPUS].mkv",
		"[MTBB] Chuunibyou demo Koi ga Shitai! - 01 [90408C52].mkv",
		"[t0r] Chuunibyou demo Koi ga Shitai! - Episode 01 (BDRip Dual Audio x264).mkv",
		"[Exiled-Destiny]_Love,_Chunibyo_And_Other_Delusions!_Ep01_(A79E4447).mkv",
		"Chuunibyou demo Koi ga Shitai! Ren Lite - 01.mkv",
		"[Yousei-raws] Chuunibyou demo Koi ga Shitai! 01 [BDrip 1920x1080 x264 FLAC][rev].mkv",
		"[MiniMTBB] Chuunibyou demo Koi ga Shitai! - 01 [9DFB13F1].mkv",
		"[anti-raws]Chuunibyou demo Koi ga Shitai! ep.01[BDRemux].mkv",
		"[Winter] Chuunibyou demo Koi ga Shitai! 01 [BDrip 1280x720 x264 Vorbis].mkv",
		"[DB]Love, Chunibyo & Other Delusions!_Heart Throb-_01_(Dual Audio_10bit_BD720p_x265).mkv",
		"Chuunibyou Demo Koi ga Shitai! - 01.mkv",
		"[OpenDub]_Chuunibyou_demo_Koi_ga_Shitai_[01]_[RUS_JAP]_[1280x720_x264_AAC]_[Mistake_&_Vina].mkv",
		"[AniFilm] Chuunibyou demo Koi ga Shitai! [TV][01 of 12][HDTVRip 1280x720 x264][Ru Jp][DemonOFmooN & MezIdA].mkv",
		"[DB]Love, Chunibyo & Other Delusions!_-_01_(Dual Audio_10bit_BD720p_x265).mkv",
		"[FTW]_Chuunibyou_demo_Koi_ga_Shitai!_Ren_-_01_[720p][9172389F].mkv",
		"[URW]_Chuunibyou_demo_Koi_ga_Shitai!_-_01_[720p][64775D00].mkv",
		"[RG Lucky Clover] Chuunibyou demo Koi ga Shitai! 01 [BDrip 852x480 x264 Vorbis].mkv",
		"[Commie] Chuunibyou Demo Koi ga Shitai! Ren - 01v2 [992F7420].mkv",
		"[gg]_Chuunibyou_Demo_Koi_ga_Shitai!_-_01_[5B6EFD1F].mkv",
	}, []string{
		"[Judas] Chuunibyou S1 - 02.mkv",
		"Love.Chunibyo.and.Other.Delusions.S01E02.1080p.BluRay.Opus2.0.H.265-LYS1TH3A.mkv",
		"[YURI] Love, Chunibyo & Other Delusions! S01E02 [BD 1080p x264 10bit FLAC]v2.mkv",
		"Love.Chunibyo.and.Other.Delusions.S01E02.1080p.BluRay.MULTi.Opus2.0.x265-Fuchs.mkv",
		"Chuunibyou demo Koi ga Shitai! - s01e02.mkv",
		"[matheousse] Chuunibyou demo Koi ga Shitai! - 02 VOSTFR v2 [BD 1080p AAC] [E0768394].mkv",
		"Love.Chunibyo.and.Other.Delusions.S01E02.1080p.BluRay.Opus2.0.H.265-SNEEDESiUM.mkv",
		"[MK] Love, Chunibyo & Other Delusions! - 02v2 [BD 1080p][Hi10][Dual][2652867E].mkv",
		"[coldhell]_Chu2byo_Lite_02_[URW][A5C3F69F].mkv",
		"[Kametsu] Love, Chunibyo & Other Delusions-Heart Throb - 02v2 [BD1080p][Hi10][Dual][BE335A93].mkv",
		"Love Chunibyo Other Delusions 2012 S01E02-[1080p][BDRIP][x265.OPUS].mkv",
		"[HorribleSubs] Chuunibyou demo Koi ga Shitai! Ren - 02 [1080p].mkv",
		"[FFF] Chuunibyou demo Koi ga Shitai! - 02 [BD][1080p-FLAC][7F54F36C].mkv",
		"Love Chunibyo Other Delusions 2012 S01E02-[1080p][BDRIP][AV1.OPUS].mkv",
		"[MTBB] Chuunibyou demo Koi ga Shitai! - 02 [14145DEF].mkv",
		"[t0r] Chuunibyou demo Koi ga Shitai! - Episode 02 (BDRip Dual Audio x264).mkv",
		"[Exiled-Destiny]_Love,_Chunibyo_And_Other_Delusions!_Ep02_(469DC958).mkv",
		"Chuunibyou demo Koi ga Shitai! Ren - Special 2.mkv",
		"Chuunibyou demo Koi ga Shitai! Ren Lite - 02.mkv",
		"[Beatrice-Raws] To LOVE-Ru Trouble Darkness 02 [BDRip 1920x1080 x264 FLAC].mkv",
		"[Yousei-raws] Chuunibyou demo Koi ga Shitai! 02 [BDrip 1920x1080 x264 FLAC][rev].mkv",
		"To Love-Ru Trouble - Darkness - 02 [BDRemux 1920x1080 x264 PCM].mkv",
		"[MiniMTBB] Chuunibyou demo Koi ga Shitai! - 02 [8ACE4A4F].mkv",
		"[anti-raws]To Love-Ru Trouble - Darkness OAD ep.2[BDRemux].mkv",
		"[anti-raws]Chuunibyou demo Koi ga Shitai! ep.02[BDRemux].mkv",
		"[Winter] Chuunibyou demo Koi ga Shitai! 02 [BDrip 1280x720 x264 Vorbis].mkv",
		"[Winter] To Love-Ru Trouble - Darkness 02 [BDrip 1280x720 x264 Vorbis].mkv",
		"[DB]Love, Chunibyo & Other Delusions!_Heart Throb-_02_(Dual Audio_10bit_BD720p_x265).mkv",
		"Chuunibyou Demo Koi ga Shitai! - 02.mkv",
		"[OpenDub]_Chuunibyou_demo_Koi_ga_Shitai_[02]_[RUS_JAP]_[1280x720_x264_AAC]_[Mistake_&_Vina].mkv",
		"Chuunibyou demo Koi ga Shitai! - 02.mkv",
		"[AniFilm] Chuunibyou demo Koi ga Shitai! [TV][02 of 12][HDTVRip 1280x720 x264][Ru Jp][DemonOFmooN & MezIdA].mkv",
		"To Love-ru Darkness - 02.mkv",
		"[DB]Love, Chunibyo & Other Delusions!_-_02_(Dual Audio_10bit_BD720p_x265).mkv",
		"[URW]_Chuunibyou_demo_Koi_ga_Shitai!_-_02_[720p][EB39C0D3].mkv",
		"[Ohys-Raws] To Love-Ru Trouble - Darkness OAD 02 [DVDRip] [576p].mp4",
		"[RG Lucky Clover] Chuunibyou demo Koi ga Shitai! 02 [BDrip 852x480 x264 Vorbis].mkv",
		"[gg]_Chuunibyou_Demo_Koi_ga_Shitai!_-_02_[D9C76A37].mkv",
		"Love,.Chunibyo.&.Other.Delusions!.S01E02.1080p.Blu-Ray.10-Bit.Dual-Audio.DTS-HD.x265-iAHD.mkv",
	}},
	{"Below", "show", 1, 1, "Come Home Year", "Crown and Anchor", "2026-06-01", "2026-06-08", []string{
		"Below.S01E01.2160p.NF.WEB-DL.DV.HDR.H.265.mkv",
		"Below.S01E01.2160p.NF.WEB-DL.DV.HDR[Ben The Men].mp4",
		"below.s01e01.dv.2160p.web.h265-croissants.mkv",
		"Below.S01E01.MULTi.2160p.NF.WEB-DL.DV.HDR.H265.DDP5.1.Atmos-K83.mkv",
		"Below.S01E01.Lanno.del.ritorno.a.casa.2160p.NF.WEB-DL.ITA.ENG.DDP5.1.DV.HDR.H.265-G66.mkv",
		"Below.S01E01.2160p.WEBRip.DV.HDR.x265-KONTRAST.mkv",
		"Below S01E01 Come Home Year 2160p NF WEB-DL DDP5 1 Atmos DV HDR H 265-FLUX.mkv",
		"Below S01E01 Come Home Year 2160p NF WEB-DL DDP5 1 Atmos DV HDR H 265-XEBEC.mkv",
		"Below S01E01 Come Home Year 2160p NF WEB-DL DDP5 1 Atmos DV HDR H 265-Kitsune.mkv",
		"Below S01E01 Come Home Year 2160p NF WEB-DL DD 5 1 Atmos H 265-playWEB.mkv",
		"Below.S01E01.2160p.SDR.mkv",
		"Below S01E01 Come Home Year 2160p NF WEB-DL DDP5 1 Atmos H 265-XEBEC.mkv",
		"Below S01E01 Come Home Year 1080p WEBRip HDR DDP5 1 H265-d3g[EZTVx.to].mkv",
		"Below.S01E01.MULTi.1080p.NF.WEB-DL.DV.HDR.H265.DDP5.1.Atmos-K83.mkv",
		"Below.S01E01.Come.Home.Year.1080p.WEBRip.10Bit.DDP5.1.x265-NeoNoir.mkv",
		"Below.S01E01.Come.Home.Year.1080p.NF.WEB-DL.H.264-EniaHD.mkv",
		"Below (2026) - S01E01 - Come Home Year (1080p NF WEB-DL x265 Silence).mkv",
		"Below S01 E01.mkv",
		"Profundezas S01E01 WEB-DL 1080p x265 DUAL 5.1.mkv",
		"Below.S01E01.MULTi.1080p.WEB.x264-GL0P.mkv",
		"Below.S01E01.Come.Home.Year.1080p.NF.WEB-DL.ENG.Atmos.ITA.DDP5.1.H264-TBK.mkv",
		"Below.S01E01.Come.Home.Year.1080p.NF.WEB-DL.DDP5.1.Atmos.H.264-FLUX.mkv",
		"Below.1x01.L.Anno.Del.Ritorno.A.Casa.ITA.ENG.1080p.NFRip.AAC.x265-Pir8.mkv",
		"Below.S01E01.Come.Home.Year.1080p.ITA-ENG.MULTI.WEBRip.x265.AAC-V3SP4EV3R.mkv",
		"Below S01E01 Come Home Year 1080P NF WEB-DL DDP5.1 Atmos. X265 POOTLED.mkv",
		"Below.S01E01.1080p.10bit.WEBRip.6CH.x265.HEVC-PSA.mkv",
		"Below.S01E01.1080p.WEBRip.x265-KONTRAST.mkv",
		"Below.S01E01.Lanno.del.ritorno.a.casa.1080p.NF.WEB-DL.ITA.ENG.DDP5.1.H.264-G66.mkv",
		"below.s01e01.1080p.web.h264-cakes.mkv",
		"Below.S01E01.Come.Home.Year.1080p.NF.WEB-DL.H.264-Morgann23.mkv",
		"Bajo.la.superficie.S01E01.2026.WEB-DL.1080p-Dual-Lat.mkv",
		"Below.S01E01.1080p.H264.AAC.WEBRip.le-production.mp4",
		"Bajo la superficie [HDTV 1080p][Cap.101].mkv",
		"Below.S01E01.Lanno.del.ritorno.a.casa.1080p.NF.WEB-DL.ITA.ENG.DDP5.1.AV1-G66.mkv",
		"Below.S01E01.1080p.WEBRip.ViruseProject.mkv",
		"W.glebinach.S01E01.MULTi.1080p.NF.WEB-DL.x264-OzW.mkv",
		"Below S01E01 Come Home Year 1080p NF WEB-DL DDP5 1 Atmos H 264-FLUX.mkv",
		"暗潮之下.Below.S01E01.1080p.CHS&ENG.www.xingfan.cc.mp4",
		"Below.S01E01.Come.Home.Year.1080p.HEVC.x265-MeGusta.mkv",
		"Below.S01E01.1080p.rus.LostFilm.TV.mkv",
		"Below.S01E01.Lanno.del.ritorno.a.casa.1080p.NF.WEB-DL.ITA.ENG.DDP5.1.H.265-G66.mkv",
	}, []string{
		"Below.S01E02.2160p.NF.WEB-DL.DV.HDR.H.265.mkv",
		"Below.S01E02.2160p.NF.WEB-DL.DV.HDR[Ben The Men].mp4",
		"Below.S01E02.MULTi.2160p.NF.WEB-DL.DV.HDR.H265.DDP5.1.Atmos-K83.mkv",
		"Below.S01E02.Garden.party.2160p.NF.WEB-DL.ITA.ENG.DDP5.1.DV.HDR.H.265-G66.mkv",
		"Below.S01E02.2160p.WEBRip.DV.HDR.x265-KONTRAST.mkv",
		"Below S01E02 Crown and Anchor 2160p NF WEB-DL DDP5 1 Atmos DV HDR H 265-XEBEC.mkv",
		"Below S01E02 Crown and Anchor 2160p NF WEB-DL DDP5 1 Atmos DV HDR H 265-FLUX.mkv",
		"Below S01E02 Crown and Anchor 2160p NF WEB-DL DD 5 1 Atmos DoVi H 265-playWEB.mkv",
		"Below S01E02 Crown and Anchor 2160p NF WEB-DL DDP5 1 Atmos DV HDR H 265-Kitsune.mkv",
		"Below.S01E02.2160p.SDR.mkv",
		"Below S01E02 Crown and Anchor 2160p NF WEB-DL DD 5 1 Atmos H 265-playWEB.mkv",
		"Below S01E02 Crown and Anchor 2160p NF WEB-DL DDP5 1 Atmos H 265-XEBEC.mkv",
		"Below S01E02 Crown and Anchor 21080p WEBRip HDR DDP5 1 H265-d3g[EZTVx.to].mkv",
		"Below.S01E02.MULTi.1080p.NF.WEB-DL.DV.HDR.H265.DDP5.1.Atmos-K83.mkv",
		"Below.S01E02.Crown.and.Anchor.1080p.WEBRip.10Bit.DDP5.1.x265-NeoNoir.mkv",
		"Below.S01E02.Crown.and.Anchor.1080p.NF.WEB-DL.H.264-EniaHD.mkv",
		"Below (2026) - S01E02 - Crown and Anchor (1080p NF WEB-DL x265 Silence).mkv",
		"Below S01 E02.mkv",
		"Profundezas S01E02 WEB-DL 1080p x265 DUAL 5.1.mkv",
		"Below.S01E02.MULTi.1080p.WEB.x264-GL0P.mkv",
		"Below.S01E02.Crown.and.Anchor.1080p.NF.WEB-DL.ENG.Atmos.ITA.DDP5.1.H264-TBK.mkv",
		"Below.S01E02.Crown.and.Anchor.1080p.NF.WEB-DL.DDP5.1.Atmos.H.264-FLUX.mkv",
		"Below.1x02.Garden.Party.ITA.ENG.1080p.NFRip.AAC.x265-Pir8.mkv",
		"Below.S01E02.Crown.and.Anchor.1080p.ITA-ENG.MULTI.WEBRip.x265.AAC-V3SP4EV3R.mkv",
		"Below S01E02 Crown and Anchor 1080P NF WEB-DL DD 5.1 Atmos. X265 POOTLED.mkv",
		"Below.S01E02.1080p.10bit.WEBRip.6CH.x265.HEVC-PSA.mkv",
		"Below.S01E02.1080p.WEBRip.x265-KONTRAST.mkv",
		"below.s01e02.1080p.web.h264-cakes.mkv",
		"Below.S01E02.Garden.party.1080p.NF.WEB-DL.ITA.ENG.DDP5.1.H.264-G66.mkv",
		"Below.S01E02.Crown.and.Anchor.1080p.NF.WEB-DL.H.264-Morgann23.mkv",
		"Bajo.la.superficie.S01E02.2026.WEB-DL.1080p-Dual-Lat.mkv",
		"Below.S01E02.1080p.H264.AAC.WEBRip.le-production.mp4",
		"Below.S01E02.Garden.party.1080p.NF.WEB-DL.ITA.ENG.DDP5.1.AV1-G66.mkv",
		"Below.S01E02.1080P.HDTV.Subtitulado.Esp.SC",
		"Below.S01E02.1080p.WEBRip.ViruseProject.mkv",
		"W.glebinach.S01E02.MULTi.1080p.NF.WEB-DL.x264-OzW.mkv",
		"Below.S01E02.Crown.and.Anchor.1080p.HEVC.x265-MeGusta.mkv",
		"Below S01E02 Crown and Anchor 1080p NF WEB-DL DDP5 1 Atmos H 264-FLUX.mkv",
		"Below S01E02 MULTI 1080p WEB X264-HiggsBoson.mkv",
		"暗潮之下.Below.S01E02.1080p.CHS&ENG.www.xingfan.cc.mp4",
		"Bajo la superficie [HDTV 1080p][Cap.102].mkv",
		"Below.S01E02.Garden.party.1080p.NF.WEB-DL.ITA.ENG.DDP5.1.H.265-G66.mkv",
	}},
	{"WWE Smackdown!", "show", 28, 42, "", "", "2026-10-16", "2026-10-23", []string{
		"WWE.SmackDown.2026.10.16.NF.iNT.1080p.WEB.h265-HEEL.mkv",
		"WWE.SmackDown.2026.10.16.1080p.WEB.h264-HEEL.mkv",
		"WWE.Friday.Night.SmackDown.2026.10.16.1080p.HDTV.h264-Star.mp4",
		"WWE.SmackDown.2026.10.16.NF.iNT.720p.WEBRip.h264-HEEL.mp4",
		"WWE.Friday.Night.SmackDown.2026.10.16.720p.HDTV.h264-Star.mp4",
		"WWE.SmackDown.Live.2026.10.16.HDTV.720p.x264.Latino.EventosHQ.mp4",
		"WWE.Friday.Night.SmackDown.2026.10.16.480p.HDTV.h264-Star.mp4",
		"SmackDown 2026.10.16 480 (DTvW).mp4",
		"SmackDown 2026.10.16 720 (DTvW).mp4",
	}, []string{
		"WWE.SmackDown.2026.10.23.NF.iNT.1080p.WEB.h265-HEEL.mkv",
		"WWE.Friday.Night.SmackDown.2026.10.23.1080p.HDTV.h264-Star.mp4",
		"WWE.SmackDown.2026.10.23.NF.iNT.720p.WEBRip.h264-HEEL.mp4",
		"WWE.Friday.Night.SmackDown.2026.10.23.720p.HDTV.h264-Star.mp4",
		"WWE.Friday.Night.SmackDown.2026.10.23.480p.HDTV.h264-Star.mp4",
		"WWE.Friday.Night.SmackDown.2026.10.23.1080p.WEB.h264-KYR.mkv",
		"WWE.SmackDown.Live.2026.10.23.HDTV.720p.x264.Latino.EventosHQ.mp4",
		"SmackDown 2026.10.23 480 (DTvW).mp4",
		"SmackDown 2026.10.23 1080 (DTvW).mp4",
		// MediaFusion's answer, other weeks mixed in.
		"WWE.Friday.Night.Smackdown.2026.07.24.1080p.WEBRip.H264-Star.mp4",
		"WWE.Friday.Night.SmackDown.2026.08.21.Satfeed.1080p.HDTV.H264-Star.mp4",
		"WWE Friday Night Smackdown 2026 05 15 1080p WEB h264-KYR [TJET]",
		"WWE.Friday.Night.SmackDown.2026.09.04.Netflix.720p.WEBRip.H264-Star.mp4",
		"WWE.Friday.Night.SmackDown.2026.10.02.720p.HDTV.H264-Star.mp4",
		"WWE.Friday.Night.SmackDown.2026.10.16.480p.HDTV.h264-Star.mp4",
		"WWE.Friday.Night.SmackDown.2025.03.21.1080p.HDTV.H264-Star.mp4",
		"WWE Friday Night SmackDown 2023 07 21 1080p HDTV x264-NWCHD",
	}},
}

func TestRealPairs(t *testing.T) {
	for _, c := range realPairs {
		q := &stremio.EpQueue{Show: stremio.Meta{Name: c.show, Source: c.source}, Season: c.season, Episodes: []stremio.Video{
			{ID: "a", Season: c.season, Episode: c.ep, Title: c.title1, Released: c.aired1 + "T00:00:00.000Z"},
			{ID: "b", Season: c.season, Episode: c.ep + 1, Title: c.title2, Released: c.aired2 + "T00:00:00.000Z"},
		}}
		if testing.Verbose() {
			fmt.Println("=====", c.show)
		}
		next := torrentio(c.second)
		for _, prevName := range c.first {
			st, ok := pickStream(next, player.PlayRequest{Queue: q, Release: prevName}, store.Config{})
			got := st.BehaviorHints.Filename
			if !ok {
				got = "(no pick: list opens)"
			}
			if testing.Verbose() {
				fmt.Printf("%-100.100s\n      -> %s\n", prevName, got)
			}
			// Its own next episode, when the list has it, is the only right pick.
			own := bump(prevName)
			if c.show == "WWE Smackdown!" {
				own = strings.Replace(prevName, c.aired1[:4]+"."+c.aired1[5:7]+"."+c.aired1[8:], c.aired2[:4]+"."+c.aired2[5:7]+"."+c.aired2[8:], 1)
			}
			if slices.Contains(c.second, own) && got != own {
				t.Errorf("%s: after %q\n   picked %q\n   wanted %q", c.show, prevName, got, own)
			}
			if ok {
				if d, ok := stremio.NameDate(got); ok && d.Format("2006-01-02") != c.aired2 {
					t.Errorf("%s: picked another week's %q", c.show, got)
				}
			}
		}
	}
}

package stremio

import (
	"strings"
	"testing"
)

func TestParseReleaseName(t *testing.T) {
	for _, c := range []struct{ name, want string }{
		{"Rick and Morty S09E05 Jer Bud 1080p AMZN WEB-DL DDP5 1 H 264-Kitsune.mkv",
			"kitsune 1080p - webdl AMZN h264 ddp []"},
		{"Rick and Morty S09E06 Ricktional Mortpoon with Audio Description 1080p AMZN WEB-DL DDP5 1 H 264-Kitsune.mkv",
			"kitsune 1080p - webdl AMZN h264 ddp [audio-description]"},
		{"Show.S01E02.2160p.NF.WEB-DL.DV.HDR10.DDP5.1.Atmos.H.265-FLUX",
			"flux 2160p DV+HDR webdl NF h265 ddp []"},
		{"Show.S02E03.HDR10+.2160p.WEBRip.x265.10bit.AAC5.1-GRP",
			"grp 2160p HDR10+ webrip - h265 aac []"},
		{"[SubsPlease] Frieren - 05 (1080p) [A1B2C3D4].mkv",
			"subsplease 1080p - - - - - []"},
		{"[Erai-raws] Frieren - 05 [1080p][HEVC][Multiple Subtitle][Dual Audio].mkv",
			"erai-raws 1080p - - - h265 - [dual-audio]"},
		{"Show.S01E01.720p.HDTV.x264-DIMENSION",
			"dimension 720p - hdtv - h264 - []"},
		{"Show S01E01 1080p BluRay REMUX AVC DTS-HD MA 5 1-GRP",
			"grp 1080p - remux - h264 dtshd []"},
		{"Show.S01E01.1080p.WEB-DL", "- 1080p - webdl - - - []"},
		{"Anno.A.D.S01E01.1080p.WEB.h264-GRP", "grp 1080p - webdl - h264 - []"},
		{"Show S01E01 Extended Dubbed 1080p BluRay x264-GRP",
			"grp 1080p - bluray - h264 - [dubbed]"},
		// Services that are also words only count right before WEB-DL.
		{"The.Last.of.Us.S02E01.1080p.MAX.WEB-DL.DDP5.1.Atmos.H.264-FLUX",
			"flux 1080p - webdl MAX h264 ddp []"},
		{"Mad.Max.Fury.Road.2015.1080p.BluRay.x264-SPARKS",
			"sparks 1080p - bluray - h264 - []"},
		{"Show.S01E01.1080p.STAN.WEB-DL.AAC2.0.H.264-NTb", "ntb 1080p - webdl STAN h264 aac []"},
		{"Show.S01E01.1080p.DSNY.WEB-DL.DDP5.1.H.264-NTb", "ntb 1080p - webdl DSNP h264 ddp []"},
		{"[SubsPlease] Show - 05 (1080p) CR WEB-DL", "subsplease 1080p - webdl CR - - []"},
		// TRaSH's audio description, sign language and upscaled formats.
		{"Show.S01E01.MULTi.AD.1080p.WEB.H264-GRP", "grp 1080p - webdl - h264 - [audio-description dual-audio]"},
		{"Show.S01E01.1080p.WEB-DL.WiTH.AD.DDP5.1.H.264-GRP",
			"grp 1080p - webdl - h264 ddp [audio-description]"},
		{"Show.S01E01.1080p.WEB-DL.with.BSL.AAC2.0.H.264-GRP", "grp 1080p - webdl - h264 aac [sign-language]"},
		{"Show.S01E01.BASL.1080p.WEB-DL.AAC2.0.H.264-GRP", "grp 1080p - webdl - h264 aac [sign-language]"},
		{"Show.S01E01.2160p.AIUS.WEB-DL.H.265-GRP", "grp 2160p - webdl - h265 - [upscaled]"},
		{"Show.S01E01.2160p.UpRez.BluRay.x265-GRP", "grp 2160p - bluray - h265 - [upscaled]"},
		// Reposter junk after the group (Sonarr's list).
		{"Show.S01E01.1080p.WEB.h264-ETHEL-Obfuscated", "ethel 1080p - webdl - h264 - []"},
		{"Show.S01E01.1080p.WEB.h264-ETHEL-postbot-xpost", "ethel 1080p - webdl - h264 - []"},
		{"Show.S01E01.1080p.WEB.h264-ETHEL-Rakuvfinhel", "ethel 1080p - webdl - h264 - []"},
		{"", "- - - - - - - []"},
	} {
		r := ParseReleaseName(c.name)
		or := func(s string) string {
			if s == "" {
				return "-"
			}
			return s
		}
		got := strings.Join([]string{or(r.Group), or(r.Resolution), or(r.HDR), or(r.Source), or(r.Service), or(r.Codec), or(r.Audio)}, " ") +
			" [" + strings.Join(r.Flags, " ") + "]"
		if got != c.want {
			t.Errorf("%q\n got  %s\n want %s", c.name, got, c.want)
		}
	}
}

func TestReleaseMatches(t *testing.T) {
	prev := ParseReleaseName("Show S01E05 1080p AMZN WEB-DL DDP5 1 H 264-Kitsune.mkv")
	ad := ParseReleaseName("Show S01E06 with Audio Description 1080p AMZN WEB-DL DDP5 1 H 264-Kitsune.mkv")
	other := ParseReleaseName("Show S01E06 1080p NF WEB-DL DDP5 1 H 264-FLUX.mkv")
	if m := prev.Matches(ad); m[0] {
		t.Error("audio description counted as the same variant")
	}
	if m := prev.Matches(other); !m[0] || !m[1] || m[3] || m[4] {
		t.Errorf("other group: %v", m)
	}
	unknown := ParseReleaseName("Show S01E05 1080p")
	if m := unknown.Matches(other); m != [8]bool{true, true, true, true, true, true, true, true} {
		t.Errorf("fields prev doesn't have should agree: %v", m)
	}
}

func TestExtendedIsNotAVariant(t *testing.T) {
	ext := ParseReleaseName("Show S01E10 Extended 1080p AMZN WEB-DL DDP5 1 H 264-NTb.mkv")
	next := ParseReleaseName("Show S01E11 1080p AMZN WEB-DL DDP5 1 H 264-NTb.mkv")
	if ext.Edition != "extended" || len(ext.Flags) != 0 {
		t.Fatalf("extended parsed as %+v", ext)
	}
	if m := ext.Matches(next); !m[0] || m[7] {
		t.Fatalf("extended should rank, not block: %v", m)
	}
}

func TestTitleWordsAreNotTags(t *testing.T) {
	for _, c := range []struct{ name, title, flags, edition string }{
		{"Show.S02E04.The.Dub.1080p.WEB.h264-GRP", "The Dub", "", ""},
		{"Show S01E08 Director's Cut 1080p AMZN WEB-DL DDP5 1 H 264-NTb.mkv", "Director's Cut", "", ""},
		{"Show.S01E08.Directors.Cut.1080p.WEB.h264-GRP", "Director's Cut", "", ""},
		{"Show.S03E01.Uncut.Gems.EXTENDED.1080p.WEB.h264-GRP", "Uncut Gems", "", "extended"},
		{"Show.S01E02.Sign.Language.WiTH.AD.1080p.WEB.h264-GRP", "Sign Language", "audio-description", ""},
		{"Show.S01E02.Dubbed.1080p.WEB.h264-GRP", "", "dubbed", ""},
		{"Show.S01E02.Dubbed.1080p.WEB.h264-GRP", "Pilot", "dubbed", ""},
	} {
		r := ParseReleaseNameTitled(c.name, c.title)
		if f := strings.Join(r.Flags, ","); f != c.flags || r.Edition != c.edition {
			t.Errorf("%q titled %q: flags %q edition %q, want %q %q", c.name, c.title, f, r.Edition, c.flags, c.edition)
		}
		if r.Group != "grp" && r.Group != "ntb" {
			t.Errorf("%q: group %q", c.name, r.Group)
		}
	}
}

func TestLanguages(t *testing.T) {
	for _, c := range []struct{ name, langs, flags string }{
		{"Show.S01E05.1080p.WEB.h264-ETHEL", "", ""},
		{"Show.S01E05.ITA.ENG.1080p.WEB.h264-GRP", "it", "dual-audio"},
		{"Show.S01E05.iTALiAN.1080p.WEB.h264-GRP", "it", ""},
		{"Show.S01E05.FRENCH.1080p.WEB.h264-GRP", "fr", ""},
		{"Show.S01E05.MULTi.TRUEFRENCH.1080p.WEB.h264-GRP", "fr", "dual-audio"},
		{"Show S01E05 Latino 1080p WEB-DL", "mx", ""},
		{"[Erai-raws] Show - 05 [1080p][Multi-Sub]", "", ""},
		{"[Erai-raws] Show - 05 [1080p][Multiple Subtitle]", "", ""},
		{"Margarita.Ita.S01E01.1080p", "it", ""}, // tag, not part of a word
		{"Margarita.S01E01.1080p", "", ""},
		{"Show.S01E05.Amor.Por.Ti.1080p.WEB.h264-GRP", "", ""},
		{"Show.S01E05.PORTUGUESE.1080p.WEB.h264-GRP", "pt", ""},
		{"Show.S01E05.PT-BR.1080p.WEB.h264-GRP", "br", ""},
		{"Show S01E05 1080p CR WEB-DL DUAL AAC2.0 H 264-VARYG", "", "dual-audio"},
		{"Show S01E05 1080p WEB-DL [ENG+JPN] [Multi-Sub]", "", "dual-audio"},
		{"Show S01E05 1080p WEB-DL [JPN-ENG]", "jp", "dual-audio"},
		{"[Judas] Show - S01E05 [1080p][Dual-Audio][Multi-Subs]", "", "dual-audio"},
		{"[Erai-raws] Show - 05 [1080p][Multiple Subtitle][ENG][POR-BR][SPA-LA][ITA]", "", ""},
		{"Show S01E05 1080p CR WEB-DL AAC2.0 H 264-VARYG", "", ""},
	} {
		r := ParseReleaseName(c.name)
		if l, f := strings.Join(r.Langs, ","), strings.Join(r.Flags, ","); l != c.langs || f != c.flags {
			t.Errorf("%q: langs %q flags %q, want %q %q", c.name, l, f, c.langs, c.flags)
		}
	}

	// Torrentio and AIOStreams show languages as flags; English is left out.
	if got := strings.Join(FlagLangs("Show.S01E05\n👤 12 💾 1.2 GB\nMulti Audio / 🇬🇧 / 🇮🇹 / 🇲🇽"), ","); got != "it,mx" {
		t.Errorf("flags: %q", got)
	}
	if got := FlagLangs("Show.S01E05\nMulti Subs / 🇬🇧 / 🇧🇷 / 🇲🇽"); got != nil {
		t.Errorf("subtitle flags read as audio: %q", got)
	}
	if got := FlagLangs("🇺🇸 only, and ⚡ cached"); got != nil {
		t.Errorf("english only: %q", got)
	}

	eng := ParseReleaseName("Show.S01E05.1080p.WEB.h264-GRP")
	ita := ParseReleaseName("Show.S01E06.1080p.WEB.h264-GRP").WithLangs([]string{"it"})
	if eng.Matches(ita)[0] || ita.Matches(eng)[0] {
		t.Error("an italian dub counted as the same variant as english")
	}
	multi := ParseReleaseName("Show.S01E05.MULTi.1080p.WEB.h264-GRP").WithLangs([]string{"fr"})
	more := ParseReleaseName("Show.S01E06.MULTi.1080p.WEB.h264-GRP").WithLangs([]string{"fr", "it"})
	if !multi.Matches(more)[0] {
		t.Error("a multi release with an extra language should still match")
	}
	if more.Matches(multi)[0] {
		t.Error("a multi release missing a language you had should not match")
	}
}

func TestShowNameWordsAreNotTags(t *testing.T) {
	with := ParseReleaseNameTitled("The.Italian.Job.S01E02.1080p.WEB.h264-GRP", "The Italian Job", "")
	without := ParseReleaseNameTitled("S01E02.1080p.WEB.h264-GRP", "The Italian Job", "")
	if len(with.Langs) != 0 || !with.Matches(without)[0] {
		t.Fatalf("show name read as a language: %v", with.Langs)
	}
	if r := ParseReleaseNameTitled("Uncut.Gems.S01E02.1080p.WEB.h264-GRP", "Uncut Gems"); r.Edition != "" {
		t.Fatalf("show name read as an edition: %q", r.Edition)
	}
	if r := ParseReleaseNameTitled("The.Italian.Job.S01E02.ITA.1080p.WEB.h264-GRP", "The Italian Job"); strings.Join(r.Langs, ",") != "it" {
		t.Fatalf("a real ITA tag was lost: %v", r.Langs)
	}
}

func TestCommonReleaseNames(t *testing.T) {
	for _, c := range []struct{ name, want string }{
		// group resolution hdr source service codec audio [flags] [langs]
		{"House.of.the.Dragon.S02E01.2160p.MAX.WEB-DL.DDP5.1.Atmos.DV.HDR10.H.265-EDITH", "edith 2160p DV+HDR webdl MAX h265 ddp [] []"},
		{"Severance.S02E01.1080p.ATVP.WEB-DL.DDP5.1.Atmos.H.264-FLUX[TGx]", "flux 1080p - webdl ATVP h264 ddp [] []"},
		{"Show.S01E01.1080p.WEBRip.x265-ELiTE[eztv.re].mkv", "elite 1080p - webrip - h265 - [] []"},
		{"Show.S01E01.1080p.BluRay.x264-GRP.Rarbg", "grp 1080p - bluray - h264 - [] []"},
		{"Show (2008) S01E01 (1080p BluRay x265 HEVC 10bit AAC 5.1 Tigole)", "tigole 1080p - bluray - h265 aac [] []"},
		{"Show (2008) S01E01 Pilot (1080p BluRay x265 Silence)", "silence 1080p - bluray - h265 - [] []"},
		{"Show (2008) S01E01 (1080p BluRay x265 10bit AAC 5.1)", "- 1080p - bluray - h265 aac [] []"},
		{"Show.S01E01.1080p.WEB.H264-GRP (Dual Audio)", "grp 1080p - webdl - h264 - [dual-audio] []"},
		{"Show.S01E01.1080p.BluRay.DTS-HD.MA", "- 1080p - bluray - - dtshd [] []"},
		{"Show.S01E01.ITA.ENG.1080p.AMZN.WEB-DL.DDP5.1.H.264-MeM.GP", "mem.gp 1080p - webdl AMZN h264 ddp [dual-audio] [it]"},
		{"Show.S01E01.GERMAN.DL.1080p.WEB.h264-WvF", "wvf 1080p - webdl - h264 - [dual-audio] [de]"},
		{"Show.S01E01.VOSTFR.1080p.WEB.H264-JiHEFF", "jiheff 1080p - webdl - h264 - [] []"},
		{"Show.S01E01.SDR.2160p.WEB.H265-RAWR", "rawr 2160p - webdl - h265 - [] []"},
		{"Show.S01E01.2160p.WEB-DL.HLG.H.265-GRP", "grp 2160p HLG webdl - h265 - [] []"},
		{"Show.S01E01.1080p.WEB-DL.LPCM.2.0.H.264-GRP", "grp 1080p - webdl - h264 pcm [] []"},
		{"Show.S01E01.1080p.KORSUB.HDRip.x264-XYZ", "xyz 1080p - - - h264 - [hardsub] []"},
		{"[Beatrice-Raws] Show 05 [BDRip 1920x1080 HEVC FLAC]", "beatrice-raws 1080p - bluray - h265 flac [] []"},
	} {
		r := ParseReleaseName(c.name)
		or := func(s string) string {
			if s == "" {
				return "-"
			}
			return s
		}
		got := strings.Join([]string{or(r.Group), or(r.Resolution), or(r.HDR), or(r.Source), or(r.Service), or(r.Codec), or(r.Audio)}, " ") +
			" [" + strings.Join(r.Flags, ",") + "] [" + strings.Join(r.Langs, ",") + "]"
		if got != c.want {
			t.Errorf("%q\n got  %s\n want %s", c.name, got, c.want)
		}
	}

	// Untagged is SDR: after an SDR episode, a Dolby Vision release isn't a match.
	sdr := ParseReleaseName("Show.S01E01.2160p.WEB.H265-RAWR")
	dv := ParseReleaseName("Show.S01E02.2160p.WEB.DV.H265-RAWR")
	if sdr.Matches(dv)[2] || !sdr.Matches(ParseReleaseName("Show.S01E02.SDR.2160p.WEB.H265-RAWR"))[2] {
		t.Error("SDR and Dolby Vision treated as the same")
	}
}

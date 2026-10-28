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
		{"Show.S01E01.MULTi.AD.1080p.WEB.H264-GRP", "grp 1080p - webdl - h264 - [audio-description]"},
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

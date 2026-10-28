package ui

import (
	"testing"

	"stremio-cliuwu/internal/player"
	"stremio-cliuwu/internal/store"
	"stremio-cliuwu/internal/stremio"
)

// Next-episode picks against each addon's stream shape. In each case the
// right stream is not first in the list.
func TestPickStreamAcrossAddons(t *testing.T) {
	st := func(url, title, desc, file, group string) stremio.Stream {
		s := stremio.Stream{URL: url, Title: title, Description: desc}
		s.BehaviorHints.Filename = file
		s.BehaviorHints.BingeGroup = group
		return s
	}
	prevOf := func(s stremio.Stream) player.PlayRequest {
		return player.PlayRequest{BingeGroup: s.BehaviorHints.BingeGroup, Release: stremio.ReleaseName(s)}
	}
	hash := "0123456789abcdef0123456789abcdef01234567"

	for _, c := range []struct {
		name string
		prev stremio.Stream
		next []stremio.Stream
		want string
	}{
		{"torrentio single episode: same group",
			st("e5", "", "", "Show.S01E05.1080p.WEB.h264-ETHEL.mkv", "torrentio|1080p|ETHEL"),
			[]stremio.Stream{
				st("ntb", "", "", "Show.S01E06.1080p.WEB.h264-NTb.mkv", "torrentio|1080p|NTb"),
				st("ethel", "", "", "Show.S01E06.1080p.WEB.h264-ETHEL.mkv", "torrentio|1080p|ETHEL"),
			}, "ethel"},
		{"torrentio season pack: same torrent",
			st("e5", "", "", "Show.S01E05.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb.mkv", "torrentio|"+hash),
			[]stremio.Stream{
				st("single", "", "", "Show.S01E06.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb.mkv", "torrentio|1080p|NTb"),
				st("pack", "", "", "Show.S01E06.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb.mkv", "torrentio|"+hash),
			}, "pack"},
		{"comet: hash groups never match, fields decide",
			st("e5", "", "📄 Show.S01E05.1080p.WEB-DL.x265-GRP", "Show.S01E05.1080p.WEB-DL.x265-GRP", "comet|realdebrid|aaa"),
			[]stremio.Stream{
				st("other", "", "", "Show.S01E06.1080p.WEB-DL.x264-OTHER", "comet|realdebrid|bbb"),
				st("grp", "", "", "Show.S01E06.1080p.WEB-DL.x265-GRP", "comet|realdebrid|ccc"),
			}, "grp"},
		{"mediafusion: coarse shared group, release decides",
			st("e5", "", "", "Show.S01E05.1080p.NF.WEB-DL.DDP5.1.Atmos.H.264-FLUX.mkv", "MediaFusion-📺 WEB-DL 🎞️ H.264 🎵 DDP-1080P"),
			[]stremio.Stream{
				st("ntb", "", "", "Show.S01E06.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb.mkv", "MediaFusion-📺 WEB-DL 🎞️ H.264 🎵 DDP-1080P"),
				st("flux", "", "", "Show.S01E06.1080p.NF.WEB-DL.DDP5.1.Atmos.H.264-FLUX.mkv", "MediaFusion-📺 WEB-DL 🎞️ H.264 🎵 DDP-1080P"),
			}, "flux"},
		{"jackettio: no filename or group, file on line 2",
			st("e5", "Show.S01.1080p.BluRay.x264-DEMAND\nShow.S01E05.1080p.BluRay.x264-DEMAND.mkv\n💾1.4 GB", "", "", ""),
			[]stremio.Stream{
				st("rovers", "Show.S01E06.1080p.BluRay.x264-ROVERS\n💾1.3 GB", "", "", ""),
				st("demand", "Show.S01.1080p.BluRay.x264-DEMAND\nShow.S01E06.1080p.BluRay.x264-DEMAND.mkv\n💾1.4 GB", "", "", ""),
			}, "demand"},
		{"aiostreams: audio description shares the group",
			st("e5", "", "", "Rick and Morty S09E05 Jer Bud 1080p AMZN WEB-DL DDP5 1 H 264-Kitsune.mkv", "aiostreams|1080p|WEB-DL|Kitsune"),
			[]stremio.Stream{
				st("ad", "", "", "Rick and Morty S09E06 Title with Audio Description 1080p AMZN WEB-DL DDP5 1 H 264-Kitsune.mkv", "aiostreams|1080p|WEB-DL|Kitsune"),
				st("plain", "", "", "Rick and Morty S09E06 Title 1080p AMZN WEB-DL DDP5 1 H 264-Kitsune.mkv", "aiostreams|1080p|WEB-DL|Kitsune"),
			}, "plain"},
		{"no exact release: closest by resolution, then service",
			st("e5", "", "", "Show.S01E05.1080p.AMZN.WEB-DL.DDP5.1.H.264-Kitsune.mkv", ""),
			[]stremio.Stream{
				st("720", "", "", "Show.S01E06.720p.AMZN.WEB-DL.DDP5.1.H.264-Kitsune.mkv", ""),
				st("nf", "", "", "Show.S01E06.1080p.NF.WEB-DL.DDP5.1.H.264-FLUX.mkv", ""),
				st("amzn", "", "", "Show.S01E06.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb.mkv", ""),
			}, "amzn"},
		{"anime: same subgroup over a higher listed one",
			st("e5", "", "", "[SubsPlease] Frieren - 05 (1080p) [A1B2C3D4].mkv", ""),
			[]stremio.Stream{
				st("erai", "", "", "[Erai-raws] Frieren - 06 [1080p][HEVC].mkv", ""),
				st("subsplease", "", "", "[SubsPlease] Frieren - 06 (1080p) [E5F6A7B8].mkv", ""),
			}, "subsplease"},
	} {
		got, ok := pickStream(c.next, prevOf(c.prev), store.Config{})
		if !ok || got.URL != c.want {
			t.Errorf("%s: picked %q (ok=%v), want %q", c.name, got.URL, ok, c.want)
		}
	}
}

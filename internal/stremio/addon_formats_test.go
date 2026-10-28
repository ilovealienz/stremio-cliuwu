package stremio

import "testing"

// Stream shapes as each addon builds them (from their source): what ends up
// in name, title, description and behaviorHints.
func addonStream(name, title, desc, filename, group string) Stream {
	st := Stream{Name: name, Title: title, Description: desc}
	st.BehaviorHints.Filename = filename
	st.BehaviorHints.BingeGroup = group
	return st
}

var addonFormats = []struct {
	addon string
	st    Stream
	want  string // ReleaseName
	group string // parsed release group
}{
	{"torrentio single episode", addonStream("Torrentio\n1080p",
		"Show.S01E05.1080p.WEB.h264-ETHEL\n👤 120 💾 1.2 GB ⚙️ ThePirateBay", "",
		"Show.S01E05.1080p.WEB.h264-ETHEL.mkv", "torrentio|1080p|ETHEL"),
		"Show.S01E05.1080p.WEB.h264-ETHEL.mkv", "ethel"},
	{"torrentio season pack", addonStream("Torrentio\n1080p",
		"Show.S01.COMPLETE.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb\nShow.S01E05.Title.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb.mkv\n👤 40 💾 1.4 GB ⚙️ 1337x", "",
		"Show.S01E05.Title.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb.mkv", "torrentio|0123456789abcdef0123456789abcdef01234567"),
		"Show.S01E05.Title.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb.mkv", "ntb"},
	{"aiostreams", addonStream("[AIOStreams] 1080P ⚡", "", "Rick and Morty S09E05 Jer Bud\n843.5 MB",
		"Rick and Morty S09E05 Jer Bud 1080p AMZN WEB-DL DDP5 1 H 264-Kitsune.mkv", "aiostreams|1080p|WEB-DL|Kitsune"),
		"Rick and Morty S09E05 Jer Bud 1080p AMZN WEB-DL DDP5 1 H 264-Kitsune.mkv", "kitsune"},
	{"comet (raw torrent title as filename)", addonStream("[RD⚡] Comet 1080p", "",
		"📄 Show.S01.1080p.WEB-DL.x265-GRP\n📹 HEVC | 🔊 DDP\n⭐ WEB-DL | 👥 GRP",
		"Show.S01.1080p.WEB-DL.x265-GRP", "comet|realdebrid|0123456789abcdef0123456789abcdef01234567"),
		"Show.S01.1080p.WEB-DL.x265-GRP", "grp"},
	{"mediafusion", addonStream("MediaFusion RD⚡️ 1080P", "",
		"📂 Show S01E05\n🎞️ H.264 🎵 DDP|Atmos\n💾 1.6 GB 👤 30",
		"Show.S01E05.1080p.NF.WEB-DL.DDP5.1.Atmos.H.264-FLUX.mkv", "MediaFusion-📺 WEB-DL 🎞️ H.264 🎵 DDP|Atmos-1080P"),
		"Show.S01E05.1080p.NF.WEB-DL.DDP5.1.Atmos.H.264-FLUX.mkv", "flux"},
	{"jackettio pack (no filename, file on line 2)", addonStream("[RD+] Jackettio 1080p",
		"Show.S01.1080p.BluRay.x264-DEMAND\nShow.S01E05.1080p.BluRay.x264-DEMAND.mkv\n💾1.4 GB 👥55 ⚙️rarbg", "", "", ""),
		"Show.S01E05.1080p.BluRay.x264-DEMAND.mkv", "demand"},
	{"description only, emoji prefix", addonStream("Some Addon\n1080p", "",
		"📄 Show.S01E05.1080p.WEB-DL.DDP5.1.H.264-FLUX\n💾 1.2 GB", "", ""),
		"Show.S01E05.1080p.WEB-DL.DDP5.1.H.264-FLUX", "flux"},
	{"anime behind an emoji", addonStream("Some Addon", "",
		"📄 [SubsPlease] Frieren - 05 (1080p) [A1B2C3D4].mkv", "", ""),
		"[SubsPlease] Frieren - 05 (1080p) [A1B2C3D4].mkv", "subsplease"},
	{"aiostreams without a filename hint", addonStream("[AIOStreams] 1080P ⚡", "",
		"🎥 1080p 📺 WEB-DL 🎞️ H.264\n📁 Rick and Morty S09E05 Jer Bud 1080p AMZN WEB-DL DDP5 1 H 264-Kitsune.mkv\n💾 843.5 MB", "", "aiostreams|1080p|WEB-DL|Kitsune"),
		"Rick and Morty S09E05 Jer Bud 1080p AMZN WEB-DL DDP5 1 H 264-Kitsune.mkv", "kitsune"},
	{"name only", addonStream("Direct 1080p", "", "", "", ""), "Direct 1080p", ""},
}

func TestAddonFormats(t *testing.T) {
	for _, c := range addonFormats {
		got := ReleaseName(c.st)
		if got != c.want {
			t.Errorf("%s: ReleaseName = %q, want %q", c.addon, got, c.want)
		}
		if g := ParseReleaseName(got).Group; g != c.group {
			t.Errorf("%s: group = %q, want %q", c.addon, g, c.group)
		}
	}
}

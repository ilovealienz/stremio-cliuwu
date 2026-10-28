package stremio

import (
	"regexp"
	"sort"
	"strings"
	"sync"
)

// ReleaseInfo is what autoplay compares between two releases. Patterns follow
// Sonarr's quality and release group parsers and the autobrr/rls tag table.
type ReleaseInfo struct {
	Group      string
	Resolution string
	HDR        string
	Source     string
	Service    string
	Codec      string
	Audio      string
	Edition    string // extended cut and the like; preferred, never required

	// Variant flags: the same release cut a different way (audio
	// description, a dub, 3D...). Sorted.
	Flags []string
}

const (
	webTag = `[ ._\-]web[ ._\-]?(?:dl|rip)`
	sep    = `[ ._\-]`
	ch     = `(?:\d(?:` + sep + `?\d)?)?` // channels written onto the codec, "DDP5 1"
)

type tag struct {
	re  *regexp.Regexp
	val string
}

func tags(pairs ...string) []tag {
	out := make([]tag, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, tag{regexp.MustCompile(`(?i)(?:^|` + sep + `|\[|\()(?:` + pairs[i] + `)(?:$|` + sep + `|\]|\))`), pairs[i+1]})
	}
	return out
}

// The patterns compile on first use rather than at startup: compiling them
// up front kept the compile's scratch memory resident for the whole session,
// even when autoplay is never used.
var rx struct {
	once sync.Once

	resolution, source, service, codec, audio, hdr, flags, edition []tag

	animeGroup, sceneGroup, junkSuffix, notGroup, trailing, ext, releaseLike *regexp.Regexp
}

func loadRx() {
	rx.once.Do(func() {
		rx.resolution = tags(
			`2160p|4k|uhd`, "2160p",
			`1440p`, "1440p",
			`1080[pi]`, "1080p",
			`720p`, "720p",
			`576p`, "576p",
			`480p`, "480p",
		)
		rx.source = tags(
			`(?:bd|uhd)?`+sep+`?remux`, "remux",
			`blu`+sep+`?ray|bdrip|brrip|bd`, "bluray",
			`web`+sep+`?rip`, "webrip",
			`web`+sep+`?dl|web`, "webdl",
			`hdtv`, "hdtv",
			`dvd(?:rip)?`, "dvd",
		)
		// Tags from the TRaSH guides' streaming service formats. The ones
		// that are also ordinary words only count right before WEB-DL/WEBRip.
		rx.service = tags(
			`amzn`, "AMZN",
			`nf`, "NF",
			`dsnp|dsny`, "DSNP",
			`(?:hmax|hbom)`+webTag, "HMAX",
			`max`+webTag, "MAX",
			`atvp|aptv`, "ATVP",
			`hulu`, "HULU",
			`pcok`, "PCOK",
			`pmtp`, "PMTP",
			`roku`, "ROKU",
			`stan`+webTag, "STAN",
			`crave?`+webTag, "CRAV",
			`sho`+webTag, "SHO",
			`it`+webTag, "iT",
			`cr|crunchyroll`, "CR",
			`funi`, "FUNI",
			`hidive`, "HIDIVE",
			`vrv`, "VRV",
		)
		rx.codec = tags(
			`[xh]`+sep+`?265|hevc`, "h265",
			`[xh]`+sep+`?264|avc`, "h264",
			`av1`, "av1",
			`xvid`, "xvid",
		)
		rx.audio = tags(
			`truehd`+ch, "truehd",
			`(?:dts`+sep+`?hd(?:`+sep+`?ma)?|dts`+sep+`?x)`+ch, "dtshd",
			`dts`+ch, "dts",
			`(?:ddp|dd\+|e`+sep+`?ac`+sep+`?3)`+ch, "ddp",
			`(?:dd|ac`+sep+`?3)`+ch, "dd",
			`aac`+ch, "aac",
			`flac`+ch, "flac",
			`opus`+ch, "opus",
		)
		rx.hdr = tags(
			`dv|dovi|dolby`+sep+`?vision`, "DV",
			`hdr10\+|hdr10plus`, "HDR10+",
			`hdr10|hdr`, "HDR",
		)
		rx.flags = tags(
			`(?:(?:with|multi|french)`+sep+`)?audio`+sep+`description|(?:with|multi|french)`+sep+`ad`, "audio-description",
			`sign`+sep+`language|with`+sep+`[ab]sl|basl`, "sign-language",
			`commentary`, "commentary",
			`3d|h?sbs|half`+sep+`sbs`, "3d",
			`(?:ai`+sep+`)?upscaled?|aius|uprez|ai`+sep+`?(?:enhanced?|remaster(?:ed)?)`, "upscaled",
			`dubbed|dub`, "dubbed",
			`dual`+sep+`audio|multi`+sep+`audio`, "dual-audio",
			`hardsub(?:bed)?|hardcoded|hc`, "hardsub",
		)
		rx.edition = tags(
			`extended|directors`+sep+`cut|uncut|unrated`, "extended",
		)

		rx.animeGroup = regexp.MustCompile(`^\[([^\]]+)\]`)
		rx.sceneGroup = regexp.MustCompile(`-([A-Za-z0-9]+)$`)
		// Suffixes indexers and reposters tack on after the group (Sonarr's list).
		rx.junkSuffix = regexp.MustCompile(`(?i)(?:-(?:RP|1|NZBGeek|Obfuscated|Scrambled|sample|Pre|postbot|xpost|Rakuv[a-z0-9]*|WhiteRev|BUYMORE|AsRequested|AlternativeToRequested|GEROV|Z0iDS3N|Chamele0n|4P|4Planet|AlteZachen|RePACKPOST|iNC0GNiTO|WRTEAM|CAPTCHA))+$`)
		rx.notGroup = regexp.MustCompile(`(?i)^(?:[se]\d+|[0-9a-f]{8}|\d+p|web|dl|rip|hd|ma|x|\d+)$`)
		rx.trailing = regexp.MustCompile(`(?:\s*[\[(][^\])]*[\])])+$`)
		rx.ext = regexp.MustCompile(`(?i)\.(?:mkv|mp4|avi|m4v|ts|mov|wmv|webm)$`)
		rx.releaseLike = regexp.MustCompile(`(?i)\bS\d{1,2}(?:E\d{1,3})?\b|\b(?:2160|1080|720|480)p\b`)
	})
}

func firstTag(name string, ts []tag) string {
	for _, t := range ts {
		if t.re.MatchString(name) {
			return t.val
		}
	}
	return ""
}

func allTags(name string, ts []tag) []string {
	var out []string
	for _, t := range ts {
		if t.re.MatchString(name) {
			out = append(out, t.val)
		}
	}
	sort.Strings(out)
	return out
}

// releaseGroup finds the group: "[Group] Title" for anime, "...-GROUP" for
// scene releases.
func releaseGroup(name string) string {
	if m := rx.animeGroup.FindStringSubmatch(name); m != nil {
		return strings.TrimSpace(m[1])
	}
	name = rx.trailing.ReplaceAllString(name, "")
	name = rx.junkSuffix.ReplaceAllString(name, "")
	if m := rx.sceneGroup.FindStringSubmatch(name); m != nil && !rx.notGroup.MatchString(m[1]) {
		return m[1]
	}
	return ""
}

// ParseReleaseName pulls the fields autoplay matches on out of a release or
// file name.
func ParseReleaseName(name string) ReleaseInfo {
	loadRx()
	name = rx.ext.ReplaceAllString(strings.TrimSpace(name), "")
	r := ReleaseInfo{
		Group:      strings.ToLower(releaseGroup(name)),
		Resolution: firstTag(name, rx.resolution),
		Source:     firstTag(name, rx.source),
		Service:    firstTag(name, rx.service),
		Codec:      firstTag(name, rx.codec),
		Audio:      firstTag(name, rx.audio),
		HDR:        strings.Join(allTags(name, rx.hdr), "+"),
		Edition:    firstTag(name, rx.edition),
		Flags:      allTags(name, rx.flags),
	}
	return r
}

// ParseReleaseNameTitled is ParseReleaseName for an episode called title.
// The title's words don't count as variant or edition tags, so an episode
// named "Dubbed" or "The Director's Cut" isn't read as one.
func ParseReleaseNameTitled(name, title string) ReleaseInfo {
	r := ParseReleaseName(name)
	re := titlePattern(title)
	if re == nil {
		return r
	}
	rest := re.ReplaceAllString(rx.ext.ReplaceAllString(strings.TrimSpace(name), ""), " ")
	r.Edition = firstTag(rest, rx.edition)
	r.Flags = allTags(rest, rx.flags)
	return r
}

// titlePattern matches title however a release writes it: any case, with
// dots, spaces or nothing between the words ("Rick's" becomes "Ricks").
func titlePattern(title string) *regexp.Regexp {
	words := strings.FieldsFunc(title, func(c rune) bool {
		return !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9')
	})
	if len(words) == 0 {
		return nil
	}
	for i, w := range words {
		words[i] = regexp.QuoteMeta(w)
	}
	re, err := regexp.Compile(`(?i)\b` + strings.Join(words, `[^A-Za-z0-9]*`) + `\b`)
	if err != nil {
		return nil
	}
	return re
}

// SameFlags reports whether two releases are the same variant.
func (r ReleaseInfo) SameFlags(o ReleaseInfo) bool {
	return strings.Join(r.Flags, ",") == strings.Join(o.Flags, ",")
}

// Matches lists, in order of importance, whether o agrees with r on each
// field autoplay cares about. A field r doesn't have counts as agreeing.
func (r ReleaseInfo) Matches(o ReleaseInfo) [8]bool {
	same := func(a, b string) bool { return a == "" || a == b }
	return [8]bool{
		r.SameFlags(o),
		same(r.Resolution, o.Resolution),
		same(r.HDR, o.HDR),
		same(r.Group, o.Group),
		same(r.Source, o.Source) && same(r.Service, o.Service),
		same(r.Codec, o.Codec),
		same(r.Audio, o.Audio),
		r.Edition == o.Edition,
	}
}

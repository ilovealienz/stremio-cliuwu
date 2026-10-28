package stremio

import (
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
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

	// Langs are the audio languages other than English, as the country
	// codes addons show them with as flags: "it", "fr", "mx" for Latino.
	// Sorted. None means English or unknown.
	Langs []string
}

// pairLangs are the languages a release names beside English for dual audio:
// ITA.ENG, ENG+JPN, Hindi + English.
const pairLangs = `(?:jpn|jap|japanese|ita|italian|fre|french|spa|spanish|ger|german|rus|russian|hindi|kor|korean)`

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

	resolution, source, service, codec, audio, hdr, flags, edition, lang []tag

	animeGroup, sceneGroup, parenGroup, byGroup, tagWord, siteSuffix, junkSuffix, notGroup, trailing, ext, releaseLike, multiSubs, subsMarker, audioNote, episodeMark, hashOrNum, seasonEp, airDate, extra *regexp.Regexp
}

func loadRx() {
	rx.once.Do(func() {
		rx.resolution = tags(
			`2160p|4k|uhd|(?:3840|4096)x2160`, "2160p",
			`1440p`, "1440p",
			`(?:bd|hd)?1080[pi]|(?:1920|1440)x1080`, "1080p",
			`(?:bd|hd)?720p|1280x720`, "720p",
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
			`atvp|aptv|atv`+webTag, "ATVP",
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
			`l?pcm`+ch, "pcm",
		)
		rx.hdr = tags(
			`dv|dovi|dolby`+sep+`?vision`, "DV",
			`hdr10\+|hdr10plus`, "HDR10+",
			`hdr10|hdr`, "HDR",
			`hlg`, "HLG",
		)
		rx.flags = tags(
			`(?:(?:with|multi|french)`+sep+`)?audio`+sep+`description|(?:with|multi|french)`+sep+`ad`, "audio-description",
			`sign`+sep+`language|with`+sep+`[ab]sl|basl`, "sign-language",
			`commentary`, "commentary",
			`3d|h?sbs|half`+sep+`sbs`, "3d",
			`(?:ai`+sep+`)?upscaled?|aius|uprez|ai`+sep+`?(?:enhanced?|remaster(?:ed)?)`, "upscaled",
			`dubbed|dub|dubbing|dab|dabing`, "dubbed",
			`dual(?:`+sep+`audio)?|multi(?:`+sep+`audio)?|(?:german|ger)`+sep+`dl|`+
				`(?:eng|english)[ ._+&/\-]+`+pairLangs+`|`+pairLangs+`[ ._+&/\-]+(?:eng|english)`, "dual-audio",
			`hardsub(?:bed)?|hardcoded|hc|korsub`, "hardsub",
		)
		rx.edition = tags(
			`extended|directors`+sep+`cut|uncut|unrated`, "extended",
		)
		// Audio language tags, as the country the addons' flags use.
		rx.lang = tags(
			`ita|italian|italiano`, "it",
			`fre|french|truefrench|vff|vfq|vfi|vf2`, "fr",
			`spa|spanish|esp|castellano`, "es",
			`latino|lat`, "mx",
			`ger|german|deutsch`, "de",
			`ru|rus|russian`, "ru",
			`jpn|jap|japanese`, "jp",
			`kor|korean`, "kr",
			`chinese|mandarin|cantonese`, "cn",
			`hindi|tamil|telugu|malayalam|kannada`, "in",
			`portuguese`, "pt", // not "por": it's a common Spanish word
			`pt`+sep+`?br|dublado|brazilian`, "br",
			`pl|pol|polish|lektor`, "pl",
			`cz|cze|czech`, "cz",
			`hun|hungarian`, "hu",
			`dutch|nld`, "nl",
			`tur|turkish`, "tr",
			`(?:\d+x)?ukr|ukrainian`, "ua",
		)

		rx.animeGroup = regexp.MustCompile(`^\[([^\]]+)\]`)
		// Multi-Sub is about subtitles; it mustn't read as multi audio.
		rx.multiSubs = regexp.MustCompile(`(?i)multi(?:ple)?[ ._\-]?sub(?:s|titles?)?`)
		// Episode numbers in their usual forms: S01E02, S01 E02, 1x02, E02,
		// Cap.102, "- 02".
		rx.episodeMark = regexp.MustCompile(`(?i)\bs\d{1,2}[ ._]?e\d{1,4}(?:v\d)?\b|\b\d{1,2}x\d{1,4}\b|\be(?:p|pisode)?[ ._]?\d{1,4}(?:v\d)?\b|\bcap\.?\d{2,4}\b|\s-\s\d{1,4}(?:v\d)?\b|_-_\d{1,4}|\b(?:19|20)\d{2}[ ._-]\d{2}[ ._-]\d{2}\b`)
		rx.seasonEp = regexp.MustCompile(`(?i)\bs(\d{1,2})[ ._]?e(\d{1,4})(?:[ ._-]?e(\d{1,4}))?|\b(\d{1,2})x(\d{1,4})\b`)
		rx.extra = regexp.MustCompile(`(?i)\b(?:specials?|sp\d*|ova|oad|ona|extras?|nc(?:op|ed)|pv|preview|recap|lite)\b|\b\d{1,3}[ ._-]?(?:to|~)[ ._-]?\d{1,3}\b`)
		rx.airDate = regexp.MustCompile(`\b((?:19|20)\d{2})[ ._-](\d{2})[ ._-](\d{2})\b`)
		// Bare numbers and CRC hashes say nothing about a release's shape.
		rx.hashOrNum = regexp.MustCompile(`^(?:\d+|[0-9a-f]{8})$`)
		rx.subsMarker = regexp.MustCompile(`(?i)multi(?:ple)?[ ._\-]?sub(?:s|titles?)?|napisy|subtitulad[ao]`)
		rx.audioNote = regexp.MustCompile(`(?i)(?:multi|dual)[ ._\-]?audio|dubbed`)
		rx.sceneGroup = regexp.MustCompile(`-([A-Za-z0-9]+(?:\.[A-Za-z][A-Za-z0-9]*)?)$`)
		// P2P groups like QxR sign off inside the last brackets: (... x265 Tigole).
		// A reposter's name after the group: -FLUX_Morgann23.
		rx.siteSuffix = regexp.MustCompile(`(?i)\.(?:rarbg|rartv|eztv|ettv|tgx|yts|vtv)$|(-[A-Za-z0-9]+)_[A-Za-z0-9]+$`)
		// Russian releases sign off "...2160p.by.AKTEP".
		rx.byGroup = regexp.MustCompile(`(?i)[ ._]by[ ._]([A-Za-z0-9]+)$`)
		rx.tagWord = regexp.MustCompile(`(?i)^(?:hi10p?|\d+bit|sdr|atmos|remastered|repack|proper|audio|subs?|dub|eng|english|ma)$`)
		rx.parenGroup = regexp.MustCompile(`\(([^()]*\s)?([A-Za-z][A-Za-z0-9]+)\)\s*$`)
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
	paren := rx.parenGroup.FindStringSubmatch(name)
	name = rx.trailing.ReplaceAllString(name, "")
	name = rx.junkSuffix.ReplaceAllString(name, "")
	name = rx.siteSuffix.ReplaceAllString(name, "$1")
	if m := rx.sceneGroup.FindStringSubmatch(name); m != nil && !slices.ContainsFunc(strings.Split(m[1], "."), isTag) {
		return m[1]
	}
	if m := rx.byGroup.FindStringSubmatch(name); m != nil && !isTag(m[1]) {
		return m[1]
	}
	if paren != nil && paren[1] != "" && !isTag(paren[2]) {
		return paren[2]
	}
	return ""
}

// isTag reports whether a word is one of the tags a release name carries
// rather than a group name.
func isTag(w string) bool {
	if rx.notGroup.MatchString(w) {
		return true
	}
	for _, ts := range [][]tag{rx.resolution, rx.source, rx.service, rx.codec, rx.audio, rx.hdr, rx.flags, rx.edition, rx.lang} {
		if firstTag(w, ts) != "" {
			return true
		}
	}
	return rx.tagWord.MatchString(w)
}

// ParseReleaseName pulls the fields autoplay matches on out of a release or
// file name.
func ParseReleaseName(name string) ReleaseInfo { return parseRelease(name) }

// parseRelease reads name, leaving the given titles' words out of the
// variant, edition and language tags.
func parseRelease(name string, titles ...string) ReleaseInfo {
	loadRx()
	name = rx.ext.ReplaceAllString(strings.TrimSpace(name), "")
	rest := name
	for _, t := range titles {
		if re := titlePattern(t); re != nil {
			rest = re.ReplaceAllString(rest, " ")
		}
	}
	r := ReleaseInfo{
		Group:      strings.ToLower(releaseGroup(name)),
		Resolution: firstTag(name, rx.resolution),
		Source:     firstTag(name, rx.source),
		Service:    firstTag(name, rx.service),
		Codec:      firstTag(name, rx.codec),
		Audio:      firstTag(name, rx.audio),
		HDR:        strings.Join(allTags(name, rx.hdr), "+"),
		Edition:    firstTag(rest, rx.edition),
		Flags:      allTags(rx.multiSubs.ReplaceAllString(rest, " "), rx.flags),
		Langs:      allTags(beforeSubs(rest), rx.lang),
	}
	return r
}

// beforeSubs is the name up to a subtitle marker: groups like Erai-raws list
// their subtitle languages after "[Multiple Subtitle]", and those aren't audio.
func beforeSubs(name string) string {
	if loc := rx.subsMarker.FindStringIndex(name); loc != nil {
		return name[:loc[0]]
	}
	return name
}

// FlagLangs reads the languages addons show as flags (Torrentio, AIOStreams)
// out of a stream's text. English (🇬🇧, 🇺🇸) is left out, as with Langs.
// Next to "Multi Subs" with nothing about the audio, the flags may be the
// subtitles', so they're not read.
func FlagLangs(text string) []string {
	loadRx()
	if rx.subsMarker.MatchString(text) && !rx.audioNote.MatchString(text) {
		return nil
	}
	var out []string
	rs := []rune(text)
	for i := 0; i+1 < len(rs); i++ {
		a, b := rs[i]-0x1F1E6, rs[i+1]-0x1F1E6
		if a < 0 || a > 25 || b < 0 || b > 25 {
			continue
		}
		if code := string([]rune{'a' + a, 'a' + b}); code != "gb" && code != "us" {
			out = append(out, code)
		}
		i++
	}
	return mergeLangs(out)
}

// WithLangs adds languages read from elsewhere, such as the stream's flags.
func (r ReleaseInfo) WithLangs(langs []string) ReleaseInfo {
	r.Langs = mergeLangs(append(append([]string{}, r.Langs...), langs...))
	return r
}

// Original drops the show's original language, which a release may or may
// not bother to tag: ToonsHub writes JPN on anime, SubsPlease doesn't.
func (r ReleaseInfo) Original(lang string) ReleaseInfo {
	r.Langs = slices.DeleteFunc(slices.Clone(r.Langs), func(l string) bool { return l == lang })
	if len(r.Langs) == 0 {
		r.Langs = nil
	}
	return r
}

func mergeLangs(langs []string) []string {
	sort.Strings(langs)
	out := langs[:0]
	for i, l := range langs {
		if i == 0 || l != langs[i-1] {
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ParseReleaseNameTitled is ParseReleaseName for a release of the given
// titles, the show's and the episode's. Their words don't count as variant,
// edition or language tags, so an episode named "Dubbed" or a show called
// "The Italian Job" isn't read as one.
func ParseReleaseNameTitled(name string, titles ...string) ReleaseInfo {
	return parseRelease(name, titles...)
}

// titlePattern matches title however a release writes it: any case, with
// dots, spaces or nothing between the words ("Rick's" becomes "Ricks").
func titlePattern(title string) *regexp.Regexp {
	titleRx.mu.Lock()
	defer titleRx.mu.Unlock()
	if re, ok := titleRx.m[title]; ok {
		return re
	}
	if len(titleRx.m) >= 16 { // a few shows' worth; autoplay asks about one at a time
		clear(titleRx.m)
	}
	re := compileTitle(title)
	titleRx.m[title] = re
	return re
}

// titleRx keeps the last few title patterns: a pick compares every stream
// against the same show and episode titles.
var titleRx = struct {
	mu sync.Mutex
	m  map[string]*regexp.Regexp
}{m: map[string]*regexp.Regexp{}}

func compileTitle(title string) *regexp.Regexp {
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

// NameShape is what's left of a release name once the show, the episode
// title and the episode number are taken out, as a set of lowercase words:
// "Carrie S01 E01.mkv" and "Carrie S01 E02.mkv" have the same shape.
func NameShape(name string, titles ...string) map[string]bool {
	loadRx()
	name = rx.ext.ReplaceAllString(strings.TrimSpace(name), "")
	out := map[string]bool{}
	// How the words are joined is part of the shape: dots, spaces or underscores.
	seps := map[rune]int{}
	for _, r := range name {
		if r == '.' || r == ' ' || r == '_' {
			seps[r]++
		}
	}
	if len(seps) > 0 {
		joint := '.'
		for r, n := range seps {
			if n > seps[joint] || n == seps[joint] && r < joint {
				joint = r
			}
		}
		out["\x00"+string(joint)] = true
	}
	for _, t := range titles {
		if re := titlePattern(t); re != nil {
			name = re.ReplaceAllString(name, " ")
		}
	}
	// So is how the episode is numbered: " - 02", "S01E02", "1x02", "Ep02".
	if m := rx.episodeMark.FindString(name); m != "" {
		out["\x00"+markStyle(m)] = true
	}
	name = rx.episodeMark.ReplaceAllString(name, " ")
	for _, w := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if !rx.hashOrNum.MatchString(w) {
			out[w] = true
		}
	}
	return out
}

// TitleWords are the words of a release name before its episode number,
// less the group tag, tags and the given episode titles: the show as that
// release names it.
func TitleWords(name string, drop ...string) map[string]bool {
	loadRx()
	name = rx.ext.ReplaceAllString(strings.TrimSpace(name), "")
	name = rx.animeGroup.ReplaceAllString(name, " ")
	for _, t := range drop {
		if re := titlePattern(t); re != nil {
			name = re.ReplaceAllString(name, " ")
		}
	}
	if loc := rx.episodeMark.FindStringIndex(name); loc != nil {
		name = name[:loc[0]]
	}
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(w) >= 3 && !rx.hashOrNum.MatchString(w) && w != "the" && w != "and" && w != "season" && !isTag(w) {
			out[w] = true
		}
	}
	return out
}

// SameTitle reports whether two sets of title words name the same show: at
// least half the shorter one is in the other. A name with no title words
// can't say, so it agrees.
func SameTitle(a, b map[string]bool) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	both := 0
	for w := range a {
		if b[w] {
			both++
		}
	}
	return 2*both >= min(len(a), len(b))
}

// EqualTitle reports whether two releases name the show with the same words,
// which keeps a season apart from the next: "Chuunibyou demo Koi ga Shitai!"
// and "... Ren". A name with no title words can't say, so it agrees.
func EqualTitle(a, b map[string]bool) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for w := range a {
		if !b[w] {
			return false
		}
	}
	return true
}

// Extra reports whether a release is something besides a regular episode:
// a special, an OVA, extras, or a batch of several.
func Extra(name string) bool {
	loadRx()
	return rx.extra.MatchString(name)
}

// NameEpisode reads the season and episodes a release name gives, as S01E02,
// S01E01E02 or 1x02.
func NameEpisode(name string) (season, first, last int, ok bool) {
	loadRx()
	m := rx.seasonEp.FindStringSubmatch(name)
	if m == nil {
		return 0, 0, 0, false
	}
	n := func(s string) int { v, _ := strconv.Atoi(s); return v }
	if m[1] != "" {
		season, first, last = n(m[1]), n(m[2]), n(m[2])
		if m[3] != "" {
			last = n(m[3])
		}
	} else {
		season, first, last = n(m[4]), n(m[5]), n(m[5])
	}
	return season, first, last, true
}

// NameDate reads an air date written into a release name, as daily shows
// are named: WWE.SmackDown.2026.10.23.
func NameDate(name string) (time.Time, bool) {
	loadRx()
	m := rx.airDate.FindStringSubmatch(name)
	if m == nil {
		return time.Time{}, false
	}
	d, err := time.Parse("2006 01 02", m[1]+" "+m[2]+" "+m[3])
	return d, err == nil
}

// markStyle names the way an episode number is written, ignoring the digits
// and keeping the case of its letters: "S01E02" and "s01e02" differ.
func markStyle(m string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return '#'
		}
		return r
	}, strings.TrimSpace(m))
}

// ShapeLikeness is how alike two name shapes are, 0 to 1.
func ShapeLikeness(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1
	}
	both := 0
	for w := range a {
		if b[w] {
			both++
		}
	}
	return float64(both) / float64(len(a)+len(b)-both)
}

// SameFlags reports whether two releases are the same variant.
func (r ReleaseInfo) SameFlags(o ReleaseInfo) bool {
	return strings.Join(r.Flags, ",") == strings.Join(o.Flags, ",")
}

// SameLangs reports whether o has the audio r had. Multi-audio releases list
// whatever they carry, so there o only needs r's languages among its own,
// and Japanese goes without saying: a dual-audio anime release has it
// whether or not the name says so.
func (r ReleaseInfo) SameLangs(o ReleaseInfo) bool {
	if strings.Join(r.Langs, ",") == strings.Join(o.Langs, ",") {
		return true
	}
	if !slices.Contains(r.Flags, "dual-audio") || !slices.Contains(o.Flags, "dual-audio") {
		return false
	}
	for _, l := range r.Langs {
		if l != "jp" && !slices.Contains(o.Langs, l) {
			return false
		}
	}
	return true
}

// Matches lists, in order of importance, whether o agrees with r on each
// field autoplay cares about. A field r doesn't have counts as agreeing,
// except HDR and edition, where none is a value of its own.
func (r ReleaseInfo) Matches(o ReleaseInfo) [8]bool {
	same := func(a, b string) bool { return a == "" || a == b }
	return [8]bool{
		r.SameFlags(o) && r.SameLangs(o),
		same(r.Resolution, o.Resolution),
		r.HDR == o.HDR, // untagged is SDR, and Dolby Vision on an SDR screen plays tinted
		same(r.Group, o.Group),
		same(r.Source, o.Source) && same(r.Service, o.Service),
		same(r.Codec, o.Codec),
		same(r.Audio, o.Audio),
		r.Edition == o.Edition,
	}
}

package stremio

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"stremio-cliuwu/internal/cache"
)

// Subtitles come from addons that declare the resource, same as streams.
//
// The protocol says the id in /subtitles/{type}/{id}.json is the OpenSubtitles
// file hash, with the video id passed as an extra argument. We have no hash —
// that needs the file on disk — so the video id goes in the id position, which
// is what the addons people actually install accept. The cost is that
// hash-matched results aren't available, only id-matched ones.

type Subtitle struct {
	ID   string `json:"id"`
	URL  string `json:"url"`
	Lang string `json:"lang"`

	// Not in the spec, but OpenSubtitles sends them and they're the only
	// thing that makes a list of forty English subtitles choosable — the id
	// on its own is a database number.
	SubtitleFileName string `json:"subtitleFileName"`
	MovieReleaseName string `json:"movieReleaseName"`

	Addon string // injected
	Rank  int    // injected: the addon's position in your list
}

var CacheSubs = cache.New[[]Subtitle](10*time.Minute, 60)

// SubtitleAddons are the installed addons offering subtitles for this item.
// Reuses the same resource check as streams and meta, so idPrefixes and
// per-resource type lists are honoured rather than re-implemented here.
func SubtitleAddons(addons []Addon, mediaType, videoID string) []Addon {
	var out []Addon
	for _, a := range addons {
		if a.Err == nil && a.SupportsResource("subtitles", mediaType, videoID) {
			out = append(out, a)
		}
	}
	return out
}

// GetSubtitles asks every subtitle addon at once and merges the results.
// SubsQuery is what the picker knows about the file being played.
type SubsQuery struct {
	MediaType string
	VideoID   string

	// All three come from the stream's behaviorHints, and all three are
	// extras the protocol defines for this request — stremio-core names them
	// videoHash, videoSize and videoFilename. The more of them an addon
	// gets, the better it can pick out subtitles cut for this exact release.
	Hash     string
	Size     int64
	Filename string
}

// paths are the endpoints worth asking, best first.
//
// Addons disagree on what the hash means. OpenSubtitles treats it as a hint
// and returns hash-matched results plus the rest; AIOStreams treats it as a
// filter and returns nothing at all when it doesn't recognise one. Since
// neither behaviour is wrong, both forms get asked and the results merged —
// hash-matched first, because those are timed to the actual file.
func (q SubsQuery) paths() []string {
	plain := q.path(false)
	if q.Hash == "" && q.Size == 0 && q.Filename == "" {
		return []string{plain}
	}
	return []string{q.path(true), plain}
}

// path builds the subtitles endpoint.
//
// The id is the video id, the same as a stream request — the SDK changed this
// deliberately for consistency, and the OpenSubtitles hash moved to an extra
// property. Sending the hash as the id, which an older description of the
// protocol suggests, gets nothing back at all.
//
// The hash is what lets an addon return subtitles timed to the exact release
// rather than to the episode in general.
func (q SubsQuery) path(withHints bool) string {
	t := q.MediaType
	if t == "" {
		t = "movie"
	}
	base := fmt.Sprintf("/subtitles/%s/%s", t, url.PathEscape(q.VideoID))
	if !withHints {
		return base + ".json"
	}

	var extra []string
	if q.Hash != "" {
		extra = append(extra, "videoHash="+url.QueryEscape(q.Hash))
	}
	if q.Size > 0 {
		extra = append(extra, "videoSize="+strconv.FormatInt(q.Size, 10))
	}
	if q.Filename != "" {
		extra = append(extra, "filename="+url.QueryEscape(q.Filename))
	}
	if len(extra) == 0 {
		return base + ".json"
	}
	return base + "/" + strings.Join(extra, "&") + ".json"
}

// SubsKey is the cache key for a subtitle query.
func SubsKey(q SubsQuery) string {
	return q.MediaType + ":" + q.VideoID + ":" + q.Hash + ":" + q.Filename
}

func GetSubtitles(addons []Addon, q SubsQuery, lang string) []Subtitle {
	videoID, mediaType := q.VideoID, q.MediaType
	if videoID == "" {
		return nil
	}
	key := SubsKey(q)
	if v, ok := CacheSubs.Get(key); ok {
		return v
	}
	if mediaType == "" {
		mediaType = "movie"
	}

	usable := SubtitleAddons(addons, mediaType, videoID)
	results := make([][]Subtitle, len(usable))

	var wg sync.WaitGroup
	for i, a := range usable {
		wg.Add(1)
		go func(i int, a Addon) {
			defer wg.Done()

			base := addonBase(a)

			var found []Subtitle
			for _, p := range q.paths() {
				var resp struct {
					Subtitles []Subtitle `json:"subtitles"`
				}
				if GetJSON(base+p, &resp) != nil {
					continue
				}
				found = append(found, resp.Subtitles...)
			}

			for j := range found {
				found[j].Addon = a.Manifest.Name
				found[j].Rank = i
			}
			results[i] = found
		}(i, a)
	}
	wg.Wait()

	var out []Subtitle
	seen := map[string]bool{}
	for _, set := range results {
		for _, s := range set {
			if s.URL == "" || seen[s.URL] {
				continue
			}
			seen[s.URL] = true
			out = append(out, s)
		}
	}

	SortSubtitles(out, lang)

	// Empty results aren't cached. "None found" is usually a broken addon or
	// one you haven't added yet, and caching it means fixing the addon
	// appears to change nothing for the next ten minutes.
	if len(out) > 0 {
		CacheSubs.Set(key, out)
	}
	return out
}

// PreferredLangs splits the subtitle language setting into canonical names.
//
// It's a list, not a single value — mpv's --slang takes a comma-separated
// preference order, so "eng, en, English" is a reasonable thing to type. It
// was being treated as one language name, which of course matched nothing.
func PreferredLangs(setting string) []string {
	var out []string
	seen := map[string]bool{}

	for _, part := range strings.Split(setting, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if n := LangName(part); !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// langRank is a language's position in the preference list, or a large number
// if it isn't in it at all.
func langRank(prefs []string, lang string) int {
	for i, p := range prefs {
		if p == lang {
			return i
		}
	}
	return len(prefs) + 1
}

// MergeSubtitles combines stream-shipped subtitles with fetched ones, drops
// duplicates by URL, and orders the result.
//
// Shipped ones keep rank 0 so they sort first within their language: they came
// with this exact file, which is a better claim to matching it than anything
// found by title.
func MergeSubtitles(shipped, found []Subtitle, preferred string) []Subtitle {
	out := make([]Subtitle, 0, len(shipped)+len(found))
	seen := map[string]bool{}

	for _, set := range [][]Subtitle{shipped, found} {
		for _, sub := range set {
			if sub.URL == "" || seen[sub.URL] {
				continue
			}
			seen[sub.URL] = true
			out = append(out, sub)
		}
	}

	SortSubtitles(out, preferred)
	return out
}

// Label is what identifies a subtitle to a human.
//
// Falls back through the names an addon might send before resorting to the
// id, which tells you nothing about which release it was cut for.
func (s Subtitle) Label() string {
	for _, v := range []string{s.SubtitleFileName, s.MovieReleaseName, s.ID} {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return "subtitle"
}

// SortSubtitles orders by your language preferences, then groups the rest by
// language, then by addon priority within each.
//
// Addon order matters here for the same reason it does for streams: if you've
// put one source above another it's because you trust its results more, and
// that shouldn't be undone by an alphabetical tiebreak on the addon's name.
func SortSubtitles(subs []Subtitle, preferred string) {
	prefs := PreferredLangs(preferred)

	sort.SliceStable(subs, func(i, j int) bool {
		li, lj := LangName(subs[i].Lang), LangName(subs[j].Lang)

		ri, rj := langRank(prefs, li), langRank(prefs, lj)
		if ri != rj {
			return ri < rj
		}
		if li != lj {
			return li < lj
		}
		return subs[i].Rank < subs[j].Rank
	})
}

// langCodes maps every code we recognise to a canonical display name.
// Package level so the settings screen can present it in reverse: which
// codes count as which language.
var langCodes = map[string]string{
	"eng": "English", "en": "English",
	"spa": "Spanish", "es": "Spanish",
	"fre": "French", "fra": "French", "fr": "French",
	"ger": "German", "deu": "German", "de": "German",
	"ita": "Italian", "it": "Italian",
	"por": "Portuguese", "pt": "Portuguese",
	"rus": "Russian", "ru": "Russian",
	"jpn": "Japanese", "ja": "Japanese",
	"kor": "Korean", "ko": "Korean",
	"chi": "Chinese", "zho": "Chinese", "zh": "Chinese",
	"ara": "Arabic", "ar": "Arabic",
	"dut": "Dutch", "nld": "Dutch", "nl": "Dutch",
	"pol": "Polish", "pl": "Polish",
	"tur": "Turkish", "tr": "Turkish",
	"swe": "Swedish", "sv": "Swedish",
	"dan": "Danish", "da": "Danish",
	"fin": "Finnish", "fi": "Finnish",
	"nor": "Norwegian", "no": "Norwegian",
	"heb": "Hebrew", "he": "Hebrew",
	"hin": "Hindi", "hi": "Hindi",
	"ell": "Greek", "gre": "Greek", "el": "Greek",
	"ces": "Czech", "cze": "Czech", "cs": "Czech",
	"ron": "Romanian", "rum": "Romanian", "ro": "Romanian",
	"hun": "Hungarian", "hu": "Hungarian",
	"tha": "Thai", "th": "Thai",
	"vie": "Vietnamese", "vi": "Vietnamese",
	"ind": "Indonesian", "id": "Indonesian",
	"ukr": "Ukrainian", "uk": "Ukrainian",
}

// LangName is the canonical display name for a language, and doubles as the
// grouping key.
//
// Addons are inconsistent: the same language arrives as "eng", "en" and
// "English" from three sources, which made three separate tabs for one
// language. Normalising on the way in collapses them. The spec explicitly
// allows free text here, so anything unrecognised passes through as itself
// rather than being dropped.
func LangName(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		return "unknown"
	}
	if n, ok := langCodes[code]; ok {
		return n
	}

	// Regional variants: en-GB, pt_BR, es-419. The region only narrows a
	// language it's already named, so the base code decides.
	if i := strings.IndexAny(code, "-_"); i > 0 {
		if n, ok := langCodes[code[:i]]; ok {
			return n
		}
	}

	// Already a name rather than a code: "english", "brazilian portuguese".
	for _, n := range langCodes {
		if strings.EqualFold(n, code) {
			return n
		}
	}
	return code
}

// LangReference lists each language with the codes that resolve to it, for
// the settings prompt — otherwise there's no way to know what to type.
func LangReference() []string {
	byName := map[string][]string{}
	for code, name := range langCodes {
		byName[name] = append(byName[name], code)
	}

	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)

	width := 0
	for _, n := range names {
		if len(n) > width {
			width = len(n)
		}
	}

	out := make([]string, 0, len(names))
	for _, n := range names {
		codes := byName[n]
		sort.Slice(codes, func(i, j int) bool {
			if len(codes[i]) != len(codes[j]) {
				return len(codes[i]) > len(codes[j]) // three-letter first
			}
			return codes[i] < codes[j]
		})
		out = append(out, fmt.Sprintf("%-*s  %s", width, n, strings.Join(codes, " · ")))
	}
	return out
}

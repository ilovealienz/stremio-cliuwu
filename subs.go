package main

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
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

	Addon string // injected
	Rank  int    // injected: the addon's position in your list
}

var cacheSubs = newCache[[]Subtitle](10*time.Minute, 60)

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
func GetSubtitles(addons []Addon, mediaType, videoID string) []Subtitle {
	if videoID == "" {
		return nil
	}
	key := mediaType + ":" + videoID
	if v, ok := cacheSubs.Get(key); ok {
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

			var resp struct {
				Subtitles []Subtitle `json:"subtitles"`
			}
			u := fmt.Sprintf("%s/subtitles/%s/%s.json",
				strings.TrimSuffix(a.TransportURL, "/manifest.json"),
				mediaType, url.PathEscape(videoID))

			if getJSON(u, &resp) != nil {
				return
			}
			for j := range resp.Subtitles {
				resp.Subtitles[j].Addon = a.Manifest.Name
				resp.Subtitles[j].Rank = i
			}
			results[i] = resp.Subtitles
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

	SortSubtitles(out, ctx.cfg.SubtitleLang)

	// Empty results aren't cached. "None found" is usually a broken addon or
	// one you haven't added yet, and caching it means fixing the addon
	// appears to change nothing for the next ten minutes.
	if len(out) > 0 {
		cacheSubs.Set(key, out)
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
		if n := langName(part); !seen[n] {
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

// SortSubtitles orders by your language preferences, then groups the rest by
// language, then by addon priority within each.
//
// Addon order matters here for the same reason it does for streams: if you've
// put one source above another it's because you trust its results more, and
// that shouldn't be undone by an alphabetical tiebreak on the addon's name.
func SortSubtitles(subs []Subtitle, preferred string) {
	prefs := PreferredLangs(preferred)

	sort.SliceStable(subs, func(i, j int) bool {
		li, lj := langName(subs[i].Lang), langName(subs[j].Lang)

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

// langName is the canonical display name for a language, and doubles as the
// grouping key.
//
// Addons are inconsistent: the same language arrives as "eng", "en" and
// "English" from three sources, which made three separate tabs for one
// language. Normalising on the way in collapses them. The spec explicitly
// allows free text here, so anything unrecognised passes through as itself
// rather than being dropped.
func langName(code string) string {
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

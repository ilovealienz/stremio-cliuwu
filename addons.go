package main

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
)

// Addons are now plain manifest URLs kept in addons.json. No account, no
// authKey, no vault — you paste the same URLs you'd paste into Stremio:
//
//	https://v3-cinemeta.strem.io/manifest.json
//	https://torrentio.strem.fun/torbox=<key>/manifest.json
//
// Anything the old GetAddons() call returned was just this list anyway.

const defaultCinemeta = "https://v3-cinemeta.strem.io/manifest.json"
const defaultKitsu = "https://anime-kitsu.strem.fun/manifest.json"

// ── Persistence ───────────────────────────────────────────────────────────────

func LoadAddonRefs() AddonList {
	var al AddonList
	if err := readJSON(addonsFile(), &al); err != nil {
		return AddonList{}
	}
	return al
}

func SaveAddonRefs(al AddonList) error { return writeJSON(addonsFile(), al) }

// SeedAddons writes the two metadata addons every install needs. Called once
// when addons.json doesn't exist yet.
func SeedAddons() AddonList {
	al := AddonList{Items: []AddonRef{
		{URL: defaultCinemeta},
		{URL: defaultKitsu},
	}}
	SaveAddonRefs(al)
	return al
}

func AddAddonRef(raw string) (string, error) {
	u, err := NormalizeManifestURL(raw)
	if err != nil {
		return "", err
	}
	al := LoadAddonRefs()
	for _, it := range al.Items {
		if it.URL == u {
			return u, fmt.Errorf("already added")
		}
	}
	al.Items = append(al.Items, AddonRef{URL: u})
	return u, SaveAddonRefs(al)
}

func RemoveAddonRef(idx int) {
	al := LoadAddonRefs()
	if idx < 0 || idx >= len(al.Items) {
		return
	}
	al.Items = append(al.Items[:idx], al.Items[idx+1:]...)
	SaveAddonRefs(al)
}

func ToggleAddonRef(idx int) {
	al := LoadAddonRefs()
	if idx < 0 || idx >= len(al.Items) {
		return
	}
	al.Items[idx].Disabled = !al.Items[idx].Disabled
	SaveAddonRefs(al)
}

// MoveAddonRef shifts an entry by delta. Order matters: stream results are
// grouped in addon order, so this is how you put your debrid addon on top.
func MoveAddonRef(idx, delta int) int {
	al := LoadAddonRefs()
	j := idx + delta
	if idx < 0 || idx >= len(al.Items) || j < 0 || j >= len(al.Items) {
		return idx
	}
	al.Items[idx], al.Items[j] = al.Items[j], al.Items[idx]
	SaveAddonRefs(al)
	return j
}

// ── URL handling ──────────────────────────────────────────────────────────────

// NormalizeManifestURL accepts anything you'd realistically paste and turns it
// into a canonical https manifest URL.
func NormalizeManifestURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("empty url")
	}

	// Markdown link paste: [text](url)
	if strings.HasPrefix(s, "[") {
		if i := strings.Index(s, "("); i >= 0 && strings.HasSuffix(s, ")") {
			s = s[i+1 : len(s)-1]
		}
	}

	// stremio:// deep links are just https with a different scheme
	s = strings.TrimPrefix(s, "stremio://")
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}

	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("not a url: %v", err)
	}
	if u.Host == "" {
		return "", fmt.Errorf("no host in url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		u.Scheme = "https"
	}
	if !strings.HasSuffix(u.Path, "/manifest.json") {
		u.Path = strings.TrimRight(u.Path, "/") + "/manifest.json"
	}
	return u.String(), nil
}

// addonLabel is a never-empty display name. Some manifests omit `name`, and an
// empty string was silently collapsing section headers to blank lines.
func addonLabel(a Addon) string {
	if n := strings.TrimSpace(a.Manifest.Name); n != "" {
		return n
	}
	if u, err := url.Parse(a.TransportURL); err == nil && u.Host != "" {
		return u.Host
	}
	if a.Manifest.ID != "" {
		return a.Manifest.ID
	}
	return "addon"
}

// addonBase strips /manifest.json to get the resource root.
func addonBase(a Addon) string {
	return strings.TrimSuffix(strings.TrimRight(a.TransportURL, "/"), "/manifest.json")
}

// RedactURL hides credentials embedded in an addon path so the TUI and any
// logs don't casually leak a debrid key.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	segs := strings.Split(u.Path, "/")
	for i, seg := range segs {
		if j := strings.Index(seg, "="); j > 0 {
			key := seg[:j]
			val := seg[j+1:]
			if len(val) > 6 {
				val = val[:3] + "…" + val[len(val)-2:]
			}
			segs[i] = key + "=" + val
		}
	}
	u.Path = strings.Join(segs, "/")
	return u.String()
}

// ── Fetching ──────────────────────────────────────────────────────────────────

func FetchAddon(manifestURL string) Addon {
	a := Addon{TransportURL: manifestURL}
	if err := getJSON(manifestURL, &a.Manifest); err != nil {
		a.Err = err
		return a
	}
	if a.Manifest.Name == "" {
		a.Manifest.Name = manifestURL
	}
	return a
}

// LoadAddons fetches every enabled manifest concurrently, preserving the
// configured order. Failed addons come back with Err set rather than being
// dropped, so the addons screen can show you what's broken.
func LoadAddons(al AddonList) []Addon {
	var enabled []AddonRef
	for _, r := range al.Items {
		if !r.Disabled {
			enabled = append(enabled, r)
		}
	}
	out := make([]Addon, len(enabled))
	var wg sync.WaitGroup
	for i, r := range enabled {
		wg.Add(1)
		go func(i int, r AddonRef) {
			defer wg.Done()
			out[i] = FetchAddon(r.URL)
		}(i, r)
	}
	wg.Wait()
	return out
}

// OKAddons filters out the ones that failed to load.
func OKAddons(in []Addon) []Addon {
	var out []Addon
	for _, a := range in {
		if a.Err == nil {
			out = append(out, a)
		}
	}
	return out
}

// AddonItem renders an addon row for the addons screen.
func AddonItem(ref AddonRef, a *Addon) Item {
	name := RedactURL(ref.URL)
	sub := ""
	badge := ""

	switch {
	case a == nil:
		badge = grey("…")
	case a.Err != nil:
		badge = bad("failed")
		sub = a.Err.Error()
	default:
		name = a.Manifest.Name
		var caps []string
		if a.HasStreams() {
			caps = append(caps, "streams")
		}
		if len(a.Manifest.Catalogs) > 0 {
			caps = append(caps, fmt.Sprintf("%d catalog(s)", len(a.Manifest.Catalogs)))
		}
		sub = strings.Join(caps, " · ")
		if a.Manifest.Version != "" {
			badge = grey("v" + a.Manifest.Version)
		}
	}

	if ref.Disabled {
		badge = grey("off")
	}
	return Item{Label: bold(name), Sub: sub, Badge: badge, Dim: ref.Disabled}
}

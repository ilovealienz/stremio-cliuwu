package stremio

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

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

// manifestTimeout bounds a single manifest fetch. Startup waits on all of
// them, so the slowest one sets how long the app takes to be usable.
const manifestTimeout = 5 * time.Second

func FetchAddon(manifestURL string) Addon {
	a := Addon{TransportURL: manifestURL}
	if err := GetJSONTimeout(manifestURL, &a.Manifest, manifestTimeout); err != nil {
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

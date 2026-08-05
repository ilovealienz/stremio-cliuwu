package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	urlCinemeta = "https://v3-cinemeta.strem.io"
	urlOMDB     = "https://www.omdbapi.com"
)

var httpClient = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:       10 * time.Second,
			KeepAlive:     30 * time.Second,
			FallbackDelay: -1, // prefer IPv4
		}).DialContext,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 10 * time.Second,
	},
}

func getJSON(u string, out any) error {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en-GB,en;q=0.9")
	r, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode >= 400 {
		return fmt.Errorf("http %d", r.StatusCode)
	}
	return json.NewDecoder(r.Body).Decode(out)
}

// ── Streams ───────────────────────────────────────────────────────────────────

// Debrid providers hand out time-limited URLs, so this TTL is deliberately
// short. InvalidateStreams drops an entry when a stream fails to open.
var cacheStreams = newCache[[]Stream](5*time.Minute, 100)

func InvalidateStreams(videoID string) { cacheStreams.Delete(videoID) }

func GetStreams(addons []Addon, mediaType, videoID string) []Stream {
	if v, ok := cacheStreams.Get(videoID); ok {
		return v
	}

	// Addons that can serve this id, in configured order.
	var usable []Addon
	for _, a := range addons {
		if a.Err != nil || !a.SupportsStream(mediaType, videoID) {
			continue
		}
		base := addonBase(a)
		if base == "" {
			continue
		}
		// Localhost addons are only reachable from inside the Stremio app.
		if strings.HasPrefix(base, "http://127.") || strings.HasPrefix(base, "http://localhost") {
			continue
		}
		usable = append(usable, a)
	}

	// Fan out — a debrid addon can take several seconds on its own, and doing
	// these serially was the single slowest thing in the old client.
	results := make([][]Stream, len(usable))
	var wg sync.WaitGroup
	for i, a := range usable {
		wg.Add(1)
		go func(i int, a Addon) {
			defer wg.Done()
			u := fmt.Sprintf("%s/stream/%s/%s.json", addonBase(a), mediaType, url.PathEscape(videoID))
			var resp struct {
				Streams []Stream `json:"streams"`
			}
			if getJSON(u, &resp) != nil {
				return
			}
			for j := range resp.Streams {
				resp.Streams[j].Addon = a.Manifest.Name
			}
			results[i] = resp.Streams
		}(i, a)
	}
	wg.Wait()

	var all []Stream
	for _, r := range results {
		all = append(all, r...)
	}
	cacheStreams.Set(videoID, all)
	return all
}

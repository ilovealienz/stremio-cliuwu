package stremio

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"stremio-cliuwu/internal/cache"
)

const (
	urlCinemeta = "https://v3-cinemeta.strem.io"
	urlOMDB     = "https://www.omdbapi.com"
)

var HTTPClient = &http.Client{
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

// HTTPError is a response with an error status.
type HTTPError struct{ Code int }

func (e *HTTPError) Error() string { return fmt.Sprintf("http %d", e.Code) }

func GetJSON(u string, out any) error { return GetJSONTimeout(u, out, 0) }

// GetJSONTimeout is GetJSON with a deadline of its own.
//
// A manifest is a few hundred bytes and either answers quickly or isn't
// coming. Letting it use the full fifteen second client timeout meant one
// unresponsive addon delayed everything waiting on the set.
func GetJSONTimeout(u string, out any, timeout time.Duration) error {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en-GB,en;q=0.9")

	if timeout > 0 {
		c, cancel := context.WithTimeout(req.Context(), timeout)
		defer cancel()
		req = req.WithContext(c)
	}

	r, err := HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode >= 400 {
		return &HTTPError{Code: r.StatusCode}
	}

	// A dead or moved addon usually answers with a web page rather than a
	// 404, and the decoder then reports `invalid character '<'`, which tells
	// you nothing about what went wrong. Peeking at the first byte costs
	// nothing and turns it into something actionable.
	body := bufio.NewReader(r.Body)
	if b, err := body.Peek(1); err == nil && (b[0] == '<') {
		return fmt.Errorf("got a web page, not json — wrong url, or the addon has moved")
	}

	err = json.NewDecoder(body).Decode(out)

	// A field whose type doesn't match isn't a reason to throw the response
	// away. Addons disagree on whether runtime, imdbRating and version are
	// strings or numbers, and encoding/json saves the type error and decodes
	// everything else — so the object is intact, and returning the error here
	// is what discarded it. The cost was a blank info panel, a missing episode
	// list, or an addon marked dead, cached for the next half hour.
	//
	// Syntax errors stay fatal because they're still returned. A wholly wrong
	// shape decodes to a zero value, which every caller already rejects by
	// checking that a required field came back.
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return nil
	}
	return err
}

// ── Streams ───────────────────────────────────────────────────────────────────

// Debrid providers hand out time-limited URLs, so this TTL is deliberately
// short. InvalidateStreams drops an entry when a stream fails to open.
// Keyed per addon, and only set when that addon answered.
var cacheStreams = cache.New[[]Stream](5*time.Minute, 400)

// streamFailed holds addons that just failed, so reopening the list doesn't
// wait out the same timeout again.
var streamFailed = cache.New[bool](30*time.Second, 400)

func streamKey(videoID, base string) string { return videoID + "\x00" + base }

// streamFlight joins concurrent fetches of the same addon and video.
var streamFlight = struct {
	mu    sync.Mutex
	calls map[string]*streamCall
}{calls: map[string]*streamCall{}}

type streamCall struct {
	done    chan struct{}
	streams []Stream
	ok      bool
}

func fetchStreams(key, u string) ([]Stream, bool) {
	streamFlight.mu.Lock()
	if c, busy := streamFlight.calls[key]; busy {
		streamFlight.mu.Unlock()
		<-c.done
		return c.streams, c.ok
	}
	c := &streamCall{done: make(chan struct{})}
	streamFlight.calls[key] = c
	streamFlight.mu.Unlock()

	var resp struct {
		Streams []Stream `json:"streams"`
	}
	err := GetJSON(u, &resp)
	var he *HTTPError
	if errors.As(err, &he) && he.Code == http.StatusNotFound {
		err = nil // the addon has nothing for this id
	}
	if err == nil {
		cacheStreams.Set(key, resp.Streams)
		c.streams, c.ok = resp.Streams, true
	} else {
		streamFailed.Set(key, true)
	}

	streamFlight.mu.Lock()
	delete(streamFlight.calls, key)
	streamFlight.mu.Unlock()
	close(c.done)
	return c.streams, c.ok
}

func InvalidateStreams(videoID string) {
	cacheStreams.DeletePrefix(videoID + "\x00")
	streamFailed.DeletePrefix(videoID + "\x00")
}

// RetryStreams is GetStreams ignoring recent failures, so only the addons that
// failed are asked again.
func RetryStreams(addons []Addon, mediaType, videoID string) ([]Stream, int) {
	streamFailed.DeletePrefix(videoID + "\x00")
	return GetStreams(addons, mediaType, videoID)
}

// ClearStreams drops every cached stream list and remembered failure.
func ClearStreams() {
	cacheStreams.Clear()
	streamFailed.Clear()
}

// GetStreams also returns how many addons failed or were skipped for failing.
func GetStreams(addons []Addon, mediaType, videoID string) ([]Stream, int) {
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
	failed := make([]bool, len(usable))
	var wg sync.WaitGroup
	for i, a := range usable {
		key := streamKey(videoID, addonBase(a))
		if v, ok := cacheStreams.Get(key); ok {
			results[i] = v
			continue
		}
		if _, ok := streamFailed.Get(key); ok {
			failed[i] = true
			continue
		}
		wg.Add(1)
		go func(i int, a Addon) {
			defer wg.Done()
			u := fmt.Sprintf("%s/stream/%s/%s.json", addonBase(a), mediaType, url.PathEscape(videoID))
			streams, ok := fetchStreams(key, u)
			results[i], failed[i] = streams, !ok
		}(i, a)
	}
	wg.Wait()

	var all []Stream
	nFailed := 0
	for i, r := range results {
		if failed[i] {
			nFailed++
		}
		for _, st := range r {
			st.Addon = usable[i].Manifest.Name
			st.Rank = i // position in the configured order
			all = append(all, st)
		}
	}
	return all, nFailed
}

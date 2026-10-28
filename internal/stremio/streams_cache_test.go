package stremio

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAddons answers by host: "flaky" fails while down, "empty" has no
// streams, "missing" is a 404, anything else has one stream.
type fakeAddons struct {
	mu        sync.Mutex
	hits      map[string]int
	flakyDown bool
}

func (f *fakeAddons) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.hits[r.URL.Host]++
	down := r.URL.Host == "flaky.test" && f.flakyDown
	f.mu.Unlock()

	rec := httptest.NewRecorder()
	switch {
	case down:
		rec.WriteHeader(http.StatusBadGateway)
	case r.URL.Host == "empty.test":
		rec.WriteString(`{"streams":[]}`)
	case r.URL.Host == "missing.test":
		rec.WriteHeader(http.StatusNotFound)
	default:
		rec.WriteString(`{"streams":[{"name":"` + r.URL.Host + ` 1080p","url":"http://x/a"}]}`)
	}
	return rec.Result(), nil
}

func (f *fakeAddons) count(host string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[host]
}

func testAddon(host string) Addon {
	return Addon{
		TransportURL: "http://" + host + "/manifest.json",
		Manifest:     Manifest{Name: host, Resources: []any{"stream"}},
	}
}

func TestStreamsCacheSkipsFailedAddons(t *testing.T) {
	f := &fakeAddons{hits: map[string]int{}, flakyDown: true}
	saved := HTTPClient.Transport
	HTTPClient.Transport = f
	t.Cleanup(func() { HTTPClient.Transport = saved; ClearStreams() })
	ClearStreams()

	addons := []Addon{testAddon("flaky.test"), testAddon("good.test"), testAddon("empty.test")}
	names := func(ss []Stream, _ int) string {
		var o []string
		for _, s := range ss {
			o = append(o, s.Addon+"#"+string(rune('0'+s.Rank)))
		}
		return strings.Join(o, ",")
	}

	if got := names(GetStreams(addons, "movie", "tt1")); got != "good.test#1" {
		t.Fatalf("first call = %q", got)
	}

	f.mu.Lock()
	f.flakyDown = false
	f.mu.Unlock()

	// A failure is remembered briefly, so reopening straight away doesn't ask again.
	if got := names(GetStreams(addons, "movie", "tt1")); got != "good.test#1" || f.count("flaky.test") != 1 {
		t.Fatalf("within failure window = %q, flaky hits %d", got, f.count("flaky.test"))
	}
	streamFailed.Clear() // the window passing

	if got := names(GetStreams(addons, "movie", "tt1")); got != "flaky.test#0,good.test#1" {
		t.Fatalf("after failure window = %q", got)
	}
	if f.count("flaky.test") != 2 || f.count("good.test") != 1 || f.count("empty.test") != 1 {
		t.Fatalf("hits after retry = %v", f.hits)
	}

	// Everything answered now, so nothing is asked again.
	GetStreams(addons, "movie", "tt1")
	if f.count("flaky.test") != 2 || f.count("good.test") != 1 || f.count("empty.test") != 1 {
		t.Fatalf("hits after cached call = %v", f.hits)
	}

	// Ranks follow the order passed in, not the order they were cached in.
	rev := []Addon{addons[1], addons[0]}
	if got := names(GetStreams(rev, "movie", "tt1")); got != "good.test#0,flaky.test#1" {
		t.Fatalf("reordered = %q", got)
	}

	InvalidateStreams("tt1")
	GetStreams(addons, "movie", "tt1")
	if f.count("flaky.test") != 3 || f.count("good.test") != 2 || f.count("empty.test") != 2 {
		t.Fatalf("hits after invalidate = %v", f.hits)
	}
}

func TestRetryStreamsAsksOnlyFailed(t *testing.T) {
	f := &fakeAddons{hits: map[string]int{}, flakyDown: true}
	saved := HTTPClient.Transport
	HTTPClient.Transport = f
	t.Cleanup(func() { HTTPClient.Transport = saved; ClearStreams() })
	ClearStreams()

	addons := []Addon{testAddon("flaky.test"), testAddon("good.test")}
	if s, failed := GetStreams(addons, "movie", "tt2"); len(s) != 1 || failed != 1 {
		t.Fatalf("first call: %d streams, %d failed", len(s), failed)
	}
	// Skipped inside the failure window, but still reported as failed.
	if _, failed := GetStreams(addons, "movie", "tt2"); failed != 1 || f.count("flaky.test") != 1 {
		t.Fatalf("skip: %d failed, flaky hits %d", failed, f.count("flaky.test"))
	}

	f.mu.Lock()
	f.flakyDown = false
	f.mu.Unlock()
	s, failed := RetryStreams(addons, "movie", "tt2")
	if len(s) != 2 || failed != 0 || f.count("flaky.test") != 2 || f.count("good.test") != 1 {
		t.Fatalf("retry: %d streams, %d failed, hits %v", len(s), failed, f.hits)
	}
}

// slowAddon holds every request until released, counting them.
type slowAddon struct {
	mu      sync.Mutex
	hits    int
	release chan struct{}
}

func (s *slowAddon) RoundTrip(r *http.Request) (*http.Response, error) {
	s.mu.Lock()
	s.hits++
	s.mu.Unlock()
	<-s.release
	rec := httptest.NewRecorder()
	rec.WriteString(`{"streams":[{"name":"x","url":"http://x/a"}]}`)
	return rec.Result(), nil
}

func TestStreamsConcurrentCallsShareRequest(t *testing.T) {
	s := &slowAddon{release: make(chan struct{})}
	saved := HTTPClient.Transport
	HTTPClient.Transport = s
	t.Cleanup(func() { HTTPClient.Transport = saved; ClearStreams() })
	ClearStreams()

	addons := []Addon{testAddon("slow.test")}
	var wg sync.WaitGroup
	got := make([]int, 2)
	for i := range got {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ss, _ := GetStreams(addons, "movie", "tt3")
			got[i] = len(ss)
		}(i)
	}
	for {
		streamFlight.mu.Lock()
		n := len(streamFlight.calls)
		streamFlight.mu.Unlock()
		if n == 1 {
			break
		}
	}
	time.Sleep(20 * time.Millisecond) // let the second call reach the join
	close(s.release)
	wg.Wait()
	if s.hits != 1 || got[0] != 1 || got[1] != 1 {
		t.Fatalf("hits=%d results=%v", s.hits, got)
	}
}

func TestStreams404IsNotAFailure(t *testing.T) {
	f := &fakeAddons{hits: map[string]int{}}
	saved := HTTPClient.Transport
	HTTPClient.Transport = f
	t.Cleanup(func() { HTTPClient.Transport = saved; ClearStreams() })
	ClearStreams()

	addons := []Addon{testAddon("missing.test")}
	if s, failed := GetStreams(addons, "movie", "tt4"); len(s) != 0 || failed != 0 {
		t.Fatalf("got %d streams, %d failed", len(s), failed)
	}
	GetStreams(addons, "movie", "tt4")
	if f.count("missing.test") != 1 {
		t.Fatalf("404 not cached: %d hits", f.count("missing.test"))
	}
}

func gib(f float64) int64 { return int64(f * (1 << 30)) }

func TestStreamBytes(t *testing.T) {
	hinted := Stream{Title: "💾 9 GB"}
	hinted.BehaviorHints.VideoSize = 1234
	for _, c := range []struct {
		st   Stream
		want int64
	}{
		{Stream{Title: "Show.mkv\n👤 50 💾 1.44 GB ⚙️ TPB"}, gib(1.44)},
		{Stream{Description: "Show\n843.5 MB"}, int64(843.5 * float64(1<<20))},
		{Stream{Description: "💾 2.1 GiB"}, gib(2.1)},
		{hinted, 1234},
		{Stream{Title: "no size here 1080p"}, 0},
	} {
		if got := StreamBytes(c.st); got != c.want {
			t.Errorf("%q: got %d, want %d", c.st.Title+c.st.Description, got, c.want)
		}
	}
}

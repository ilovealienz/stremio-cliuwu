package update

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"stremio-cliuwu/internal/stremio"
)

type fakeGitHub struct {
	body  string
	calls *atomic.Int32
}

func (f fakeGitHub) RoundTrip(r *http.Request) (*http.Response, error) {
	f.calls.Add(1)
	rec := httptest.NewRecorder()
	if f.body == "" {
		rec.WriteHeader(http.StatusServiceUnavailable)
	} else {
		rec.WriteString(f.body)
	}
	return rec.Result(), nil
}

// useGitHub answers the releases API with body, or a 503 when it's empty.
func useGitHub(t *testing.T, body string) *atomic.Int32 {
	calls := new(atomic.Int32)
	saved, savedVer := stremio.HTTPClient.Transport, Version
	stremio.HTTPClient.Transport = fakeGitHub{body, calls}
	t.Cleanup(func() {
		stremio.HTTPClient.Transport, Version = saved, savedVer
		updateMu.Lock()
		updateLoaded = false
		updateMu.Unlock()
	})
	updateMu.Lock()
	updateLoaded = false
	updateMu.Unlock()
	return calls
}

func check() Info { return Check()().(CheckedMsg).Info }

func TestCheckCaches(t *testing.T) {
	calls := useGitHub(t, `{"tag_name":"v0.7.0","html_url":"https://github.com/x/releases/v0.7.0"}`)
	Version = "0.6.9"
	if got := check(); got.State != Available || got.Latest != "v0.7.0" || got.URL == "" {
		t.Fatalf("got %+v", got)
	}
	check()
	if calls.Load() != 1 {
		t.Fatalf("asked GitHub %d times, want once", calls.Load())
	}
}

func TestCheckRetriesAfterFailure(t *testing.T) {
	calls := useGitHub(t, "")
	Version = "0.6.9"
	if got := check(); got.State != Unknown {
		t.Fatalf("failed check: %+v", got)
	}
	check()
	if calls.Load() != 1 {
		t.Fatalf("a failure was asked again straight away: %d calls", calls.Load())
	}
	// Five minutes on, it asks again rather than waiting the full six hours.
	updateMu.Lock()
	updateAt = updateAt.Add(-updateRetry - time.Second)
	updateMu.Unlock()
	check()
	if calls.Load() != 2 {
		t.Fatalf("not retried after %v: %d calls", updateRetry, calls.Load())
	}
}

func TestCheckIgnoresDraft(t *testing.T) {
	useGitHub(t, `{"tag_name":"v9.0.0","draft":true}`)
	Version = "0.6.9"
	if got := check(); got.State != Unknown {
		t.Fatalf("draft counted: %+v", got)
	}
}

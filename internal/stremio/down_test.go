package stremio

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"
)

// downWorld answers on up.test, never answers on down.test, and fails at once
// with a 500 on broken.test.
type downWorld struct{}

func (downWorld) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	switch r.URL.Host {
	case "down.test":
		<-r.Context().Done()
		return nil, r.Context().Err()
	case "broken.test":
		rec.WriteHeader(http.StatusInternalServerError)
	default:
		rec.WriteString(`{"streams":[{"name":"ok","url":"http://x/1"}]}`)
	}
	return rec.Result(), nil
}

// An addon that doesn't answer costs the timeout once, not once per episode.
func TestDownAddonIsSkipped(t *testing.T) {
	saved, savedTimeout := HTTPClient.Transport, HTTPClient.Timeout
	HTTPClient.Transport, HTTPClient.Timeout = downWorld{}, 300*time.Millisecond
	t.Cleanup(func() { HTTPClient.Transport, HTTPClient.Timeout = saved, savedTimeout; ClearCaches() })
	ClearCaches()
	addon := func(host string) Addon {
		return manifestAddon(host, `{"id":"`+host+`","resources":["stream"],"types":["series"]}`)
	}
	addons := []Addon{addon("up.test"), addon("down.test"), addon("broken.test")}
	fetch := func(id string) (time.Duration, int, int) {
		t0 := time.Now()
		s, failed := GetStreams(addons, "series", id)
		return time.Since(t0), len(s), failed
	}

	if d, n, failed := fetch("tt1:1:1"); d < 250*time.Millisecond || n != 1 || failed != 2 {
		t.Fatalf("first episode: %v, %d streams, %d failed", d, n, failed)
	}
	if d, n, failed := fetch("tt1:1:2"); d > 100*time.Millisecond || n != 1 || failed != 2 {
		t.Fatalf("next episode waited on the down addon: %v, %d streams, %d failed", d, n, failed)
	}
	if _, ok := addonDown.Get("https://broken.test"); ok {
		t.Fatal("a quick failure marked the addon down")
	}
	ForgetDownAddons() // R
	if d, _, _ := fetch("tt1:1:3"); d < 250*time.Millisecond {
		t.Fatal("R didn't ask the down addon again")
	}
}

type endless struct{}

func (endless) RoundTrip(r *http.Request) (*http.Response, error) {
	pr, pw := io.Pipe()
	go func() {
		pw.Write([]byte(`{"streams":[{"name":"`))
		chunk := bytes.Repeat([]byte("a"), 1<<16)
		for {
			if _, err := pw.Write(chunk); err != nil {
				return
			}
		}
	}()
	return &http.Response{StatusCode: 200, Body: pr, Header: http.Header{}}, nil
}

// A reply that never ends stops at the cap instead of filling memory.
func TestEndlessReplyIsCut(t *testing.T) {
	saved := HTTPClient.Transport
	HTTPClient.Transport = endless{}
	t.Cleanup(func() { HTTPClient.Transport = saved })
	var resp struct{ Streams []Stream }
	err := GetJSON("https://endless.test/stream/movie/tt1.json", &resp)
	if err == nil || !strings.Contains(err.Error(), "MB") {
		t.Fatalf("got %v", err)
	}
}

func TestCleanErrSaysWhatHappened(t *testing.T) {
	wrap := func(err error) error {
		return CleanErr(&url.Error{Op: "Get", URL: "https://addon.test/manifest.json", Err: err})
	}
	for err, want := range map[error]string{
		&net.DNSError{Err: "no such host", Name: "addon.test", IsNotFound: true}: "can't find addon.test — offline, or the address is wrong",
		&net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}:                      "addon.test refused the connection",
		context.DeadlineExceeded: "addon.test didn't answer in time",
	} {
		got := wrap(err)
		if got.Error() != want || !errors.Is(got, err) && !isTimeout(got) {
			t.Errorf("%v: got %q", err, got)
		}
	}
	if !isTimeout(wrap(context.DeadlineExceeded)) {
		t.Fatal("a cleaned timeout no longer reads as one")
	}
}

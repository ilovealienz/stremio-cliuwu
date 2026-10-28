package ui

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"stremio-cliuwu/internal/player"
	"stremio-cliuwu/internal/store"
	"stremio-cliuwu/internal/stremio"
)

// onceDown fails the first request to flaky.test, then answers.
type onceDown struct {
	mu    sync.Mutex
	flaky int
}

func (o *onceDown) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	o.mu.Lock()
	if r.URL.Host == "flaky.test" {
		o.flaky++
		if o.flaky == 1 {
			o.mu.Unlock()
			rec.WriteHeader(http.StatusBadGateway)
			return rec.Result(), nil
		}
	}
	o.mu.Unlock()
	rec.WriteString(`{"streams":[{"name":"` + r.URL.Host + `","url":"http://x/` + r.URL.Host + `"}]}`)
	return rec.Result(), nil
}

func TestAutoFetchPatientRetriesFailedAddons(t *testing.T) {
	savedDelays, savedTransport, savedCtx := streamRetryDelays, stremio.HTTPClient.Transport, ctx
	t.Cleanup(func() {
		streamRetryDelays, stremio.HTTPClient.Transport, ctx = savedDelays, savedTransport, savedCtx
		stremio.ClearStreams()
	})
	streamRetryDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}

	addon := func(host string) stremio.Addon {
		return stremio.Addon{
			TransportURL: "http://" + host + "/manifest.json",
			Manifest:     stremio.Manifest{Name: host, Resources: []any{"stream"}},
		}
	}
	aired := "2020-01-01T00:00:00.000Z"
	prev := player.PlayRequest{Queue: &stremio.EpQueue{Season: 1, Episodes: []stremio.Video{
		{ID: "tt9:1:1", Season: 1, Episode: 1, Released: aired},
		{ID: "tt9:1:2", Season: 1, Episode: 2, Released: aired},
	}}}

	// The flaky addon comes first in addon order, so it's picked once it answers.
	for _, c := range []struct {
		patient bool
		want    string
	}{{false, "good.test"}, {true, "flaky.test"}} {
		stremio.ClearStreams()
		stremio.HTTPClient.Transport = &onceDown{}
		ctx = &appCtx{cfg: &store.Config{}, player: player.New(store.Config{}),
			addons: []stremio.Addon{addon("flaky.test"), addon("good.test")}}
		m := autoFetch(prev, c.patient)().(autoPickMsg)
		if !m.ok || m.pick.Addon != c.want {
			t.Errorf("patient=%v: picked %q from %q, want %q", c.patient, m.pick.Name, m.pick.Addon, c.want)
		}
	}
}

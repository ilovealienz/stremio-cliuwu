package download

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFetchEdgeCases(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	body := bytes.Repeat([]byte("v"), 1000)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/page":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte("<html>link expired</html>"))
		case "/smaller":
			w.Header().Set("Content-Range", "bytes */500")
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		default:
			if r.Header.Get("Range") == fmt.Sprintf("bytes=%d-", len(body)) {
				w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", len(body)))
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			w.Write(body)
		}
	}))
	defer srv.Close()

	d := New()
	run := func(name string, total int64) (string, error) {
		path := filepath.Join(t.TempDir(), "ep.mkv")
		os.WriteFile(partPath(path), body, 0644)
		writePartInfo(path, partInfo{URL: srv.URL + name, Total: total})
		return path, d.fetch(&Download{ID: 1, URL: srv.URL + name, Path: path})
	}
	done := func(path string) bool {
		b, err := os.ReadFile(path)
		_, perr := os.Stat(partPath(path))
		_, ierr := os.Stat(infoPath(path))
		return err == nil && len(b) == len(body) && os.IsNotExist(perr) && os.IsNotExist(ierr)
	}

	// Whole file already in the partial, size known: no request needed.
	hits.Store(0)
	if path, err := run("/ep", int64(len(body))); err != nil || !done(path) || hits.Load() != 0 {
		t.Fatalf("known size: err=%v done=%v hits=%d", err, done(path), hits.Load())
	}
	// Size unknown, server says the range starts at the end.
	if path, err := run("/ep", 0); err != nil || !done(path) {
		t.Fatalf("416 at the end: err=%v", err)
	}
	// Server's file is now smaller: start over, no finished file.
	path, err := run("/smaller", 0)
	if err == nil || !strings.Contains(err.Error(), "start over") {
		t.Fatalf("416 mismatch: %v", err)
	}
	if _, err := os.Stat(partPath(path)); !os.IsNotExist(err) {
		t.Fatal("mismatched partial kept")
	}
	// An error page sent as 200.
	path = filepath.Join(t.TempDir(), "ep.mkv")
	err = d.fetch(&Download{ID: 2, URL: srv.URL + "/page", Path: path})
	if err == nil || !strings.Contains(err.Error(), "web page") {
		t.Fatalf("html: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("html page saved as the video")
	}
}

// A server that never sends a size: the finished download still has one.
func TestFetchUnknownSize(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte("v"), 3000))
		w.(http.Flusher).Flush() // chunked, so no Content-Length
		w.Write(bytes.Repeat([]byte("v"), 3000))
	}))
	defer srv.Close()

	dl := &Download{ID: 1, URL: srv.URL + "/ep", Path: filepath.Join(t.TempDir(), "ep.mkv")}
	if err := New().fetch(dl); err != nil {
		t.Fatal(err)
	}
	if dl.Done != 6000 || dl.Total != 6000 {
		t.Fatalf("done %d of %d, want 6000 of 6000", dl.Done, dl.Total)
	}
}

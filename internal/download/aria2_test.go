package download

import (
	"bytes"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"stremio-cliuwu/internal/store"
)

// throttled slows writes so a download can be stopped partway.
type throttled struct {
	http.ResponseWriter
	cut *atomic.Bool
}

func (t throttled) Write(b []byte) (int, error) {
	n := 0
	for len(b) > 0 {
		if t.cut.Load() {
			panic(http.ErrAbortHandler) // an expired link drops live connections too
		}
		c := min(len(b), 32<<10)
		w, err := t.ResponseWriter.Write(b[:c])
		n += w
		if err != nil {
			return n, err
		}
		b = b[c:]
		time.Sleep(20 * time.Millisecond)
	}
	return n, nil
}

type fileServer struct {
	*httptest.Server
	files    map[string][]byte
	resolves atomic.Int64
	expired  atomic.Bool
}

func newFileServer(t *testing.T) *fileServer {
	fs := &fileServer{files: map[string][]byte{}}
	for _, name := range []string{"ep.mkv", "other.mkv"} {
		b := make([]byte, 24<<20)
		rand.Read(b)
		fs.files[name] = b
	}
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/resolve/") {
			fs.resolves.Add(1)
			http.Redirect(w, r, "/files/"+strings.TrimPrefix(r.URL.Path, "/resolve/")+"?"+r.URL.RawQuery, http.StatusFound)
			return
		}
		if fs.expired.Load() {
			http.Error(w, "expired", http.StatusForbidden)
			return
		}
		b, ok := fs.files[filepath.Base(r.URL.Path)]
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(throttled{w, &fs.expired}, r, "", time.Time{}, bytes.NewReader(b))
	}))
	t.Cleanup(fs.Close)
	return fs
}

func aria2Setup(t *testing.T) (*Downloader, string) {
	if !Aria2Found() {
		t.Skip("aria2c not installed")
	}
	saved := store.CfgNow()
	on := saved
	on.Aria2 = true
	store.UseConfig(on)
	t.Cleanup(func() { store.UseConfig(saved) })
	freshState(t)
	path := filepath.Join(t.TempDir(), "Show S01E01.mkv")
	return testDownloader(t), path
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(60 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// probed reports whether the attempt using the link with this query has
// written its sidecar, which it does once it has decided where to start.
func probed(path, query string) bool {
	pi, ok := readPartInfo(path)
	return ok && strings.HasSuffix(pi.URL, query)
}

func only(d *Downloader) Download { return d.Snapshot()[0] }

func stopAt(t *testing.T, d *Downloader, frac float64) Download {
	waitFor(t, "progress", func() bool { dl := only(d); return dl.State == Active && dl.Frac() > frac })
	d.Cancel(only(d).ID)
	waitFor(t, "stop", func() bool { return only(d).State == Cancelled })
	return only(d)
}

func sameBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("file differs from the server's (err %v, %d of %d bytes)", err, len(got), len(want))
	}
	for _, f := range []string{partPath(path), infoPath(path), controlPath(path)} {
		if _, err := os.Stat(f); err == nil {
			t.Errorf("%s left behind", filepath.Base(f))
		}
	}
}

func TestAria2DownloadsThroughRedirect(t *testing.T) {
	d, path := aria2Setup(t)
	fs := newFileServer(t)
	d.Add("ep", fs.URL+"/resolve/ep.mkv?k=a", path)
	waitFor(t, "done", func() bool { return only(d).State == Done || only(d).State == Failed })
	if dl := only(d); dl.State != Done {
		t.Fatalf("state %v: %v", dl.State, dl.Err)
	}
	sameBytes(t, path, fs.files["ep.mkv"])
	if n := fs.resolves.Load(); n != 1 {
		t.Errorf("addon link asked %d times, want 1", n)
	}
}

func TestAria2StopAndResume(t *testing.T) {
	d, path := aria2Setup(t)
	fs := newFileServer(t)
	d.Add("ep", fs.URL+"/files/ep.mkv?k=a", path)
	dl := stopAt(t, d, 0.3)
	if _, err := os.Stat(controlPath(path)); err != nil {
		t.Fatal("no resume file after stopping")
	}
	if pi, _ := readPartInfo(path); pi.Done < dl.Total*3/10 || pi.Name != "ep.mkv" {
		t.Fatalf("sidecar after stop: %+v", pi)
	}

	// A restart reads progress from the sidecar, not the gappy file's size.
	d2 := New()
	d2.Scan(filepath.Dir(path))
	if got := only(d2); got.Done != dl.Done && got.Done < dl.Total*3/10 {
		t.Errorf("scan shows %d done, want about %d", got.Done, dl.Done)
	}

	d.Resume(dl.ID)
	waitFor(t, "resume to start", func() bool { return only(d).State == Active })
	waitFor(t, "done", func() bool { return only(d).State == Done || only(d).State == Failed })
	if got := only(d); got.State != Done || !got.Resume {
		t.Fatalf("state %v resume %v: %v", got.State, got.Resume, got.Err)
	}
	sameBytes(t, path, fs.files["ep.mkv"])
}

func TestAria2RefetchedLink(t *testing.T) {
	d, path := aria2Setup(t)
	fs := newFileServer(t)
	d.Add("ep", fs.URL+"/files/ep.mkv?k=old", path)
	stopAt(t, d, 0.3)

	// The same file under a new link carries on.
	d.Add("ep", fs.URL+"/files/ep.mkv?k=new", path)
	waitFor(t, "start", func() bool { return probed(path, "k=new") })
	if dl := only(d); !dl.Resume {
		t.Fatal("refetched link to the same file started over")
	}
	stopAt(t, d, 0.6)

	// Another file of the same size starts over, and comes out whole.
	d.Add("ep", fs.URL+"/files/other.mkv?k=x", path)
	waitFor(t, "start", func() bool { return probed(path, "k=x") })
	if dl := only(d); dl.Resume {
		t.Fatal("carried on into a different file")
	}
	waitFor(t, "done", func() bool { return only(d).State == Done || only(d).State == Failed })
	sameBytes(t, path, fs.files["other.mkv"])
}

func TestBuiltinRestartsAria2Partial(t *testing.T) {
	d, path := aria2Setup(t)
	fs := newFileServer(t)
	d.Add("ep", fs.URL+"/files/ep.mkv", path)
	dl := stopAt(t, d, 0.3)

	off := store.CfgNow()
	off.Aria2 = false
	store.UseConfig(off)
	d.Resume(dl.ID)
	waitFor(t, "done", func() bool { return only(d).State == Done || only(d).State == Failed })
	if got := only(d); got.State != Done {
		t.Fatalf("state %v: %v", got.State, got.Err)
	}
	sameBytes(t, path, fs.files["ep.mkv"])
}

func TestAria2RemoveWhileActive(t *testing.T) {
	d, path := aria2Setup(t)
	fs := newFileServer(t)
	d.Add("ep", fs.URL+"/files/ep.mkv", path)
	waitFor(t, "progress", func() bool { dl := only(d); return dl.State == Active && dl.Frac() > 0.2 })
	d.Remove(only(d).ID)
	// The row goes at once; the worker deletes the files once aria2c stops.
	waitFor(t, "files to be deleted", func() bool {
		for _, f := range []string{partPath(path), infoPath(path), controlPath(path), path} {
			if _, err := os.Stat(f); err == nil {
				return false
			}
		}
		return true
	})
}

func TestAria2ExpiredLink(t *testing.T) {
	d, path := aria2Setup(t)
	fs := newFileServer(t)
	d.Add("ep", fs.URL+"/files/ep.mkv", path)
	waitFor(t, "progress", func() bool { dl := only(d); return dl.State == Active && dl.Frac() > 0.2 })
	fs.expired.Store(true)
	waitFor(t, "failure", func() bool { return only(d).State == Failed })
	if err := only(d).Err; err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("error %v", err)
	}
	if _, err := os.Stat(controlPath(path)); err != nil {
		t.Error("resume file gone after an expired link")
	}
}

// An expired link answered with a 200 and an error page.
func TestAria2WebPage(t *testing.T) {
	d, path := aria2Setup(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<html>link expired</html>"))
	}))
	defer srv.Close()
	d.Add("ep", srv.URL+"/ep.mkv", path)
	waitFor(t, "failure", func() bool { return only(d).State == Failed || only(d).State == Done })
	if err := only(d).Err; err == nil || !strings.Contains(err.Error(), "web page") {
		t.Fatalf("error %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("page saved as the video")
	}
}

func TestAria2FileChangedOnServer(t *testing.T) {
	d, path := aria2Setup(t)
	fs := newFileServer(t)
	d.Add("ep", fs.URL+"/files/ep.mkv", path)
	dl := stopAt(t, d, 0.3)

	changed := make([]byte, 20<<20)
	rand.Read(changed)
	fs.files["ep.mkv"] = changed
	d.Resume(dl.ID)
	waitFor(t, "done", func() bool { return only(d).State == Done || only(d).State == Failed })
	if got := only(d); got.State != Done {
		t.Fatalf("state %v: %v", got.State, got.Err)
	}
	sameBytes(t, path, changed)
}

func TestAria2NoRangeServer(t *testing.T) {
	d, path := aria2Setup(t)
	fs := newFileServer(t)
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(fs.files["ep.mkv"])))
		throttled{w, &fs.expired}.Write(fs.files["ep.mkv"])
	}))
	t.Cleanup(plain.Close)

	d.Add("ep", plain.URL+"/ep.mkv", path)
	dl := stopAt(t, d, 0.3)
	d.Resume(dl.ID)
	waitFor(t, "done", func() bool { return only(d).State == Done || only(d).State == Failed })
	if got := only(d); got.State != Done {
		t.Fatalf("state %v: %v", got.State, got.Err)
	}
	sameBytes(t, path, fs.files["ep.mkv"])
}

func TestAria2Names(t *testing.T) {
	for _, name := range []string{
		"Love, Chunibyo & Other Delusions! - 01 'Ren' [1080p].mkv",
		"進撃の巨人 第1話.mkv",
		fitFile(strings.Repeat("Long Name ", 40) + ".mkv"),
	} {
		d, dir := aria2Setup(t)
		path := filepath.Join(filepath.Dir(dir), name)
		fs := newFileServer(t)
		d.Add("ep", fs.URL+"/files/ep.mkv", path)
		stopAt(t, d, 0.2)
		if _, err := os.Stat(controlPath(path)); err != nil && aria2Fits(path) {
			t.Errorf("%d-byte name: no resume file", len(name))
			continue
		}
		d.Resume(only(d).ID)
		waitFor(t, "done", func() bool { return only(d).State == Done || only(d).State == Failed })
		sameBytes(t, path, fs.files["ep.mkv"])
	}
}

// A path too long for aria2c (Windows' MAX_PATH) downloads the built-in way.
func TestAria2PathTooLong(t *testing.T) {
	d, path := aria2Setup(t)
	saved := aria2MaxPath
	aria2MaxPath = len(path) // shorter than the path and its partial files
	t.Cleanup(func() { aria2MaxPath = saved })

	fs := newFileServer(t)
	d.Add("ep", fs.URL+"/files/ep.mkv", path)
	waitFor(t, "progress", func() bool { dl := only(d); return dl.State == Active && dl.Frac() > 0.1 })
	if _, err := os.Stat(controlPath(path)); err == nil {
		t.Fatal("aria2c used for a path it can't write")
	}
	waitFor(t, "done", func() bool { return only(d).State == Done || only(d).State == Failed })
	sameBytes(t, path, fs.files["ep.mkv"])
}

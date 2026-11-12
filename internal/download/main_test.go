package download

import (
	"os"
	"testing"
	"time"
)

// TestMain points every data folder at a temp dir, so no test can touch the
// real config, addons, history or downloads.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "download-test")
	if err != nil {
		panic(err)
	}
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "APPDATA"} {
		os.Setenv(v, dir)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// freshState gives a test its own state folder. Windows keeps everything
// under %APPDATA%, so that's set too.
func freshState(t testing.TB) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("APPDATA", dir)
}

// testDownloader is New, and at the end of the test waits for the worker to
// finish, so its last save doesn't land in a folder being removed. Make it
// after the test's temp folders: cleanups run newest first.
func testDownloader(t testing.TB) *Downloader {
	d := New()
	t.Cleanup(func() {
		for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
			d.mu.Lock()
			running := d.running
			d.mu.Unlock()
			if !running {
				return
			}
		}
		t.Error("download worker still running")
	})
	return d
}

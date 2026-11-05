package download

import (
	"os"
	"path/filepath"
	"testing"

	"stremio-cliuwu/internal/store"
)

// The index is reloaded at launch, with disk deciding each entry's state.
func TestIndexReload(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	done := filepath.Join(dir, "done.mkv")
	partial := filepath.Join(dir, "partial.mkv")
	orphan := filepath.Join(dir, "orphan.mkv") // part file but no sidecar
	gone := filepath.Join(dir, "gone.mkv")

	os.WriteFile(done, make([]byte, 100), 0644)
	os.WriteFile(partPath(partial), make([]byte, 40), 0644)
	writePartInfo(partial, partInfo{URL: "https://debrid.test/resolve/KEY/1", Total: 100, Label: "Partial"})
	os.WriteFile(partPath(orphan), make([]byte, 40), 0644)

	d := New()
	d.items = []*Download{
		{ID: 1, Label: "Done", Path: done, State: Done},
		{ID: 2, Label: "", Path: partial, State: Active},
		{ID: 3, Label: "Orphan", Path: orphan, State: Active},
		{ID: 4, Label: "Gone", Path: gone, State: Done},
	}
	d.save()

	if fi, err := os.Stat(store.DownloadsFile()); err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("index perms: %v %v", fi.Mode(), err)
	}
	if fi, err := os.Stat(infoPath(partial)); err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("sidecar holds the stream URL, should be 0600: %v %v", fi.Mode(), err)
	}

	r := New()
	r.Load()
	if len(r.items) != 2 {
		t.Fatalf("reloaded %d items, want done + partial", len(r.items))
	}
	a, b := r.items[0], r.items[1]
	if a.State != Done || a.Total != 100 || a.Done != 100 {
		t.Errorf("done item: %+v", *a)
	}
	// Killed mid-download: comes back stopped and resumable, label from the sidecar.
	if b.State != Cancelled || !b.Resume || b.Done != 40 || b.Total != 100 || b.Label != "Partial" || b.URL == "" {
		t.Errorf("partial item: %+v", *b)
	}
	if _, ok := r.Active(); ok {
		t.Error("something active straight after load")
	}

	if n := r.Clear(); n != 1 || len(r.items) != 1 || r.items[0].Path != partial {
		t.Fatalf("Clear removed %d, left %v", n, r.items)
	}
	// Clear only drops the row; the finished file stays.
	if _, err := os.Stat(done); err != nil {
		t.Fatal("Clear deleted a finished file")
	}
}

func TestStateNames(t *testing.T) {
	for s, want := range map[State]string{Queued: "queued", Active: "downloading", Done: "done", Failed: "failed", Cancelled: "cancelled"} {
		if s.String() != want {
			t.Errorf("%d = %q", s, s.String())
		}
	}
}

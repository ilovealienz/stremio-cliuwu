package ui

import (
	"os"
	"testing"

	"stremio-cliuwu/internal/store"
)

// TestMain keeps the whole run out of the real config: history writes on a
// timer, which can fire after a test's own temp dir has been put back.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ui-test")
	if err != nil {
		panic(err)
	}
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"} {
		os.Setenv(v, dir)
	}
	os.Setenv("APPDATA", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func TestStreamListReadsResumeFresh(t *testing.T) {
	s := newTestStreamScreen(t)
	s.target.Resume = 42
	if got := s.savedResume(); got != 42 {
		t.Fatalf("nothing in history should keep the list's value, got %v", got)
	}

	e := store.HistoryEntry{Name: "Film", ID: "tt-resume", Type: "movie", VideoID: "tt-resume"}
	store.AddHistory(e, 100)
	s.target.VideoID = "tt-resume"

	store.UpdatePosition("tt-resume", 1800, 7200)
	if got := s.savedResume(); got != 1800 {
		t.Fatalf("stopped at 1800, list offers %v", got)
	}
	store.UpdatePosition("tt-resume", 7190, 7200)
	if got := s.savedResume(); got != 0 {
		t.Fatalf("finished, list offers %v", got)
	}
}

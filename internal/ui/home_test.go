package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"stremio-cliuwu/internal/download"
	"stremio-cliuwu/internal/player"
	"stremio-cliuwu/internal/store"
)

func TestResumeRowRefitsWhenPanelToggles(t *testing.T) {
	saved := ctx
	t.Cleanup(func() { ctx = saved })
	ctx = &appCtx{cfg: &store.Config{}, player: player.New(store.Config{}), downloader: download.New()}

	e := store.HistoryEntry{Name: "WATAMOTE: No Matter How I Look at It, It's You Guys' Fault I'm Not Popular!",
		ID: "tt-wata", Type: "series", Season: 1, Episode: 1, VideoID: "tt-wata:1:1",
		EpTitle: "Since I'm Not Popular, I'll Change My Image a Bit", EpisodeTotal: 12}
	store.AddHistory(e, 100)
	store.UpdatePosition(e.VideoID, 400, 1440)
	t.Cleanup(func() { store.ClearShow("tt-wata") })

	for _, width := range []int{190, 120, 90} {
		s := newMenuScreen()
		s.SetSize(width, 40) // the panel starts closed: auto-open is off
		for _, open := range []bool{true, false, true} {
			s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
			if s.panel.On() != open {
				t.Fatalf("panel on = %v", s.panel.On())
			}
			row := strings.Split(s.list.View(), "\n")[1] // under the "resume" header
			if w, max := lipgloss.Width(row), continuePanelLayout(s.w, &s.panel); w > max || !strings.Contains(row, "28%") {
				t.Errorf("width %d, panel %v: resume row %d wide in %d, %q", width, open, w, max, stripANSI(row))
			}
		}
	}
}

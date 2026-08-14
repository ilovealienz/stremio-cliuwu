package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

type downloadsScreen struct {
	baseScreen
	list  listModel
	items []Download
}

func newDownloadsScreen() *downloadsScreen {
	l := newList()
	l.Empty = "nothing downloaded yet — press D on a stream"
	s := &downloadsScreen{list: l}
	s.rebuild()
	return s
}

func (s *downloadsScreen) Init() tea.Cmd { return nil }
func (s *downloadsScreen) Title() string { return "downloads" }
func (s *downloadsScreen) Typing() bool  { return s.list.Typing() }

func (s *downloadsScreen) SetSize(w, h int) {
	s.baseScreen.SetSize(w, h)
	s.list.SetSize(w, h)
}

func (s *downloadsScreen) Footer() string {
	return keyHint(
		[2]string{"x", "cancel"},
		[2]string{"C", "clear finished"},
		[2]string{"b/esc", "back"},
	) + "   " + stHint.Render(s.list.Status())
}

func (s *downloadsScreen) rebuild() {
	s.items = ctx.downloader.Snapshot()

	items := make([]Item, len(s.items))
	for i, d := range s.items {
		it := Item{Label: bold(d.Label)}

		switch d.State {
		case DLActive:
			// Bar plus figures, since a bar alone doesn't tell you whether a
			// stalled download is stalled at 400MB or 4MB.
			bar := progressBar(d.Frac(), 20)
			sub := fmt.Sprintf("%s / %s", fmtBytes(d.Done), fmtBytes(d.Total))
			if d.Speed > 0 {
				sub += "  ·  " + fmtBytes(d.Speed) + "/s"
			}
			if eta := fmtETA(d); eta != "" {
				sub += "  ·  " + eta
			}
			it.Sub = bar + "  " + sub
			if d.Resume {
				it.Badge = yell("resumed")
			}

		case DLQueued:
			it.Sub = stHint.Render("queued")
			it.Dim = true

		case DLDone:
			it.Sub = stHint.Render(fmtBytes(d.Total))
			it.Badge = good("✓")
			it.Watched = true

		case DLFailed:
			it.Sub = stErr.Render(d.Err.Error())
			it.Badge = bad("failed")

		case DLCancelled:
			it.Sub = stHint.Render("cancelled · D on the stream to resume")
			it.Badge = grey("stopped")
			it.Dim = true
		}
		items[i] = it
	}
	s.list.SetItems(items)
}

func (s *downloadsScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch m := msg.(type) {
	case DownloadTickMsg:
		cur := s.list.Selected()
		s.rebuild()
		if cur >= 0 {
			s.list.Focus(cur)
		}
		return s, nil

	case tea.KeyMsg:
		if consumed, cmd := s.list.Update(msg); consumed {
			return s, cmd
		}
		switch m.String() {
		case "x":
			if i := s.list.Selected(); i >= 0 && i < len(s.items) {
				ctx.downloader.Cancel(s.items[i].ID)
				s.rebuild()
				return s, toast("cancelled")
			}
		case "C":
			ctx.downloader.Clear()
			s.rebuild()
			return s, toast("cleared finished downloads")
		case "esc", "backspace":
			return s, pop()
		case "q":
			return s, popRoot()
		}
	}
	return s, nil
}

func (s *downloadsScreen) View() string {
	head := "  " + stSub.Render(orDash(ctx.cfg.DownloadDir)) + "\n"
	return head + s.list.View()
}

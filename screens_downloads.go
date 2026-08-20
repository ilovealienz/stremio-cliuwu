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
	l.Empty = "nothing here — press D on a stream to download it"
	s := &downloadsScreen{list: l}
	s.rebuild()
	return s
}

func (s *downloadsScreen) Init() tea.Cmd {
	// An empty list may just mean the index is missing or predates it, while
	// half-finished files sit on disk. Walking the folder is only wasteful
	// when there's already something to show, so do it exactly then.
	if len(s.items) == 0 {
		if n := ctx.downloader.Scan(ctx.cfg.DownloadDir); n > 0 {
			s.rebuild()
			return toast(fmt.Sprintf("found %d unfinished download(s)", n))
		}
	}
	return nil
}

func (s *downloadsScreen) Title() string { return "downloads" }
func (s *downloadsScreen) Typing() bool  { return s.list.Typing() }

func (s *downloadsScreen) SetSize(w, h int) {
	s.baseScreen.SetSize(w, h)
	s.list.SetSize(w, h)
}

func (s *downloadsScreen) Footer() string {
	return withStatus(s.list.Status(), keyHint(
		[2]string{"enter", "resume"},
		[2]string{"x", "cancel"},
		[2]string{"C", "clear finished"},
		[2]string{"R", "rescan folder"},
		[2]string{"b/esc", "back"},
	))
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
			it.Sub = stHint.Render("waiting its turn")
			it.Badge = grey("queued")

		case DLDone:
			it.Sub = stHint.Render(fmtBytes(d.Total))
			it.Badge = good("✓")
			it.Watched = true
			it.Dim = true // finished, nothing to do with it

		case DLFailed:
			// Err is nil for a failure restored from the index — the message
			// isn't persisted, and calling Error() on nil would panic.
			msg := "failed · enter to retry"
			if d.Err != nil {
				msg = d.Err.Error()
			}
			it.Sub = stErr.Render(msg)
			it.Badge = bad("failed")

		case DLCancelled:
			// Not dimmed: a stopped download is the one row here you're
			// likely to want to act on, and greying it made it the hardest
			// thing on the screen to see.
			pct := ""
			if d.Total > 0 {
				pct = fmt.Sprintf("  ·  %.0f%% of %s", d.Frac()*100, fmtBytes(d.Total))
			}
			it.Sub = progressBar(d.Frac(), 20) + stStop.Render("  stopped"+pct)
			it.Badge = stStop.Render("enter to resume")
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
		case "enter", "r":
			if i := s.list.Selected(); i >= 0 && i < len(s.items) {
				if ctx.downloader.Resume(s.items[i].ID) {
					s.rebuild()
					return s, toast("resuming " + s.items[i].Label)
				}
				return s, nil
			}
		case "R":
			if n := ctx.downloader.Scan(ctx.cfg.DownloadDir); n > 0 {
				s.rebuild()
				return s, toast(fmt.Sprintf("found %d unfinished download(s)", n))
			}
			return s, toast("nothing unfinished on disk")
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

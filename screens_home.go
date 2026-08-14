package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type menuEntry struct {
	key   string // accelerator, "" for a section label
	label string
	sub   string
	badge string
	head  string // non-empty => section label row
	open  func() tea.Cmd
}

type menuScreen struct {
	baseScreen
	list    listModel
	entries []menuEntry
	resume  *ContinueItem
}

func newMenuScreen() *menuScreen {
	s := &menuScreen{list: newList()}
	s.rebuild()
	return s
}

func (s *menuScreen) Init() tea.Cmd { return nil }

func (s *menuScreen) Title() string { return "" }
func (s *menuScreen) Typing() bool  { return false }

func (s *menuScreen) SetSize(w, h int) {
	s.baseScreen.SetSize(w, h)
	s.list.SetSize(w, h)
}

func (s *menuScreen) Footer() string {
	return keyHint([2]string{"enter", "open"}, [2]string{"ctrl+q", "quit"})
}

// padPlain pads to n columns measured on the text with escapes stripped.
func padPlain(s string, n int) string {
	w := len([]rune(stripANSI(s)))
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

func downloadsSub() string {
	if n := ctx.downloader.Pending(); n > 0 {
		return fmt.Sprintf("%d in progress", n)
	}
	return "saved files"
}

func section(name string) menuEntry { return menuEntry{head: name} }

func spacer() menuEntry { return menuEntry{head: " "} }

func (s *menuScreen) rebuild() {
	s.entries = nil
	s.resume = ContinueTarget()

	if c := s.resume; c != nil {
		e := c.Entry

		what := e.Name
		if e.Season > 0 && e.Episode > 0 {
			what += " · " + fmtVideoID(e.VideoID)
		}
		if e.EpTitle != "" {
			what += " — " + e.EpTitle
		}

		label := "continue"
		var detail string

		if c.NextUp {
			label = "next up"
			detail = "finished " + c.LastLabel
			if c.Total > 0 {
				detail += fmt.Sprintf(" · %d/%d", c.Index, c.Total)
			}
		} else {
			pos, dur := c.Position, c.Duration
			if live := ctx.player.State(); live.Alive && live.VideoID == e.VideoID && live.Duration > 0 {
				pos, dur = live.Pos, live.Duration
			}
			if dur > 0 && pos > 0 {
				detail = progressGlyph(pos, dur)
			} else {
				detail = "not started"
			}
			if c.Total > 0 && e.Episode > 0 {
				detail += fmt.Sprintf(" · %d/%d", c.Index, c.Total)
			}
		}

		entry := e
		s.entries = append(s.entries,
			section("resume"),
			menuEntry{
				// Progress sits inline after the title rather than pinned to
				// the far right, where it read as unrelated to the row.
				key:   "w",
				label: label,
				sub:   what + "   ·   " + detail,
				open:  func() tea.Cmd { return push(resumeScreen(entry)) },
			},
			spacer(),
		)
	}

	s.entries = append(s.entries,
		section("browse"),
	)

	// Browse entries come from the installed addons now, so only offer the
	// buckets that actually have catalogs behind them.
	counts := KindsAvailable(ctx.addons)
	for _, b := range []struct{ key, kind, label string }{
		{"m", "movie", "movies"},
		{"s", "show", "shows"},
		{"a", "anime", "anime"},
		{"d", "other", "library"}, // debrid libraries: DMM, torrentio cloud
	} {
		n := counts[b.kind]
		if n == 0 {
			continue
		}
		sub := fmt.Sprintf("%d catalog", n)
		if n > 1 {
			sub += "s"
		}
		kind, label := b.kind, b.label
		s.entries = append(s.entries, menuEntry{
			key: b.key, label: label, sub: sub,
			open: func() tea.Cmd { return browseKind(kind, label) },
		})
	}

	searchable := len(SearchCatalogs(ctx.addons))
	s.entries = append(s.entries,
		menuEntry{key: "/", label: "search", sub: fmt.Sprintf("%d searchable catalog(s)", searchable),
			open: func() tea.Cmd { return push(searchPrompt()) }},
		spacer(),

		section("yours"),
		menuEntry{key: "f", label: "favourites", sub: "saved titles",
			open: func() tea.Cmd { return push(newFavsScreen()) }},
		menuEntry{key: "h", label: "history", sub: "recently watched",
			open: func() tea.Cmd { return push(newHistoryScreen()) }},
		menuEntry{key: "D", label: "downloads", sub: downloadsSub(),
			open: func() tea.Cmd { return push(newDownloadsScreen()) }},
		spacer(),

		section("setup"),
		menuEntry{key: "A", label: "addons", sub: "manifest urls",
			open: func() tea.Cmd { return push(newAddonsScreen()) }},
		menuEntry{key: "c", label: "settings", sub: "mpv, quality, autoplay",
			open: func() tea.Cmd { return push(newSettingsScreen()) }},
	)

	items := make([]Item, len(s.entries))
	for i, e := range s.entries {
		if e.head != "" {
			items[i] = Item{Header: true, Label: strings.TrimSpace(e.head)}
			continue
		}
		items[i] = Item{
			Label: stKey.Render(padPlain(e.key, 2)) + " " + padPlain(bold(e.label), 12),
			Sub:   e.sub,
			Badge: e.badge,
		}
	}
	s.list.SetItems(items)
}

// accel handles the single-key shortcuts. Checked before the list gets the
// key, so `/` opens search rather than the list's filter — the menu is ten
// items, it doesn't need filtering.
func (s *menuScreen) accel(k string) tea.Cmd {
	for _, e := range s.entries {
		if e.open != nil && e.key == k {
			return e.open()
		}
	}
	return nil
}

func (s *menuScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch m := msg.(type) {
	case DownloadTickMsg:
		cur := s.list.Selected()
		s.rebuild()
		if cur >= 0 {
			s.list.Focus(cur)
		}
		return s, nil

	case PlayerStateMsg:
		cur := s.list.Selected()
		s.rebuild()
		if cur >= 0 {
			s.list.Focus(cur)
		}
		return s, nil

	case tea.KeyMsg:
		if cmd := s.accel(m.String()); cmd != nil {
			return s, cmd
		}
		if consumed, cmd := s.list.Update(msg); consumed {
			return s, cmd
		}
		switch m.String() {
		case "enter":
			if i := s.list.Selected(); i >= 0 && s.entries[i].open != nil {
				return s, s.entries[i].open()
			}
		case "q", "esc":
			return s, quitCmd()
		}
	}
	return s, nil
}

func (s *menuScreen) View() string { return s.list.View() }

// ── Continue watching ─────────────────────────────────────────────────────────

// resumeScreen opens a history entry where you left off.
//
// For a series this goes in via the season screen rather than jumping straight
// to the stream picker. Both intermediate screens load asynchronously and push
// the next one themselves, so the stack ends up
// menu › show › season › streams — meaning `b` out of the stream picker lands
// on the episode list, the way the old client behaved.
func resumeScreen(e HistoryEntry) screen {
	m := Meta{ID: e.ID, Type: e.Type, Name: e.Name, Year: e.Year, Source: e.Source}

	if e.Type == "series" && e.Season > 0 {
		s := newSeasonScreen(m, e.Season)
		s.autoEpisode = e.Episode
		s.autoResume = e.Position
		return s
	}

	videoID := e.ID
	if e.VideoID != "" {
		videoID = e.VideoID
	}
	return newStreamScreen(streamTarget{
		Meta:      m,
		MediaType: "movie",
		VideoID:   videoID,
		Label:     e.Name,
		Resume:    e.Position,
	})
}

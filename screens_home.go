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
	panel   continuePanel
	fetched string // videoID the panel's episode info belongs to
}

func newMenuScreen() *menuScreen {
	s := &menuScreen{list: newList(), panel: newContinuePanel()}
	s.rebuild()
	return s
}

func (s *menuScreen) layout() {
	s.list.SetSize(continuePanelLayout(s.w, &s.panel), s.h)
	s.panel.SetSize(PaneWidth(s.w), s.h)
}

func (s *menuScreen) Init() tea.Cmd { return s.fetchTargetInfo() }

// syncInfo satisfies resyncable, so returning to the menu reloads the panel's
// synopsis. Deleting a history entry changes what w resumes, and without this
// the new target sat there with a title and no summary until something else
// happened to trigger a fetch.
func (s *menuScreen) syncInfo() tea.Cmd { return s.fetchTargetInfo() }

// fetchTargetInfo loads the synopsis for whatever w would resume, once per
// title. Returns nil when the panel already has the right one, so this can be
// called from anywhere without worrying about repeat requests.
func (s *menuScreen) fetchTargetInfo() tea.Cmd {
	e, ok := s.panel.NextEntry()
	if !ok {
		return nil
	}

	// Same key the panel uses to decide whether a result belongs to what's
	// on screen — if these two disagreed, the fetch would fire and the result
	// would then be rejected as stale.
	key := targetKey(e)
	if key == "" || key == s.fetched {
		return nil
	}

	s.fetched = key
	return FetchTargetInfo(e)
}

func (s *menuScreen) Title() string { return "" }
func (s *menuScreen) Typing() bool  { return false }

func (s *menuScreen) SetSize(w, h int) {
	s.baseScreen.SetSize(w, h)
	s.panel.AutoFit(w)
	s.layout()
	s.panel.Refresh()
}

func (s *menuScreen) Footer() string {
	pairs := [][2]string{{"enter", "open"}}
	if n := s.panel.Count(); n > 0 && s.panel.On() {
		pairs = append(pairs, [2]string{fmt.Sprintf("1-%d", min(n, 9)), "resume"})
	}
	pairs = append(pairs, [2]string{"i", "panel"}, [2]string{"ctrl+q", "quit"})
	return keyHint(pairs...)
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
	// A header, so the cursor skips it and there's nothing to press.
	if ctx.loading {
		s.entries = append(s.entries, menuEntry{head: "loading addons…"})
	}
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
	s.panel.Refresh()
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
	case tea.WindowSizeMsg:
		// The panel is off until AutoFit runs during the first resize, so at
		// Init there's no target to fetch for yet — this is the first moment
		// there is one.
		return s, s.fetchTargetInfo()

	case addonsReadyMsg:
		// The panel's fetch runs at startup, before the addons exist, and
		// comes back empty. Clearing the key lets it run again now there's
		// something to ask.
		s.fetched = ""
		return s, s.fetchTargetInfo()

	case targetInfoMsg:
		s.panel.SetTargetInfo(m)
		return s, nil

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
		return s, s.fetchTargetInfo()

	case tea.KeyMsg:
		// Numbers open a panel entry directly, so there's no second cursor to
		// move around and no focus to switch between halves.
		if k := m.String(); len(k) == 1 && k[0] >= '1' && k[0] <= '9' && s.panel.On() {
			if sc, ok := s.panel.At(int(k[0] - '0')); ok {
				return s, push(sc)
			}
			return s, nil
		}
		if m.String() == "i" {
			s.panel.Toggle()
			s.layout()
			s.panel.Refresh()
			return s, s.fetchTargetInfo()
		}
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

func (s *menuScreen) View() string {
	if s.panel.On() && !s.panel.Split(s.w) {
		return s.list.View() // too narrow to split; the menu wins
	}
	return joinContinuePanel(s.list.View(), continuePanelLayout(s.w, &s.panel), &s.panel)
}

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

package main

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Favourites ────────────────────────────────────────────────────────────────

type favsScreen struct {
	baseScreen
	list listModel
	favs []Favourite
}

func newFavsScreen() *favsScreen {
	l := newList()
	l.Empty = "no favourites yet — press f on any title"
	s := &favsScreen{list: l}
	s.rebuild()
	return s
}

func (s *favsScreen) Init() tea.Cmd  { return nil }
func (s *favsScreen) Title() string  { return "favourites" }
func (s *favsScreen) Typing() bool   { return s.list.Typing() }

func (s *favsScreen) SetSize(w, h int) {
	s.baseScreen.SetSize(w, h)
	s.list.SetSize(w, h)
}

func (s *favsScreen) Footer() string {
	return withStatus(s.list.Status(), keyHint(
		[2]string{"enter", "open"},
		[2]string{"d", "remove"},
		[2]string{"/", "filter"},
		[2]string{"b/esc", "back"},
	))
}

func (s *favsScreen) rebuild() {
	s.favs = LoadFavs().Items
	items := make([]Item, len(s.favs))
	for i, f := range s.favs {
		items[i] = FavItem(f)
	}
	s.list.SetItems(items)
}

func (s *favsScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		if consumed, cmd := s.list.Update(msg); consumed {
			return s, cmd
		}
		switch k.String() {
		case "enter":
			if i := s.list.Selected(); i >= 0 {
				f := s.favs[i]
				m := Meta{ID: f.ID, Type: f.Type, Name: f.Name, Year: f.Year, Source: f.Source}
				return s, push(openMeta(m, f.Season))
			}
		case "d":
			if i := s.list.Selected(); i >= 0 {
				name := s.favs[i].Name
				RemoveFav(i)
				s.rebuild()
				return s, toast("removed " + name)
			}
		case "esc", "backspace":
			return s, pop()
		case "q":
			return s, popRoot()
		}
	}
	return s, nil
}

func (s *favsScreen) View() string { return s.list.View() }

// ── History ───────────────────────────────────────────────────────────────────

type historyScreen struct {
	baseScreen
	list    listModel
	entries []HistoryEntry
}

func newHistoryScreen() *historyScreen {
	l := newList()
	l.Empty = "nothing watched yet"
	s := &historyScreen{list: l}
	s.rebuild()
	return s
}

func (s *historyScreen) Init() tea.Cmd { return nil }
func (s *historyScreen) Title() string { return "history" }
func (s *historyScreen) Typing() bool  { return s.list.Typing() }

func (s *historyScreen) SetSize(w, h int) {
	s.baseScreen.SetSize(w, h)
	s.list.SetSize(w, h)
}

func (s *historyScreen) Footer() string {
	return withStatus(s.list.Status(), keyHint(
		[2]string{"enter", "resume"},
		[2]string{"d", "remove"},
		[2]string{"D", "clear all"},
		[2]string{"b/esc", "back"},
	))
}

func (s *historyScreen) rebuild() {
	s.entries = LoadHistory().Items
	items := make([]Item, len(s.entries))
	for i, e := range s.entries {
		items[i] = HistoryItem(e)
	}
	s.list.SetItems(items)
}

func (s *historyScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	if _, ok := msg.(PlayerStateMsg); ok {
		cur := s.list.Selected()
		s.rebuild()
		if cur >= 0 {
			s.list.Focus(cur)
		}
		return s, nil
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		if consumed, cmd := s.list.Update(msg); consumed {
			return s, cmd
		}
		switch k.String() {
		case "enter":
			if i := s.list.Selected(); i >= 0 {
				return s, push(resumeScreen(s.entries[i]))
			}
		case "d":
			if i := s.list.Selected(); i >= 0 {
				ClearHistoryEntry(i)
				invalidateInProgress()
				s.rebuild()
				return s, toast("removed")
			}
		case "D":
			return s, push(newDestructive("clear history", "clear the entire watch history?", "clear",
				func() tea.Cmd {
					ClearAllHistory()
					s.rebuild()
					return toast("history cleared")
				}))
		case "esc", "backspace":
			return s, pop()
		case "q":
			return s, popRoot()
		}
	}
	return s, nil
}

func (s *historyScreen) View() string { return s.list.View() }

// ── Addons ────────────────────────────────────────────────────────────────────

type addonsLoadedMsg struct {
	id     asyncID
	addons []Addon
}

type addonsScreen struct {
	baseScreen
	id     asyncID
	list   listModel
	refs   []AddonRef
	live   map[string]*Addon
	busy   busy
	loaded bool
}

func newAddonsScreen() *addonsScreen {
	l := newList()
	l.Empty = "no addons — press a and paste a manifest url"
	s := &addonsScreen{list: l, live: map[string]*Addon{}, busy: newBusy("fetching manifests…")}
	s.rebuild()
	return s
}

func (s *addonsScreen) Init() tea.Cmd { return s.refresh() }

func (s *addonsScreen) refresh() tea.Cmd {
	s.id = newAsyncID()
	s.loaded = false
	id := s.id
	refs := LoadAddonRefs()
	return tea.Batch(
		s.busy.start("fetching manifests…"),
		func() tea.Msg {
			// Fetch everything, disabled included, so the list can show names.
			all := AddonList{}
			for _, r := range refs.Items {
				all.Items = append(all.Items, AddonRef{URL: r.URL})
			}
			return addonsLoadedMsg{id: id, addons: LoadAddons(all)}
		},
	)
}

func (s *addonsScreen) Title() string { return "addons" }
func (s *addonsScreen) Typing() bool  { return s.list.Typing() }

func (s *addonsScreen) SetSize(w, h int) {
	s.baseScreen.SetSize(w, h)
	s.list.SetSize(w, h-2)
}

func (s *addonsScreen) Footer() string {
	return keyHint(
		[2]string{"a", "add"},
		[2]string{"d", "remove"},
		[2]string{"t", "on/off"},
		[2]string{"J/K", "reorder"},
		[2]string{"r", "refresh"},
		[2]string{"b/esc", "back"},
	)
}

func (s *addonsScreen) rebuild() {
	s.refs = LoadAddonRefs().Items
	items := make([]Item, len(s.refs))
	for i, r := range s.refs {
		items[i] = AddonItem(r, s.live[r.URL])
	}
	s.list.SetItems(items)
}

// commit re-reads the addon list into the shared context so the rest of the
// app picks up changes without a restart.
func (s *addonsScreen) commit() tea.Cmd {
	return func() tea.Msg { return reloadAddonsMsg{} }
}

func (s *addonsScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch m := msg.(type) {
	case addonsLoadedMsg:
		if m.id != s.id {
			return s, nil
		}
		s.busy.stop()
		s.loaded = true
		for i := range m.addons {
			a := m.addons[i]
			s.live[a.TransportURL] = &a
		}
		s.rebuild()
		return s, s.commit()

	case tea.KeyMsg:
		if consumed, cmd := s.list.Update(msg); consumed {
			return s, cmd
		}
		switch m.String() {
		case "a":
			return s, push(newPrompt("add addon", "https://…/manifest.json", "", func(v string) tea.Cmd {
				if v == "" {
					return nil
				}
				u, err := AddAddonRef(v)
				if err != nil {
					return toastErr(err.Error())
				}
				s.rebuild()
				return tea.Batch(toast("added "+RedactURL(u)), s.refresh())
			},
				"paste the same manifest url you'd paste into stremio",
				"stremio:// links and trailing-slash urls are fine too",
			))

		case "d":
			if i := s.list.Selected(); i >= 0 {
				url := s.refs[i].URL
				return s, push(newDestructive("remove addon", "remove "+RedactURL(url)+"?", "remove",
					func() tea.Cmd {
						RemoveAddonRef(i)
						s.rebuild()
						return tea.Batch(toast("removed"), s.commit())
					}))
			}

		case "t":
			if i := s.list.Selected(); i >= 0 {
				ToggleAddonRef(i)
				s.rebuild()
				s.list.Focus(i)
				return s, s.commit()
			}

		case "K":
			if i := s.list.Selected(); i >= 0 {
				j := MoveAddonRef(i, -1)
				s.rebuild()
				s.list.Focus(j)
				return s, s.commit()
			}

		case "J":
			if i := s.list.Selected(); i >= 0 {
				j := MoveAddonRef(i, 1)
				s.rebuild()
				s.list.Focus(j)
				return s, s.commit()
			}

		case "r":
			return s, s.refresh()

		case "esc", "backspace":
			return s, pop()
		case "q":
			return s, popRoot()
		}
	}
	return s, s.busy.update(msg)
}

func (s *addonsScreen) View() string {
	head := "  " + stSub.Render("order matters — stream results are grouped in this order") + "\n"
	if !s.loaded {
		return head + s.busy.view()
	}
	return head + s.list.View()
}

// ── Settings ──────────────────────────────────────────────────────────────────

// settingRow is one line of the settings screen. Actions hang off the row
// itself rather than a switch on the row index — that switch had to be
// renumbered every time a setting was added, and getting it wrong wires a
// label to the wrong action silently.
type settingRow struct {
	head  string // section label; no action, not selectable
	label string
	sub   string
	badge string
	act   func() tea.Cmd
}

type settingsScreen struct {
	baseScreen
	list listModel
	rows []settingRow
}

func newSettingsScreen() *settingsScreen {
	s := &settingsScreen{list: newList()}
	s.rebuild()
	return s
}

func (s *settingsScreen) Init() tea.Cmd { return nil }
func (s *settingsScreen) Title() string { return "settings" }
func (s *settingsScreen) Typing() bool  { return s.list.Typing() }

func (s *settingsScreen) SetSize(w, h int) {
	s.baseScreen.SetSize(w, h)
	s.list.SetSize(w, h-1)
}

func (s *settingsScreen) Footer() string {
	return keyHint([2]string{"enter", "edit / toggle"}, [2]string{"b/esc", "back"})
}

func onOff(b bool) string {
	if b {
		return good("on")
	}
	return grey("off")
}

func orDash(s string) string {
	if s == "" {
		return grey("—")
	}
	return s
}

func (s *settingsScreen) save() tea.Cmd {
	ctx.cfg.SetDefaults()
	SaveConfig(ctx.cfg)
	ctx.player.SetConfig(ctx.cfg)
	invalidateInProgress()
	s.rebuild()
	return toast("saved")
}

// prompt is the common shape: edit a value, save, rebuild.
func (s *settingsScreen) prompt(title, placeholder, current string, set func(string) tea.Cmd, help ...string) tea.Cmd {
	return push(newPrompt(title, placeholder, current, set, help...))
}

func (s *settingsScreen) rebuild() {
	c := ctx.cfg

	s.rows = []settingRow{
		{head: "playback"},
		{label: "mpv path", badge: orDash(c.MpvPath), act: func() tea.Cmd {
			return s.prompt("mpv path", "mpv", ctx.cfg.MpvPath, func(v string) tea.Cmd {
				ctx.cfg.MpvPath = v
				return s.save()
			}, "leave blank to use `mpv` from PATH", "detected: "+orDash(detectMpv()))
		}},
		{label: "preferred quality", sub: "streams matching this sort first", badge: orDash(c.PreferredQuality), act: func() tea.Cmd {
			return s.prompt("preferred quality", "1080p", ctx.cfg.PreferredQuality, func(v string) tea.Cmd {
				ctx.cfg.PreferredQuality = v
				return s.save()
			}, "substring match on the stream name, e.g. 2160p / 1080p / HDR")
		}},
		{label: "cached streams first", sub: "float instantly-available debrid results", badge: onOff(c.CachedFirst), act: func() tea.Cmd {
			ctx.cfg.CachedFirst = !ctx.cfg.CachedFirst
			return s.save()
		}},
		{label: "subtitle language", sub: "preferred track, and where S opens",
			badge: orDash(c.SubtitleLang), act: func() tea.Cmd {
				help := []string{
					"comma-separated for a preference order, e.g. eng, spa",
					"the name works too, and regional variants fold in: en-GB is English",
					"passed to mpv as --slang, and preselected in the S picker",
					"",
				}
				help = append(help, LangReference()...)

				return s.prompt("subtitle language", "eng, en, English", ctx.cfg.SubtitleLang,
					func(v string) tea.Cmd {
						ctx.cfg.SubtitleLang = v
						return s.save()
					}, help...)
			}},
		{label: "ask to resume", sub: "off always starts from the beginning", badge: onOff(c.AutoResume), act: func() tea.Cmd {
			ctx.cfg.AutoResume = !ctx.cfg.AutoResume
			return s.save()
		}},
		{label: "open next episode", sub: "show its streams when one finishes", badge: onOff(c.AutoNext), act: func() tea.Cmd {
			ctx.cfg.AutoNext = !ctx.cfg.AutoNext
			return s.save()
		}},
		{label: "close mpv on exit", sub: "off leaves playback running after you quit", badge: onOff(c.CloseMpvOnExit), act: func() tea.Cmd {
			ctx.cfg.CloseMpvOnExit = !ctx.cfg.CloseMpvOnExit
			return s.save()
		}},

		{head: ""},
		{head: "library"},
		{label: "history size", sub: "rows on the history screen · watched state is kept forever",
			badge: strconv.Itoa(c.HistoryMax), act: func() tea.Cmd {
			return s.prompt("history size", "300", strconv.Itoa(ctx.cfg.HistoryMax), func(v string) tea.Cmd {
				n, err := strconv.Atoi(v)
				if err != nil || n <= 0 {
					return toastErr("needs to be a positive number")
				}
				ctx.cfg.HistoryMax = n
				return s.save()
			})
		}},
		{label: "omdb key", sub: "episode titles for imdb shows", badge: orDash(c.OmdbKey), act: func() tea.Cmd {
			return s.prompt("omdb key", "trilogy", ctx.cfg.OmdbKey, func(v string) tea.Cmd {
				ctx.cfg.OmdbKey = v
				return s.save()
			})
		}},
		{label: "date format", sub: "air dates and release dates",
			badge: stSub.Render(dateSample(c.DateFormat)) + grey("  "+dateFormatName(c.DateFormat)), act: func() tea.Cmd {
			ctx.cfg.DateFormat = nextDateFormat(ctx.cfg.DateFormat)
			return tea.Batch(s.save(), themeChanged())
		}},

		{head: ""},
		{head: "appearance"},
		{label: "accent colour", sub: "highlights, rules and the cursor",
			badge: accentSwatch(c.Accent) + "  " + orDash(c.Accent), act: func() tea.Cmd {
				return s.prompt("accent colour", "pink", ctx.cfg.Accent, func(v string) tea.Cmd {
					ctx.cfg.Accent = v
					applyAccent(v)
					return tea.Batch(s.save(), themeChanged())
				}, "presets: "+strings.Join(accentOrder, " "),
					"or a hex value like #ff8800, or a terminal colour 0-255")
			}},
		{label: "auto-open info panel", sub: "on wide terminals · i toggles it anyway", badge: onOff(c.AutoInfo), act: func() tea.Cmd {
			ctx.cfg.AutoInfo = !ctx.cfg.AutoInfo
			return s.save()
		}},
		{label: "posters", sub: "block art in the info panel · looks rough, be warned", badge: onOff(c.Posters), act: func() tea.Cmd {
			ctx.cfg.Posters = !ctx.cfg.Posters
			return s.save()
		}},
		{label: "poster size", sub: "bigger is sharper but eats the panel", badge: orDash(c.PosterSize), act: func() tea.Cmd {
			ctx.cfg.PosterSize = nextPosterSize(ctx.cfg.PosterSize)
			posterGen++ // force panels to redraw at the new size
			return s.save()
		}},

		{head: ""},
		{head: "downloads"},
		{label: "download location", sub: "where D on a stream saves to", badge: orDash(c.DownloadDir), act: func() tea.Cmd {
			return s.prompt("download location", defaultDownloadDir(), ctx.cfg.DownloadDir, func(v string) tea.Cmd {
				ctx.cfg.DownloadDir = expandPath(v)
				return s.save()
			}, "~ and environment variables are expanded", "folders are created as needed")
		}},
		{label: "organise downloads", sub: "off saves everything flat", badge: onOff(c.DownloadFolders), act: func() tea.Cmd {
			ctx.cfg.DownloadFolders = !ctx.cfg.DownloadFolders
			return s.save()
		}},
		{label: "movie filename", sub: orDash(c.MoviePattern), act: func() tea.Cmd {
			return s.prompt("movie filename", DefaultMoviePattern, ctx.cfg.MoviePattern, func(v string) tea.Cmd {
				ctx.cfg.MoviePattern = v
				return s.save()
			}, "placeholders: {title} {year}", "/ makes a folder · extension is added for you")
		}},
		{label: "episode filename", sub: orDash(c.EpisodePattern), act: func() tea.Cmd {
			return s.prompt("episode filename", DefaultEpisodePattern, ctx.cfg.EpisodePattern, func(v string) tea.Cmd {
				ctx.cfg.EpisodePattern = v
				return s.save()
			}, "placeholders: {show} {season} {episode} {title} {year}",
				"/ makes a folder · extension is added for you")
		}},
	}

	items := make([]Item, len(s.rows))
	for i, r := range s.rows {
		if r.act == nil {
			items[i] = Item{Header: true, Label: r.head}
			continue
		}
		items[i] = Item{Label: bold(r.label), Sub: r.sub, Badge: r.badge}
	}
	s.list.SetItems(items)
}

func (s *settingsScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		if consumed, cmd := s.list.Update(msg); consumed {
			return s, cmd
		}
		switch k.String() {
		case "enter":
			if i := s.list.Selected(); i >= 0 && i < len(s.rows) && s.rows[i].act != nil {
				return s, s.rows[i].act()
			}
		case "esc", "backspace":
			return s, pop()
		case "q":
			return s, popRoot()
		}
	}
	return s, nil
}

func (s *settingsScreen) View() string {
	// Version alongside the config path: the two things you'd want to quote
	// when something's wrong, and the one place you'd think to look for them.
	left := "  " + stSub.Render(cfgFile())
	right := stHint.Render(appName+" ") + stKey.Render(version)

	head := left
	if gap := s.w - lipgloss.Width(left) - lipgloss.Width(right) - 2; gap > 1 {
		head += strings.Repeat(" ", gap) + right
	}
	return head + "\n" + s.list.View()
}

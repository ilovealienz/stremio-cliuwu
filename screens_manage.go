package main

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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
	return keyHint(
		[2]string{"enter", "open"},
		[2]string{"d", "remove"},
		[2]string{"/", "filter"},
		[2]string{"b/esc", "back"},
	) + "   " + stHint.Render(s.list.Status())
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
	return keyHint(
		[2]string{"enter", "resume"},
		[2]string{"d", "remove"},
		[2]string{"D", "clear all"},
		[2]string{"b/esc", "back"},
	) + "   " + stHint.Render(s.list.Status())
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

type settingsScreen struct {
	baseScreen
	list listModel
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

func (s *settingsScreen) rebuild() {
	c := ctx.cfg
	s.list.SetItems([]Item{
		{Label: bold("mpv path"), Badge: orDash(c.MpvPath)},
		{Label: bold("preferred quality"), Sub: "streams matching this sort first", Badge: orDash(c.PreferredQuality)},
		{Label: bold("subtitle language"), Sub: "mpv --slang", Badge: orDash(c.SubtitleLang)},
		{Label: bold("history size"), Badge: strconv.Itoa(c.HistoryMax)},
		{Label: bold("omdb key"), Sub: "episode titles for imdb shows", Badge: orDash(c.OmdbKey)},
		{Label: bold("open next episode"), Sub: "show its streams when one finishes", Badge: onOff(c.AutoNext)},
		{Label: bold("ask to resume"), Sub: "off always starts from the beginning", Badge: onOff(c.AutoResume)},
		{Label: bold("close mpv on exit"), Sub: "off leaves playback running after you quit", Badge: onOff(c.CloseMpvOnExit)},
		{Label: bold("cached streams first"), Sub: "float instantly-available debrid results", Badge: onOff(c.CachedFirst)},
		{Label: bold("accent colour"), Sub: "used for highlights, rules and the cursor",
			Badge: accentSwatch(c.Accent) + "  " + orDash(c.Accent)},
		{Label: bold("auto-open info panel"), Sub: "on wide terminals · i toggles it anyway",
			Badge: onOff(c.AutoInfo)},
		{Label: bold("posters"), Sub: "block art in the info panel · looks rough, be warned", Badge: onOff(c.Posters)},
		{Label: bold("poster size"), Sub: "bigger is sharper but eats the panel", Badge: orDash(c.PosterSize)},
		{Label: bold("download location"), Sub: "where D on a stream saves to", Badge: orDash(c.DownloadDir)},
		{Label: bold("organise downloads"), Sub: "off saves everything flat", Badge: onOff(c.DownloadFolders)},
		{Label: bold("movie filename"), Sub: orDash(c.MoviePattern)},
		{Label: bold("episode filename"), Sub: orDash(c.EpisodePattern)},
	})
}

func (s *settingsScreen) save() tea.Cmd {
	ctx.cfg.SetDefaults()
	SaveConfig(ctx.cfg)
	invalidateInProgress()
	ctx.player.SetConfig(ctx.cfg)
	s.rebuild()
	return toast("saved")
}

func (s *settingsScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		if consumed, cmd := s.list.Update(msg); consumed {
			return s, cmd
		}
		switch k.String() {
		case "enter":
			switch s.list.Selected() {
			case 0:
				return s, push(newPrompt("mpv path", "mpv", ctx.cfg.MpvPath, func(v string) tea.Cmd {
					ctx.cfg.MpvPath = v
					return s.save()
				}, "leave blank to just use `mpv` from PATH", "detected: "+orDash(detectMpv())))
			case 1:
				return s, push(newPrompt("preferred quality", "1080p", ctx.cfg.PreferredQuality, func(v string) tea.Cmd {
					ctx.cfg.PreferredQuality = v
					return s.save()
				}, "substring match against the stream name, e.g. 2160p / 1080p / HDR"))
			case 2:
				return s, push(newPrompt("subtitle language", "eng", ctx.cfg.SubtitleLang, func(v string) tea.Cmd {
					ctx.cfg.SubtitleLang = v
					return s.save()
				}, "applied on mpv launch — restart mpv for it to take effect"))
			case 3:
				return s, push(newPrompt("history size", "100", strconv.Itoa(ctx.cfg.HistoryMax), func(v string) tea.Cmd {
					n, err := strconv.Atoi(v)
					if err != nil || n <= 0 {
						return toastErr("needs to be a positive number")
					}
					ctx.cfg.HistoryMax = n
					return s.save()
				}))
			case 4:
				return s, push(newPrompt("omdb key", "trilogy", ctx.cfg.OmdbKey, func(v string) tea.Cmd {
					ctx.cfg.OmdbKey = v
					return s.save()
				}))
			case 5:
				ctx.cfg.AutoNext = !ctx.cfg.AutoNext
				return s, s.save()
			case 6:
				ctx.cfg.AutoResume = !ctx.cfg.AutoResume
				return s, s.save()
			case 7:
				ctx.cfg.CloseMpvOnExit = !ctx.cfg.CloseMpvOnExit
				return s, s.save()
			case 8:
				ctx.cfg.CachedFirst = !ctx.cfg.CachedFirst
				return s, s.save()
			case 10:
				ctx.cfg.AutoInfo = !ctx.cfg.AutoInfo
				return s, s.save()
			case 11:
				ctx.cfg.Posters = !ctx.cfg.Posters
				return s, s.save()
			case 12:
				ctx.cfg.PosterSize = nextPosterSize(ctx.cfg.PosterSize)
				posterGen++ // force panels to redraw at the new size
				return s, s.save()
			case 13:
				return s, push(newPrompt("download location", defaultDownloadDir(), ctx.cfg.DownloadDir,
					func(v string) tea.Cmd {
						ctx.cfg.DownloadDir = expandPath(v)
						return s.save()
					}, "~ and environment variables are expanded",
					"folders are created as needed"))
			case 14:
				ctx.cfg.DownloadFolders = !ctx.cfg.DownloadFolders
				return s, s.save()
			case 15:
				return s, push(newPrompt("movie filename", DefaultMoviePattern, ctx.cfg.MoviePattern,
					func(v string) tea.Cmd {
						ctx.cfg.MoviePattern = v
						return s.save()
					},
					"placeholders: {title} {year}",
					"/ makes a folder · extension is added for you",
				))
			case 16:
				return s, push(newPrompt("episode filename", DefaultEpisodePattern, ctx.cfg.EpisodePattern,
					func(v string) tea.Cmd {
						ctx.cfg.EpisodePattern = v
						return s.save()
					},
					"placeholders: {show} {season} {episode} {title} {year}",
					"/ makes a folder · extension is added for you",
				))
			case 9:
				return s, push(newPrompt("accent colour", "pink", ctx.cfg.Accent, func(v string) tea.Cmd {
					ctx.cfg.Accent = v
					applyAccent(v)
					// Restyling isn't enough on its own — already-rendered
					// rows hold the old escape codes.
					return tea.Batch(s.save(), themeChanged())
				},
					"presets: "+strings.Join(accentOrder, " "),
					"or a hex value like #ff8800, or a terminal colour 0-255",
				))
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
	head := "  " + stSub.Render(fmt.Sprintf("config: %s", cfgFile())) + "\n"
	return head + s.list.View()
}

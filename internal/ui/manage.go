package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"stremio-cliuwu/internal/player"
	"stremio-cliuwu/internal/poster"
	"stremio-cliuwu/internal/store"
	"stremio-cliuwu/internal/stremio"
	"stremio-cliuwu/internal/update"
)

// ── Favourites ────────────────────────────────────────────────────────────────

type favsScreen struct {
	baseScreen
	list listModel
	favs []store.Favourite
}

func newFavsScreen() *favsScreen {
	l := newList()
	l.Empty = "no favourites yet — press f on any title"
	s := &favsScreen{list: l}
	s.rebuild()
	return s
}

func (s *favsScreen) Init() tea.Cmd { return nil }

func (s *favsScreen) Title() string { return "favourites" }

func (s *favsScreen) Typing() bool { return s.list.Typing() }

func (s *favsScreen) SetSize(w, h int) {
	s.baseScreen.SetSize(w, h)
	s.list.SetSize(w, h)
}

func (s *favsScreen) Footer() string {
	return withStatus(s.list.Status(), keyHint(
		[2]string{"enter", "open"},
		[2]string{"d", "remove entry"},
		[2]string{"/", "filter"},
		[2]string{"b/esc", "back"},
	))
}

func (s *favsScreen) rebuild() {
	s.favs = store.LoadFavs().Items
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
				m := stremio.Meta{ID: f.ID, Type: f.Type, Name: f.Name, Year: f.Year, Source: f.Source, Base: f.Base}
				return s, push(openMeta(m, func() int {
					// A favourited show has season 0 meaning "the whole
					// thing", not the specials season.
					if f.Season == 0 {
						return noSeason
					}
					return f.Season
				}()))
			}
		case "d":
			if i := s.list.Selected(); i >= 0 {
				name := s.favs[i].Name
				store.RemoveFav(i)
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
	entries []store.HistoryEntry
	shows   []store.ShowSummary

	// Grouped by title rather than one row per episode. Clearing a show you
	// finished months ago otherwise means deleting thirty rows by hand.
	grouped bool
}

func newHistoryScreen() *historyScreen {
	l := newList()
	l.Empty = "nothing watched yet"
	s := &historyScreen{list: l}
	s.rebuild()
	return s
}

func (s *historyScreen) Init() tea.Cmd { return nil }

func (s *historyScreen) Title() string {
	if s.grouped {
		return "history · by title"
	}
	return "history"
}

func (s *historyScreen) Typing() bool { return s.list.Typing() }

func (s *historyScreen) SetSize(w, h int) {
	s.baseScreen.SetSize(w, h)
	s.list.SetSize(w, h)
}

func (s *historyScreen) Footer() string {
	remove := "remove"
	if s.grouped {
		remove = "remove title"
	}
	return withStatus(s.list.Status(), keyHint(
		[2]string{"enter", "resume"},
		[2]string{"d", remove},
		[2]string{"tab", "episodes/titles"},
		[2]string{"D", "clear all"},
		[2]string{"b/esc", "back"},
	))
}

func (s *historyScreen) rebuild() {
	if s.grouped {
		s.shows = store.HistoryShows()
		items := make([]Item, len(s.shows))
		for i, sh := range s.shows {
			items[i] = ShowHistoryItem(sh)
		}
		s.list.SetItems(items)
		return
	}

	s.entries = store.LoadHistory().Items
	items := make([]Item, len(s.entries))
	for i, e := range s.entries {
		items[i] = HistoryItem(e)
	}
	s.list.SetItems(items)
}

func (s *historyScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	if _, ok := msg.(player.StateMsg); ok {
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
		case "tab":
			s.grouped = !s.grouped
			s.rebuild()
			s.list.Focus(0)
			return s, nil

		case "enter":
			i := s.list.Selected()
			if i < 0 {
				return s, nil
			}
			if s.grouped {
				if i >= len(s.shows) {
					return s, nil
				}
				sh := s.shows[i]
				return s, push(openMeta(stremio.Meta{
					ID: sh.ID, Type: sh.Type, Name: sh.Name,
					Year: sh.Year, Source: sh.Source, Base: sh.Base,
				}, noSeason))
			}
			if i >= len(s.entries) {
				return s, nil
			}
			return s, push(resumeScreen(s.entries[i]))

		case "d":
			i := s.list.Selected()
			if i < 0 {
				return s, nil
			}

			// A title takes everything with it, so it asks first — the
			// difference between losing one episode and losing a season is
			// worth a keypress.
			if s.grouped {
				if i >= len(s.shows) {
					return s, nil
				}
				sh := s.shows[i]
				return s, push(newDestructive("remove title",
					fmt.Sprintf("remove %s and all %d of its entries?", sh.Name, sh.Episodes),
					"remove",
					func() tea.Cmd {
						store.ClearShow(sh.ID)
						store.InvalidateInProgress()
						s.rebuild()
						return toast("removed " + sh.Name)
					}))
			}

			if i >= len(s.entries) {
				return s, nil
			}
			store.ClearHistoryEntry(i)
			store.InvalidateInProgress()
			s.rebuild()
			return s, toast("removed")
		case "D":
			return s, push(newDestructive("clear history", "clear the entire watch history?", "clear",
				func() tea.Cmd {
					store.ClearAllHistory()
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
	id     poster.AsyncID
	addons []stremio.Addon
}

type addonsScreen struct {
	baseScreen
	id     poster.AsyncID
	list   listModel
	refs   []stremio.AddonRef
	live   map[string]*stremio.Addon
	busy   busy
	loaded bool
}

func newAddonsScreen() *addonsScreen {
	l := newList()
	l.Empty = "no addons — press a and paste a manifest url"
	s := &addonsScreen{list: l, live: map[string]*stremio.Addon{}, busy: newBusy("fetching manifests…")}
	s.rebuild()
	return s
}

func (s *addonsScreen) Init() tea.Cmd { return s.refresh() }

func (s *addonsScreen) refresh() tea.Cmd {
	s.id = poster.NewAsyncID()
	s.loaded = false
	id := s.id
	refs := store.LoadAddonRefs()
	return tea.Batch(
		s.busy.start("fetching manifests…"),
		func() tea.Msg {
			// Fetch everything, disabled included, so the list can show names.
			all := stremio.AddonList{}
			for _, r := range refs.Items {
				all.Items = append(all.Items, stremio.AddonRef{URL: r.URL})
			}
			return addonsLoadedMsg{id: id, addons: stremio.LoadAddons(all)}
		},
	)
}

func (s *addonsScreen) Title() string { return "addons" }

func (s *addonsScreen) Typing() bool { return s.list.Typing() }

func (s *addonsScreen) SetSize(w, h int) {
	s.baseScreen.SetSize(w, h)
	s.list.SetSize(w, h-2)
}

func (s *addonsScreen) Footer() string {
	return keyHint(
		[2]string{"a", "add addon"},
		[2]string{"d", "remove entry"},
		[2]string{"t", "enable / disable"},
		[2]string{"J/K", "move up / down"},
		[2]string{"r", "refresh list"},
		[2]string{"b/esc", "back"},
	)
}

func (s *addonsScreen) rebuild() {
	s.refs = store.LoadAddonRefs().Items
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
				u, err := store.AddAddonRef(v)
				if err != nil {
					return toastErr(err.Error())
				}
				s.rebuild()
				return tea.Batch(toast("added "+stremio.RedactURL(u)), s.refresh())
			},
				"paste the same manifest url you'd paste into stremio",
				"stremio:// links and trailing-slash urls are fine too",
			))

		case "d":
			if i := s.list.Selected(); i >= 0 {
				url := s.refs[i].URL
				return s, push(newDestructive("remove addon", "remove "+stremio.RedactURL(url)+"?", "remove",
					func() tea.Cmd {
						store.RemoveAddonRef(i)
						s.rebuild()
						return tea.Batch(toast("removed"), s.commit())
					}))
			}

		case "t":
			if i := s.list.Selected(); i >= 0 {
				store.ToggleAddonRef(i)
				s.rebuild()
				s.list.Focus(i)
				return s, s.commit()
			}

		case "K":
			if i := s.list.Selected(); i >= 0 {
				j := store.MoveAddonRef(i, -1)
				s.rebuild()
				s.list.Focus(j)
				return s, s.commit()
			}

		case "J":
			if i := s.list.Selected(); i >= 0 {
				j := store.MoveAddonRef(i, 1)
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
	head := fitLine("  "+stSub.Render("order matters — stream results are grouped in this order"), s.w) + "\n"
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
	step  func(dir int) tea.Cmd // left/right; nil leaves them paging the list
}

type settingsScreen struct {
	baseScreen
	list   listModel
	rows   []settingRow
	update update.Info
}

func newSettingsScreen() *settingsScreen {
	s := &settingsScreen{list: newList()}
	s.rebuild()
	return s
}

func (s *settingsScreen) Init() tea.Cmd { return update.Check() }

func (s *settingsScreen) Title() string { return "settings" }

func (s *settingsScreen) Typing() bool { return s.list.Typing() }

func (s *settingsScreen) SetSize(w, h int) {
	s.baseScreen.SetSize(w, h)
	// One row shorter: the update line sits below the list.
	s.list.SetSize(w, h-2)
}

func (s *settingsScreen) Footer() string {
	pairs := [][2]string{{"enter", "edit / toggle"}}
	if i := s.list.Selected(); i >= 0 && i < len(s.rows) && s.rows[i].step != nil {
		pairs = append(pairs, [2]string{"←/→", "adjust"})
	}
	if s.update.State == update.Available {
		pairs = append(pairs, [2]string{"u", "open the release"})
	}
	pairs = append(pairs, [2]string{"b/esc", "back"})
	return keyHint(pairs...)
}

// Past this, preloading would start evicting from the 40-entry poster cache.
const maxPreload = 10

func preloadLabel(n int) string {
	if n <= 0 {
		return "off"
	}
	return strconv.Itoa(n)
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
	store.SaveConfig(*ctx.cfg)
	ctx.player.SetConfig(*ctx.cfg)
	store.InvalidateInProgress()
	s.rebuild()
	return toast("saved")
}

// prompt is the common shape: edit a value, save, rebuild.
func (s *settingsScreen) prompt(title, placeholder, current string, set func(string) tea.Cmd, help ...string) tea.Cmd {
	return push(newPrompt(title, placeholder, current, set, help...))
}

func (s *settingsScreen) rebuild() {
	c := *ctx.cfg

	s.rows = []settingRow{
		{head: "playback"},

		{label: "mpv title", sub: "default is the release name, formatted is Show - S03E01",
			badge: orDash(c.MpvTitle), act: func() tea.Cmd {
				ctx.cfg.MpvTitle = poster.NextMpvTitle(ctx.cfg.MpvTitle)
				return s.save()
			}},

		{label: "mpv path", badge: orDash(c.MpvPath), act: func() tea.Cmd {
			return s.prompt("mpv path", "mpv", ctx.cfg.MpvPath, func(v string) tea.Cmd {
				ctx.cfg.MpvPath = v
				return s.save()
			}, "leave blank to use `mpv` from PATH", "detected: "+orDash(store.DetectMpv()))
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
		{label: "blocked terms", sub: "hide streams matching these",
			badge: orDash(strings.Join(c.Blocked, ", ")), act: func() tea.Cmd {
				return s.prompt("blocked terms", "ai upscale, cam, telesync",
					strings.Join(ctx.cfg.Blocked, ", "), func(v string) tea.Cmd {
						var out []string
						for _, t := range strings.Split(v, ",") {
							if t = strings.TrimSpace(t); t != "" {
								out = append(out, t)
							}
						}
						ctx.cfg.Blocked = out
						return s.save()
					},
					"comma-separated whole words — cam hides a camrip, not Camelot",
					"trailing * matches word starts: upscale* covers upscaled",
					"dots and underscores count as spaces, so ai upscale finds AI.Upscale",
					"hidden from the picker, not deleted — B shows them anyway",
					"",
					"the usual junk-quality tags, if you want them:",
					"  cam, camrip, hdcam, ts, telesync, hdts, tc, telecine,",
					"  scr, screener, dvdscr, workprint, wp, pdvd")
			}},
		{label: "subtitle language", sub: "preferred track, and where S opens",
			badge: orDash(c.SubtitleLang), act: func() tea.Cmd {
				help := []string{
					"comma-separated for a preference order, e.g. eng, spa",
					"the name works too, and regional variants fold in: en-GB is English",
					"passed to mpv as --slang, and preselected in the S picker",
					"",
				}
				help = append(help, stremio.LangReference()...)

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
		{label: "autoplay", sub: "pick the next episode's stream and play it · a on a stream list", badge: onOff(c.Autoplay), act: func() tea.Cmd {
			flipAutoplay()
			return tea.Batch(s.save(), playerStateCmd(), autoplayChanged)
		}},
		{label: "close mpv on exit", sub: "off leaves playback running after you quit", badge: onOff(c.CloseMpvOnExit), act: func() tea.Cmd {
			ctx.cfg.CloseMpvOnExit = !ctx.cfg.CloseMpvOnExit
			return s.save()
		}},

		{head: ""},
		{head: "library"},
		{label: "auto load more", sub: "fetch the next page when you reach the end",
			badge: onOff(c.AutoLoadMore), act: func() tea.Cmd {
				ctx.cfg.AutoLoadMore = !ctx.cfg.AutoLoadMore
				return s.save()
			}},

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
			ctx.cfg.PosterSize = poster.NextSize(ctx.cfg.PosterSize)
			poster.Gen++ // force panels to redraw at the new size
			return s.save()
		}},

		{label: "episode images", sub: "stills can spoil the episode",
			badge: onOff(c.EpisodeImages), act: func() tea.Cmd {
				ctx.cfg.EpisodeImages = !ctx.cfg.EpisodeImages
				poster.Gen++
				return s.save()
			}},

		{label: "preload ahead", sub: "info and posters for the next rows · ←/→ to change",
			badge: preloadLabel(c.PreloadAhead),
			act: func() tea.Cmd {
				ctx.cfg.PreloadAhead = (ctx.cfg.PreloadAhead + 1) % (maxPreload + 1)
				return s.save()
			},
			step: func(dir int) tea.Cmd {
				n := min(max(ctx.cfg.PreloadAhead+dir, 0), maxPreload)
				if n == ctx.cfg.PreloadAhead {
					return nil
				}
				ctx.cfg.PreloadAhead = n
				return s.save()
			}},

		{label: "poster quality", sub: "kitty only, half-blocks always use small",
			badge: orDash(c.PosterQuality), act: func() tea.Cmd {
				ctx.cfg.PosterQuality = poster.NextQuality(ctx.cfg.PosterQuality)
				poster.Gen++
				return s.save()
			}},

		{label: "poster protocol", sub: "how posters are drawn", badge: orDash(c.KittyMode),
			act: func() tea.Cmd {
				ctx.cfg.KittyMode = poster.NextKittyMode(ctx.cfg.KittyMode)
				poster.Gen++ // redraw through whichever path now applies
				return s.save()
			}},

		{head: ""},
		{head: "downloads"},
		{label: "download location", sub: "where D on a stream saves to", badge: orDash(c.DownloadDir), act: func() tea.Cmd {
			return s.prompt("download location", store.DefaultDownloadDir(), ctx.cfg.DownloadDir, func(v string) tea.Cmd {
				ctx.cfg.DownloadDir = store.ExpandPath(v)
				return s.save()
			}, "~ and environment variables are expanded", "folders are created as needed")
		}},
		{label: "organise downloads", sub: "off saves everything flat", badge: onOff(c.DownloadFolders), act: func() tea.Cmd {
			ctx.cfg.DownloadFolders = !ctx.cfg.DownloadFolders
			return s.save()
		}},
		{label: "movie filename", sub: orDash(c.MoviePattern), act: func() tea.Cmd {
			return s.prompt("movie filename", store.DefaultMoviePattern, ctx.cfg.MoviePattern, func(v string) tea.Cmd {
				ctx.cfg.MoviePattern = v
				return s.save()
			}, "placeholders: {title} {year}", "/ makes a folder · extension is added for you")
		}},
		{label: "episode filename", sub: orDash(c.EpisodePattern), act: func() tea.Cmd {
			return s.prompt("episode filename", store.DefaultEpisodePattern, ctx.cfg.EpisodePattern, func(v string) tea.Cmd {
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
	if m, ok := msg.(update.CheckedMsg); ok {
		s.update = m.Info
		return s, nil
	}

	if k, ok := msg.(tea.KeyMsg); ok {
		// Before the list, which pages on left/right. Not while typing a filter.
		if key := k.String(); !s.list.Typing() && (key == "left" || key == "right") {
			if i := s.list.Selected(); i >= 0 && i < len(s.rows) && s.rows[i].step != nil {
				dir := 1
				if key == "left" {
					dir = -1
				}
				return s, s.rows[i].step(dir)
			}
		}

		if consumed, cmd := s.list.Update(msg); consumed {
			return s, cmd
		}
		switch k.String() {
		case "u":
			// Only when there's somewhere to go — an unadvertised key that
			// does nothing is worse than no key.
			if s.update.State == update.Available && s.update.URL != "" {
				_ = poster.OpenURL(s.update.URL)
				return s, toast("opened the release page")
			}
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
	left := "  " + stSub.Render(store.CfgFile())
	right := stHint.Render(store.AppName+" ") + stKey.Render(update.Version)

	head := left
	if gap := s.w - lipgloss.Width(left) - lipgloss.Width(right) - 2; gap > 1 {
		head += strings.Repeat(" ", gap) + right
	}

	out := fitLine(head, s.w) + "\n" + s.list.View()

	// At the foot rather than the header: it's the least urgent thing here,
	// and nothing about it should pull attention while you're changing a
	// setting.
	if line := updateLine(s.update); line != "" {
		out += "\n  " + line
	}
	return out
}

package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"stremio-cliuwu/internal/player"
	"stremio-cliuwu/internal/poster"
	"stremio-cliuwu/internal/stremio"
)

// Subtitles are picked while something is playing, not while choosing a
// stream: you generally find out you want them a minute in, and by then the
// stream list is long gone.

type subsLoadedMsg struct {
	id   poster.AsyncID
	subs []stremio.Subtitle
}

type subsScreen struct {
	baseScreen
	id     poster.AsyncID
	query  stremio.SubsQuery
	label  string
	subs   []stremio.Subtitle
	shown  []int
	active string // url of the track currently loaded, for the marker

	list   listModel
	busy   busy
	loaded bool

	langs  []string // "all" plus each language present
	langIx int
}

func newSubsScreen(label string, q stremio.SubsQuery, shipped []stremio.Subtitle) *subsScreen {
	l := newList()
	l.Empty = "no subtitles found"
	l.Numbered = true

	// Whatever came with the stream is already in hand, so it shows before
	// any request goes out.
	for i := range shipped {
		shipped[i].Addon = "stream"
	}

	return &subsScreen{
		id: poster.NewAsyncID(), query: q, label: label, subs: shipped, list: l,
		busy: newBusy("looking for subtitles…"),
	}
}

func (s *subsScreen) Title() string { return "subtitles" }

func (s *subsScreen) Typing() bool { return s.list.Typing() }

func (s *subsScreen) SetSize(w, h int) {
	s.baseScreen.SetSize(w, h)
	s.list.SetSize(w, h-2)
}

func (s *subsScreen) Init() tea.Cmd {
	id, q, addons := s.id, s.query, ctx.addons
	shipped := s.subs

	return tea.Batch(
		s.busy.start("looking for subtitles…"),
		func() tea.Msg {
			return subsLoadedMsg{
				id:   id,
				subs: stremio.MergeSubtitles(shipped, stremio.GetSubtitles(addons, q, ctx.cfg.SubtitleLang), ctx.cfg.SubtitleLang),
			}
		},
	)
}

func (s *subsScreen) Footer() string {
	pairs := [][2]string{{"enter", "try"}, {"0-9", "jump"}}
	if len(s.langs) > 2 {
		pairs = append(pairs, [2]string{"tab", "filter by language"})
	}
	pairs = append(pairs, [2]string{"R", "refetch streams"}, [2]string{"/", "filter"},
		[2]string{"b/esc", "back"})
	return withStatus(s.list.Status(), keyHint(pairs...))
}

// collectLangs builds the language tabs, and opens on your preferred one.
//
// Grouped by canonical name rather than raw code, so "eng", "en" and
// "English" from three different addons are one tab instead of three.
func (s *subsScreen) collectLangs() {
	seen := map[string]bool{}
	s.langs = []string{"all"}

	for _, sub := range s.subs {
		if n := stremio.LangName(sub.Lang); !seen[n] {
			seen[n] = true
			s.langs = append(s.langs, n)
		}
	}

	// Land on the first of your preferred languages that actually came back.
	// The setting is a list, so "eng, en, English" means try each in turn.
	s.langIx = 0
outer:
	for _, want := range stremio.PreferredLangs(ctx.cfg.SubtitleLang) {
		for i, l := range s.langs {
			if i > 0 && l == want {
				s.langIx = i
				break outer
			}
		}
	}
}

func (s *subsScreen) rebuild() {
	s.shown = s.shown[:0]

	filter := ""
	if s.langIx > 0 && s.langIx < len(s.langs) {
		filter = s.langs[s.langIx]
	}

	var items []Item
	for i, sub := range s.subs {
		if filter != "" && stremio.LangName(sub.Lang) != filter {
			continue
		}
		s.shown = append(s.shown, i)
		badge := grey(sub.Addon)
		if sub.URL == s.active {
			badge = good("✓ on") + "  " + badge
		}

		items = append(items, Item{
			Label: bold(stremio.LangName(sub.Lang)),
			Sub:   sub.Label(),
			Badge: badge,
		})
	}
	s.list.SetItems(items)
}

// langBar shows a window of tabs around the selected one.
//
// Twenty-odd languages don't fit on one line, and the whole row ran off the
// right edge — including, sometimes, the tab you were on. Windowing keeps the
// selection visible and marks that there's more either side.
func (s *subsScreen) langBar() string {
	if len(s.langs) < 3 {
		return ""
	}

	tab := func(i int) string {
		if i == s.langIx {
			return stCursor.Render(" " + s.langs[i] + " ")
		}
		return stHint.Render(" " + s.langs[i] + " ")
	}

	// Grow outwards from the selection until the line is full.
	budget := s.w - 8 // margins, plus room for the ellipses
	lo, hi := s.langIx, s.langIx
	used := lipgloss.Width(tab(s.langIx))

	for lo > 0 || hi < len(s.langs)-1 {
		grew := false
		if hi < len(s.langs)-1 {
			if w := lipgloss.Width(tab(hi+1)) + 1; used+w <= budget {
				hi, used, grew = hi+1, used+w, true
			}
		}
		if lo > 0 {
			if w := lipgloss.Width(tab(lo-1)) + 1; used+w <= budget {
				lo, used, grew = lo-1, used+w, true
			}
		}
		if !grew {
			break
		}
	}

	out := "  "
	if lo > 0 {
		out += stHint.Render("‹ ")
	}
	for i := lo; i <= hi; i++ {
		out += tab(i) + " "
	}
	if hi < len(s.langs)-1 {
		out += stHint.Render("›")
	}
	return out
}

func (s *subsScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch m := msg.(type) {
	case subsLoadedMsg:
		if m.id != s.id {
			return s, nil
		}
		s.busy.stop()
		s.loaded = true
		s.subs = m.subs
		s.collectLangs()
		s.rebuild()
		return s, nil

	case player.SubtitleAddedMsg:
		if m.Err != nil {
			return s, toastErr("mpv couldn't load that one — try another")
		}
		cur := s.list.Selected()
		s.active = m.URL
		s.rebuild()
		if cur >= 0 {
			s.list.Focus(cur)
		}
		return s, toast("subtitles on — " + m.Title)

	case tea.KeyMsg:
		if consumed, cmd := s.list.Update(msg); consumed {
			return s, cmd
		}
		switch m.String() {
		case "enter":
			i := s.list.Selected()
			if i < 0 || i >= len(s.shown) {
				return s, nil
			}
			s.list.ClearNum()
			sub := s.subs[s.shown[i]]

			// Stays open. The first pick often has the wrong timing, and
			// closing the list each time meant reopening it from scratch to
			// try the next one. The ✓ waits for mpv to confirm.
			return s, tea.Batch(
				ctx.player.AddSubtitle(sub.URL, stremio.LangName(sub.Lang), sub.Lang),
				toast("loading "+stremio.LangName(sub.Lang)+" subtitles…"),
			)

		case "R":
			stremio.CacheSubs.Delete(stremio.SubsKey(s.query))
			s.loaded = false
			s.id = poster.NewAsyncID()
			return s, s.Init()
		case "tab":
			if len(s.langs) > 1 {
				s.langIx = (s.langIx + 1) % len(s.langs)
				s.rebuild()
			}
		case "shift+tab":
			if len(s.langs) > 1 {
				s.langIx = (s.langIx - 1 + len(s.langs)) % len(s.langs)
				s.rebuild()
			}
		case "esc", "backspace":
			return s, pop()
		case "q":
			return s, popRoot()
		}
	}
	return s, s.busy.update(msg)
}

func (s *subsScreen) View() string {
	if !s.loaded {
		return s.busy.view()
	}

	head := fitLine("  "+stSub.Render(s.label), s.w) + "\n"
	if bar := s.langBar(); bar != "" {
		head += fitLine(bar, s.w) + "\n"
	}
	if len(s.subs) == 0 {
		head += "\n" + stHint.Render(fmt.Sprintf(
			"  none of your addons returned subtitles for %s", s.query.VideoID))
	}
	return head + s.list.View()
}

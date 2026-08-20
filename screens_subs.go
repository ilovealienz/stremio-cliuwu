package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Subtitles are picked while something is playing, not while choosing a
// stream: you generally find out you want them a minute in, and by then the
// stream list is long gone.

type subsLoadedMsg struct {
	id   asyncID
	subs []Subtitle
}

type subsScreen struct {
	baseScreen
	id     asyncID
	target streamTarget
	subs   []Subtitle
	shown  []int

	list   listModel
	busy   busy
	loaded bool

	langs  []string // "all" plus each language present
	langIx int
}

func newSubsScreen(t streamTarget) *subsScreen {
	l := newList()
	l.Empty = "no subtitles found"
	l.Numbered = true

	return &subsScreen{
		id: newAsyncID(), target: t, list: l,
		busy: newBusy("looking for subtitles…"),
	}
}

func (s *subsScreen) Title() string { return "subtitles" }
func (s *subsScreen) Typing() bool  { return s.list.Typing() }

func (s *subsScreen) SetSize(w, h int) {
	s.baseScreen.SetSize(w, h)
	s.list.SetSize(w, h-2)
}

func (s *subsScreen) Init() tea.Cmd {
	id, t := s.id, s.target
	addons := ctx.addons

	return tea.Batch(
		s.busy.start("looking for subtitles…"),
		func() tea.Msg {
			return subsLoadedMsg{id: id, subs: GetSubtitles(addons, t.MediaType, t.VideoID)}
		},
	)
}

func (s *subsScreen) Footer() string {
	pairs := [][2]string{{"enter", "turn on"}, {"0-9", "jump"}}
	if len(s.langs) > 2 {
		pairs = append(pairs, [2]string{"tab", "language"})
	}
	pairs = append(pairs, [2]string{"R", "refetch"}, [2]string{"/", "filter"},
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
		if n := langName(sub.Lang); !seen[n] {
			seen[n] = true
			s.langs = append(s.langs, n)
		}
	}

	// Land on the first of your preferred languages that actually came back.
	// The setting is a list, so "eng, en, English" means try each in turn.
	s.langIx = 0
outer:
	for _, want := range PreferredLangs(ctx.cfg.SubtitleLang) {
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
		if filter != "" && langName(sub.Lang) != filter {
			continue
		}
		s.shown = append(s.shown, i)
		items = append(items, Item{
			Label: bold(langName(sub.Lang)),
			Sub:   sub.ID,
			Badge: grey(sub.Addon),
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

			// Back to what you were doing: the point of turning subtitles on
			// is to carry on watching, not to sit on a list of them.
			return s, tea.Batch(
				ctx.player.AddSubtitle(sub.URL, langName(sub.Lang), sub.Lang),
				pop(),
			)

		case "R":
			cacheSubs.Delete(s.target.MediaType + ":" + s.target.VideoID)
			s.loaded = false
			s.id = newAsyncID()
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

	head := "  " + stSub.Render(s.target.Label) + "\n"
	if bar := s.langBar(); bar != "" {
		head += bar + "\n"
	}
	if len(s.subs) == 0 {
		head += "\n" + stHint.Render(fmt.Sprintf(
			"  none of your addons returned subtitles for %s", s.target.VideoID))
	}
	return head + s.list.View()
}

package main

import (
	"fmt"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

// The specials row of a merged anime opens this rather than an episode list.
//
// Season 0 collects every OVA, recap and set of shorts a series ever had —
// forty-odd unrelated things in one flat list. Each of them is a proper title
// in kitsu with its own name and episodes, so this offers those instead, and
// picking one opens it like any other series.

type specialsNamedMsg struct {
	id     asyncID
	names  map[string]string
	counts map[string]int
}

type specialsScreen struct {
	baseScreen
	show   Meta
	groups []specialGroup

	id   asyncID
	list listModel
}

func newSpecialsScreen(show Meta, sm SeriesMeta) *specialsScreen {
	l := newList()
	l.Empty = "no specials"
	l.Numbered = true

	s := &specialsScreen{
		id: newAsyncID(), show: show,
		groups: groupSpecials(sm.Videos), list: l,
	}
	s.rebuild()
	return s
}

func (s *specialsScreen) Title() string { return "specials" }
func (s *specialsScreen) Typing() bool  { return s.list.Typing() }

func (s *specialsScreen) SetSize(w, h int) {
	s.baseScreen.SetSize(w, h)
	s.list.SetSize(w, h-1)
}

func (s *specialsScreen) Footer() string {
	return withStatus(s.list.Status(),
		keyHint([2]string{"enter", "open"}, [2]string{"0-9", "jump to row"},
			[2]string{"/", "filter"}, [2]string{"b/esc", "back"}))
}

// Init fetches each collection's real name and episode count.
//
// Both have to come from the collection itself. The merged series tags its
// season 0 videos with a kitsu id, but that tagging is coarser than the
// entries are — one id can be stamped on far more videos than its own entry
// actually has, so counting them there reported 37 episodes for a four-part
// OVA. The list renders immediately with derived names and no count, and
// fills in once the real numbers arrive.
func (s *specialsScreen) Init() tea.Cmd {
	id, groups, addons := s.id, s.groups, ctx.addons

	return func() tea.Msg {
		names := map[string]string{}
		counts := map[string]int{}

		var mu sync.Mutex
		var wg sync.WaitGroup

		for _, g := range groups {
			wg.Add(1)
			go func(kitsuID string) {
				defer wg.Done()

				m := Meta{ID: "kitsu:" + kitsuID, Type: "series"}
				sm := GetSeriesMeta(addons, m)

				// Every video counts. Filtering on the episode number assumed
				// one is always present, and a specials entry may not number
				// its parts at all — which reported zero for a four-part OVA.
				n := len(sm.Videos)

				mu.Lock()
				if sm.Name != "" {
					names[kitsuID] = sm.Name
				}
				if n > 0 {
					counts[kitsuID] = n
				}
				mu.Unlock()
			}(g.KitsuID)
		}
		wg.Wait()

		return specialsNamedMsg{id: id, names: names, counts: counts}
	}
}

func (s *specialsScreen) rebuild() {
	items := make([]Item, len(s.groups))
	for i, g := range s.groups {
		// Blank until the real count arrives — a wrong number is worse than
		// none, and it's only a moment.
		sub := ""
		switch {
		case g.Count == 1:
			sub = "1 episode"
		case g.Count > 1:
			sub = fmt.Sprintf("%d episodes", g.Count)
		}

		items[i] = Item{
			Label: bold(g.Title),
			Sub:   sub,
			Badge: grey("kitsu:" + g.KitsuID),
		}
	}
	s.list.SetItems(items)
}

func (s *specialsScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch m := msg.(type) {
	case specialsNamedMsg:
		if m.id != s.id {
			return s, nil
		}
		for i, g := range s.groups {
			if n, ok := m.names[g.KitsuID]; ok {
				s.groups[i].Title = n
			}
			if c, ok := m.counts[g.KitsuID]; ok {
				s.groups[i].Count = c
			}
		}
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
		case "enter":
			i := s.list.Selected()
			if i < 0 || i >= len(s.groups) {
				return s, nil
			}
			s.list.ClearNum()
			g := s.groups[i]

			// Opened as the title it is. It won't merge back into the parent
			// series — that only happens for entries mapping to a numbered
			// season, and a special maps to zero.
			return s, push(openMeta(Meta{
				ID:     "kitsu:" + g.KitsuID,
				Type:   "series",
				Name:   g.Title,
				Source: s.show.Source,
			}, noSeason))

		case "esc", "backspace":
			return s, pop()
		case "q":
			return s, popRoot()
		}
	}
	return s, nil
}

func (s *specialsScreen) View() string {
	head := "  " + stSub.Render(s.show.Name) + "\n"
	return head + s.list.View()
}

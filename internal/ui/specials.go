package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"stremio-cliuwu/internal/poster"
	"stremio-cliuwu/internal/stremio"
)

// The specials row of a merged anime opens this rather than an episode list.
//
// Season 0 collects every OVA, recap and set of shorts a series ever had —
// forty-odd unrelated things in one flat list. Each of them is a proper title
// in kitsu with its own name and episodes, so this offers those instead, and
// picking one opens it like any other series. Specials with no kitsu entry
// of their own, often films, are gathered under one row at the end.

// specialNamedMsg is one collection's real name and episode count.
type specialNamedMsg struct {
	id      poster.AsyncID
	kitsuID string
	name    string
	count   int
}

// specialsAtOnce is how many names are fetched at a time. A long series has
// dozens of collections.
const specialsAtOnce = 6

type specialsScreen struct {
	baseScreen
	show   stremio.Meta
	sm     stremio.SeriesMeta
	groups []specialGroup
	loose  []stremio.Video // season 0 with no kitsu entry

	id    poster.AsyncID
	queue []string        // kitsu ids still to ask for
	named map[string]bool // ids whose name has arrived
	list  listModel
}

func newSpecialsScreen(show stremio.Meta, sm stremio.SeriesMeta) *specialsScreen {
	l := newList()
	l.Empty = "no specials"
	l.Numbered = true

	s := &specialsScreen{
		show: show, sm: sm, groups: groupSpecials(sm.Videos), loose: looseSpecials(sm.Videos),
		named: map[string]bool{}, list: l,
	}
	s.rebuild()
	return s
}

func (s *specialsScreen) Title() string { return "specials" }

func (s *specialsScreen) Typing() bool { return s.list.Typing() }

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
// each row fills in as its answer arrives.
func (s *specialsScreen) Init() tea.Cmd { return s.fetchNames() }

// syncInfo runs on coming back to the screen. Answers only reach the top
// screen, so any that arrived while a collection was open were lost; this
// asks again for the names still missing.
func (s *specialsScreen) syncInfo() tea.Cmd { return s.fetchNames() }

// fetchNames starts on the names not in yet, a few at a time. A new id drops
// answers meant for an earlier round.
func (s *specialsScreen) fetchNames() tea.Cmd {
	s.id = poster.NewAsyncID()
	s.queue = s.queue[:0]
	for _, g := range s.groups {
		if !s.named[g.KitsuID] {
			s.queue = append(s.queue, g.KitsuID)
		}
	}
	var cmds []tea.Cmd
	for range specialsAtOnce {
		if c := s.next(); c != nil {
			cmds = append(cmds, c)
		}
	}
	return tea.Batch(cmds...)
}

// next fetches the next name in the queue, if there is one.
func (s *specialsScreen) next() tea.Cmd {
	if len(s.queue) == 0 {
		return nil
	}
	kitsuID := s.queue[0]
	s.queue = s.queue[1:]
	id, addons := s.id, ctx.addons
	return func() tea.Msg {
		sm := stremio.GetSeriesMeta(addons, stremio.Meta{ID: "kitsu:" + kitsuID, Type: "series"})

		// Every video counts. Filtering on the episode number assumed one is
		// always present, and a specials entry may not number its parts at
		// all — which reported zero for a four-part OVA.
		return specialNamedMsg{id: id, kitsuID: kitsuID, name: sm.Name, count: len(sm.Videos)}
	}
}

func (s *specialsScreen) rebuild() {
	items := make([]Item, len(s.groups), len(s.groups)+1)
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
	if len(s.loose) > 0 {
		items = append(items, Item{
			Label: bold(otherSpecials),
			Sub:   plural(len(s.loose), "episode"),
			Badge: grey("no kitsu entry"),
		})
	}
	s.list.SetItems(items)
}

const otherSpecials = "other specials"

func (s *specialsScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch m := msg.(type) {
	case specialNamedMsg:
		if m.id != s.id {
			return s, nil
		}
		if m.name != "" { // a failed one is asked again next time you come back
			s.named[m.kitsuID] = true
		}
		for i, g := range s.groups {
			if g.KitsuID != m.kitsuID {
				continue
			}
			if m.name != "" {
				s.groups[i].Title = m.name
			}
			if m.count > 0 {
				s.groups[i].Count = m.count
			}
		}
		cur := s.list.Selected()
		s.rebuild()
		if cur >= 0 {
			s.list.Focus(cur)
		}
		return s, s.next()

	case tea.KeyMsg:
		if consumed, cmd := s.list.Update(msg); consumed {
			return s, cmd
		}
		switch m.String() {
		case "enter":
			i := s.list.Selected()
			if i < 0 || i > len(s.groups) || i == len(s.groups) && len(s.loose) == 0 {
				return s, nil
			}
			s.list.ClearNum()

			// The ones with no kitsu entry, as an episode list of their own.
			if i == len(s.groups) {
				es := newEpisodeScreen(s.show, stremio.SeriesMeta{Videos: s.loose}, 0)
				es.title, es.byName = otherSpecials, true
				return s, push(es)
			}
			g := s.groups[i]

			// Opened as the title it is. It won't merge back into the parent
			// series — that only happens for entries mapping to a numbered
			// season, and a special maps to zero.
			return s, push(openMeta(stremio.Meta{
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
	head := fitLine("  "+stSub.Render(s.show.Name), s.w) + "\n"
	return head + s.list.View()
}

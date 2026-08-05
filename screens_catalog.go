package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ── Catalog picker ────────────────────────────────────────────────────────────

// catalogPicker lists the catalogs of one kind, grouped by addon. The menu
// skips straight past this when there's only one, so it only shows up when
// there's an actual choice to make.
type catalogPickerScreen struct {
	baseScreen
	kind  string
	refs  []CatalogRef
	list  listModel
	title string
}

func newCatalogPicker(kind, title string) *catalogPickerScreen {
	l := newList()
	l.Empty = "no catalogs — add an addon that provides them"
	s := &catalogPickerScreen{kind: kind, title: title, list: l}
	s.rebuild()
	return s
}

func (s *catalogPickerScreen) Init() tea.Cmd { return nil }
func (s *catalogPickerScreen) Title() string { return s.title }
func (s *catalogPickerScreen) Typing() bool  { return s.list.Typing() }

func (s *catalogPickerScreen) SetSize(w, h int) {
	s.baseScreen.SetSize(w, h)
	s.list.SetSize(w, h)
}

func (s *catalogPickerScreen) Footer() string {
	return keyHint([2]string{"enter", "browse"}, [2]string{"/", "filter"}, [2]string{"b/esc", "back"}) +
		"   " + stHint.Render(s.list.Status())
}

func (s *catalogPickerScreen) rebuild() {
	all := CatalogsOfKind(ctx.addons, s.kind)

	// Group by addon, with a section label per addon, but only when more than
	// one addon contributes — otherwise the header is just noise.
	multi := false
	if len(all) > 0 {
		first := all[0].AddonName
		for _, c := range all {
			if c.AddonName != first {
				multi = true
				break
			}
		}
	}

	s.refs = nil
	var items []Item
	last := ""
	groups := 0
	for _, c := range all {
		if multi && c.AddonName != last {
			// Keyed off the group counter, not `last != ""` — an addon with an
			// empty name would otherwise swallow the first header entirely.
			if groups > 0 {
				items = append(items, Item{Header: true})
				s.refs = append(s.refs, CatalogRef{})
			}
			items = append(items, Item{Header: true, Label: c.AddonName})
			s.refs = append(s.refs, CatalogRef{})
			last = c.AddonName
			groups++
		}
		badge := ""
		if c.Search {
			badge = grey("searchable")
		}
		items = append(items, Item{Label: bold(c.Name), Badge: badge})
		s.refs = append(s.refs, c)
	}
	s.list.SetItems(items)
}

func (s *catalogPickerScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		if consumed, cmd := s.list.Update(msg); consumed {
			return s, cmd
		}
		switch k.String() {
		case "enter":
			if i := s.list.Selected(); i >= 0 && s.refs[i].ID != "" {
				return s, push(newCatalogScreen(s.refs[i]))
			}
		case "esc", "backspace":
			return s, pop()
		case "q":
			return s, popRoot()
		}
	}
	return s, nil
}

func (s *catalogPickerScreen) View() string { return s.list.View() }

// ── Catalog browser ───────────────────────────────────────────────────────────

type catalogPageMsg struct {
	id      asyncID
	metas   []Meta
	hasMore bool
	err     error
}

type catalogScreen struct {
	baseScreen
	id      asyncID
	ref     CatalogRef
	metas   []Meta
	hasMore bool

	list   listModel
	busy   busy
	loaded bool
}

func newCatalogScreen(ref CatalogRef) *catalogScreen {
	l := newList()
	l.Empty = "empty catalog"
	return &catalogScreen{id: newAsyncID(), ref: ref, list: l, busy: newBusy("loading…")}
}

func (s *catalogScreen) Init() tea.Cmd { return s.loadPage(0) }

func (s *catalogScreen) loadPage(skip int) tea.Cmd {
	s.id = newAsyncID()
	id, ref := s.id, s.ref
	return tea.Batch(
		s.busy.start("loading "+ref.Name+"…"),
		func() tea.Msg {
			metas, more, err := FetchCatalog(ref, skip)
			return catalogPageMsg{id: id, metas: metas, hasMore: more, err: err}
		},
	)
}

func (s *catalogScreen) Title() string { return s.ref.Name }
func (s *catalogScreen) Typing() bool  { return s.list.Typing() }

func (s *catalogScreen) SetSize(w, h int) {
	s.baseScreen.SetSize(w, h)
	s.list.SetSize(w, h-1)
}

func (s *catalogScreen) Footer() string {
	pairs := [][2]string{{"enter", "open"}, {"f", "favourite"}, {"/", "filter"}}
	if s.hasMore {
		pairs = append(pairs, [2]string{"m", "load more"})
	}
	pairs = append(pairs, [2]string{"b/esc", "back"})
	return keyHint(pairs...) + "   " + stHint.Render(s.list.Status())
}

func (s *catalogScreen) rebuild() {
	items := make([]Item, len(s.metas))
	for i, m := range s.metas {
		items[i] = metaItem(m)
	}
	s.list.SetItems(items)
}

func (s *catalogScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch m := msg.(type) {
	case catalogPageMsg:
		if m.id != s.id {
			return s, nil
		}
		s.busy.stop()
		s.loaded = true
		if m.err != nil {
			return s, toastErr(m.err.Error())
		}
		s.metas = append(s.metas, m.metas...)
		s.hasMore = m.hasMore && len(m.metas) > 0
		s.rebuild()
		return s, nil

	case tea.KeyMsg:
		if consumed, cmd := s.list.Update(msg); consumed {
			return s, cmd
		}
		switch m.String() {
		case "enter":
			if i := s.list.Selected(); i >= 0 {
				return s, push(openMeta(s.metas[i], 0))
			}
		case "f":
			if i := s.list.Selected(); i >= 0 {
				mt := s.metas[i]
				AddFav(Favourite{Name: mt.Name, ID: mt.ID, Type: mt.Type, Source: mt.Source, Year: mt.Year})
				return s, toast("favourited " + mt.Name)
			}
		case "m":
			if s.hasMore {
				return s, s.loadPage(len(s.metas))
			}
		case "esc", "backspace":
			return s, pop()
		case "q":
			return s, popRoot()
		}
	}
	return s, s.busy.update(msg)
}

func (s *catalogScreen) View() string {
	if !s.loaded {
		return s.busy.view()
	}
	head := "  " + stSub.Render(s.ref.AddonName) + "\n"
	return head + s.list.View()
}

// ── Search ────────────────────────────────────────────────────────────────────

type searchMsg struct {
	id    asyncID
	metas []Meta
}

type searchScreen struct {
	baseScreen
	id    asyncID
	query string
	metas []Meta
	shown []int // indices into metas after the category filter

	cats   []string // "all" plus each category present in the results
	catIdx int

	list   listModel
	busy   busy
	loaded bool
}

func newSearchScreen(query string) *searchScreen {
	l := newList()
	l.Empty = "nothing found"
	return &searchScreen{id: newAsyncID(), query: query, list: l, busy: newBusy("searching…")}
}

func (s *searchScreen) Init() tea.Cmd {
	id, q := s.id, s.query
	addons := ctx.addons
	return tea.Batch(
		s.busy.start("searching…"),
		func() tea.Msg { return searchMsg{id: id, metas: Search(addons, q)} },
	)
}

func (s *searchScreen) Title() string { return "search" }
func (s *searchScreen) Typing() bool  { return s.list.Typing() }

func (s *searchScreen) SetSize(w, h int) {
	s.baseScreen.SetSize(w, h)
	s.list.SetSize(w, h-2)
}

func (s *searchScreen) Footer() string {
	return keyHint(
		[2]string{"enter", "open"},
		[2]string{"tab", "category"},
		[2]string{"f", "favourite"},
		[2]string{"/", "filter"},
		[2]string{"b/esc", "back"},
	) + "   " + stHint.Render(s.list.Status())
}

func (s *searchScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch m := msg.(type) {
	case searchMsg:
		if m.id != s.id {
			return s, nil
		}
		s.busy.stop()
		s.loaded = true
		s.metas = m.metas
		s.collectCats()
		s.rebuild()
		return s, nil

	case tea.KeyMsg:
		if consumed, cmd := s.list.Update(msg); consumed {
			return s, cmd
		}
		switch m.String() {
		case "enter":
			if i := s.list.Selected(); i >= 0 && i < len(s.shown) {
				return s, push(openMeta(s.metas[s.shown[i]], 0))
			}
		case "f":
			if i := s.list.Selected(); i >= 0 && i < len(s.shown) {
				mt := s.metas[s.shown[i]]
				AddFav(Favourite{Name: mt.Name, ID: mt.ID, Type: mt.Type, Source: mt.Source, Year: mt.Year})
				return s, toast("favourited " + mt.Name)
			}
		case "tab":
			if len(s.cats) > 1 {
				s.catIdx = (s.catIdx + 1) % len(s.cats)
				s.rebuild()
			}
		case "shift+tab":
			if len(s.cats) > 1 {
				s.catIdx = (s.catIdx - 1 + len(s.cats)) % len(s.cats)
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

// collectCats builds the category tabs from whatever the search returned, so
// you only ever see categories that have results behind them.
func (s *searchScreen) collectCats() {
	s.cats = []string{"all"}
	seen := map[string]bool{}
	for _, m := range s.metas {
		if m.Source == "" || seen[m.Source] {
			continue
		}
		seen[m.Source] = true
		s.cats = append(s.cats, m.Source)
	}
	if s.catIdx >= len(s.cats) {
		s.catIdx = 0
	}
}

func (s *searchScreen) catFilter() string {
	if s.catIdx <= 0 || s.catIdx >= len(s.cats) {
		return ""
	}
	return s.cats[s.catIdx]
}

func (s *searchScreen) rebuild() {
	want := s.catFilter()

	s.shown = s.shown[:0]
	var items []Item
	for i, m := range s.metas {
		if want != "" && m.Source != want {
			continue
		}
		s.shown = append(s.shown, i)
		items = append(items, metaItem(m))
	}
	s.list.SetItems(items)
}

func (s *searchScreen) catBar() string {
	if len(s.cats) <= 2 {
		return ""
	}
	var parts []string
	for i, c := range s.cats {
		n := 0
		if i == 0 {
			n = len(s.metas)
		} else {
			for _, m := range s.metas {
				if m.Source == c {
					n++
				}
			}
		}
		label := fmt.Sprintf("%s %d", c, n)
		if i == s.catIdx {
			parts = append(parts, stCursor.Render("["+label+"]"))
		} else {
			parts = append(parts, stHint.Render(" "+label+" "))
		}
	}
	return "  " + strings.Join(parts, stHint.Render("·")) + "   " + stHint.Render("tab")
}

func (s *searchScreen) View() string {
	if !s.loaded {
		return s.busy.view()
	}
	n := len(SearchCatalogs(ctx.addons))
	out := "  " + stSub.Render(fmt.Sprintf("%q across %d searchable catalog(s)", s.query, n)) + "\n"
	if bar := s.catBar(); bar != "" {
		out += bar + "\n"
	}
	return out + s.list.View()
}

func searchPrompt() *promptScreen {
	n := len(SearchCatalogs(ctx.addons))
	help := fmt.Sprintf("queries %d searchable catalog(s) from your addons", n)
	if n == 0 {
		help = "none of your addons expose a searchable catalog"
	}
	return newPrompt("search", "title…", "", func(q string) tea.Cmd {
		if q == "" {
			return nil
		}
		return push(newSearchScreen(q))
	}, help)
}

// ── Shared ────────────────────────────────────────────────────────────────────

// openMeta picks the right next screen for a title.
func openMeta(m Meta, season int) screen {
	switch m.Type {
	case "movie":
		return newStreamScreen(streamTarget{
			Meta:      m,
			MediaType: "movie",
			VideoID:   m.ID,
			Label:     m.Name,
		})
	case "series", "anime":
		return newSeasonScreen(m, season)
	}
	// "other" and anything else: a debrid library entry, whose meta carries
	// the playable files directly.
	return newFileListScreen(m)
}

// browseKind opens the catalogs for a menu bucket, skipping the picker when
// there's only one catalog to pick.
func browseKind(kind, title string) tea.Cmd {
	refs := CatalogsOfKind(ctx.addons, kind)
	switch len(refs) {
	case 0:
		return toastErr("no " + title + " catalogs — check your addons")
	case 1:
		return push(newCatalogScreen(refs[0]))
	}
	return push(newCatalogPicker(kind, title))
}

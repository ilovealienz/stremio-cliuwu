package main

import (
	"fmt"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
)

type seriesMetaMsg struct {
	id      asyncID
	sm      SeriesMeta
	seasons []int
}

type episodesMsg struct {
	id  asyncID
	eps []Video
}

// ── Season picker ─────────────────────────────────────────────────────────────

type seasonScreen struct {
	baseScreen
	id      asyncID
	show    Meta
	jumpTo  int // season to open immediately (from favourites/history)
	jumped  bool

	// Set when resuming from history: the episode to open, and where in it.
	autoEpisode int
	autoResume  float64
	sm      SeriesMeta
	seasons []int

	list   listModel
	busy   busy
	loaded bool
}

func newSeasonScreen(m Meta, jumpTo int) *seasonScreen {
	l := newList()
	l.Empty = "no season data"
	return &seasonScreen{
		id: newAsyncID(), show: m, jumpTo: jumpTo,
		list: l, busy: newBusy("fetching episode list…"),
	}
}

func (s *seasonScreen) Init() tea.Cmd {
	id, m := s.id, s.show
	return tea.Batch(
		s.busy.start("fetching episode list…"),
		func() tea.Msg {
			sm := GetSeriesMeta(ctx.addons, m)
			set := map[int]bool{}
			for _, v := range sm.Videos {
				if v.Season > 0 {
					set[v.Season] = true
				}
			}
			seasons := make([]int, 0, len(set))
			for k := range set {
				seasons = append(seasons, k)
			}
			sort.Ints(seasons)
			return seriesMetaMsg{id: id, sm: sm, seasons: seasons}
		},
	)
}

func (s *seasonScreen) Title() string { return s.show.Name }
func (s *seasonScreen) Typing() bool  { return s.list.Typing() }

func (s *seasonScreen) SetSize(w, h int) {
	s.baseScreen.SetSize(w, h)
	s.list.SetSize(w, h)
}

func (s *seasonScreen) Footer() string {
	return keyHint(
		[2]string{"enter", "episodes"},
		[2]string{"f", "favourite"},
		[2]string{"w", "mark season"},
		[2]string{"b/esc", "back"},
	) + "   " + stHint.Render(s.list.Status())
}

func (s *seasonScreen) rebuild() {
	states := EpisodeStates(s.show.ID)
	items := make([]Item, len(s.seasons))
	for i, season := range s.seasons {
		total, done := 0, 0
		for _, v := range s.sm.Videos {
			if v.Season == season {
				total++
				if states[[2]int{season, v.Episode}].Watched {
					done++
				}
			}
		}
		badge := grey(fmt.Sprintf("%d/%d", done, total))
		if done == total && total > 0 {
			badge = good(fmt.Sprintf("%d/%d ✓", done, total))
		}
		items[i] = Item{
			Label:   bold(fmt.Sprintf("season %d", season)),
			Badge:   badge,
			Watched: total > 0 && done == total,
		}
	}
	s.list.SetItems(items)
}

func (s *seasonScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch m := msg.(type) {
	case seriesMetaMsg:
		if m.id != s.id {
			return s, nil
		}
		s.busy.stop()
		s.loaded = true
		s.sm, s.seasons = m.sm, m.seasons
		s.rebuild()
		if s.jumpTo > 0 && !s.jumped {
			s.jumped = true
			for i, season := range s.seasons {
				if season == s.jumpTo {
					s.list.Focus(i)
					es := newEpisodeScreen(s.show, s.sm, s.jumpTo)
					es.autoEpisode = s.autoEpisode
					es.autoResume = s.autoResume
					return s, push(es)
				}
			}
		}
		return s, nil

	case PlayerStateMsg:
		if s.loaded {
			cur := s.list.Selected()
			s.rebuild()
			if cur >= 0 {
				s.list.Focus(cur)
			}
		}
		return s, nil

	case tea.KeyMsg:
		if consumed, cmd := s.list.Update(msg); consumed {
			return s, cmd
		}
		switch m.String() {
		case "enter":
			if i := s.list.Selected(); i >= 0 {
				return s, push(newEpisodeScreen(s.show, s.sm, s.seasons[i]))
			}
		case "f":
			if i := s.list.Selected(); i >= 0 {
				AddFav(Favourite{
					Name: s.show.Name, ID: s.show.ID, Type: s.show.Type,
					Source: s.show.Source, Year: s.show.Year, Season: s.seasons[i],
				})
				return s, toast(fmt.Sprintf("favourited %s S%02d", s.show.Name, s.seasons[i]))
			}
		case "w", "W":
			if i := s.list.Selected(); i >= 0 {
				season := s.seasons[i]
				var eps []Video
				for _, v := range s.sm.Videos {
					if v.Season == season {
						eps = append(eps, v)
					}
				}
				states := EpisodeStates(s.show.ID)
				all := len(eps) > 0
				for _, v := range eps {
					if !states[[2]int{season, v.Episode}].Watched {
						all = false
						break
					}
				}
				SetSeasonWatched(s.show, season, eps, !all)
				s.rebuild()
				s.list.Focus(i)
				if all {
					return s, toast(fmt.Sprintf("season %d marked unwatched", season))
				}
				return s, toast(fmt.Sprintf("season %d marked watched", season))
			}
		case "esc", "backspace":
			return s, pop()
		case "q":
			return s, popRoot()
		}
	}
	return s, s.busy.update(msg)
}

func (s *seasonScreen) View() string {
	if !s.loaded {
		return s.busy.view()
	}
	return s.list.View()
}

// ── Episode list ──────────────────────────────────────────────────────────────

type episodeScreen struct {
	baseScreen
	id     asyncID
	show   Meta
	sm     SeriesMeta
	season int
	eps    []Video

	// Resume target, set when arriving from continue-watching.
	autoEpisode int
	autoResume  float64
	autoDone    bool

	list   listModel
	busy   busy
	loaded bool
}

func newEpisodeScreen(m Meta, sm SeriesMeta, season int) *episodeScreen {
	l := newList()
	l.Empty = "no episodes"
	return &episodeScreen{
		id: newAsyncID(), show: m, sm: sm, season: season,
		list: l, busy: newBusy("loading episodes…"),
	}
}

func (s *episodeScreen) Init() tea.Cmd {
	id, m, sm, season := s.id, s.show, s.sm, s.season
	return tea.Batch(
		s.busy.start("loading episodes…"),
		func() tea.Msg {
			return episodesMsg{id: id, eps: GetSeasonEpisodes(m, season, sm, ctx.cfg.OmdbKey)}
		},
	)
}

func (s *episodeScreen) Title() string { return fmt.Sprintf("season %d", s.season) }
func (s *episodeScreen) Typing() bool  { return s.list.Typing() }

func (s *episodeScreen) SetSize(w, h int) {
	s.baseScreen.SetSize(w, h)
	s.list.SetSize(w, h)
}

func (s *episodeScreen) Footer() string {
	return keyHint(
		[2]string{"enter", "streams"},
		[2]string{"w", "watched"},
		[2]string{"W", "whole season"},
		[2]string{"/", "filter"},
		[2]string{"b/esc", "back"},
	) + "   " + stHint.Render(s.list.Status())
}

func (s *episodeScreen) rebuild() {
	states := EpisodeStates(s.show.ID) // one read, not one per row
	live := ctx.player.State()

	items := make([]Item, len(s.eps))
	for i, v := range s.eps {
		st := states[[2]int{s.season, v.Episode}]
		it := epItem(v, st.Watched)

		pos, dur := st.Position, st.Duration

		// The on-disk position lags by up to a second and isn't written at all
		// until mpv reports a duration, so prefer live player state for the
		// episode actually on screen right now.
		playing := live.Alive && live.VideoID == v.ID
		if playing {
			pos, dur = live.Pos, live.Duration
		}

		if pos > 0 && dur > 0 && !it.Watched {
			glyph := "▶ " + fmtSecs(pos) + " / " + fmtSecs(dur)
			if playing {
				it.Badge = accent(glyph) + "  " + it.Badge
			} else {
				it.Badge = yell(glyph) + "  " + it.Badge
			}
		}
		items[i] = it
	}
	s.list.SetItems(items)
}

// firstUnwatched returns the index of the first episode not yet watched.
func (s *episodeScreen) firstUnwatched() int {
	states := EpisodeStates(s.show.ID)
	for i, v := range s.eps {
		if !states[[2]int{s.season, v.Episode}].Watched {
			return i
		}
	}
	return 0
}

func (s *episodeScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch m := msg.(type) {
	case episodesMsg:
		if m.id != s.id {
			return s, nil
		}
		s.busy.stop()
		s.loaded = true
		s.eps = m.eps
		s.rebuild()

		// Coming from continue-watching: focus and open the episode we left
		// off on. Doing it here rather than in the menu means the season and
		// episode screens end up on the stack underneath, so backing out of
		// the stream picker lands on the episode list like it used to.
		if s.autoEpisode > 0 && !s.autoDone {
			s.autoDone = true
			for i, v := range s.eps {
				if v.Episode == s.autoEpisode {
					s.list.Focus(i)
					return s, push(s.streamFor(i, s.autoResume))
				}
			}
		}

		s.list.Focus(s.firstUnwatched())
		return s, nil

	case PlayerStateMsg:
		if s.loaded {
			cur := s.list.Selected()
			s.rebuild()
			if cur >= 0 {
				s.list.Focus(cur)
			}
		}
		return s, nil

	case tea.KeyMsg:
		if consumed, cmd := s.list.Update(msg); consumed {
			return s, cmd
		}
		switch m.String() {
		case "enter":
			i := s.list.Selected()
			if i < 0 {
				return s, nil
			}
			pos, _, _ := GetPositionByEpisode(s.show.ID, s.season, s.eps[i].Episode)
			return s, push(s.streamFor(i, pos))
		case "w":
			if i := s.list.Selected(); i >= 0 {
				v := s.eps[i]
				now := ToggleWatchedByEpisode(HistoryEntry{
					Name: s.show.Name, ID: s.show.ID, Type: "series",
					Source: s.show.Source, Year: s.show.Year,
					Season: s.season, Episode: v.Episode,
					VideoID: v.ID, EpTitle: v.Title,
				})
				s.rebuild()
				s.list.Focus(i)
				if now {
					return s, toast("marked " + fmtVideoID(v.ID) + " watched")
				}
				return s, toast("marked " + fmtVideoID(v.ID) + " unwatched")
			}
		case "W":
			// Whole season, in one write rather than one per episode.
			allWatched := true
			states := EpisodeStates(s.show.ID)
			for _, v := range s.eps {
				if !states[[2]int{s.season, v.Episode}].Watched {
					allWatched = false
					break
				}
			}
			SetSeasonWatched(s.show, s.season, s.eps, !allWatched)
			cur := s.list.Selected()
			s.rebuild()
			if cur >= 0 {
				s.list.Focus(cur)
			}
			if allWatched {
				return s, toast(fmt.Sprintf("season %d marked unwatched", s.season))
			}
			return s, toast(fmt.Sprintf("season %d marked watched", s.season))
		case "esc", "backspace":
			return s, pop()
		case "q":
			return s, popRoot()
		}
	}
	return s, s.busy.update(msg)
}

// streamFor builds the stream picker for episode index i.
func (s *episodeScreen) streamFor(i int, resume float64) screen {
	v := s.eps[i]
	return newStreamScreen(streamTarget{
		Meta:      s.show,
		MediaType: "series",
		VideoID:   v.ID,
		Label:     s.show.Name + " · " + fmtVideoID(v.ID),
		Resume:    resume,
		Queue:     &EpQueue{Show: s.show, Season: s.season, Episodes: s.eps, Index: i},
	})
}

func (s *episodeScreen) View() string {
	if !s.loaded {
		return s.busy.view()
	}
	return s.list.View()
}

package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type streamsMsg struct {
	id      asyncID
	streams []Stream
}

// streamTarget is everything needed to fetch streams for one playable thing.
type streamTarget struct {
	Meta      Meta
	MediaType string
	VideoID   string
	Label     string
	Resume    float64
	Queue     *EpQueue // nil for movies
}

type streamScreen struct {
	baseScreen
	id     asyncID
	target streamTarget

	streams []Stream
	shown   []int // indices into streams after the provider filter

	providers []string // "all" plus one entry per addon that returned results
	provIdx   int

	list    listModel
	busy    busy
	loaded  bool
	reverse bool
}

func newStreamScreen(t streamTarget) *streamScreen {
	l := newList()
	l.Numbered = true
	l.Empty = "no playable streams — check your stream addons"
	return &streamScreen{
		id: newAsyncID(), target: t, list: l,
		busy: newBusy("fetching streams…"),
	}
}

func (s *streamScreen) Init() tea.Cmd { return s.load() }

// reload drops the cached stream list first — used when a link turns out to be
// dead, which for debrid results usually means the URL simply expired.
func (s *streamScreen) reload() tea.Cmd {
	InvalidateStreams(s.target.VideoID)
	return s.load()
}

func (s *streamScreen) load() tea.Cmd {
	s.id = newAsyncID()
	s.loaded = false
	id := s.id
	mt, vid := s.target.MediaType, s.target.VideoID
	addons := ctx.StreamAddons()
	return tea.Batch(
		s.busy.start("fetching streams…"),
		func() tea.Msg {
			return streamsMsg{id: id, streams: GetStreams(addons, mt, vid)}
		},
	)
}

func (s *streamScreen) Title() string {
	if s.target.MediaType == "series" {
		return fmtVideoID(s.target.VideoID)
	}
	return "streams"
}

func (s *streamScreen) Typing() bool { return s.list.Typing() }

func (s *streamScreen) SetSize(w, h int) {
	changed := w != s.w
	s.baseScreen.SetSize(w, h)
	s.list.SetSize(w, h-2) // header + provider bar

	// Stream labels are truncated to the width they were built at, so a
	// resize has to rebuild them or they stay clipped to the old width.
	if changed && s.loaded {
		cur := s.list.Selected()
		s.rebuild()
		if cur >= 0 {
			s.list.Focus(cur)
		}
	}
}

func (s *streamScreen) Footer() string {
	pairs := [][2]string{
		{"enter", "play"},
		{"0-9", "jump"},
		{"D", "download"},
		{"/", "filter"},
		{"r", "reverse"},
		{"R", "refetch"},
	}
	if len(s.providers) > 2 {
		pairs = append(pairs, [2]string{"tab", "provider"})
	}
	if s.target.Queue.HasPrev() {
		pairs = append(pairs, [2]string{"[", "prev ep"})
	}
	if s.target.Queue.HasNext() {
		pairs = append(pairs, [2]string{"]", "next ep"})
	}
	pairs = append(pairs, [2]string{"b/esc", "back"})
	out := keyHint(pairs...)

	if n := s.list.NumBuf(); n != "" {
		out += "   " + stKey.Render("#"+n)
	}
	return out + "   " + stHint.Render(s.list.Status())
}

// providerFilter is the addon name currently selected, "" meaning all.
func (s *streamScreen) providerFilter() string {
	if s.provIdx <= 0 || s.provIdx >= len(s.providers) {
		return ""
	}
	return s.providers[s.provIdx]
}

func (s *streamScreen) rebuild() {
	want := s.providerFilter()

	s.shown = s.shown[:0]
	var items []Item
	for i, st := range s.streams {
		if want != "" && st.Addon != want {
			continue
		}
		s.shown = append(s.shown, i)
		items = append(items, Item{Label: FmtStream(st, s.w)})
	}
	s.list.SetItems(items)
}

// collectProviders builds the filter bar from whichever addons replied.
func (s *streamScreen) collectProviders() {
	s.providers = []string{"all"}
	seen := map[string]bool{}
	for _, st := range s.streams {
		if st.Addon == "" || seen[st.Addon] {
			continue
		}
		seen[st.Addon] = true
		s.providers = append(s.providers, st.Addon)
	}
	if s.provIdx >= len(s.providers) {
		s.provIdx = 0
	}
}

func (s *streamScreen) providerBar() string {
	if len(s.providers) <= 2 {
		return "" // one provider, nothing to switch between
	}
	var parts []string
	for i, p := range s.providers {
		n := 0
		if i == 0 {
			n = len(s.streams)
		} else {
			for _, st := range s.streams {
				if st.Addon == p {
					n++
				}
			}
		}
		label := fmt.Sprintf("%s %d", p, n)
		if i == s.provIdx {
			parts = append(parts, stCursor.Render("["+label+"]"))
		} else {
			parts = append(parts, stHint.Render(" "+label+" "))
		}
	}
	return "  " + strings.Join(parts, stHint.Render("·")) + "   " + stHint.Render("tab")
}

// hop moves to an adjacent episode without unwinding the nav stack.
func (s *streamScreen) hop(delta int) tea.Cmd {
	q := s.target.Queue
	if q == nil {
		return nil
	}
	i := q.Index + delta
	if i < 0 || i >= len(q.Episodes) {
		return nil
	}
	nq := *q
	nq.Index = i
	v := nq.Episodes[i]
	pos, _, _ := GetPositionByEpisode(nq.Show.ID, nq.Season, v.Episode)

	s.target = streamTarget{
		Meta:      nq.Show,
		MediaType: "series",
		VideoID:   v.ID,
		Label:     nq.Show.Name + " · " + fmtVideoID(v.ID),
		Resume:    pos,
		Queue:     &nq,
	}
	s.streams = nil
	s.list.SetItems(nil)
	return s.load()
}

// launch starts playback at an explicit position.
func (s *streamScreen) launch(st Stream, resume float64) tea.Cmd {
	t := s.target

	entry := HistoryEntry{
		Name:   t.Meta.Name,
		ID:     t.Meta.ID,
		Type:   t.Meta.Type,
		Source: t.Meta.Source,
		Year:   t.Meta.Year,
		VideoID: t.VideoID,
	}
	if t.Queue != nil {
		v := t.Queue.Episodes[t.Queue.Index]
		entry.Season, entry.Episode, entry.EpTitle = t.Queue.Season, v.Episode, v.Title
		entry.EpisodeTotal = len(t.Queue.Episodes)

		if n, season, ok := t.Queue.Next(); ok {
			entry.NextVideoID = n.ID
			entry.NextSeason = season
			entry.NextEpisode = n.Episode
			entry.NextTitle = n.Title
		}
	}

	return ctx.player.Play(PlayRequest{
		VideoID:   t.VideoID,
		MediaType: t.MediaType,
		Label:     t.Label,
		URL:       st.URL,
		Resume:    resume,
		Entry:     entry,
		Queue:     t.Queue,
		Addons:    ctx.StreamAddons(),
	})
}

// play asks before seeking. Silently jumping into the middle of something is
// the wrong default — often enough you want to start it over.
func (s *streamScreen) play(st Stream) tea.Cmd {
	resume := s.target.Resume
	if !ctx.cfg.AutoResume || resume <= 5 {
		return tea.Batch(s.launch(st, 0), toast("loading "+s.target.Label+"…"))
	}

	return push(newChoice(
		"resume",
		"resume "+s.target.Label+" from "+fmtSecs(resume)+"?",
		"resume", "start over",
		func() tea.Cmd {
			return tea.Batch(s.launch(st, resume), toast("resuming at "+fmtSecs(resume)))
		},
		func() tea.Cmd {
			return tea.Batch(s.launch(st, 0), toast("starting over"))
		},
	))
}

// download queues the selected stream to disk.
func (s *streamScreen) download(st Stream) tea.Cmd {
	if ctx.cfg.DownloadDir == "" {
		return toastErr("set a download location in settings first")
	}
	if st.URL == "" {
		return toastErr("that stream has no url")
	}

	path := DownloadPath(ctx.cfg.DownloadDir, s.target, st.URL)
	msg, ok := ctx.downloader.Add(s.target.Label, st.URL, path)
	if !ok {
		return toastErr(msg)
	}
	return toast(msg)
}

// nextEpisodeTarget advances a finished request's queue by one, continuing
// into the next season when the current one is done.
func nextEpisodeTarget(prev PlayRequest) (streamTarget, bool) {
	v, season, ok := prev.Queue.Next()
	if !ok {
		return streamTarget{}, false
	}

	q := *prev.Queue
	if season != q.Season {
		// Rebuild the queue around the new season so autoplay keeps working
		// past the boundary rather than stopping after one episode.
		q.Season = season
		q.Episodes = q.SeasonEpisodes(season)
		q.Index = 0
		for i, e := range q.Episodes {
			if e.ID == v.ID {
				q.Index = i
				break
			}
		}
	} else {
		q.Index++
	}

	return streamTarget{
		Meta:      q.Show,
		MediaType: "series",
		VideoID:   v.ID,
		Label:     q.Show.Name + " · " + fmtVideoID(v.ID),
		Queue:     &q,
	}, true
}

func (s *streamScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch m := msg.(type) {
	case streamsMsg:
		if m.id != s.id {
			return s, nil
		}
		s.busy.stop()
		s.loaded = true

		var playable []Stream
		for _, st := range m.streams {
			if st.URL != "" {
				playable = append(playable, st)
			}
		}
		s.streams = SortStreams(playable, ctx.cfg.PreferredQuality, ctx.cfg.CachedFirst)
		s.collectProviders()
		s.rebuild()
		return s, nil

	case tea.KeyMsg:
		if consumed, cmd := s.list.Update(msg); consumed {
			return s, cmd
		}
		switch m.String() {
		case "enter":
			if i := s.list.Selected(); i >= 0 && i < len(s.shown) {
				s.list.ClearNum()
				return s, s.play(s.streams[s.shown[i]])
			}
		case "tab":
			if len(s.providers) > 1 {
				s.provIdx = (s.provIdx + 1) % len(s.providers)
				s.rebuild()
			}
		case "shift+tab":
			if len(s.providers) > 1 {
				s.provIdx = (s.provIdx - 1 + len(s.providers)) % len(s.providers)
				s.rebuild()
			}
		case "D":
			// Shifted deliberately: it writes to disk, and it sits right
			// beside the keys you're mashing to pick a stream.
			if i := s.list.Selected(); i >= 0 && i < len(s.shown) {
				return s, s.download(s.streams[s.shown[i]])
			}
		case "R":
			return s, tea.Batch(s.reload(), toast("refetching streams…"))
		case "r":
			for i, j := 0, len(s.streams)-1; i < j; i, j = i+1, j-1 {
				s.streams[i], s.streams[j] = s.streams[j], s.streams[i]
			}
			s.reverse = !s.reverse
			s.rebuild()
		case "[":
			return s, s.hop(-1)
		case "]":
			return s, s.hop(1)
		case "esc", "backspace":
			return s, pop()
		case "q":
			return s, popRoot()
		}
	}
	return s, s.busy.update(msg)
}

func (s *streamScreen) View() string {
	if !s.loaded {
		return s.busy.view()
	}

	sub := s.target.Label
	if q := s.target.Queue; q != nil {
		sub += fmt.Sprintf("  ·  %d/%d", q.Index+1, len(q.Episodes))
	}
	if s.target.Resume > 5 && ctx.cfg.AutoResume {
		sub += "  ·  " + yell("watched to "+fmtSecs(s.target.Resume))
	}
	out := "  " + stSub.Render(sub) + "\n"
	if bar := s.providerBar(); bar != "" {
		out += bar + "\n"
	}
	return out + s.list.View()
}

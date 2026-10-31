package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"stremio-cliuwu/internal/download"
	"stremio-cliuwu/internal/player"
	"stremio-cliuwu/internal/poster"
	"stremio-cliuwu/internal/store"
	"stremio-cliuwu/internal/stremio"
)

type streamsMsg struct {
	id      poster.AsyncID
	streams []stremio.Stream
	failed  int // addons that failed to answer
}

type streamsRetryMsg struct{ id poster.AsyncID }

// streamRetryDelays are the waits before each retry of an empty stream list.
var streamRetryDelays = []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}

type streamScreen struct {
	baseScreen
	id     poster.AsyncID
	target store.StreamTarget

	streams []stremio.Stream
	shown   []int // indices into streams after the provider filter

	providers []string // "all" plus one entry per addon that returned results
	provIdx   int

	// A filter handed over from the previous episode, applied once results
	// arrive. Autoplay builds a fresh screen, which would otherwise drop
	// whatever you'd narrowed the list down to.
	filter string

	// The addon tab picked, by name. Only tab changes it.
	chosen string

	// Hidden by the block list, and whether you've asked to see them anyway.
	blocked     int
	showBlocked bool

	list    listModel
	busy    busy
	loaded  bool
	reverse bool

	tries    int  // retries used on the current load
	retrying bool // waiting on a retry; back stops it

	upNext string // URL of the stream marked as up next, if it's in this list
}

// newStreamScreenFiltered starts with a filter already applied.
//
// Autoplay builds a fresh screen for the next episode, which dropped whatever
// you were filtering by — so having narrowed forty results down to one release
// group, you'd get all forty again on the next episode.
func newStreamScreenFiltered(t store.StreamTarget, filter, chosen string) *streamScreen {
	s := newStreamScreen(t)
	s.filter = filter
	s.chosen = chosen
	return s
}

func newStreamScreen(t store.StreamTarget) *streamScreen {
	l := newList()
	l.Numbered = true
	l.Empty = "no playable streams — check your stream addons"
	return &streamScreen{
		id: poster.NewAsyncID(), target: t, list: l,
		busy: newBusy("fetching streams…"),
	}
}

func (s *streamScreen) Init() tea.Cmd { return s.load() }

// reload drops the cached stream list first — used when a link turns out to be
// dead, which for debrid results usually means the URL simply expired.
func (s *streamScreen) reload() tea.Cmd {
	stremio.InvalidateStreams(s.target.FetchID())
	return s.load()
}

func (s *streamScreen) load() tea.Cmd {
	s.tries, s.retrying = 0, false
	return tea.Batch(s.busy.start("fetching streams…"), s.fetch(stremio.GetStreams))
}

func (s *streamScreen) fetch(get func([]stremio.Addon, string, string) ([]stremio.Stream, int)) tea.Cmd {
	s.id = poster.NewAsyncID()
	s.loaded = false
	id := s.id
	mt, vid := s.target.MediaType, s.target.FetchID()
	addons := ctx.StreamAddons()
	return func() tea.Msg {
		streams, failed := get(addons, mt, vid)
		return streamsMsg{id: id, streams: streams, failed: failed}
	}
}

// resume restarts a load whose messages went to a screen pushed on top.
func (s *streamScreen) resume() tea.Cmd {
	if s.loaded {
		return nil
	}
	get := stremio.GetStreams
	if s.retrying {
		get = stremio.RetryStreams
	}
	return tea.Batch(s.busy.sp.Tick, s.fetch(get))
}

// queuedURL is the up-next stream's URL when it belongs to this episode.
func (s *streamScreen) queuedURL() string {
	if q := ctx.player.Queued(); q != nil && q.VideoID == s.target.VideoID {
		return q.URL
	}
	return ""
}

// HandleBack stops a pending retry instead of leaving the screen.
func (s *streamScreen) HandleBack() bool {
	if !s.retrying {
		return false
	}
	s.id = poster.NewAsyncID() // drops the retry in flight
	s.Update(streamsMsg{id: s.id})
	return true
}

func (s *streamScreen) Title() string {
	if s.target.MediaType != "series" {
		return "streams"
	}
	// The queue has the episode's real numbers; the id may not.
	if q := s.target.Queue; q != nil && q.Index < len(q.Episodes) {
		v := q.Episodes[q.Index]
		return stremio.FmtEp(v.Season, v.Episode, s.target.VideoID)
	}
	return stremio.FmtVideoID(s.target.VideoID)
}

func (s *streamScreen) Typing() bool { return s.list.Typing() }

func (s *streamScreen) SetSize(w, h int) {
	s.baseScreen.SetSize(w, h)
	s.list.SetSize(w, h-2) // header + provider bar
}

func (s *streamScreen) Footer() string {
	if s.retrying {
		return keyHint([2]string{"R", "refetch streams"}, [2]string{"b/esc", "stop retrying"})
	}
	// Ordered for the footer, which keeps only the first few: the ones worth
	// a permanent reminder come first, and everything else is a ? away.
	pairs := [][2]string{
		{"enter", "play"},
		{"/", "filter"},
		{"D", "download"},
		{"0-9", "jump to row"},
	}
	if ctx.player.State().Alive {
		// Only meaningful while something's playing — otherwise there's
		// nothing for it to follow.
		pairs = append(pairs, [2]string{"n", "queue as next"})
	}
	if s.blocked > 0 || s.showBlocked {
		pairs = append(pairs, [2]string{"B", "show blocked results"})
	}
	pairs = append(pairs,
		[2]string{"w", "mark watched"},
		[2]string{"r", "reverse order"},
		[2]string{"R", "refetch streams"},
	)
	if len(s.providers) > 2 {
		pairs = append(pairs, [2]string{"tab", "filter by addon"})
	}
	if s.target.Queue.HasPrev() {
		pairs = append(pairs, [2]string{"[", "previous episode"})
	}
	if s.target.Queue.HasNext() {
		pairs = append(pairs, [2]string{"]", "next episode"})
	}
	if s.target.Queue != nil {
		state := "off"
		if ctx.cfg.Autoplay {
			state = "on"
		}
		pairs = append(pairs, [2]string{"a", "autoplay " + state})
	}
	pairs = append(pairs, [2]string{"b/esc", "back"})
	out := keyHint(pairs...)

	if n := s.list.NumBuf(); n != "" {
		out += "   " + stKey.Render("#"+n)
	}
	return withStatus(s.list.Status(), out)
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
	terms := ctx.cfg.Blocked
	s.upNext = s.queuedURL()

	s.shown = s.shown[:0]
	s.blocked = 0

	var items []Item
	for i, st := range s.streams {
		if want != "" && st.Addon != want {
			continue
		}

		// Counted rather than silently dropped. A picker that quietly shows
		// twelve of forty results is one you stop trusting.
		if !s.showBlocked && stremio.StreamBlocked(st, terms) {
			s.blocked++
			continue
		}

		s.shown = append(s.shown, i)
		it := Item{Label: FmtStream(st)}
		if s.upNext != "" && st.URL == s.upNext {
			it.Badge = stKey.Render("▶ up next")
		}
		items = append(items, it)
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

	// By name, since tab order changes; "all" if that addon returned nothing.
	s.provIdx = 0
	for i, p := range s.providers {
		if i > 0 && p == s.chosen {
			s.provIdx = i
			break
		}
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
	out := "  " + strings.Join(parts, stHint.Render("·")) + "   " + stHint.Render("tab")

	// Says so rather than just showing fewer rows, so a short list is never
	// mistaken for a bad search.
	switch {
	case s.showBlocked:
		out += "   " + stWarn.Render("blocked shown")
	case s.blocked > 0:
		out += "   " + stHint.Render(fmt.Sprintf("%d blocked", s.blocked))
	}
	return out
}

// hop moves to an adjacent episode without unwinding the nav stack.
func (s *streamScreen) hop(delta int) tea.Cmd {
	t, ok := stepTarget(s.target.Queue, delta)
	if !ok {
		return nil
	}
	s.target = t
	s.streams = nil
	s.list.SetItems(nil)
	return s.load()
}

// request builds the play request for a stream. Shared by launch and queue so
// the two can't drift — a queued episode has to record exactly the same
// history and episode context as one played directly.
func (s *streamScreen) request(st stremio.Stream, resume float64) player.PlayRequest {
	req := playRequest(s.target, st, resume)
	req.Filter, req.AddonTab = s.list.Query(), s.providerFilter()
	return req
}

// playRequest is what playing st for t sends to the player. enter, n and
// autoplay all build their requests here.
func playRequest(t store.StreamTarget, st stremio.Stream, resume float64) player.PlayRequest {
	entry := store.HistoryEntry{
		Name:    t.Meta.Name,
		ID:      t.Meta.ID,
		Type:    t.Meta.Type,
		Source:  t.Meta.Source,
		Year:    t.Meta.Year,
		VideoID: t.VideoID,
	}
	if t.Queue != nil {
		v := t.Queue.Episodes[t.Queue.Index]
		entry.Season, entry.Episode, entry.EpTitle = t.Queue.Season, v.Episode, v.Title
		entry.EpisodeTotal = len(t.Queue.Episodes)

		// Upcoming rather than Next: an episode that hasn't aired still needs
		// recording, or catching up with a season looks like finishing it.
		if n, season, ok := t.Queue.Upcoming(); ok {
			entry.NextVideoID = n.ID
			entry.NextSeason = season
			entry.NextEpisode = n.Episode
			entry.NextTitle = n.Title
			entry.NextReleased = n.Released
		}
	}

	return player.PlayRequest{
		VideoID:   t.VideoID,
		FetchID:   t.FetchID(),
		MediaType: t.MediaType,
		Label:     t.Label,
		URL:       st.URL,
		Resume:    resume,
		Entry:     entry,
		Queue:     t.Queue,
		Addons:    ctx.StreamAddons(),

		VideoHash: st.BehaviorHints.VideoHash,
		VideoSize: st.BehaviorHints.VideoSize,
		Filename:  st.BehaviorHints.Filename,
		Subs:      st.Subtitles,

		BingeGroup: st.BehaviorHints.BingeGroup,
		Release:    stremio.ReleaseName(st),
		Langs:      stremio.FlagLangs(st.Name + "\n" + st.Title + "\n" + st.Description),
		Addon:      st.Addon,
		Size:       stremio.StreamBytes(st),
	}
}

// launch starts playback at an explicit position.
func (s *streamScreen) launch(st stremio.Stream, resume float64) tea.Cmd {
	return ctx.player.Play(s.request(st, resume))
}

// queue lines a stream up to follow whatever is playing. Pressing n again on
// the same stream takes it back off.
func (s *streamScreen) queue(st stremio.Stream) tea.Cmd {
	if !ctx.player.State().Alive {
		return toastErr("nothing is playing to queue behind")
	}
	if st.URL == "" {
		return toastErr("that stream has no url")
	}

	req := s.request(st, 0)
	if q := ctx.player.Queued(); q != nil && q.VideoID == req.VideoID && q.URL == req.URL {
		ctx.player.Unqueue()
		return tea.Batch(toast("removed from up next"), playerStateCmd())
	}

	if ctx.player.Queue(req) {
		return tea.Batch(toast("playing "+req.Label), playerStateCmd())
	}
	return tea.Batch(toast("up next — "+req.Label), playerStateCmd())
}

// savedResume is where history says to resume from, so stopping while the
// list was open is picked up. Nothing in history keeps what the list had.
func (s *streamScreen) savedResume() float64 {
	pos, dur, _ := store.GetPosition(s.target.VideoID)
	if dur <= 0 {
		return s.target.Resume
	}
	if store.HasResumePoint(pos, dur) {
		return pos
	}
	return 0
}

// play asks before seeking. Silently jumping into the middle of something is
// the wrong default — often enough you want to start it over.
func (s *streamScreen) play(st stremio.Stream) tea.Cmd {
	s.target.Resume = s.savedResume()
	resume := s.target.Resume
	if !ctx.cfg.AutoResume || resume <= 5 {
		return tea.Batch(s.launch(st, 0), toast("loading "+s.target.Label+"…"))
	}

	return push(newChoice(
		"resume",
		"resume "+s.target.Label+" from "+download.FmtSecs(resume)+"?",
		"resume", "start over",
		func() tea.Cmd {
			return tea.Batch(s.launch(st, resume), toast("resuming at "+download.FmtSecs(resume)))
		},
		func() tea.Cmd {
			return tea.Batch(s.launch(st, 0), toast("starting over"))
		},
	))
}

// download queues the selected stream to disk.
func (s *streamScreen) download(st stremio.Stream) tea.Cmd {
	if ctx.cfg.DownloadDir == "" {
		return toastErr("set a download location in settings first")
	}
	if st.URL == "" {
		return toastErr("that stream has no url")
	}

	path := download.Path(ctx.cfg.DownloadDir, s.target, st.URL)
	msg, ok := ctx.downloader.Add(s.target.Label, st.URL, path)
	if !ok {
		return toastErr(msg)
	}
	return toast(msg)
}

// toggleWatched flips the watched flag for whatever this screen is showing.
func (s *streamScreen) toggleWatched() tea.Cmd {
	t := s.target

	e := store.HistoryEntry{
		Name: t.Meta.Name, ID: t.Meta.ID, Type: t.Meta.Type,
		Source: t.Meta.Source, Year: t.Meta.Year, VideoID: t.VideoID,
	}
	if t.Queue != nil {
		v := t.Queue.Episodes[t.Queue.Index]
		e.Season, e.Episode, e.EpTitle = t.Queue.Season, v.Episode, v.Title
	}

	if store.ToggleWatchedByEpisode(e) {
		return toast("marked " + t.Label + " watched")
	}
	return toast("marked " + t.Label + " unwatched")
}

// stepTarget is the stream target delta episodes along the season, for [
// and ].
func stepTarget(q *stremio.EpQueue, delta int) (store.StreamTarget, bool) {
	nq, ok := q.Step(delta)
	return queueTarget(nq, ok)
}

// nextTarget is the episode autoplay and "open next episode" move on to,
// which can be the next season's first.
func nextTarget(q *stremio.EpQueue) (store.StreamTarget, bool) {
	nq, ok := q.Next(ctx.cfg.NextSeason)
	return queueTarget(nq, ok)
}

func queueTarget(nq *stremio.EpQueue, ok bool) (store.StreamTarget, bool) {
	if !ok {
		return store.StreamTarget{}, false
	}
	v := nq.Episodes[nq.Index]
	pos, _, _ := store.GetPositionByEpisode(nq.Show.ID, nq.Season, v.Episode)
	return store.StreamTarget{
		Meta:      nq.Show,
		MediaType: "series",
		VideoID:   v.ID,
		StreamID:  v.StreamID(),
		Label:     nq.Show.Name + " · " + stremio.FmtEp(v.Season, v.Episode, v.ID),
		Resume:    pos,
		Queue:     nq,
	}, true
}

func (s *streamScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch m := msg.(type) {
	case streamsMsg:
		if m.id != s.id {
			return s, nil
		}

		var playable []stremio.Stream
		for _, st := range m.streams {
			if st.URL != "" {
				playable = append(playable, st)
			}
		}

		if len(playable) == 0 && m.failed > 0 && s.tries < len(streamRetryDelays) {
			s.tries++
			s.retrying = true
			s.busy.what = fmt.Sprintf("retrying %d/%d…", s.tries, len(streamRetryDelays))
			id := s.id
			return s, tea.Tick(streamRetryDelays[s.tries-1], func(time.Time) tea.Msg {
				return streamsRetryMsg{id: id}
			})
		}
		s.retrying = false
		s.busy.stop()
		s.loaded = true

		s.streams = stremio.SortStreams(playable, ctx.cfg.PreferredQuality, ctx.cfg.CachedFirst)
		s.collectProviders()
		s.rebuild()

		// Applied once the results are in, since there'd be nothing to
		// filter before that. Cleared afterwards so it isn't reapplied if
		// you refetch having deliberately cleared it.
		if s.filter != "" {
			s.list.SetQuery(s.filter)
			s.filter = ""
		}
		return s, nil

	case streamsRetryMsg:
		if m.id != s.id {
			return s, nil
		}
		return s, s.fetch(stremio.RetryStreams)

	case player.StateMsg:
		s.target.Resume = s.savedResume()
		// Rebuilt only when up next changes; this arrives every second.
		if s.loaded && s.queuedURL() != s.upNext {
			s.rebuild()
		}
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
				s.chosen = s.providerFilter()
				s.rebuild()
			}
		case "shift+tab":
			if len(s.providers) > 1 {
				s.provIdx = (s.provIdx - 1 + len(s.providers)) % len(s.providers)
				s.chosen = s.providerFilter()
				s.rebuild()
			}
		case "B":
			if s.blocked > 0 || s.showBlocked {
				s.showBlocked = !s.showBlocked
				s.rebuild()
				if s.showBlocked {
					return s, toast("showing blocked results")
				}
				return s, toast("hiding blocked results")
			}
		case "n":
			if i := s.list.Selected(); i >= 0 && i < len(s.shown) {
				return s, s.queue(s.streams[s.shown[i]])
			}
		case "a":
			if s.target.Queue != nil {
				return s, toggleAutoplay()
			}
		case "w":
			// Films had no way back from a wrong watched mark: w exists on
			// episode lists, and a film never appears in one.
			return s, s.toggleWatched()
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
			if s.HandleBack() {
				return s, nil
			}
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
		sub += "  ·  " + yell("watched to "+download.FmtSecs(s.target.Resume))
	}
	out := fitLine("  "+stSub.Render(sub), s.w) + "\n"
	if bar := s.providerBar(); bar != "" {
		out += fitLine(bar, s.w) + "\n"
	}
	return out + s.list.View()
}

package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"stremio-cliuwu/internal/player"
	"stremio-cliuwu/internal/store"
	"stremio-cliuwu/internal/stremio"
)

// autoPickMsg carries the next episode's streams for autoplay to choose from.
type autoPickMsg struct {
	prev    player.PlayRequest
	target  store.StreamTarget
	streams []stremio.Stream
}

// autoFetch loads the streams for the episode after prev, retrying like the
// stream list does when every addon failed. A patient fetch, made while the
// current episode still has a while to go, also retries failed addons when
// other streams came back: the one that failed may be the one the last
// episode played from.
func autoFetch(prev player.PlayRequest, patient bool) tea.Cmd {
	t, ok := stepTarget(prev.Queue, 1)
	if !ok {
		return nil
	}
	addons := ctx.StreamAddons()
	return func() tea.Msg {
		streams, failed := stremio.GetStreams(addons, t.MediaType, t.FetchID())
		for _, d := range streamRetryDelays {
			if failed == 0 || !patient && hasPlayable(streams) {
				break
			}
			time.Sleep(d)
			streams, failed = stremio.RetryStreams(addons, t.MediaType, t.FetchID())
		}
		return autoPickMsg{prev: prev, target: t, streams: streams}
	}
}

func hasPlayable(streams []stremio.Stream) bool {
	for _, st := range streams {
		if st.URL != "" {
			return true
		}
	}
	return false
}

// pickStream chooses the next episode's stream the way prev was chosen. The
// addon tab and filter narrow the list first; a tab or filter that matches
// nothing means no pick, so the list opens.
//
// The rest follows Sonarr: candidates are the streams sharing prev's
// bingeGroup, or with no group match the ones at least as cached as the top
// stream.
// They're compared field by field in order (variant, resolution, HDR, group,
// source and service, codec, audio) and the first difference decides; a tie
// keeps list order. A different variant, like audio description or a dub,
// is never autoplayed.
func pickStream(streams []stremio.Stream, prev player.PlayRequest, cfg store.Config) (stremio.Stream, bool) {
	var cands []stremio.Stream
	for _, st := range streams {
		if st.URL != "" && !stremio.StreamBlocked(st, cfg.Blocked) {
			cands = append(cands, st)
		}
	}
	cands = stremio.SortStreams(cands, cfg.PreferredQuality, cfg.CachedFirst)

	keep := func(match func(stremio.Stream) bool) {
		var out []stremio.Stream
		for _, st := range cands {
			if match(st) {
				out = append(out, st)
			}
		}
		cands = out
	}
	if prev.AddonTab != "" {
		keep(func(st stremio.Stream) bool { return st.Addon == prev.AddonTab })
	}
	if q := strings.ToLower(strings.TrimSpace(prev.Filter)); q != "" {
		keep(func(st stremio.Stream) bool {
			return strings.Contains(strings.ToLower(stripANSI(FmtStream(st))), q)
		})
	}
	if len(cands) == 0 {
		return stremio.Stream{}, false
	}

	var pool []stremio.Stream
	if g := prev.BingeGroup; g != "" {
		for _, st := range cands {
			if st.BehaviorHints.BingeGroup == g {
				pool = append(pool, st)
			}
		}
	}
	if len(pool) == 0 {
		top := stremio.StreamCached(cands[0])
		keep(func(st stremio.Stream) bool { return stremio.StreamCached(st) >= top })
		pool = cands
	}
	if prev.Release == "" {
		return pool[0], true
	}

	// Episode titles are part of release names; they mustn't read as tags.
	nextTitle := epTitle(prev.Queue, 1)
	parse := func(st stremio.Stream) stremio.ReleaseInfo {
		return stremio.ParseReleaseNameTitled(stremio.ReleaseName(st), nextTitle)
	}
	want := stremio.ParseReleaseNameTitled(prev.Release, epTitle(prev.Queue, 0))
	best, bestKey := pool[0], want.Matches(parse(pool[0]))
	for _, st := range pool[1:] {
		if k := want.Matches(parse(st)); better(k, bestKey) {
			best, bestKey = st, k
		}
	}
	if !bestKey[0] {
		return stremio.Stream{}, false // only a different variant is left
	}
	return best, true
}

// epTitle is the title of the episode delta away in q, or "".
func epTitle(q *stremio.EpQueue, delta int) string {
	if nq, ok := q.Step(delta); ok {
		return nq.Episodes[nq.Index].Title
	}
	return ""
}

// better compares two match lists in order; the first difference decides.
func better(a, b [8]bool) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i]
		}
	}
	return false
}

// autoPicked queues the pick, or falls back to opening the list.
func (a *app) autoPicked(m autoPickMsg) tea.Cmd {
	if !ctx.cfg.Autoplay {
		return nil // turned off while the streams loaded
	}
	st, ok := pickStream(m.streams, m.prev, *ctx.cfg)
	if !ok {
		return a.advanceTo(m.prev, "next up — ")
	}
	req := playRequest(m.target, st, 0)
	req.Filter, req.AddonTab = m.prev.Filter, m.prev.AddonTab

	queued, started := ctx.player.QueueAuto(req, m.prev.VideoID)
	if !queued {
		return nil // something else is lined up, or you cleared it
	}
	note := toast("next: " + m.target.Label + " (auto)")
	if started {
		note = toast("playing " + m.target.Label + " (auto)")
	}

	// Sitting on the list for the episode that's playing: show the next one's,
	// with the pick marked, as "open next episode" does.
	if cur, isStreams := a.top().(*streamScreen); isStreams && cur.target.VideoID == m.prev.VideoID {
		return tea.Sequence(note, playerStateCmd(),
			replaceTop(newStreamScreenFiltered(m.target, cur.list.Query(), cur.chosen)))
	}
	return tea.Batch(note, playerStateCmd())
}

// flipAutoplay toggles the setting; turning it off drops an auto pick.
func flipAutoplay() {
	ctx.cfg.Autoplay = !ctx.cfg.Autoplay
	if !ctx.cfg.Autoplay {
		ctx.player.ClearAuto()
	}
}

// toggleAutoplay flips the setting from the stream list.
func toggleAutoplay() tea.Cmd {
	flipAutoplay()
	store.SaveConfig(*ctx.cfg)
	ctx.player.SetConfig(*ctx.cfg)
	return tea.Batch(autoplayChanged, playerStateCmd())
}

// autoplayChangedMsg tells the app to update the footer tag.
type autoplayChangedMsg struct{}

type autoplayOffExpireMsg struct{ at time.Time }

func autoplayChanged() tea.Msg { return autoplayChangedMsg{} }

// autoOffShown is how long [AUTOPLAY OFF] stays up after turning it off.
const autoOffShown = 3 * time.Second

// autoplayTag is the footer's autoplay state: always while on, briefly after
// turning it off.
func (a *app) autoplayTag(now time.Time) string {
	switch {
	case ctx.cfg.Autoplay:
		return good("[AUTOPLAY ON]")
	case !a.autoOffAt.IsZero() && now.Sub(a.autoOffAt) < autoOffShown:
		return bad("[AUTOPLAY OFF]")
	}
	return ""
}

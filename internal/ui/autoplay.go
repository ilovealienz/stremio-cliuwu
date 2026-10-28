package ui

import (
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"stremio-cliuwu/internal/player"
	"stremio-cliuwu/internal/store"
	"stremio-cliuwu/internal/stremio"
)

// autoPickMsg carries autoplay's pick for the next episode, made off the UI
// loop: reading every release name takes a while on a long list.
type autoPickMsg struct {
	prev   player.PlayRequest
	target store.StreamTarget
	pick   stremio.Stream
	ok     bool
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
	cfg := *ctx.cfg
	return func() tea.Msg {
		streams, failed := stremio.GetStreams(addons, t.MediaType, t.FetchID())
		for _, d := range streamRetryDelays {
			if failed == 0 || !patient && hasPlayable(streams) {
				break
			}
			time.Sleep(d)
			streams, failed = stremio.RetryStreams(addons, t.MediaType, t.FetchID())
		}
		st, ok := pickStream(streams, prev, cfg)
		return autoPickMsg{prev: prev, target: t, pick: st, ok: ok}
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
// bingeGroup, then the ones at least as cached as the top stream.
// Releases that name another episode, or are a special or a batch when prev
// wasn't, are dropped. The rest are compared field by field in order
// (variant and language, the same show, the same title words, resolution,
// HDR, group, source and service, codec, audio) and the first difference
// decides. A tie goes to the same addon, then the most alike name, then the
// nearer size, then list order. A different variant, like audio description, a dub or another
// language, is never autoplayed.
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
	// Addons sometimes answer with other episodes: another week of a daily
	// show, or a mislabelled file. A name that says which episode it is
	// has to say this one.
	if nq, ok := prev.Queue.Step(1); ok {
		v := nq.Episodes[nq.Index]
		aired, _ := time.Parse(time.RFC3339, v.Released)
		extra := stremio.Extra(prev.Release)
		keep(func(st stremio.Stream) bool {
			name := stremio.ReleaseName(st)
			return !otherEpisode(name, v, aired) && (extra || !stremio.Extra(name))
		})
	}
	if len(cands) == 0 {
		return stremio.Stream{}, false
	}

	var group []stremio.Stream
	if g := prev.BingeGroup; g != "" {
		for _, st := range cands {
			if st.BehaviorHints.BingeGroup == g {
				group = append(group, st)
			}
		}
	}
	top := stremio.StreamCached(cands[0])
	keep(func(st stremio.Stream) bool { return stremio.StreamCached(st) >= top })
	if prev.Release == "" {
		if len(group) > 0 {
			return group[0], true
		}
		return cands[0], true
	}

	// Episode titles are part of release names; they mustn't read as tags.
	show, original := "", ""
	if prev.Queue != nil {
		show = prev.Queue.Show.Name
		if prev.Queue.Show.Source == "anime" {
			original = "jp" // Japanese audio is the original, tagged or not
		}
	}
	nextTitle := epTitle(prev.Queue, 1)
	parsed := map[string]stremio.ReleaseInfo{} // each stream is read once, by URL
	parse := func(st stremio.Stream) stremio.ReleaseInfo {
		if r, ok := parsed[st.URL]; ok {
			return r
		}
		r := stremio.ParseReleaseNameTitled(stremio.ReleaseName(st), show, nextTitle).
			WithLangs(stremio.FlagLangs(st.Name + "\n" + st.Title + "\n" + st.Description)).Original(original)
		parsed[st.URL] = r
		return r
	}
	want := stremio.ParseReleaseNameTitled(prev.Release, show, epTitle(prev.Queue, 0)).WithLangs(prev.Langs).Original(original)
	shape := stremio.NameShape(prev.Release, show, epTitle(prev.Queue, 0))
	alike := map[string]float64{}
	likeness := func(st stremio.Stream) float64 {
		if l, ok := alike[st.URL]; ok {
			return l
		}
		l := stremio.ShapeLikeness(shape, stremio.NameShape(stremio.ReleaseName(st), show, nextTitle))
		alike[st.URL] = l
		return l
	}
	// The show as the last release named it, or as the metadata does: Torrentio
	// mixes in other shows' releases from the same group.
	title, showTitle := stremio.TitleWords(prev.Release, epTitle(prev.Queue, 0)), stremio.TitleWords(show)
	keys := map[string][10]bool{}
	key := func(st stremio.Stream) [10]bool {
		if k, ok := keys[st.URL]; ok {
			return k
		}
		m := want.Matches(parse(st))
		t := stremio.TitleWords(stremio.ReleaseName(st), nextTitle)
		same := stremio.SameTitle(title, t) || stremio.SameTitle(showTitle, t)
		k := [10]bool{m[0], same, stremio.EqualTitle(title, t), m[1], m[2], m[3], m[4], m[5], m[6], m[7]}
		keys[st.URL] = k
		return k
	}
	pick := func(pool []stremio.Stream) (stremio.Stream, bool) {
		best, bestKey := pool[0], key(pool[0])
		for _, st := range pool[1:] {
			if k := key(st); better(k, bestKey) || k == bestKey && closer(st, best, prev, likeness) {
				best, bestKey = st, k
			}
		}
		return best, bestKey[0] // false: only a different variant is left
	}

	// The same binge group first. If that only has a different variant, the
	// rest of the list may still have the right one.
	if len(group) > 0 {
		if st, ok := pick(group); ok {
			return st, true
		}
	}
	return pick(cands)
}

// epTitle is the title of the episode delta away in q, or "".
func epTitle(q *stremio.EpQueue, delta int) string {
	if nq, ok := q.Step(delta); ok {
		return nq.Episodes[nq.Index].Title
	}
	return ""
}

// closer breaks a tie between two streams that match prev equally well: the
// same addon as last time, as Stremio only binge-plays within one, then the
// name shaped most like the last one, then the nearer size. Otherwise list
// order stands.
func closer(a, b stremio.Stream, prev player.PlayRequest, likeness func(stremio.Stream) float64) bool {
	if prev.Addon != "" && (a.Addon == prev.Addon) != (b.Addon == prev.Addon) {
		return a.Addon == prev.Addon
	}
	if la, lb := likeness(a), likeness(b); la != lb {
		return la > lb
	}
	return sizeStep(stremio.StreamBytes(a), prev.Size) < sizeStep(stremio.StreamBytes(b), prev.Size)
}

// sizeStep is how many 1.5x steps (AIOStreams' size buckets) n is from
// want, so small differences don't override list order. Unknown sizes come
// last; with nothing to compare to, it's a tie.
func sizeStep(n, want int64) int {
	if want <= 0 {
		return 0
	}
	if n <= 0 {
		return math.MaxInt
	}
	return int(math.Abs(math.Round(math.Log(float64(n)/float64(want)) / math.Log(1.5))))
}

// otherEpisode reports whether a release name says it's a different episode
// from v: another number in the same season, or a date days off its airing.
func otherEpisode(name string, v stremio.Video, aired time.Time) bool {
	if s, first, last, ok := stremio.NameEpisode(name); ok && s == v.Season && (v.Episode < first || v.Episode > last) {
		return true
	}
	if d, ok := stremio.NameDate(name); ok && !aired.IsZero() {
		return d.Sub(aired).Abs() > 3*24*time.Hour
	}
	return false
}

// better compares two match lists in order; the first difference decides.
func better(a, b [10]bool) bool {
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
	if !m.ok {
		return a.advanceTo(m.prev, "next up — ")
	}
	req := playRequest(m.target, m.pick, 0)
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

package main

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// today renders the current date in whatever format is configured, doubling as
// the stats section heading — it's the one thing on this screen that isn't
// derived from your library.
func today() string {
	kind := "dmy"
	if ctx != nil {
		kind = ctx.cfg.DateFormat
	}
	return time.Now().Format(dateLayout(kind))
}

// The main menu is eleven fixed rows on a terminal that's usually far wider
// than it needs. This puts the shows you're partway through in that space.
//
// Entries are numbered and opened with 1-9 rather than by moving a cursor
// into the panel: a focus model — tab to switch sides, arrows meaning
// different things depending on which half is active — is a lot of machinery
// for one screen, and a number is fewer keystrokes anyway.

const (
	continuePanelMax = 8

	// episodeBlockMax caps the synopsis so a wordy one can't push the lists
	// below it off the panel.
	episodeBlockMax = 11
)

type continuePanel struct {
	on     bool
	manual bool
	w, h   int

	items     []ContinueItem
	downloads []Download
	watched   []HistoryEntry
	next      *ContinueItem // what w resumes
	epVideo   Video      // episode record, for a series
	epMovie   MetaDetail // meta, for a film
	epKey     string     // which target the two above belong to
	stats     Stats

	// actions is the flat numbered list across all sections, so 1-9 means one
	// thing regardless of which section a row sits in.
	actions []panelAction
}

// panelAction is a numbered row and what pressing its number does.
type panelAction struct {
	open func() screen
}

func newContinuePanel() continuePanel { return continuePanel{} }

func (p *continuePanel) On() bool { return p.on }

func (p *continuePanel) Toggle() {
	p.on = !p.on
	p.manual = true
}

// AutoFit opens the panel when there's room, unless the user has said
// otherwise on this screen.
func (p *continuePanel) AutoFit(total int) {
	if p.manual || ctx == nil || !ctx.cfg.AutoInfo {
		return
	}
	p.on = total >= autoInfoWidth && PaneWidth(total) > 0
}

func (p *continuePanel) Split(total int) bool { return PaneWidth(total) > 0 }

func (p *continuePanel) SetSize(w, h int) {
	p.w = max(0, w-paneRightPad)
	p.h = h
}

// Refresh reloads from history. Cheap — it's a map walk, not a file read.
func (p *continuePanel) Refresh() {
	if !p.on {
		p.items, p.downloads, p.watched = nil, nil, nil
		return
	}

	p.items = ContinueList(continuePanelMax)

	// Everything except cleared entries: a finished download is exactly what
	// you came to the panel to check.
	p.downloads = ctx.downloader.Snapshot()
	p.watched = RecentlyWatched(continuePanelMax)
	p.next = ContinueTarget()
	p.stats = HistoryStats()
}

// Count is how many entries are numbered, for the key hint.
func (p *continuePanel) Count() int { return len(p.actions) }

// At returns what a 1-based number opens, if anything.
func (p *continuePanel) At(n int) (screen, bool) {
	if n < 1 || n > len(p.actions) {
		return nil, false
	}
	return p.actions[n-1].open(), true
}

// SetTargetInfo takes whatever the fetch found for the continue target.
func (p *continuePanel) SetTargetInfo(m targetInfoMsg) {
	p.epVideo, p.epMovie, p.epKey = m.Video, m.Detail, m.Key
}

// targetKey identifies a continue target.
//
// Used instead of comparing the fetched episode's id against the entry's:
// a next-up entry's video id is synthesised by bumping the episode number, so
// it often doesn't match what the addon actually calls that episode — the
// fetch then matches on season and episode, and an id comparison would
// wrongly conclude the result was for something else and hide the synopsis.
func targetKey(e HistoryEntry) string {
	if e.VideoID != "" {
		return e.VideoID
	}
	return e.ID
}

// NextEntry is what w resumes, so the caller can decide whether the episode
// it already fetched is still the right one.
func (p *continuePanel) NextEntry() (HistoryEntry, bool) {
	if p.next == nil {
		return HistoryEntry{}, false
	}
	return p.next.Entry, true
}

// FetchTargetInfo loads the synopsis for the continue target.
//
// Neither an episode overview nor a film's description is in history — only
// the title — so this is the one thing on the panel that needs the network.
// Both underlying calls cache, so it's one request per title per session.
func FetchTargetInfo(e HistoryEntry) tea.Cmd {
	return func() tea.Msg {
		if e.ID == "" {
			return targetInfoMsg{}
		}

		// A film has no episode to look up; it's the meta object itself.
		if e.Season <= 0 {
			d, ok := GetMetaDetail(ctx.addons, "movie", e.ID, "")
			if !ok {
				return targetInfoMsg{}
			}
			return targetInfoMsg{Key: targetKey(e), Detail: d}
		}

		m := Meta{ID: e.ID, Type: e.Type, Name: e.Name, Source: e.Source}
		for _, v := range GetSeriesMeta(ctx.addons, m).Videos {
			if v.ID == e.VideoID || (v.Season == e.Season && v.Episode == e.Episode) {
				return targetInfoMsg{Key: targetKey(e), Video: v}
			}
		}
		return targetInfoMsg{}
	}
}

type targetInfoMsg struct {
	Key    string
	Video  Video
	Detail MetaDetail
}

// One line per entry, in fixed columns.
//
// The two-line version had six styles competing — accent number, bold title,
// dim episode, accent bar, dim percentage, dim count — so nothing read as
// subordinate to anything else. Splitting each entry across two lines also
// broke the left edge you scan down. Columns give a consistent left margin for
// names and a consistent right one for status, and the percentage carries the
// progress that the bar was duplicating.
//
// The same three columns are reused by every section, so the panel reads as
// one table with headings rather than three unrelated widgets.

const (
	colNum    = 3 // " 1 "
	colStatus = 5 // "next" / " 52%"
	colEp     = 8 // "S01E09  "
)

// row renders one line in the shared column layout.
func (p *continuePanel) row(num, name, ep, status string) string {
	nameW := max(8, p.w-colNum-colStatus-colEp)
	return padTo(num, colNum) +
		padTo(ellipsize(bold(name), nameW-1), nameW) +
		stSub.Render(padTo(ep, colEp)) +
		status
}

func (p *continuePanel) View() string {
	if !p.on || p.w <= 0 || p.h <= 0 {
		return ""
	}

	// The stats footer is held back by pad() and pinned to the bottom edge, so
	// the space the sections may use is the height minus that. Measuring
	// against p.h instead let a section fill to the bottom and then get
	// silently chopped when the footer was appended.
	foot := p.statsLines()
	avail := max(0, p.h-len(foot))

	var lines []string
	room := func() int { return avail - len(lines) }

	num := func(open func() screen) string {
		p.actions = append(p.actions, panelAction{open: open})
		if len(p.actions) > 9 {
			return "   " // past what a single keypress can reach
		}
		return stKey.Render(fmt.Sprintf("%2d ", len(p.actions)))
	}

	section := func(title string) {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, stCrumb.Render(title), "")
	}

	p.actions = nil

	// ── today ─────────────────────────────────────────────────────────────
	// At the top because it's the one line here that isn't about your
	// library, so it reads as a header rather than a section.
	lines = append(lines, stTitle.Render(today()))

	// ── the continue target, in full ──────────────────────────────────────
	// Top of the panel so it sits level with the menu's resume row on the
	// left, which is the thing it describes. No heading: the menu already
	// says w continues, so repeating it here just spends a row.
	// Bounded by what's left as well as by its own cap: a long synopsis on a
	// short panel would otherwise leave nothing for the lists below it.
	if c := p.next; c != nil && room() > 6 {
		lines = append(lines, "")
		lines = append(lines, p.episodeBlock(c.Entry, min(episodeBlockMax, room()-5))...)
	}

	// ── continue watching ─────────────────────────────────────────────────
	if room() > 3 {
		section("continue watching")
		if len(p.items) == 0 {
			lines = append(lines, stHint.Render("   nothing in progress"))
		}
	}

	live := ctx.player.State()
	for _, c := range p.items {
		if room() <= 1 {
			break
		}
		e := c.Entry

		pos, dur := c.Position, c.Duration
		if live.Alive && live.VideoID == e.VideoID && live.Duration > 0 {
			pos, dur = live.Pos, live.Duration
		}

		status := stHint.Render("   ·")
		switch {
		case c.NextUp:
			status = stWarn.Render("next")
		case dur > 0 && pos > 0:
			status = stSub.Render(fmt.Sprintf("%3.0f%%", pos/dur*100))
		}

		ep := ""
		if e.Season > 0 && e.Episode > 0 {
			ep = fmtVideoID(e.VideoID)
		}

		entry := e
		lines = append(lines, p.row(
			num(func() screen { return resumeScreen(entry) }),
			bold(e.Name), ep, status))
	}

	// ── recently watched ──────────────────────────────────────────────────
	if len(p.watched) > 0 && room() > 4 {
		section("recently watched")
		for _, e := range p.watched {
			if room() <= 1 {
				break
			}
			ep := ""
			if e.Season > 0 && e.Episode > 0 {
				ep = fmtVideoID(e.VideoID)
			}
			lines = append(lines, p.row("", grey(e.Name), ep, good("   ✓")))
		}
	}

	// ── downloads ─────────────────────────────────────────────────────────
	// Last: the only section that's about the machine rather than about what
	// you're watching.
	if len(p.downloads) > 0 && room() > 4 {
		section("downloads")
		for _, d := range p.downloads {
			if room() <= 1 {
				break
			}

			name, status := bold(d.Label), stHint.Render("   ·")
			switch d.State {
			case DLActive:
				if d.Total > 0 {
					status = stSub.Render(fmt.Sprintf("%3.0f%%", d.Frac()*100))
				}
			case DLQueued:
				status = stHint.Render("wait")
			case DLCancelled:
				status = stStop.Render("stop")
			case DLFailed:
				status = stErr.Render("fail")
			case DLDone:
				// Greyed with a tick: it's on disk, there's nothing left to
				// do with it, and it shouldn't compete with the one that's
				// still running.
				name, status = grey(d.Label), good("   ✓")
			}
			lines = append(lines, p.row("", name, "", status))
		}
	}

	return p.pad(lines, foot)
}

// episodeBlock renders the continue target the way the info panel renders an
// episode: identifier, title, show, air date, then the synopsis.
func (p *continuePanel) episodeBlock(e HistoryEntry, budget int) []string {
	wrap := lipgloss.NewStyle().Width(p.w)
	var out []string

	// A film: title, the facts line, genres, synopsis. Everything the info
	// panel shows down to the summary, and nothing after it — cast and
	// awards are for when you've gone looking, not for a glance.
	if e.Season <= 0 {
		d := p.epMovie
		if p.epKey != targetKey(e) {
			d = MetaDetail{} // fetch hasn't landed, or is for something else
		}

		name := e.Name
		if d.Name != "" {
			name = d.Name
		}
		for _, ln := range strings.Split(wrap.Render(name), "\n") {
			out = append(out, stTitle.Render(ln))
		}

		var facts []string
		if d.ReleaseInfo != "" {
			facts = append(facts, d.ReleaseInfo)
		} else if e.Year != "" {
			facts = append(facts, e.Year)
		}
		if d.Runtime != "" {
			facts = append(facts, d.Runtime)
		}
		if d.ImdbRating != "" && d.ImdbRating != "N/A" {
			facts = append(facts, "★ "+d.ImdbRating)
		}
		if len(facts) > 0 {
			out = append(out, stKey.Render(strings.Join(facts, "  ·  ")))
		}
		if g := d.AllGenres(); len(g) > 0 {
			for _, ln := range strings.Split(wrap.Render(strings.Join(g, ", ")), "\n") {
				out = append(out, stSub.Render(ln))
			}
		}
		if d.Description != "" {
			out = append(out, "")
			out = append(out, strings.Split(wrap.Render(d.Description), "\n")...)
		}

		if len(out) > budget {
			out = out[:max(0, budget)]
		}
		return out
	}

	if e.Season > 0 && e.Episode > 0 {
		out = append(out, stKey.Render(fmtVideoID(e.VideoID)))
	}

	title := e.EpTitle
	if title == "" {
		title = p.epVideo.Title
	}
	if title != "" {
		for _, ln := range strings.Split(wrap.Render(title), "\n") {
			out = append(out, stTitle.Render(ln))
		}
	}
	out = append(out, stSub.Render(e.Name))

	// Only meaningful once the fetch for this target has landed.
	if p.epKey == targetKey(e) {
		if r := fmtRelease(p.epVideo.Released); r != "" {
			if videoAired(p.epVideo) {
				out = append(out, stSub.Render("aired "+r))
			} else {
				line := "○ airs " + r
				if when := untilRelease(p.epVideo.Released); when != "" {
					line += "  ·  " + when
				}
				out = append(out, stWarn.Render(line))
			}
		}
		if p.epVideo.Overview != "" {
			out = append(out, "")
			out = append(out, strings.Split(wrap.Render(p.epVideo.Overview), "\n")...)
		}
	}

	if len(out) > budget {
		out = out[:max(0, budget)]
	}
	return out
}

// statsLines is pinned to the bottom of the panel rather than flowing with
// the sections: it's a footnote about your library as a whole, not something
// you act on, so it belongs out of the way.
func (p *continuePanel) statsLines() []string {
	st := p.stats

	var parts []string
	if st.Episodes > 0 {
		parts = append(parts, fmt.Sprintf("%d episodes", st.Episodes))
	}
	if st.Films > 0 {
		parts = append(parts, fmt.Sprintf("%d films", st.Films))
	}
	if st.Shows > 0 {
		parts = append(parts, fmt.Sprintf("%d shows", st.Shows))
	}
	if len(parts) == 0 {
		return nil
	}

	out := []string{clamp(stHint.Render(strings.Join(parts, "  ·  ")), p.w)}
	if st.Hours >= 1 {
		out = append(out, clamp(stHint.Render(fmt.Sprintf("%.0f hours watched", st.Hours)), p.w))
	}
	return out
}

// padTo truncates or space-fills to exactly w columns, ANSI-aware.
func padTo(s string, w int) string {
	s = ellipsize(s, w)
	if gap := w - lipgloss.Width(s); gap > 0 {
		s += strings.Repeat(" ", gap)
	}
	return s
}

// pad squares the block off to the panel height, holding foot back for the
// last rows so it sits on the bottom edge whatever the sections above did.
func (p *continuePanel) pad(lines, foot []string) string {
	body := max(0, p.h-len(foot))
	if len(lines) > body {
		lines = lines[:body]
	}
	for len(lines) < body {
		lines = append(lines, "")
	}
	lines = append(lines, foot...)

	if len(lines) > p.h {
		lines = lines[:p.h]
	}
	for i, l := range lines {
		lines[i] = clamp(l, p.w) + cReset
	}
	return strings.Join(lines, "\n")
}

// panelWidth mirrors paneLayout, so the menu and the panel agree on the split.
func continuePanelLayout(total int, p *continuePanel) int {
	if !p.On() {
		return total
	}
	pw := PaneWidth(total)
	if pw == 0 {
		return total
	}
	return total - pw - 3
}

func joinContinuePanel(menu string, menuW int, p *continuePanel) string {
	if !p.On() || p.w <= 0 {
		return menu
	}
	divider := strings.TrimRight(strings.Repeat(stHint.Render("│")+"\n", p.h), "\n")
	return lipgloss.JoinHorizontal(lipgloss.Top,
		padBlock(menu, menuW), " ", divider, " ", p.View())
}

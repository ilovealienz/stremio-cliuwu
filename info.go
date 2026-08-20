package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// infoPane is the `i` panel: metadata for whatever row the cursor is on.
//
// Meta is fetched lazily per highlighted title rather than up front for the
// whole catalog — a catalog page is 100 rows, and asking for all of them would
// be 100 requests to draw one screen.

type metaDetailMsg struct {
	id     asyncID
	metaID string
	detail MetaDetail
	ok     bool
}

// autoInfoWidth is the terminal width at which the pane opens on its own.
// Deliberately above the minimum it can render at: at 80 columns the pane
// fits, but the list is squeezed enough that you'd rather choose.
const autoInfoWidth = 110

type infoPane struct {
	on     bool
	manual bool // user pressed i — stop opening and closing it for them
	w, h   int

	id     asyncID
	forID  string // meta id currently shown or loading
	detail MetaDetail
	loaded bool
	busy   busy

	posterID asyncID
	poster   string // rendered half-block art
	gen      int    // poster size generation this was rendered at

	// Episode mode: rendered straight from the season's video list, which
	// GetSeriesMeta already fetched. No request, no cache, no async.
	ep     *Video
	epShow string

	full    bool // taking over the whole screen rather than sitting beside
	indent  int  // left margin, used when centred
	offset  int  // first visible line
	overflow bool // content is taller than the panel
}

func newInfoPane() infoPane {
	return infoPane{busy: newBusy("loading…")}
}

func (p *infoPane) On() bool { return p.on }

func (p *infoPane) Toggle() {
	p.on = !p.on
	p.manual = true
}

// AutoFit opens or closes the pane to suit the terminal width, unless the
// user has taken manual control of it on this screen.
func (p *infoPane) AutoFit(total int) {
	if p.manual || ctx == nil || !ctx.cfg.AutoInfo {
		return
	}
	// Only ever opens itself as a side pane — taking over the whole screen
	// isn't something to do without being asked.
	p.on = total >= autoInfoWidth && PaneWidth(total) > 0
}

// paneRightPad keeps the text off the right edge of the terminal. The pane is
// allotted its full column width; the content is rendered narrower so the
// difference shows as margin.
const paneRightPad = 2

func (p *infoPane) SetSize(w, h int, full bool) {
	p.full = full
	p.h = h

	if full {
		// Taking over the screen: cap the measure and centre it, rather than
		// running text the entire width hard against the left edge.
		content := min(w-8, 76)
		p.w = max(20, content)
		p.indent = max(2, (w-p.w)/2)
		return
	}

	p.w = max(0, w-paneRightPad)
	p.indent = 0
}

// minSplitWidth is the narrowest terminal worth splitting. Below it the pane
// would be ~25 columns — four words a line, with the list truncated beside it
// — so `i` shows the info full width instead of side by side.
const minSplitWidth = 96

// PaneWidth is how much horizontal space the pane wants, given the whole
// screen width. Returns 0 when there isn't room to sit alongside the list.
func PaneWidth(total int) int {
	if total < minSplitWidth {
		return 0
	}
	return min(56, total/3)
}

// Split reports whether the pane can sit beside the list at this width.
func (p *infoPane) Split(total int) bool { return PaneWidth(total) > 0 }

// Show loads meta for a title, if it isn't already showing.
func (p *infoPane) Show(m Meta) tea.Cmd {
	if !p.on || m.ID == "" {
		return nil
	}
	// Same title is normally a no-op, unless the poster size has changed
	// underneath us and the artwork needs redrawing.
	if m.ID == p.forID && p.gen == posterGen {
		return nil
	}
	p.gen = posterGen

	p.forID = m.ID
	p.loaded = false
	p.detail = MetaDetail{}
	p.ep = nil
	p.poster = ""
	p.offset = 0

	p.id = newAsyncID()
	id := p.id
	mediaType, metaID, hint := m.Type, m.ID, m.Base
	if mediaType == "" {
		mediaType = "movie"
	}
	addons := ctx.addons

	return tea.Batch(
		p.busy.start("loading…"),
		func() tea.Msg {
			d, ok := GetMetaDetail(addons, mediaType, metaID, hint)
			return metaDetailMsg{id: id, metaID: metaID, detail: d, ok: ok}
		},
	)
}

// ShowEpisode switches the panel to an episode. Everything it needs is
// already in hand, so unlike Show there's nothing to fetch.
func (p *infoPane) ShowEpisode(show string, v Video) tea.Cmd {
	if !p.on || v.ID == "" || v.ID == p.forID {
		return nil
	}

	p.forID = v.ID
	p.ep = &v
	p.epShow = show
	p.detail = MetaDetail{}
	p.poster = ""
	p.loaded = true
	p.offset = 0
	p.busy.stop()
	return nil
}

func (p *infoPane) Update(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case metaDetailMsg:
		if m.id != p.id {
			return nil // a load for a row we've since moved off
		}
		p.busy.stop()
		p.loaded = true
		p.detail = m.detail
		return p.loadPoster()

	case posterMsg:
		if m.id == p.posterID {
			p.poster = m.art
		}
		return nil
	}
	return p.busy.update(msg)
}

// loadPoster renders the artwork once the meta has arrived, since that's what
// carries the URL. Separate from the meta fetch so a slow image doesn't hold
// up the text.
func (p *infoPane) loadPoster() tea.Cmd {
	if !ctx.cfg.Posters || p.detail.Poster == "" || p.w < 12 {
		return nil
	}

	p.posterID = newAsyncID()
	id, url := p.posterID, p.detail.Poster
	maxW, maxH := posterBudget(ctx.cfg.PosterSize, p.w, p.h)

	return func() tea.Msg {
		return posterMsg{id: id, url: url, art: FetchPoster(url, maxW, maxH)}
	}
}

// Scroll moves the visible window through the content.
func (p *infoPane) Scroll(d int) {
	p.offset += d
	if p.offset < 0 {
		p.offset = 0
	}
	// The upper bound is clamped in View, where the content height is known.
}

func (p *infoPane) CanScroll() bool { return p.overflow }

func (p *infoPane) View() string {
	if !p.on || p.w <= 0 || p.h <= 0 {
		return ""
	}

	all := strings.Split(p.render(), "\n")

	// The scroll indicator gets a row of its own. Writing it over the last
	// line meant losing a line of content every time it appeared, which is
	// exactly when you can least afford it.
	avail := p.h
	p.overflow = len(all) > avail
	if p.overflow {
		avail = p.h - 1
	}

	maxOff := max(0, len(all)-avail)
	if p.offset > maxOff {
		p.offset = maxOff
	}

	lines := append([]string(nil), all[p.offset:min(len(all), p.offset+avail)]...)
	for len(lines) < avail {
		lines = append(lines, "")
	}

	pad := strings.Repeat(" ", p.indent)
	for i, l := range lines {
		lines[i] = pad + clamp(l, p.w) + cReset
	}

	if p.overflow {
		shown := min(p.offset+avail, len(all))
		marker := stHint.Render(fmt.Sprintf("%d/%d  J/K", shown, len(all)))
		gap := max(0, p.w-lipgloss.Width(marker))
		lines = append(lines, pad+strings.Repeat(" ", gap)+marker+cReset)
	}
	return strings.Join(lines, "\n")
}

func (p *infoPane) render() string {
	if p.forID == "" {
		return stHint.Render("nothing selected")
	}
	if !p.loaded {
		return p.busy.view()
	}

	if p.ep != nil {
		return p.renderEpisode()
	}

	d := p.detail
	if d.Name == "" {
		return stHint.Render("no details available")
	}

	wrap := lipgloss.NewStyle().Width(p.w)
	lines := func(s string) []string { return strings.Split(s, "\n") }

	// Built in sections so the description can be trimmed to fit without
	// pushing the credits off the bottom — losing the cast because a synopsis
	// ran long is the wrong trade.
	var head, body, tail []string

	if p.poster != "" {
		head = append(head, lines(p.poster)...)
	}
	if d.Poster != "" {
		head = append(head, p.posterLink())
	}
	if len(head) > 0 {
		head = append(head, "")
	}

	// Wrap first, then style each line. Styling the whole string and wrapping
	// afterwards leaves the escape at the start of line one, so a title that
	// runs onto a second line renders that line unstyled — it ends up looking
	// like the synopsis below it.
	for _, ln := range lines(wrap.Render(d.Name)) {
		head = append(head, stTitle.Render(ln))
	}

	var facts []string
	if d.ReleaseInfo != "" {
		facts = append(facts, d.ReleaseInfo)
	}
	if d.Runtime != "" {
		facts = append(facts, d.Runtime)
	}
	if d.ImdbRating != "" && d.ImdbRating != "N/A" {
		facts = append(facts, "★ "+d.ImdbRating)
	}
	if len(facts) > 0 {
		head = append(head, stKey.Render(strings.Join(facts, "  ·  ")))
	}
	if g := d.AllGenres(); len(g) > 0 {
		for _, ln := range lines(wrap.Render(strings.Join(g, ", "))) {
			head = append(head, stSub.Render(ln))
		}
	}

	if d.Description != "" {
		body = append(body, "")
		body = append(body, lines(wrap.Render(d.Description))...)
	}

	var tb strings.Builder
	p.field(&tb, "cast", strings.Join(d.Cast, ", "))
	p.field(&tb, "director", strings.Join(d.Director, ", "))
	p.field(&tb, "writer", strings.Join(d.Writer, ", "))
	p.field(&tb, "country", d.Country)
	p.field(&tb, "status", d.Status)
	if d.Awards != "N/A" {
		p.field(&tb, "awards", d.Awards)
	}
	if tb.Len() > 0 {
		tail = lines(strings.TrimRight(tb.String(), "\n"))
	}

	out := make([]string, 0, len(head)+len(body)+len(tail))
	out = append(out, head...)
	out = append(out, body...)
	out = append(out, tail...)
	return strings.Join(out, "\n")
}

// posterLink renders the OSC 8 hyperlink to the full-size image, with the
// label trimmed to whatever the panel width allows.
// renderEpisode draws the panel for a single episode. Video carries only
// title, air date and overview — no rating, runtime or cast — so this is
// deliberately sparser than the title panel.
func (p *infoPane) renderEpisode() string {
	v := *p.ep
	wrap := lipgloss.NewStyle().Width(p.w)
	lines := func(s string) []string { return strings.Split(s, "\n") }

	var out []string

	for _, ln := range lines(wrap.Render(fmtVideoID(v.ID))) {
		out = append(out, stKey.Render(ln))
	}
	if v.Title != "" {
		for _, ln := range lines(wrap.Render(v.Title)) {
			out = append(out, stTitle.Render(ln))
		}
	}
	if p.epShow != "" {
		out = append(out, stSub.Render(p.epShow))
	}

	if r := fmtRelease(v.Released); r != "" {
		if videoAired(v) {
			out = append(out, stSub.Render("aired "+r))
		} else {
			line := "○ airs " + r
			if when := untilRelease(v.Released); when != "" {
				line += "  ·  " + when
			}
			out = append(out, stWarn.Render(line))
		}
	}

	if v.Overview != "" {
		out = append(out, "")
		out = append(out, lines(wrap.Render(v.Overview))...)
	} else {
		out = append(out, "", stHint.Render("no synopsis for this episode"))
	}
	return strings.Join(out, "\n")
}

// posterLink is a keybinding hint, not a hyperlink.
//
// OSC 8 terminal links can't be used here: lipgloss understands CSI escapes
// but not OSC, so Width counts the URL as visible text, truncation cuts
// through the middle of the escape, and the horizontal join sizes the block
// off a line that isn't really that wide. A key press is more reliable and
// less fiddly to hit than ctrl+clicking a link anyway.
func (p *infoPane) posterLink() string {
	// Rendered like the footer hints — a bare dim letter read as a bullet
	// point rather than a key you're meant to press.
	return stKey.Render("p") + stHint.Render("=full resolution poster")
}

// field writes a "label: value" block, wrapped as a whole.
//
// Wrapping the value on its own and then gluing the label to the front pushes
// the first line past the pane width — which then drags the entire block wider
// when it's joined horizontally, and the pane slides off screen.
func (p *infoPane) field(b *strings.Builder, label, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}

	text := label + ": " + value
	lines := strings.Split(lipgloss.NewStyle().Width(p.w).Render(text), "\n")

	// Re-colour just the label, now that wrapping is settled.
	if len(lines) > 0 && strings.HasPrefix(lines[0], label+":") {
		lines[0] = stHint.Render(label+":") + lines[0][len(label)+1:]
	}

	b.WriteString("\n" + strings.Join(lines, "\n") + "\n")
}

// splitBody lays a list and the pane side by side. Returns the width the list
// should be sized to, and a function to join the two once rendered.
func paneLayout(total int, p *infoPane) int {
	if !p.On() {
		return total
	}
	pw := PaneWidth(total)
	if pw == 0 {
		return total
	}
	return total - pw - 3 // gap plus divider
}

// paneSize is the width the pane itself should be given: alongside the list
// when there's room, the whole screen when there isn't.
func paneSize(total int, p *infoPane) int {
	if pw := PaneWidth(total); pw > 0 {
		return pw
	}
	return total
}

// padBlock squares a block off to exactly w columns, so the divider sits where
// the layout says rather than wherever the longest row happened to end.
func padBlock(block string, w int) string {
	lines := strings.Split(block, "\n")
	for i, l := range lines {
		l = clamp(l, w)
		if pad := w - lipgloss.Width(l); pad > 0 {
			l += strings.Repeat(" ", pad)
		}
		lines[i] = l + cReset
	}
	return strings.Join(lines, "\n")
}

func joinPane(listView string, listW int, p *infoPane) string {
	if !p.On() || p.w <= 0 {
		return listView
	}
	divider := strings.TrimRight(strings.Repeat(stHint.Render("│")+"\n", p.h), "\n")
	return lipgloss.JoinHorizontal(lipgloss.Top,
		padBlock(listView, listW), " ", divider, " ", p.View())
}

// infoKeys handles panel scrolling for a screen. Reports whether it consumed
// the key.
//
// In fullscreen the list isn't visible, so the ordinary movement keys scroll
// the panel instead of moving a selection you can't see. Alongside a list,
// only the shifted pair applies, leaving the list's own keys alone.
func infoKeys(p *infoPane, w, h int, k tea.KeyMsg) (tea.Cmd, bool) {
	if !p.On() {
		return nil, false
	}

	switch k.String() {
	case "p":
		if p.detail.Poster == "" {
			return nil, false
		}
		if err := openURL(p.detail.Poster); err != nil {
			return toastErr("couldn't open: " + err.Error()), true
		}
		return toast("opening poster…"), true
	case "J":
		p.Scroll(3)
		return nil, true
	case "K":
		p.Scroll(-3)
		return nil, true
	}

	if p.Split(w) {
		return nil, false // list keeps its movement keys
	}

	switch k.String() {
	case "down", "j":
		p.Scroll(1)
		return nil, true
	case "up", "k":
		p.Scroll(-1)
		return nil, true
	case "pgdown", "right":
		p.Scroll(max(1, h-2))
		return nil, true
	case "pgup", "left":
		p.Scroll(-max(1, h-2))
		return nil, true
	case "home":
		p.offset = 0
		return nil, true
	}
	return nil, false
}

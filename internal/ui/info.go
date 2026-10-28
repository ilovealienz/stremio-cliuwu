package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"stremio-cliuwu/internal/poster"
	"stremio-cliuwu/internal/stremio"
)

// infoPane is the `i` panel: metadata for whatever row the cursor is on.
//
// Meta is fetched lazily per highlighted title rather than up front for the
// whole catalog — a catalog page is 100 rows, and asking for all of them would
// be 100 requests to draw one screen.

// paneDwell is how long the cursor rests on an uncached row before it's fetched.
const paneDwell = 150 * time.Millisecond

type paneSettleMsg struct{ id poster.AsyncID }

type metaDetailMsg struct {
	id     poster.AsyncID
	metaID string
	detail stremio.MetaDetail
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

	id     poster.AsyncID
	forID  string // meta id currently shown or loading
	detail stremio.MetaDetail
	loaded bool
	busy   busy

	posterID poster.AsyncID
	poster   string // rendered half-block art
	gen      int    // poster size generation this was rendered at

	// Kitty graphics state. kittyOn means the pane shows placeholder cells
	// (see kitty.go) instead of the half-block art above; the pixels live in
	// the terminal under kittyImg, addressed by URL hash.
	kittyOn              bool
	kittyImg             uint32
	kittyURL             string
	kittyCols, kittyRows int

	// The fetch waiting out paneDwell, and what to run once this row has loaded.
	pending   tea.Cmd
	afterLoad func()

	// poster.KittyBlock for the image above, rebuilt only when it changes.
	kbID           uint32
	kbCols, kbRows int
	kb             string

	// Episode mode: rendered straight from the season's video list, which
	// GetSeriesMeta already fetched. No request, no cache, no async.
	ep     *stremio.Video
	epShow string

	full     bool // taking over the whole screen rather than sitting beside
	indent   int  // left margin, used when centred
	offset   int  // first visible line
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

func (p *infoPane) Show(m stremio.Meta) tea.Cmd {
	if !p.on || m.ID == "" {
		return nil
	}
	// Same title is a no-op once loaded, unless the poster size has changed.
	// Not before: a result delivered to another screen would leave it spinning.
	if m.ID == p.forID && p.gen == poster.Gen && p.loaded {
		p.loadedNow()
		return nil
	}
	p.gen = poster.Gen

	p.forID = m.ID
	p.loaded = false
	p.detail = stremio.MetaDetail{}
	p.ep = nil
	p.poster = ""
	p.offset = 0

	// Moving to a new title orphans the previous kitty image: its
	// placeholders leave the screen with the old pane, so drop it from the
	// terminal now rather than holding the pixels until quit.
	var drop tea.Cmd
	if p.kittyImg != 0 {
		drop = poster.KittyDeleteCmd(p.kittyImg)
	}
	p.kittyOn, p.kittyImg, p.kittyURL = false, 0, ""
	p.kittyCols, p.kittyRows = 0, 0

	p.id = poster.NewAsyncID()
	id := p.id
	mediaType, metaID, hint := stremio.MetaArgs(m)
	addons := ctx.addons
	fetch := func() tea.Msg {
		d, ok := stremio.GetMetaDetail(addons, mediaType, metaID, hint)
		return metaDetailMsg{id: id, metaID: metaID, detail: d, ok: ok}
	}

	// Cached rows (revisited or preloaded) cost nothing to show, so no wait.
	if _, cached := stremio.CacheMetaDetail.Get(mediaType + ":" + metaID); cached {
		p.pending = nil
		return tea.Batch(drop, p.busy.start("loading…"), fetch)
	}
	p.pending = fetch
	return tea.Batch(drop, p.busy.start("loading…"), tea.Tick(paneDwell, func(time.Time) tea.Msg {
		return paneSettleMsg{id: id}
	}))
}

// AfterLoad sets what to run once the current row has fully loaded.
func (p *infoPane) AfterLoad(f func()) { p.afterLoad = f }

func (p *infoPane) loadedNow() {
	if f := p.afterLoad; f != nil {
		p.afterLoad = nil
		f()
	}
}

// ShowEpisode switches the panel to an episode. Everything it needs is
// already in hand, so unlike Show there's nothing to fetch.
func (p *infoPane) ShowEpisode(show string, v stremio.Video) tea.Cmd {
	if !p.on || v.ID == "" || v.ID == p.forID {
		return nil
	}

	p.forID = v.ID
	p.ep = &v
	p.epShow = show
	p.detail = stremio.MetaDetail{}
	p.poster = ""
	p.loaded = true
	p.offset = 0
	p.busy.stop()

	// The title's image goes whatever happens next: its placeholders left
	// the screen with the poster.
	var cmds []tea.Cmd
	if p.kittyImg != 0 {
		cmds = append(cmds, poster.KittyDeleteCmd(p.kittyImg))
	}
	p.kittyOn, p.kittyImg, p.kittyURL = false, 0, ""
	p.kittyCols, p.kittyRows = 0, 0

	// Kitty only, and opt-in: a still is a frame from an episode you haven't
	// watched yet.
	if ctx.cfg.EpisodeImages && poster.KittySupported() && v.Thumbnail != "" {
		p.posterID = poster.NewAsyncID()
		maxW, maxH := poster.Budget(ctx.cfg.PosterSize, p.w, p.h)
		cmds = append(cmds, poster.KittyLoadCmd(p.posterID, v.Thumbnail, maxW, maxH))
	}

	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (p *infoPane) Update(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case paneSettleMsg:
		if m.id != p.id || p.pending == nil || !p.on {
			return nil // moved on, or closed, before the dwell was up
		}
		fetch := p.pending
		p.pending = nil
		return fetch

	case metaDetailMsg:
		if m.id != p.id {
			return nil // a load for a row we've since moved off
		}
		p.busy.stop()
		p.loaded = true
		p.detail = m.detail
		cmd := p.loadPoster()
		if cmd == nil {
			p.loadedNow() // no poster to wait for
		}
		return cmd

	case poster.Msg:
		if m.ID == p.posterID {
			p.poster = m.Art
			p.loadedNow()
		}
		return nil

	case poster.KittyPosterMsg:
		if m.PID == p.posterID {
			if m.OK {
				p.kittyOn, p.kittyImg, p.kittyURL = true, m.ID, m.URL
				p.kittyCols, p.kittyRows = m.Cols, m.Rows
			} else {
				p.kittyOn = false
			}
			p.loadedNow()
		}
		return nil

	case tea.WindowSizeMsg:
		// SetSize already ran (screens resize before updating), so p.w/p.h
		// are the new ones. Placeholders reflow with the text on their own;
		// only a changed rectangle needs re-binding to the live image, and
		// that sends control bytes, not pixels.
		if p.kittyOn && p.kittyImg != 0 {
			if d, ok := poster.CacheDims.Get(p.kittyURL); ok {
				maxW, maxH := poster.Budget(ctx.cfg.PosterSize, p.w, p.h)
				cols, rows := poster.Size(d.W, d.H, maxW, maxH)
				if cols != p.kittyCols || rows != p.kittyRows {
					p.kittyCols, p.kittyRows = cols, rows
					return poster.KittyPlaceCmd(p.kittyImg, cols, rows)
				}
			}
		}
		return p.busy.update(msg)
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

	p.posterID = poster.NewAsyncID()
	id, url := p.posterID, p.detail.Poster
	maxW, maxH := poster.Budget(ctx.cfg.PosterSize, p.w, p.h)

	// Kitty-capable terminals get the real image via placeholders; the
	// transmit reports back before any placeholder reaches the View.
	if poster.KittySupported() {
		return poster.KittyLoadCmd(id, url, maxW, maxH)
	}

	return func() tea.Msg {
		return poster.Msg{ID: id, URL: url, Art: poster.Fetch(url, maxW, maxH)}
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

	// One builder, so each line is copied once.
	pad := strings.Repeat(" ", p.indent)
	size := 64
	for _, l := range lines {
		size += len(pad) + len(l) + len(cReset) + 1
	}
	var b strings.Builder
	b.Grow(size)
	for i, l := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(pad)
		b.WriteString(clamp(l, p.w))
		b.WriteString(cReset)
	}

	if p.overflow {
		shown := min(p.offset+avail, len(all))
		marker := stHint.Render(fmt.Sprintf("%d/%d  J/K", shown, len(all)))
		gap := max(0, p.w-lipgloss.Width(marker))
		if len(lines) > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(pad + strings.Repeat(" ", gap) + marker + cReset)
	}
	return b.String()
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

	if p.kittyOn && p.kittyImg != 0 {
		// Placeholder cells, not pixels: plain diffable text emitting the
		// bound image where it sits. Capped to the pane width so the
		// per-line clamp in View stays a no-op — these lines must not be
		// truncated or restyled.
		cols := min(p.kittyCols, p.w)
		head = append(head, lines(p.kittyGrid(cols))...)
	} else if p.poster != "" {
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
		facts = append(facts, string(d.Runtime))
	}
	if d.ImdbRating != "" && d.ImdbRating != "N/A" {
		facts = append(facts, "★ "+fmtRating(string(d.ImdbRating)))
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

	// Same placeholder block the poster uses, above the text for the same
	// reason: it's what the rest of the pane is describing.
	if p.kittyOn && p.kittyImg != 0 {
		cols := min(p.kittyCols, p.w)
		out = append(out, lines(p.kittyGrid(cols))...)
		out = append(out, "")
	}

	for _, ln := range lines(wrap.Render(stremio.FmtEp(v.Season, v.Episode, v.ID))) {
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
		if stremio.VideoAired(v) {
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
	hint := stKey.Render("p") + stHint.Render("=full resolution poster")
	if p.imdbID() != "" {
		hint += stHint.Render("   ") + stKey.Render("I") + stHint.Render("=imdb")
	}
	return hint
}

// imdbID is the title's imdb id, if it has one.
//
// Cinemeta sends imdb_id alongside its own id, and for anything it serves the
// two match. It matters for addons with their own scheme — a kitsu title's id
// is kitsu:7442, and imdb_id is the only route to a page about it.
func (p *infoPane) imdbID() string {
	if id := p.detail.ImdbID; strings.HasPrefix(id, "tt") {
		return id
	}
	if id := p.detail.ID; strings.HasPrefix(id, "tt") {
		return id
	}
	return ""
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
// kittyGrid is poster.KittyBlock for the current image, rebuilt only when it changes.
func (p *infoPane) kittyGrid(cols int) string {
	if p.kb == "" || p.kbID != p.kittyImg || p.kbCols != cols || p.kbRows != p.kittyRows {
		p.kbID, p.kbCols, p.kbRows = p.kittyImg, cols, p.kittyRows
		p.kb = poster.KittyBlock(p.kittyImg, cols, p.kittyRows)
	}
	return p.kb
}

// joinSide matches lipgloss.JoinHorizontal(Top, padded left, " ", divider, " ",
// right) byte for byte, without re-measuring every line.
func joinSide(left string, leftW, divRows int, right string) string {
	ll := strings.Split(left, "\n")
	rl := strings.Split(right, "\n")

	leftWidest := max(0, leftW)
	divW, divLines := 1, divRows
	if divRows <= 0 {
		divW, divLines = 0, 1 // an empty divider is one zero-width line
	}
	div := stHint.Render("│")

	rw := make([]int, len(rl))
	rightWidest := 0
	for i, l := range rl {
		rw[i] = ansi.StringWidth(l)
		rightWidest = max(rightWidest, rw[i])
	}

	rows := max(len(ll), divLines, len(rl), 1)
	spaces := strings.Repeat(" ", max(leftWidest, rightWidest, 1))

	// Sized by bytes: colour codes make lines far longer than their width.
	var b strings.Builder
	b.Grow(len(left) + len(right) + rows*(leftWidest+rightWidest+len(div)+len(cReset)+4))
	for i := 0; i < rows; i++ {
		if i < len(ll) {
			l, w := clampW(ll[i], leftW)
			b.WriteString(l)
			b.WriteString(spaces[:max(0, leftW-w)])
			b.WriteString(cReset)
		} else {
			b.WriteString(spaces[:leftWidest])
		}
		b.WriteByte(' ')
		switch {
		case i < divRows:
			b.WriteString(div)
		case i >= divLines:
			b.WriteString(spaces[:divW])
		}
		b.WriteByte(' ')
		if i < len(rl) {
			b.WriteString(rl[i])
			b.WriteString(spaces[:rightWidest-rw[i]])
		} else {
			b.WriteString(spaces[:rightWidest])
		}
		if i < rows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func joinPane(listView string, listW int, p *infoPane) string {
	if !p.On() || p.w <= 0 {
		return listView
	}
	return joinSide(listView, listW, p.h, p.View())
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
	case "I":
		if id := p.imdbID(); id != "" {
			if err := poster.OpenURL("https://www.imdb.com/title/" + id + "/"); err != nil {
				return toastErr("couldn't open: " + err.Error()), true
			}
			return toast("opened imdb"), true
		}
		return toastErr("no imdb id for this one"), true

	case "p":
		if p.detail.Poster == "" {
			return nil, false
		}
		// Largest variant: this opens in a browser, where there are no cells
		// to downscale to and the small file is just a worse picture.
		posterURL := p.detail.Poster
		if v := poster.Variants(posterURL, true); len(v) > 0 {
			posterURL = v[0]
		}
		if err := poster.OpenURL(posterURL); err != nil {
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

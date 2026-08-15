package main

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Shared context ────────────────────────────────────────────────────────────

// appCtx is the small pile of state every screen needs. A package-level
// pointer beats threading five fields through every constructor.
type appCtx struct {
	cfg    AppConfig
	refs   AddonList
	addons []Addon
	player     *Player
	downloader *Downloader
	prog       *tea.Program
}

var ctx *appCtx

// StreamAddons returns addons that can actually serve streams.
func (c *appCtx) StreamAddons() []Addon {
	var out []Addon
	for _, a := range c.addons {
		if a.Err == nil && a.HasStreams() {
			out = append(out, a)
		}
	}
	return out
}

// ── Screen interface ──────────────────────────────────────────────────────────

type screen interface {
	Init() tea.Cmd
	Update(tea.Msg) (screen, tea.Cmd)
	View() string
	Title() string  // breadcrumb
	Footer() string // key hints
	SetSize(w, h int)
	Typing() bool // suppress global single-key bindings
}

// backHandler lets a screen consume the global back key for its own internal
// navigation instead of being popped off the stack.
type backHandler interface {
	HandleBack() bool
}

// baseScreen supplies the boring half of the interface.
type baseScreen struct{ w, h int }

func (b *baseScreen) SetSize(w, h int) { b.w, b.h = w, h }
func (b *baseScreen) Typing() bool     { return false }
func (b *baseScreen) Footer() string   { return "" }

// ── Navigation messages ───────────────────────────────────────────────────────

type pushMsg struct{ s screen }
type popMsg struct{ n int }
type popRootMsg struct{}
type replaceMsg struct{ s screen }
type toastMsg struct {
	text  string
	isErr bool
}
type toastExpireMsg struct{ at time.Time }
type reloadAddonsMsg struct{}

// themeChangedMsg forces every screen to re-render its rows.
//
// Screens bake styled text into Item.Label when they build their list, so
// swapping the accent styles doesn't touch strings that were rendered
// earlier — the menu would keep its old colour until you navigated away and
// back. Section headers updated immediately because those are rendered live
// in the row painter, which is what made the inconsistency visible.
type themeChangedMsg struct{}

func themeChanged() tea.Cmd { return func() tea.Msg { return themeChangedMsg{} } }

// rebuildable is any screen that can regenerate its rows on demand.
type rebuildable interface{ rebuild() }

func push(s screen) tea.Cmd    { return func() tea.Msg { return pushMsg{s} } }
func pop() tea.Cmd             { return func() tea.Msg { return popMsg{1} } }
func popN(n int) tea.Cmd       { return func() tea.Msg { return popMsg{n} } }
func popRoot() tea.Cmd         { return func() tea.Msg { return popRootMsg{} } }
func replaceTop(s screen) tea.Cmd { return func() tea.Msg { return replaceMsg{s} } }
func toast(s string) tea.Cmd   { return func() tea.Msg { return toastMsg{text: s} } }
func toastErr(s string) tea.Cmd { return func() tea.Msg { return toastMsg{text: s, isErr: true} } }

// ── Root model ────────────────────────────────────────────────────────────────

type app struct {
	stack  []screen
	w, h   int
	pstate PlayerState

	toastText string
	toastErr  bool
	toastAt   time.Time

	startup tea.Cmd
}

func newApp(root screen) *app { return &app{stack: []screen{root}} }

// startup runs once the program is up: used to jump straight to a search or a
// catalog from the command line.
func (a *app) setStartup(cmd tea.Cmd) { a.startup = cmd }

func (a *app) top() screen { return a.stack[len(a.stack)-1] }

func (a *app) Init() tea.Cmd {
	if a.startup != nil {
		return tea.Batch(a.top().Init(), a.startup)
	}
	return a.top().Init()
}

func (a *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {

	case tea.WindowSizeMsg:
		a.w, a.h = m.Width, m.Height
		a.resize()
		// Screens may need to react — the info pane opens itself on a wide
		// enough terminal and then wants meta for the highlighted row.
		next, cmd := a.top().Update(msg)
		a.stack[len(a.stack)-1] = next
		return a, cmd

	case PlayerStateMsg:
		a.pstate = m.State
		a.resize()
		// Fall through to the top screen as well: episode lists and the main
		// menu redraw their progress readouts off this.
		next, cmd := a.top().Update(msg)
		a.stack[len(a.stack)-1] = next
		return a, cmd

	case PlayerNoticeMsg:
		return a, toast(m.Text)

	case DownloadTickMsg:
		next, cmd := a.top().Update(msg)
		a.stack[len(a.stack)-1] = next
		a.resize()
		return a, cmd

	case DownloadDoneMsg:
		if m.Err != nil {
			return a, toastErr("download failed — " + m.Err.Error())
		}
		return a, toast("downloaded " + m.Label)

	case PrefetchNextMsg:
		return a, a.advanceTo(m.Prev, "next up — ")

	case EpisodeEndedMsg:
		return a, a.advanceTo(m.Prev, "next up — ")

	case PlayerErrMsg:
		if m.Err != nil {
			return a, toastErr(m.Err.Error())
		}
		return a, nil

	case toastMsg:
		a.toastText, a.toastErr, a.toastAt = m.text, m.isErr, time.Now()
		at := a.toastAt
		a.resize()
		return a, tea.Tick(4*time.Second, func(time.Time) tea.Msg {
			return toastExpireMsg{at: at}
		})

	case toastExpireMsg:
		if m.at.Equal(a.toastAt) {
			a.toastText = ""
			a.resize()
		}
		return a, nil

	case pushMsg:
		m.s.SetSize(a.w, a.bodyHeight())
		a.stack = append(a.stack, m.s)
		return a, m.s.Init()

	case popMsg:
		for i := 0; i < m.n && len(a.stack) > 1; i++ {
			a.stack = a.stack[:len(a.stack)-1]
		}
		a.resize()
		return a, a.refreshTop()

	case popRootMsg:
		a.stack = a.stack[:1]
		a.resize()
		return a, a.refreshTop()

	case replaceMsg:
		m.s.SetSize(a.w, a.bodyHeight())
		a.stack[len(a.stack)-1] = m.s
		return a, m.s.Init()

	case themeChangedMsg:
		for _, sc := range a.stack {
			if r, ok := sc.(rebuildable); ok {
				r.rebuild()
			}
		}
		return a, nil

	case reloadAddonsMsg:
		ctx.refs = LoadAddonRefs()
		ctx.addons = LoadAddons(ctx.refs)
		return a, nil

	case tea.KeyMsg:
		if cmd, handled := a.globalKey(m); handled {
			return a, cmd
		}
	}

	next, cmd := a.top().Update(msg)
	a.stack[len(a.stack)-1] = next
	return a, cmd
}

func (a *app) globalKey(k tea.KeyMsg) (tea.Cmd, bool) {
	if k.String() == "ctrl+c" {
		return tea.Quit, true
	}
	if a.top().Typing() {
		return nil, false
	}

	switch k.String() {
	case "b":
		// Back, from anywhere. Typing() above keeps this out of text fields.
		//
		// A screen with its own internal hierarchy — the folder browser — gets
		// first refusal, so `b` walks up a directory before leaving.
		if bh, ok := a.top().(backHandler); ok && bh.HandleBack() {
			return nil, true
		}
		if len(a.stack) > 1 {
			return pop(), true
		}
	case "ctrl+q":
		return quitCmd(), true
	// Pause and seek are deliberately absent: mpv already owns those, and
	// having two sets of bindings for the same thing just invites drift.
	case "X":
		if a.pstate.Alive {
			return ctx.player.Stop(), true
		}
	}
	return nil, false
}

// advanceTo moves on to the next episode.
//
// It only takes over the screen if you're already sitting on the stream list
// for the episode that just played — otherwise you'd get yanked out of the
// menu, or out of a completely different show, mid-browse. When you're
// elsewhere the streams are fetched in the background instead, so they're
// warm when you do go looking, and the menu's "next up" entry points at them.
func (a *app) advanceTo(prev PlayRequest, prefix string) tea.Cmd {
	t, ok := nextEpisodeTarget(prev)
	if !ok {
		return toast("finished — " + prev.Label)
	}

	if cur, isStreams := a.top().(*streamScreen); isStreams && cur.target.VideoID == prev.VideoID {
		return tea.Sequence(
			toast(prefix+t.Label),
			replaceTop(newStreamScreen(t)),
		)
	}

	addons := ctx.StreamAddons()
	videoID := t.VideoID
	go GetStreams(addons, "series", videoID) // warm the cache, don't navigate

	return toast(prefix + t.Label + " · ready when you are")
}

// refreshTop rebuilds whichever screen we've just come back to.
//
// Screens cache their rows, and only the top screen receives messages — so a
// buried screen misses everything that happened while it was buried. The main
// menu is the obvious casualty: it sits at the bottom of the stack from
// startup, so its continue-watching entry stayed frozen at whatever it said
// when the app launched, even though history was being written correctly all
// along.
func (a *app) refreshTop() tea.Cmd {
	if r, ok := a.top().(rebuildable); ok {
		r.rebuild()
	}
	return nil
}

// quitCmd confirms first when something is playing, since quitting takes mpv
// with it under the default settings.
func quitCmd() tea.Cmd {
	if !ctx.player.State().Alive {
		return tea.Quit
	}
	msg := "stop watching and quit?"
	noLabel := "keep watching"
	if !ctx.cfg.CloseMpvOnExit {
		msg = "quit? mpv will keep playing."
		noLabel = "stay"
	}
	return push(newChoice("quit", msg, "quit", noLabel,
		func() tea.Cmd { return tea.Quit },
		nil,
	))
}

// ── Layout ────────────────────────────────────────────────────────────────────

// chromeLayout decides what fits.
//
// The body used to be floored at 3 rows, which meant View emitted more lines
// than the terminal had on a short window: the alt screen scrolled and the
// header disappeared off the top with no way back. Nothing can be floored —
// if the space isn't there, something has to go.
type chromeLayout struct {
	compact    bool // title only, no rules or blank line
	showRule   bool // divider above the footer
	showToast    bool
	showPlayer   bool
	showDownload bool
	body         int
}

func (c chromeLayout) headerLines() int {
	if c.compact {
		return 1
	}
	return 4 // rule, title, rule, blank
}

func (c chromeLayout) footerLines() int {
	n := 1 // key hints
	if c.showRule {
		n++
	}
	if c.showToast {
		n++
	}
	if c.showPlayer {
		n += 2 // now-playing line + progress bar
	}
	if c.showDownload {
		n++
	}
	return n
}

// chrome drops decoration until the body has at least one row, in order of
// what's least painful to lose.
func (a *app) chrome() chromeLayout {
	c := chromeLayout{
		showRule:     true,
		showToast:    a.toastText != "",
		showPlayer:   a.pstate.Alive,
		showDownload: ctx != nil && ctx.downloader != nil && ctx.downloader.Pending() > 0,
	}

	for range 5 {
		if a.h >= c.headerLines()+c.footerLines()+1 {
			break
		}
		switch {
		case c.showDownload:
			c.showDownload = false
		case c.showPlayer:
			c.showPlayer = false
		case c.showToast:
			c.showToast = false
		case !c.compact:
			c.compact = true
		case c.showRule:
			c.showRule = false
		}
	}

	c.body = max(1, a.h-c.headerLines()-c.footerLines())
	return c
}

func (a *app) bodyHeight() int { return a.chrome().body }

func (a *app) resize() {
	bh := a.bodyHeight()
	for _, s := range a.stack {
		s.SetSize(a.w, bh)
	}
}

func (a *app) View() string {
	if a.w == 0 {
		return "\n  starting…\n"
	}

	c := a.chrome()
	var b strings.Builder

	// Header
	crumbs := []string{stBrand.Render("stremio-cli") + stTitle.Render("uwu")}
	for _, s := range a.stack[1:] {
		if t := s.Title(); t != "" {
			crumbs = append(crumbs, stTitle.Render(t))
		}
	}
	title := " " + strings.Join(crumbs, stCrumb.Render(" › "))

	if c.compact {
		b.WriteString(clamp(title, a.w) + "\n")
	} else {
		bar := stRule.Render(strings.Repeat("▓", a.w))
		b.WriteString(bar + "\n" + clamp(title, a.w) + "\n" + bar + "\n\n")
	}

	// Body, padded to exactly c.body lines
	lines := strings.Split(a.top().View(), "\n")
	if len(lines) > c.body {
		lines = lines[:c.body]
	}
	b.WriteString(strings.Join(lines, "\n"))
	b.WriteString(strings.Repeat("\n", c.body-len(lines)+1))

	// Footer
	if c.showRule {
		b.WriteString(rule(a.w) + "\n")
	}

	hints := a.top().Footer()
	if a.pstate.Alive {
		hints += keyHint([2]string{"X", "stop mpv"})
	}
	b.WriteString(clamp(hints, a.w))

	if c.showToast {
		st := stToast
		if a.toastErr {
			st = stErr
		}
		b.WriteString("\n " + clamp(st.Render(a.toastText), a.w-1))
	}

	if c.showDownload {
		b.WriteString("\n" + a.downloadBar())
	}
	if c.showPlayer {
		b.WriteString("\n" + a.playerBar())
	}
	return b.String()
}

// downloadBar is a single line: enough to know something's happening and
// roughly how far along, without leaving the screen you're on.
func (a *app) downloadBar() string {
	d, ok := ctx.downloader.Active()
	if !ok {
		return " " + stHint.Render(fmt.Sprintf("%d download(s) queued", ctx.downloader.Pending()))
	}

	right := fmt.Sprintf("%s / %s", fmtBytes(d.Done), fmtBytes(d.Total))
	if d.Speed > 0 {
		right += "  " + fmtBytes(d.Speed) + "/s"
	}
	if n := ctx.downloader.Pending(); n > 1 {
		right = fmt.Sprintf("+%d  ", n-1) + right
	}

	left := " " + stKey.Render("↓") + " " + stSelected.Render(d.Label)
	pad := a.w - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if pad < 1 {
		left = clamp(left, a.w-lipgloss.Width(right)-2)
		pad = max(1, a.w-lipgloss.Width(left)-lipgloss.Width(right)-1)
	}
	return left + strings.Repeat(" ", pad) + stSub.Render(right) + cReset
}

func (a *app) playerBar() string {
	s := a.pstate

	glyph := stBarFill.Render("▶")
	switch {
	case s.Loading:
		glyph = stWarn.Render("◌")
	case s.Buffering:
		glyph = stWarn.Render("⣾")
	case s.Paused:
		glyph = stWarn.Render("❚❚")
	}

	label := s.Label
	if label == "" {
		label = s.Title
	}

	right := progressGlyph(s.Pos, s.Duration)
	if s.QueueLen > 1 {
		right = stHint.Render(itoa(s.QueuePos)+"/"+itoa(s.QueueLen)) + "  " + right
	}

	left := " " + glyph + " " + stSelected.Render(label)
	pad := a.w - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if pad < 1 {
		left = clamp(left, a.w-lipgloss.Width(right)-2)
		pad = max(1, a.w-lipgloss.Width(left)-lipgloss.Width(right)-1)
	}

	line := left + strings.Repeat(" ", pad) + stSub.Render(right)
	return line + "\n" + progressBar(s.Frac(), a.w)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	p := len(buf)
	for i > 0 {
		p--
		buf[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		buf[p] = '-'
	}
	return string(buf[p:])
}

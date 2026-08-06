package main

import (
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
	player *Player
	prog   *tea.Program
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
type toastMsg struct {
	text  string
	isErr bool
}
type toastExpireMsg struct{ at time.Time }
type reloadAddonsMsg struct{}

func push(s screen) tea.Cmd    { return func() tea.Msg { return pushMsg{s} } }
func pop() tea.Cmd             { return func() tea.Msg { return popMsg{1} } }
func popN(n int) tea.Cmd       { return func() tea.Msg { return popMsg{n} } }
func popRoot() tea.Cmd         { return func() tea.Msg { return popRootMsg{} } }
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
}

func newApp(root screen) *app { return &app{stack: []screen{root}} }

func (a *app) top() screen { return a.stack[len(a.stack)-1] }

func (a *app) Init() tea.Cmd { return a.top().Init() }

func (a *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {

	case tea.WindowSizeMsg:
		a.w, a.h = m.Width, m.Height
		a.resize()
		return a, nil

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

	case PrefetchNextMsg:
		t, ok := nextEpisodeTarget(m.Prev)
		if !ok {
			return a, nil
		}
		return a, tea.Sequence(
			toast("next up — "+t.Label),
			push(newStreamScreen(t)),
		)

	case EpisodeEndedMsg:
		t, ok := nextEpisodeTarget(m.Prev)
		if !ok {
			return a, toast("finished — " + m.Prev.Label)
		}
		return a, tea.Sequence(
			toast("next up — "+t.Label),
			push(newStreamScreen(t)),
		)

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
		return a, nil

	case popRootMsg:
		a.stack = a.stack[:1]
		a.resize()
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

const headerLines = 4 // rule, title, rule, blank

func (a *app) footerLines() int {
	n := 2 // rule + hints
	if a.toastText != "" {
		n++
	}
	if a.pstate.Alive {
		n += 2 // now-playing line + progress bar
	}
	return n
}

func (a *app) bodyHeight() int {
	h := a.h - headerLines - a.footerLines()
	if h < 3 {
		h = 3
	}
	return h
}

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
	var b strings.Builder

	// Header
	b.WriteString(stRule.Render(strings.Repeat("▓", a.w)) + "\n")
	crumbs := []string{stBrand.Render("stremio-cli") + stTitle.Render("uwu")}
	for _, s := range a.stack[1:] {
		if t := s.Title(); t != "" {
			crumbs = append(crumbs, stTitle.Render(t))
		}
	}
	b.WriteString(" " + strings.Join(crumbs, stCrumb.Render(" › ")) + "\n")
	b.WriteString(stRule.Render(strings.Repeat("▓", a.w)) + "\n\n")

	// Body, padded to exactly bodyHeight lines
	body := a.top().View()
	lines := strings.Split(body, "\n")
	bh := a.bodyHeight()
	if len(lines) > bh {
		lines = lines[:bh]
	}
	b.WriteString(strings.Join(lines, "\n"))
	b.WriteString(strings.Repeat("\n", bh-len(lines)+1))

	// Footer
	b.WriteString(rule(a.w) + "\n")

	hints := a.top().Footer()
	if a.pstate.Alive {
		hints += keyHint([2]string{"X", "stop mpv"})
	}
	b.WriteString(clamp(hints, a.w))

	if a.toastText != "" {
		st := stToast
		if a.toastErr {
			st = stErr
		}
		b.WriteString("\n " + clamp(st.Render(a.toastText), a.w-1))
	}

	if a.pstate.Alive {
		b.WriteString("\n" + a.playerBar())
	}
	return b.String()
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

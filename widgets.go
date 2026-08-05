package main

import (
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// asyncID tags in-flight loads. Bubble Tea delivers messages to whatever screen
// is on top, so a load that finishes after you've navigated away would
// otherwise land on the wrong screen.
type asyncID int

var asyncSeq asyncID

func newAsyncID() asyncID { asyncSeq++; return asyncSeq }

// ── Busy indicator ────────────────────────────────────────────────────────────

type busy struct {
	sp   spinner.Model
	on   bool
	what string
}

func newBusy(what string) busy {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = stKey
	return busy{sp: sp, what: what, on: true}
}

func (b *busy) start(what string) tea.Cmd {
	b.what = what
	b.on = true
	return b.sp.Tick
}

func (b *busy) stop() { b.on = false }

func (b *busy) update(msg tea.Msg) tea.Cmd {
	if !b.on {
		return nil
	}
	if _, ok := msg.(spinner.TickMsg); !ok {
		return nil
	}
	var cmd tea.Cmd
	b.sp, cmd = b.sp.Update(msg)
	return cmd
}

func (b busy) view() string {
	return "  " + b.sp.View() + stHint.Render(" "+b.what)
}

// ── Prompt screen ─────────────────────────────────────────────────────────────

// promptScreen is a one-line text input pushed onto the stack. onDone runs with
// the entered value; returning nil just closes it.
type promptScreen struct {
	baseScreen
	title  string
	help   []string
	ti     textinput.Model
	onDone func(string) tea.Cmd
}

func newPrompt(title, placeholder, initial string, onDone func(string) tea.Cmd, help ...string) *promptScreen {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.SetValue(initial)
	ti.CursorEnd()
	ti.Prompt = "› "
	ti.PromptStyle = stKey
	ti.CharLimit = 512
	ti.Width = 60
	return &promptScreen{title: title, ti: ti, onDone: onDone, help: help}
}

func (s *promptScreen) Init() tea.Cmd   { return s.ti.Focus() }
func (s *promptScreen) Title() string   { return s.title }
func (s *promptScreen) Typing() bool    { return true }
func (s *promptScreen) Footer() string  { return keyHint([2]string{"enter", "confirm"}, [2]string{"esc", "cancel"}) }

func (s *promptScreen) SetSize(w, h int) {
	s.baseScreen.SetSize(w, h)
	s.ti.Width = max(20, w-8)
}

func (s *promptScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			return s, pop()
		case "enter":
			v := strings.TrimSpace(s.ti.Value())
			// Sequence, not Batch: Batch runs commands concurrently, so a
			// push from onDone could land before this pop and get torn
			// straight back off the stack.
			return s, tea.Sequence(pop(), s.onDone(v))
		}
	}
	var cmd tea.Cmd
	s.ti, cmd = s.ti.Update(msg)
	return s, cmd
}

func (s *promptScreen) View() string {
	var b strings.Builder
	b.WriteString("  " + s.ti.View() + "\n")
	for _, h := range s.help {
		b.WriteString("\n  " + stHint.Render(h))
	}
	return b.String()
}

// ── Confirm screen ────────────────────────────────────────────────────────────

type confirmScreen struct {
	baseScreen
	title    string
	message  string
	yesLabel string
	noLabel  string
	onYes    func() tea.Cmd
	onNo     func() tea.Cmd // nil = just close

	// defaultNo flips the default from yes, so enter picks "no" instead.
	defaultNo bool
}

func newConfirm(title, message string, onYes func() tea.Cmd) *confirmScreen {
	return &confirmScreen{title: title, message: message, onYes: onYes}
}

// newDestructive defaults to "no", so enter on a confirm that deletes
// something doesn't do the deleting.
func newDestructive(title, message, yesLabel string, onYes func() tea.Cmd) *confirmScreen {
	return &confirmScreen{
		title: title, message: message,
		yesLabel: yesLabel, noLabel: "cancel",
		onYes: onYes, defaultNo: true,
	}
}

// newChoice is a confirm where "no" is a real second action rather than a
// cancel — resume vs start over, say.
func newChoice(title, message, yesLabel, noLabel string, onYes, onNo func() tea.Cmd) *confirmScreen {
	return &confirmScreen{
		title: title, message: message,
		yesLabel: yesLabel, noLabel: noLabel,
		onYes: onYes, onNo: onNo,
	}
}

func (s *confirmScreen) Init() tea.Cmd  { return nil }
func (s *confirmScreen) Title() string  { return s.title }
func (s *confirmScreen) labels() (string, string) {
	yes, no := s.yesLabel, s.noLabel
	if yes == "" {
		yes = "yes"
	}
	if no == "" {
		no = "no"
	}
	return yes, no
}

func (s *confirmScreen) Footer() string {
	yes, no := s.labels()
	return keyHint(
		[2]string{"y", yes},
		[2]string{"n", no},
		[2]string{"enter", "default"},
		[2]string{"esc", "cancel"},
	)
}

// prompt renders "(Y/n)" with the default capitalised and highlighted, so the
// choice sits next to the question instead of only in the footer.
func (s *confirmScreen) prompt() string {
	y, n := "y", "n"
	if s.defaultNo {
		n = "N"
	} else {
		y = "Y"
	}

	yStyled, nStyled := stSub.Render(y), stSub.Render(n)
	if s.defaultNo {
		nStyled = stCursor.Render(n)
	} else {
		yStyled = stCursor.Render(y)
	}
	return stHint.Render("(") + yStyled + stHint.Render("/") + nStyled + stHint.Render(")")
}

func (s *confirmScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "y", "Y":
			return s, tea.Sequence(pop(), s.onYes())
		case "n", "N":
			if s.onNo != nil {
				return s, tea.Sequence(pop(), s.onNo())
			}
			return s, pop()
		case "enter":
			// Enter takes the capitalised option.
			if s.defaultNo {
				if s.onNo != nil {
					return s, tea.Sequence(pop(), s.onNo())
				}
				return s, pop()
			}
			return s, tea.Sequence(pop(), s.onYes())
		case "esc", "q":
			return s, pop()
		}
	}
	return s, nil
}

func (s *confirmScreen) View() string {
	yes, no := s.labels()

	line := "  " + stTitle.Render(s.message) + "   " + s.prompt()
	detail := "  " + stHint.Render("y = "+yes+"     n = "+no)

	return line + "\n\n" + detail
}

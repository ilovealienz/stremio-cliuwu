package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// listModel is the one list widget the whole TUI uses. Screens call Update
// first; it reports whether it consumed the key, and the screen handles the
// rest (enter, f, d, [, ] …).
type listModel struct {
	items  []Item
	view   []int // indices into items after filtering
	cursor int   // index into view
	offset int
	w, h   int

	ti     textinput.Model
	typing bool
	query  string

	// Numbered draws a [ n] index on each row and lets you jump by typing it.
	// Indices follow the visible list, so filtering renumbers.
	Numbered bool
	numBuf   string

	// Wrap rolls the cursor between the ends instead of stopping dead.
	Wrap bool

	Empty string
}

func newList() listModel {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.CharLimit = 64
	ti.PromptStyle = stKey
	return listModel{ti: ti, Empty: "nothing here", Wrap: true}
}

func (l *listModel) SetItems(items []Item) {
	l.items = items
	l.reindex()
	l.clampOffset()
	l.clampCursor()
}

func (l *listModel) SetSize(w, h int) {
	l.w, l.h = w, h
	l.clampOffset()
	l.clampCursor()
}

// clampOffset re-derives the page after anything that can change rows() —
// a resize, or the filter/jump prompt opening and closing.
func (l *listModel) clampOffset() { l.scrollIntoView() }

func (l *listModel) Typing() bool { return l.typing }

func (l *listModel) Query() string { return l.query }

// Selected returns the absolute index into items, or -1.
func (l *listModel) Selected() int {
	if !l.selectableAt(l.cursor) {
		return -1
	}
	return l.view[l.cursor]
}

func (l *listModel) SelectedItem() (Item, bool) {
	i := l.Selected()
	if i < 0 {
		return Item{}, false
	}
	return l.items[i], true
}

// Focus moves the cursor to an absolute item index.
func (l *listModel) Focus(abs int) {
	for vi, ai := range l.view {
		if ai == abs && l.items[ai].selectable() {
			l.cursor = vi
			l.scrollIntoView()
			return
		}
	}
}

func (l *listModel) reindex() {
	l.view = l.view[:0]
	q := strings.ToLower(strings.TrimSpace(l.query))
	for i, it := range l.items {
		if q == "" {
			l.view = append(l.view, i)
			continue
		}
		// Section labels only make sense against the full list.
		if it.Header {
			continue
		}
		hay := strings.ToLower(stripANSI(it.Label + " " + it.Sub + " " + it.Badge))
		if strings.Contains(hay, q) {
			l.view = append(l.view, i)
		}
	}
}

func (l *listModel) rows() int {
	n := l.h
	if l.typing || l.query != "" {
		n-- // filter line
	}
	if l.numBuf != "" {
		n-- // jump prompt
	}
	if n < 1 {
		n = 1
	}
	return n
}

func (l *listModel) clampCursor() {
	if l.cursor >= len(l.view) {
		l.cursor = len(l.view) - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
	if !l.selectableAt(l.cursor) {
		l.snapToSelectable(1)
	}
	l.scrollIntoView()
}

func (l *listModel) selectableAt(vi int) bool {
	if vi < 0 || vi >= len(l.view) {
		return false
	}
	return l.items[l.view[vi]].selectable()
}

// snapToSelectable walks in dir until it lands on a real row, then tries the
// other way if it runs off the end.
func (l *listModel) snapToSelectable(dir int) {
	for i := l.cursor; i >= 0 && i < len(l.view); i += dir {
		if l.selectableAt(i) {
			l.cursor = i
			return
		}
	}
	for i := l.cursor; i >= 0 && i < len(l.view); i -= dir {
		if l.selectableAt(i) {
			l.cursor = i
			return
		}
	}
}

// scrollIntoView snaps the window to the page containing the cursor.
//
// Discrete pages rather than a sliding window: rows never repeat between
// screens, positions are stable, and the last page shows only what's left
// instead of scrolling back to stay full.
func (l *listModel) scrollIntoView() {
	r := l.rows()
	if r < 1 || len(l.view) == 0 {
		l.offset = 0
		return
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
	if l.cursor >= len(l.view) {
		l.cursor = len(l.view) - 1
	}
	l.offset = (l.cursor / r) * r
}

func (l *listModel) move(d int) {
	if d == 0 {
		return
	}
	step := 1
	if d < 0 {
		step = -1
	}

	target := l.cursor + d
	switch {
	case target < 0:
		if l.Wrap && l.cursor == 0 && d == -1 {
			target = len(l.view) - 1
			step = -1
		} else {
			target = 0
		}
	case target >= len(l.view):
		if l.Wrap && l.cursor == len(l.view)-1 && d == 1 {
			target = 0
			step = 1
		} else {
			target = len(l.view) - 1
		}
	}
	l.cursor = target

	// Land on a real row, never a section label.
	if !l.selectableAt(l.cursor) {
		l.snapToSelectable(step)
	}
	l.scrollIntoView()
}

// Update returns (consumed, cmd).
func (l *listModel) Update(msg tea.Msg) (bool, tea.Cmd) {
	key, isKey := msg.(tea.KeyMsg)
	if !isKey {
		return false, nil
	}

	if l.typing {
		switch key.String() {
		case "esc":
			l.typing = false
			l.query = ""
			l.ti.SetValue("")
			l.ti.Blur()
			l.reindex()
			l.clampCursor()
			return true, nil
		case "enter":
			l.typing = false
			l.ti.Blur()
			return true, nil
		}
		var cmd tea.Cmd
		l.ti, cmd = l.ti.Update(msg)
		l.query = l.ti.Value()
		l.reindex()
		l.clampCursor()
		return true, cmd
	}

	// Numeric jump. Digits accumulate so you can reach 2- and 3-digit rows;
	// enter then plays whatever the cursor landed on, which keeps a single
	// confirm path for both numbers and arrows.
	if l.Numbered {
		k := key.String()
		if len(k) == 1 && k[0] >= '0' && k[0] <= '9' {
			l.pushDigit(k)
			return true, nil
		}
		if k == "backspace" && l.numBuf != "" {
			l.numBuf = l.numBuf[:len(l.numBuf)-1]
			if n, err := strconv.Atoi(l.numBuf); err == nil && n >= 1 && n <= len(l.view) {
				l.cursor = n - 1
				l.scrollIntoView()
			}
			return true, nil
		}
		if k != "enter" {
			l.numBuf = ""
		}
	}

	switch key.String() {
	case "up", "k", "ctrl+p":
		l.move(-1)
	case "down", "j", "ctrl+n":
		l.move(1)
	case "pgup", "left":
		l.page(-1)
	case "pgdown", "right":
		l.page(1)
	case "ctrl+u":
		l.move(-l.rows() / 2)
	case "ctrl+d":
		l.move(l.rows() / 2)
	case "home", "g":
		l.cursor = 0
		l.snapToSelectable(1)
		l.scrollIntoView()
	case "end", "G":
		l.cursor = len(l.view) - 1
		l.snapToSelectable(-1)
		l.scrollIntoView()
	case "/":
		l.typing = true
		l.ti.SetValue(l.query)
		l.ti.CursorEnd()
		return true, l.ti.Focus()
	case "esc":
		if l.numBuf != "" {
			l.numBuf = ""
			return true, nil
		}
		if l.query != "" {
			l.query = ""
			l.ti.SetValue("")
			l.reindex()
			l.clampCursor()
			return true, nil
		}
		return false, nil
	default:
		return false, nil
	}
	return true, nil
}

// page moves a whole screen. With a page-aligned viewport this is just a
// cursor jump — scrollIntoView works out which page that lands on.
func (l *listModel) page(dir int) {
	r := l.rows()
	if len(l.view) == 0 {
		return
	}

	target := l.cursor + dir*r
	if target < 0 {
		target = 0
	}
	if target > len(l.view)-1 {
		target = len(l.view) - 1
	}

	l.cursor = target
	if !l.selectableAt(l.cursor) {
		step := 1
		if dir < 0 {
			step = -1
		}
		l.snapToSelectable(step)
	}
	l.scrollIntoView()
}

// pushDigit appends to the jump buffer, restarting it when the running number
// would overshoot the list — so in a 30-row list, "4" then "5" means row 5,
// not a dead 45.
func (l *listModel) pushDigit(d string) {
	cand := l.numBuf + d
	n, err := strconv.Atoi(cand)
	if err != nil || n > len(l.view) {
		cand = d
		n, err = strconv.Atoi(cand)
		if err != nil {
			return
		}
	}
	if n < 1 || n > len(l.view) {
		return
	}
	l.numBuf = cand
	l.cursor = n - 1
	l.scrollIntoView()
}

// NumBuf exposes the pending jump digits so screens can echo them.
func (l *listModel) NumBuf() string { return l.numBuf }

func (l *listModel) ClearNum() { l.numBuf = "" }

// Status is a "3/48" style counter for the footer.
func (l *listModel) Status() string {
	if len(l.view) == 0 {
		return ""
	}
	s := fmt.Sprintf("%d/%d", l.cursor+1, len(l.view))
	if l.query != "" && len(l.view) != len(l.items) {
		s += fmt.Sprintf(" of %d", len(l.items))
	}
	if r := l.rows(); r > 0 && len(l.view) > r {
		pages := (len(l.view) + r - 1) / r
		s += fmt.Sprintf("  ·  page %d/%d", l.cursor/r+1, pages)
	}
	return s
}

func (l *listModel) View() string {
	l.clampOffset()
	var b strings.Builder

	if len(l.view) == 0 {
		b.WriteString(stHint.Render("  " + l.Empty))
		for i := 1; i < l.rows(); i++ {
			b.WriteString("\n")
		}
		if l.typing || l.query != "" {
			b.WriteString("\n  " + l.filterLine())
		}
		return b.String()
	}

	r := l.rows()
	end := min(l.offset+r, len(l.view))
	for i := l.offset; i < end; i++ {
		b.WriteString(l.row(i, i == l.cursor))
		if i < end-1 {
			b.WriteString("\n")
		}
	}
	for i := end - l.offset; i < r; i++ {
		b.WriteString("\n")
	}
	if l.typing || l.query != "" {
		b.WriteString("\n  " + l.filterLine())
	}
	if l.numBuf != "" {
		b.WriteString("\n  " + l.jumpLine())
	}
	return b.String()
}

// jumpLine echoes the digits typed so far. The footer counter alone was far
// too quiet to tell you the app was listening.
func (l *listModel) jumpLine() string {
	label, _ := l.SelectedItem()
	target := stripANSI(label.Label)
	if len(target) > 40 {
		target = target[:40] + "…"
	}
	return stKey.Render("go to #") + stCursor.Render(l.numBuf+"▌") +
		stHint.Render("   "+target) +
		stHint.Render("   enter=play  esc=cancel")
}

func (l *listModel) filterLine() string {
	if l.typing {
		return l.ti.View()
	}
	return stKey.Render("/") + stSub.Render(l.query) + stHint.Render("  (esc to clear)")
}

func (l *listModel) row(vi int, selected bool) string {
	it := l.items[l.view[vi]]

	if it.Header {
		if it.Label == "" {
			return ""
		}
		return " " + stSection.Render(it.Label)
	}

	marker := "  "
	if selected {
		marker = stCursor.Render("▌ ")
	}

	num := ""
	if l.Numbered {
		num = stKey.Render(fmt.Sprintf("%3d ", vi+1))
		if selected {
			num = stCursor.Render(fmt.Sprintf("%3d ", vi+1))
		}
	}

	tick := "  "
	if it.Watched {
		tick = good("✓") + " "
	}

	label := it.Label
	if it.Dim {
		label = grey(stripANSI(label))
	}

	left := marker + num + tick + label
	if it.Sub != "" {
		left += "  " + stSub.Render(stripANSI(it.Sub))
	}

	badge := it.Badge
	pad := l.w - lipgloss.Width(left) - lipgloss.Width(badge) - 2
	if pad < 1 {
		// No room for both — drop the badge before mangling the label.
		return clamp(left, l.w-1)
	}
	return left + strings.Repeat(" ", pad) + badge
}

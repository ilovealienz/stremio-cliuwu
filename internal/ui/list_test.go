package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Backspace in a number jump lands on the numbered row, past any headers.
func TestJumpBackspaceSkipsHeaders(t *testing.T) {
	l := newList()
	l.Numbered = true
	l.SetItems([]Item{{Label: "Season 1", Header: true}, {Label: "a"}, {Label: "b"}, {Label: "Season 2", Header: true}, {Label: "c"}})
	press := func(k tea.KeyMsg) { l.Update(k) }
	press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")}) // "12" is out of range, so this is row 2
	press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	if l.numBuf != "3" || l.cursor != 4 {
		t.Fatalf("after 3: buf=%q cursor=%d", l.numBuf, l.cursor)
	}
	l.numBuf = "21"
	press(tea.KeyMsg{Type: tea.KeyBackspace})
	if l.numBuf != "2" || l.cursor != 2 {
		t.Fatalf("after backspace to 2: cursor=%d, want 2 (row b)", l.cursor)
	}
}

func TestCommonPrefixWholeCharacters(t *testing.T) {
	if got := commonPrefix("テストあ", "テストい"); got != "テスト" {
		t.Fatalf("got %q", got)
	}
}

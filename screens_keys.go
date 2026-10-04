package main

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// The ? overlay: every key the screen behind it offers, including the ones
// the footer had no room for.
//
// Built from the hints that screen already declares, so there is no second
// list to fall out of step with the first. The globals are appended here
// rather than by each screen, since that is where they are added to the
// footer too.

// keyDetail expands a footer label into a sentence, for the ? list only.
//
// The footer has no room for these — "enter=play this stream" would crowd out
// the next three keys — but a list has nothing but room, and "jump" on its
// own says nothing about what jumps where.
//
// Keyed by the pair the screen declared, since the same key means different
// things on different screens. Anything missing falls back to its short
// label, so a new key is never wrong here, only terse.
var keyDetail = map[string]string{
	"0-9|jump to row":            "type a row number to jump straight to it",
	"/|filter":                   "filter the list as you type",
	"enter|play":                 "play the highlighted stream",
	"enter|streams":              "list streams for this episode",
	"enter|episodes":             "list episodes in this season",
	"enter|open":                 "open the highlighted item",
	"enter|resume":               "carry on from where you left off",
	"|resume from the panel":     "carry on with one of the shows listed on the right",
	"enter|try":                  "load this subtitle, keeping the list open",
	"enter|browse":               "browse this catalogue",
	"enter|choose":               "use this one",
	"enter|edit / toggle":        "change this setting",
	"B|show blocked results":     "also show results your block list hides",
	"D|download":                 "download the highlighted stream",
	"D|clear all":                "wipe the whole list",
	"A|download everything here": "queue every file in this folder",
	"w|mark watched":             "mark this episode watched, or unwatched",
	"w|mark season watched":      "mark every episode up to this one",
	"W|mark whole season":        "mark the entire season watched",
	"r|reverse order":            "flip the list end to end",
	"r|refresh list":             "fetch the list again",
	"R|refetch streams":          "ask the addons again, ignoring the cache",
	"R|rescan folder":            "look for downloads left on disk",
	"tab|filter by addon":        "cycle through the addons that returned results, shift+tab to go back",
	"tab|filter by language":     "cycle through the languages available, shift+tab to go back",
	"tab|filter by type":         "cycle between films, shows and anime, shift+tab to go back",
	"tab|episodes/titles":        "switch between one row per episode and one per show",
	"n|queue as next":            "play this after whatever is playing now",
	"[|previous episode":         "jump back an episode without leaving",
	"]|next episode":             "jump forward an episode without leaving",
	"i|toggle info":              "show or hide the side panel",
	"i|toggle panel":             "show or hide the side panel",
	"p|open full poster":         "open the full size poster in a browser",
	"I|imdb":                     "open this title on imdb",
	"f|favourite":                "add or remove from favourites",
	"f|favourite show":           "add or remove the whole show",
	"f|favourite season":         "add or remove this season",
	"g|pick genre":               "narrow the catalogue to one genre",
	"s|change sort":              "cycle how the list is ordered",
	"F|flat or folders":          "show the folder tree, or every file at once",
	"d|remove entry":             "remove this entry",
	"a|add addon":                "paste an addon manifest url",
	"t|enable / disable":         "turn this addon on or off",
	"J/K|move up / down":         "reorder — addons higher up win",
	"J/K|scroll the panel":       "scroll the side panel",
	"x|cancel download":          "stop this download and discard it",
	"C|clear finished":           "remove finished downloads from the list",
	"0-9|jump":                   "type a row number to jump straight to it",
	"b/esc|back":                 "go back a screen",
	"ctrl+q|quit":                "leave the app",
	"?|this list":                "you are here",
	"S|subtitles":                "pick subtitles for what is playing",
	"X|stop mpv":                 "stop playback and close mpv",
	"?|close":                    "close this list",
	"u|open the release":         "open the new release on github",
	"enter|default":              "go ahead with the highlighted choice",
	"esc|cancel":                 "leave without changing anything",
}

// detail is the long description for a pair, or its short label.
//
// Falls back to matching the label alone, for keys built at runtime: the
// menu's resume shortcut is "1-7" or "1-9" depending on how much history
// there is, so no fixed lookup can ever match it.
func detail(k, short string) string {
	if d, ok := keyDetail[k+"|"+short]; ok {
		return d
	}
	if d, ok := keyDetail["|"+short]; ok {
		return d
	}
	return short
}

type keysScreen struct {
	baseScreen
	of    string
	pairs [][2]string
	list  listModel
}

func newKeysScreen(of string, pairs [][2]string) *keysScreen {
	l := newList()
	l.Empty = "no keys on this screen"

	s := &keysScreen{of: of, pairs: pairs, list: l}
	s.rebuild()
	return s
}

// rebuild lays the keys out as list rows.
//
// A list rather than a block of text: sixteen keys plus the app's own header
// and footer overflow a short terminal, and a reference you cannot scroll is
// no use at the moment you need it. Every row is a header, so nothing is
// selectable — there is nothing here to pick.
func (s *keysScreen) rebuild() {
	w := 0
	for _, p := range s.pairs {
		if n := len(p[0]); n > w {
			w = n
		}
	}

	items := make([]Item, 0, len(s.pairs))
	for _, p := range s.pairs {
		items = append(items, Item{
			Label:  stKey.Render(padKey(p[0], w)) + "   " + stHint.Render(detail(p[0], p[1])),
			Header: true,
		})
	}
	s.list.SetItems(items)
}

func (s *keysScreen) Init() tea.Cmd { return nil }

func (s *keysScreen) SetSize(w, h int) {
	s.baseScreen.SetSize(w, h)
	s.list.SetSize(w, h-1)
}

func (s *keysScreen) Title() string { return "keys" }

func (s *keysScreen) Footer() string {
	return withStatus(s.list.Status(),
		keyHint([2]string{"?", "close"}, [2]string{"b/esc", "back"}))
}

func (s *keysScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		if consumed, cmd := s.list.Update(msg); consumed {
			return s, cmd
		}
		switch k.String() {
		case "?", "esc", "backspace", "q":
			return s, pop()
		}
	}
	return s, nil
}

func (s *keysScreen) View() string {
	head := ""
	if s.of != "" {
		head = "  " + stSub.Render(s.of) + "\n"
	}
	return head + s.list.View()
}

// pad right-aligns a key into a column of width w.
func padKey(s string, w int) string {
	if n := w - len(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

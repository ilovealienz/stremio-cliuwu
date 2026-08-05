package main

import (
	"fmt"
	"net/url"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Catalogs of type "other" are debrid libraries — DMM, Torrentio's cloud
// listing. Their meta objects carry a `videos` array where each entry is a
// file inside a torrent, with its stream URL already attached.
//
// This used to be library.go: a parallel set of types, fetchers and three
// screens that duplicated the catalog layer. Now that browsing is addon
// driven, an "other" catalog is just a catalog, and only the leaf differs.

type FileVideo struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Streams []struct {
		URL string `json:"url"`
	} `json:"streams"`
}

func (v FileVideo) URL() string {
	if len(v.Streams) == 0 {
		return ""
	}
	return v.Streams[0].URL
}

// FetchFiles pulls the file list for one catalog item.
func FetchFiles(m Meta) ([]FileVideo, error) {
	base := m.Base
	if base == "" {
		return nil, fmt.Errorf("no addon recorded for %s", m.Name)
	}
	mediaType := m.Type
	if mediaType == "" {
		mediaType = "other"
	}

	var resp struct {
		Meta struct {
			Videos []FileVideo `json:"videos"`
		} `json:"meta"`
	}
	u := fmt.Sprintf("%s/meta/%s/%s.json", base, mediaType, url.PathEscape(m.ID))
	if err := getJSON(u, &resp); err != nil {
		return nil, err
	}
	return resp.Meta.Videos, nil
}

// fileOf strips the leading folder from a "/Folder/file.mkv" title.
func fileOf(title string) string {
	s := strings.TrimPrefix(title, "/")
	if i := strings.Index(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func folderOf(title string) string {
	s := strings.TrimPrefix(title, "/")
	i := strings.Index(s, "/")
	if i < 0 {
		return ""
	}
	return s[:i]
}

// ── Screen ────────────────────────────────────────────────────────────────────

type filesMsg struct {
	id     asyncID
	videos []FileVideo
	err    error
}

type fileListScreen struct {
	baseScreen
	id     asyncID
	meta   Meta
	videos []FileVideo

	list   listModel
	busy   busy
	loaded bool
}

func newFileListScreen(m Meta) *fileListScreen {
	l := newList()
	l.Numbered = true
	l.Empty = "no playable files"
	return &fileListScreen{id: newAsyncID(), meta: m, list: l, busy: newBusy("loading files…")}
}

func (s *fileListScreen) Init() tea.Cmd {
	id, m := s.id, s.meta
	return tea.Batch(
		s.busy.start("loading files…"),
		func() tea.Msg {
			v, err := FetchFiles(m)
			return filesMsg{id: id, videos: v, err: err}
		},
	)
}

func (s *fileListScreen) Title() string { return s.meta.Name }
func (s *fileListScreen) Typing() bool  { return s.list.Typing() }

func (s *fileListScreen) SetSize(w, h int) {
	s.baseScreen.SetSize(w, h)
	s.list.SetSize(w, h)
}

func (s *fileListScreen) Footer() string {
	return keyHint(
		[2]string{"enter", "play"},
		[2]string{"0-9", "jump"},
		[2]string{"/", "filter"},
		[2]string{"b/esc", "back"},
	) + "   " + stHint.Render(s.list.Status())
}

func (s *fileListScreen) rebuild() {
	items := make([]Item, len(s.videos))
	for i, v := range s.videos {
		it := Item{Label: bold(fileOf(v.Title))}
		if f := folderOf(v.Title); f != "" {
			it.Sub = f
		}
		if v.URL() == "" {
			it.Badge = grey("no url")
			it.Dim = true
		}
		items[i] = it
	}
	s.list.SetItems(items)
}

func (s *fileListScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch m := msg.(type) {
	case filesMsg:
		if m.id != s.id {
			return s, nil
		}
		s.busy.stop()
		s.loaded = true
		if m.err != nil {
			return s, toastErr(m.err.Error())
		}
		s.videos = m.videos
		s.rebuild()
		return s, nil

	case tea.KeyMsg:
		if consumed, cmd := s.list.Update(msg); consumed {
			return s, cmd
		}
		switch m.String() {
		case "enter":
			i := s.list.Selected()
			if i < 0 {
				return s, nil
			}
			v := s.videos[i]
			if v.URL() == "" {
				return s, toastErr("no stream url for that file")
			}
			s.list.ClearNum()

			label := fileOf(v.Title)
			// No Cinemeta id here, so history keys off the file's own id.
			return s, tea.Batch(
				ctx.player.Play(PlayRequest{
					VideoID: v.ID,
					Label:   label,
					URL:     v.URL(),
					Entry: HistoryEntry{
						Name: label, ID: v.ID, Type: "movie",
						Source: s.meta.Source, VideoID: v.ID,
					},
				}),
				toast("loading "+label+"…"),
			)
		case "esc", "backspace":
			return s, pop()
		case "q":
			return s, popRoot()
		}
	}
	return s, s.busy.update(msg)
}

func (s *fileListScreen) View() string {
	if !s.loaded {
		return s.busy.view()
	}
	return s.list.View()
}

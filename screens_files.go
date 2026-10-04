package main

import (
	"fmt"
	"net/url"
	"sort"
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

// QueueEntry downloads every file in a library entry.
//
// Offered from the catalog list as well as from inside an entry: a library
// row is usually one whole pack, and having to open it just to press A is a
// step for nothing. Runs off the UI goroutine — it fetches before queueing.
func QueueEntry(m Meta) (string, bool) {
	if ctx.cfg.DownloadDir == "" {
		return "set a download location in settings first", false
	}

	videos, err := FetchFiles(m)
	if err != nil {
		return err.Error(), false
	}
	if len(videos) == 0 {
		return "no files in " + m.Name, false
	}

	// Same trimming the file list does, so a download started from either
	// place lands in the same layout.
	paths := make([]string, len(videos))
	for i, v := range videos {
		paths[i] = strings.TrimPrefix(v.Title, "/")
	}
	prefix := commonDirPrefix(paths)

	queued := 0
	for i, v := range videos {
		if v.URL() == "" {
			continue
		}
		rel := strings.TrimPrefix(paths[i], prefix)
		path := LibraryPath(ctx.cfg.DownloadDir, m.Name, rel)
		if _, ok := ctx.downloader.Add(baseName(rel), v.URL(), path); ok {
			queued++
		}
	}

	switch {
	case queued == 0:
		return "nothing new to queue", false
	case queued == 1:
		return "queued 1 file", true
	}
	return fmt.Sprintf("queued %d files", queued), true
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

// Paths in a torrent are arbitrarily deep — typically
// "<torrent name>/Season 01/Family Guy - S01E01.mp4". Treating only the first
// segment as "the folder" grouped everything under the torrent name, which is
// already the breadcrumb, and left "Season 01/" glued to every filename.

func baseName(p string) string {
	p = strings.TrimSuffix(p, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// commonDirPrefix finds the directory prefix shared by every path, so the
// torrent-name wrapper doesn't become a folder you have to click through.
func commonDirPrefix(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	dirsOf := func(p string) []string {
		parts := strings.Split(strings.Trim(p, "/"), "/")
		if len(parts) > 0 {
			parts = parts[:len(parts)-1] // drop the filename
		}
		return parts
	}

	common := dirsOf(paths[0])
	for _, p := range paths[1:] {
		parts := dirsOf(p)
		n := 0
		for n < len(common) && n < len(parts) && common[n] == parts[n] {
			n++
		}
		common = common[:n]
		if n == 0 {
			break
		}
	}
	if len(common) == 0 {
		return ""
	}
	return strings.Join(common, "/") + "/"
}

// ── Screen ────────────────────────────────────────────────────────────────────

type filesMsg struct {
	id     asyncID
	videos []FileVideo
	err    error
}

// sort modes for the file list
const (
	sortAddon = iota // whatever order the addon returned
	sortName         // natural A→Z: S01E02 before S01E10
	sortNameDesc
)

var sortNames = map[int]string{
	sortAddon:    "addon order",
	sortName:     "name",
	sortNameDesc: "name ↓",
}

type fileListScreen struct {
	baseScreen
	id     asyncID
	meta   Meta
	videos []FileVideo

	rel    []string // paths with the shared prefix stripped, index-aligned
	dir    string   // directory currently being shown, "" = root
	flat   bool     // show every file with its full relative path instead
	nested bool     // true when there are subdirectories to descend into

	sortMode int
	rows     []fileRow

	list   listModel
	busy   busy
	loaded bool
}

func newFileListScreen(m Meta) *fileListScreen {
	l := newList()
	l.Numbered = true
	l.Empty = "no playable files"
	return &fileListScreen{
		id: newAsyncID(), meta: m, list: l,
		busy: newBusy("loading files…"),
		// Season packs come back in whatever order the tracker stored them,
		// so default to sorted rather than making you hunt.
		sortMode: sortName,
	}
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
	s.list.SetSize(w, h-1)
}

func (s *fileListScreen) Footer() string {
	pairs := [][2]string{
		{"enter", "play"},
		{"/", "filter"},
		{"0-9", "jump to row"},
	}
	if ctx.player.State().Alive {
		pairs = append(pairs, [2]string{"n", "queue as next"})
	}
	pairs = append(pairs,
		[2]string{"D", "download"},
		[2]string{"A", "download everything here"},
		[2]string{"s", "change sort"})
	if s.nested {
		pairs = append(pairs, [2]string{"F", "flat or folders"})
	}
	pairs = append(pairs, [2]string{"/", "filter"}, [2]string{"b/esc", "back"})
	return withStatus(s.list.Status(), keyHint(pairs...))
}

// fileRow is one visible line: either a subdirectory or a playable file.
type fileRow struct {
	name  string
	dir   bool
	count int // files beneath, for directories
	idx   int // index into videos, for files
}

// request builds the play request for a library file. Shared by play and
// queue so the two can't record different history for the same file.
// request builds the play request for the file at index idx in s.videos.
//
// History records the entry as the title and the file as one item under it,
// rather than filing every file as its own film. A season pack then appears
// once with twelve items instead of twelve unrelated "films", and clearing it
// is one action.
//
// The index doubles as the episode number: files here have no season or
// episode of their own, and without something to tell them apart they'd all
// collide on the same key under the entry.
func (s *fileListScreen) request(idx int) PlayRequest {
	v := s.videos[idx]
	file := baseName(v.Title)

	name := s.meta.Name
	if name == "" {
		name = file
	}

	return PlayRequest{
		VideoID:   v.ID,
		MediaType: "other",
		Label:     file,
		URL:       v.URL(),
		Entry: HistoryEntry{
			Name: name, ID: s.meta.ID, Type: "other",
			Source: s.meta.Source, Year: s.meta.Year,
			Episode: idx + 1, EpTitle: file,
			EpisodeTotal: len(s.videos),
			VideoID:      v.ID,
		},
	}
}

// queueNext lines a file up to follow whatever is playing — the same n as on
// the stream picker. A season pack is the case it exists for: you're on
// episode one and want the next without going anywhere.
func (s *fileListScreen) queueNext(idx int) tea.Cmd {
	if !ctx.player.State().Alive {
		return toastErr("nothing is playing to queue behind")
	}
	if s.videos[idx].URL() == "" {
		return toastErr("no stream url for that file")
	}

	req := s.request(idx)
	if q := ctx.player.Queued(); q != nil && q.VideoID == req.VideoID {
		ctx.player.Unqueue()
		return tea.Batch(toast("removed from up next"), playerStateCmd())
	}

	ctx.player.Queue(req)
	return tea.Batch(toast("up next — "+req.Label), playerStateCmd())
}

// download queues a file, or every file beneath a directory.
//
// Whole-folder is the point of this on a library entry: a season pack is one
// row with twelve files under it, and queueing them one at a time is the
// thing you'd want the app to do for you.
func (s *fileListScreen) download(row fileRow) tea.Cmd {
	var picked []int
	if row.dir {
		prefix := s.dir + row.name + "/"
		for i, rel := range s.rel {
			if strings.HasPrefix(rel, prefix) {
				picked = append(picked, i)
			}
		}
	} else {
		picked = []int{row.idx}
	}
	return s.queue(picked)
}

// queue adds the chosen files, skipping anything already downloaded or
// waiting.
func (s *fileListScreen) queue(picked []int) tea.Cmd {
	if ctx.cfg.DownloadDir == "" {
		return toastErr("set a download location in settings first")
	}

	queued, skipped := 0, 0
	for _, i := range picked {
		v := s.videos[i]
		if v.URL() == "" {
			skipped++
			continue
		}
		path := LibraryPath(ctx.cfg.DownloadDir, s.meta.Name, s.rel[i])
		if _, ok := ctx.downloader.Add(baseName(s.rel[i]), v.URL(), path); ok {
			queued++
		} else {
			skipped++
		}
	}

	switch {
	case queued == 0 && skipped > 0:
		return toastErr("nothing new to queue")
	case queued == 0:
		return toastErr("no files there")
	case queued == 1:
		return toast("queued " + baseName(s.rel[picked[0]]))
	}
	return toast(fmt.Sprintf("queued %d files", queued))
}

// downloadAll queues everything at or below the current directory.
//
// D covers a folder row, but a pack with no subfolders has none to press it
// on — and neither does the root. This is the "all of it" case, which for a
// season pack is usually what you meant.
func (s *fileListScreen) downloadAll() tea.Cmd {
	var picked []int
	for i, rel := range s.rel {
		if strings.HasPrefix(rel, s.dir) {
			picked = append(picked, i)
		}
	}
	return s.queue(picked)
}

func (s *fileListScreen) rebuild() {
	s.rows = s.rows[:0]

	if s.flat {
		for i := range s.videos {
			s.rows = append(s.rows, fileRow{name: s.rel[i], idx: i})
		}
	} else {
		seen := map[string]int{}
		var dirs []string
		for i, rel := range s.rel {
			if !strings.HasPrefix(rel, s.dir) {
				continue
			}
			rest := rel[len(s.dir):]
			if j := strings.Index(rest, "/"); j >= 0 {
				name := rest[:j]
				if _, ok := seen[name]; !ok {
					dirs = append(dirs, name)
				}
				seen[name]++
				continue
			}
			s.rows = append(s.rows, fileRow{name: rest, idx: i})
		}

		// Directories first, then files.
		sort.SliceStable(dirs, func(a, b int) bool { return naturalLess(dirs[a], dirs[b]) })
		dirRows := make([]fileRow, 0, len(dirs))
		for _, d := range dirs {
			dirRows = append(dirRows, fileRow{name: d, dir: true, count: seen[d]})
		}
		s.rows = append(dirRows, s.rows...)
	}

	// Sort the file rows, leaving directories pinned at the top.
	firstFile := 0
	for firstFile < len(s.rows) && s.rows[firstFile].dir {
		firstFile++
	}
	files := s.rows[firstFile:]
	switch s.sortMode {
	case sortName:
		sort.SliceStable(files, func(a, b int) bool { return naturalLess(files[a].name, files[b].name) })
	case sortNameDesc:
		sort.SliceStable(files, func(a, b int) bool { return naturalLess(files[b].name, files[a].name) })
	}

	items := make([]Item, len(s.rows))
	for i, r := range s.rows {
		if r.dir {
			items[i] = Item{
				Label: bold(accent(r.name + "/")),
				Badge: grey(fmt.Sprintf("%d files", r.count)),
			}
			continue
		}
		it := Item{Label: bold(baseName(r.name))}
		if s.flat {
			if d := strings.TrimSuffix(strings.TrimSuffix(r.name, baseName(r.name)), "/"); d != "" {
				it.Sub = d
			}
		}
		if s.videos[r.idx].URL() == "" {
			it.Badge = grey("no url")
			it.Dim = true
		}
		items[i] = it
	}
	s.list.SetItems(items)
}

// HandleBack lets the global back key walk up the tree first.
func (s *fileListScreen) HandleBack() bool { return s.up() }

// up moves out one directory. Reports false at the root.
func (s *fileListScreen) up() bool {
	if s.dir == "" {
		return false
	}
	trimmed := strings.TrimSuffix(s.dir, "/")
	if i := strings.LastIndex(trimmed, "/"); i >= 0 {
		s.dir = trimmed[:i+1]
	} else {
		s.dir = ""
	}
	s.rebuild()
	return true
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

		paths := make([]string, len(m.videos))
		for i, v := range m.videos {
			paths[i] = strings.TrimPrefix(v.Title, "/")
		}
		prefix := commonDirPrefix(paths)

		s.rel = make([]string, len(paths))
		for i, p := range paths {
			s.rel[i] = strings.TrimPrefix(p, prefix)
			if strings.Contains(s.rel[i], "/") {
				s.nested = true
			}
		}

		s.rebuild()
		return s, nil

	case tea.KeyMsg:
		if consumed, cmd := s.list.Update(msg); consumed {
			return s, cmd
		}
		switch m.String() {
		case "s":
			s.sortMode = (s.sortMode + 1) % 3
			s.rebuild()
			return s, toast("sorted by " + sortNames[s.sortMode])

		case "F":
			if s.nested {
				s.flat = !s.flat
				s.dir = ""
				s.rebuild()
				if s.flat {
					return s, toast("showing every file")
				}
				return s, toast("browsing folders")
			}

		case "enter":
			i := s.list.Selected()
			if i < 0 || i >= len(s.rows) {
				return s, nil
			}
			if r := s.rows[i]; r.dir {
				s.dir += r.name + "/"
				s.rebuild()
				return s, nil
			}
			v := s.videos[s.rows[i].idx]
			if v.URL() == "" {
				return s, toastErr("no stream url for that file")
			}
			s.list.ClearNum()

			req := s.request(s.rows[i].idx)
			return s, tea.Batch(ctx.player.Play(req), toast("loading "+req.Label+"…"))
		case "n":
			if i := s.list.Selected(); i >= 0 && i < len(s.rows) && !s.rows[i].dir {
				return s, s.queueNext(s.rows[i].idx)
			}
		case "D":
			if i := s.list.Selected(); i >= 0 && i < len(s.rows) {
				return s, s.download(s.rows[i])
			}
		case "A":
			return s, s.downloadAll()
		case "esc", "backspace", "b":
			// Walk back out of the folder tree before leaving the screen.
			if s.up() {
				return s, nil
			}
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
	where := "/"
	if s.dir != "" {
		where = "/" + s.dir
	}
	if s.flat {
		where = "all files"
	}
	head := "  " + stSub.Render(fmt.Sprintf("%d files · %s · ", len(s.videos), sortNames[s.sortMode])) +
		stKey.Render(where) + "\n"
	return head + s.list.View()
}

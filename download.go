package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Downloads run one at a time. Debrid providers cap concurrent connections
// per account, so parallel downloads mostly just make each other slower.

// downloadClient has no overall timeout — the shared httpClient caps requests
// at 15s, which is right for a JSON call and fatal for a 2GB file. Connection
// and header timeouts still apply, so a dead server won't hang forever.
// downloadUA matters: Go announces itself as "Go-http-client/1.1" by default,
// and plenty of CDNs reject anything that isn't a browser or media player.
// mpv plays these same links happily because ffmpeg sends its own agent — the
// 403 was the header, not the credentials.
const downloadUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/124.0 Safari/537.36"

var downloadClient = &http.Client{
	Transport: &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		ForceAttemptHTTP2:     true,
	},
}

type DownloadState int

const (
	DLQueued DownloadState = iota
	DLActive
	DLDone
	DLFailed
	DLCancelled
)

func (s DownloadState) String() string {
	switch s {
	case DLActive:
		return "downloading"
	case DLDone:
		return "done"
	case DLFailed:
		return "failed"
	case DLCancelled:
		return "cancelled"
	}
	return "queued"
}

type Download struct {
	ID    int
	Label string
	URL   string
	Path  string // final destination

	Total int64
	Done  int64
	Speed int64 // bytes/sec, rolling

	State  DownloadState
	Err    error
	Resume bool // picked up from a partial file
}

func (d Download) Frac() float64 {
	if d.Total <= 0 {
		return 0
	}
	return float64(d.Done) / float64(d.Total)
}

// ── Messages ──────────────────────────────────────────────────────────────────

type DownloadTickMsg struct{}
type DownloadDoneMsg struct {
	Label string
	Err   error
}

// ── Sidecar ───────────────────────────────────────────────────────────────────

// partInfo sits beside a .part file and records which URL produced it.
//
// Resuming against a different link would silently splice two different files
// together — debrid URLs expire and get reissued pointing at different
// releases, so matching on show and episode alone isn't enough.
type partInfo struct {
	URL   string `json:"url"`
	Total int64  `json:"total"`
}

func partPath(final string) string { return final + ".part" }
func infoPath(final string) string { return final + ".part.json" }

func readPartInfo(final string) (partInfo, bool) {
	var pi partInfo
	b, err := os.ReadFile(infoPath(final))
	if err != nil {
		return pi, false
	}
	if json.Unmarshal(b, &pi) != nil {
		return pi, false
	}
	return pi, true
}

func writePartInfo(final string, pi partInfo) {
	if b, err := json.Marshal(pi); err == nil {
		os.WriteFile(infoPath(final), b, 0644)
	}
}

// ── Naming ────────────────────────────────────────────────────────────────────

// winReserved are DOS device names that Windows still refuses as filenames.
// A name is reserved on its base alone, so NUL.txt and NUL.tar.gz are both
// the null device. Windows 11 relaxed most of this, but older versions
// haven't, and applying it everywhere keeps a synced download folder portable.
var winReserved = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com0": true, "com1": true, "com2": true, "com3": true, "com4": true,
	"com5": true, "com6": true, "com7": true, "com8": true, "com9": true,
	"lpt0": true, "lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true,
	"lpt5": true, "lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// safeName strips anything a filesystem would object to. The Windows set is
// the strictest, so use it everywhere and keep filenames portable.
func safeName(s string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '-'
		}
		if r < 32 {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	s = strings.Trim(s, ".") // leading/trailing dots are stripped by Windows
	if s == "" {
		return "untitled"
	}

	base := s
	if i := strings.IndexByte(base, '.'); i > 0 {
		base = base[:i]
	}
	if winReserved[strings.ToLower(base)] {
		s = "_" + s
	}
	return s
}

// extFromURL pulls a file extension off the URL, defaulting to .mkv.
func extFromURL(raw string) string {
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	ext := strings.ToLower(filepath.Ext(raw))
	switch ext {
	case ".mkv", ".mp4", ".avi", ".m4v", ".mov", ".ts", ".webm", ".wmv", ".flv":
		return ext
	}
	return ".mkv"
}

// Naming is pattern-driven so it can be changed without a rebuild.
// Placeholders: {title} {year} {show} {season} {episode}
//
// Either slash makes a folder separator. When "organise downloads" is off only the
// last segment is used, so one setting controls depth for both patterns.
const (
	DefaultMoviePattern   = "Movies/({year}) {title}"
	DefaultEpisodePattern = "{show}/Season {season}/{episode} {title}"
)

var spaceRun = regexp.MustCompile(`\s+`)

// tidySegment cleans up after a placeholder resolved to nothing — an absent
// year would otherwise leave "() Inception" behind.
func tidySegment(s string) string {
	for _, empty := range []string{"()", "[]", "{}", "- -"} {
		s = strings.ReplaceAll(s, empty, "")
	}
	s = spaceRun.ReplaceAllString(s, " ")
	return strings.Trim(s, " -_·.")
}

func expandPattern(pattern string, vals map[string]string) []string {
	out := pattern
	for k, v := range vals {
		out = strings.ReplaceAll(out, "{"+k+"}", v)
	}

	// Either separator: a Windows user will reach for a backslash, and
	// letting it fall through to safeName would turn the folder break into a
	// literal "-" in the filename.
	var segs []string
	for _, seg := range strings.FieldsFunc(out, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if seg = tidySegment(seg); seg != "" {
			segs = append(segs, safeName(seg))
		}
	}
	return segs
}

// DownloadPath works out where a stream should land.
func DownloadPath(root string, t streamTarget, url string) string {
	ext := extFromURL(url)

	var segs []string
	if t.Queue == nil {
		name := t.Meta.Name
		if name == "" {
			name = t.Label
		}
		segs = expandPattern(ctx.cfg.MoviePattern, map[string]string{
			"title": name,
			"year":  t.Meta.Year,
		})
	} else {
		q := t.Queue
		v := q.Episodes[q.Index]
		segs = expandPattern(ctx.cfg.EpisodePattern, map[string]string{
			"show":    q.Show.Name,
			"season":  fmt.Sprintf("%02d", q.Season),
			"episode": fmt.Sprintf("%02d", v.Episode),
			"title":   v.Title,
			"year":    q.Show.Year,
		})
	}

	if len(segs) == 0 {
		segs = []string{"untitled"}
	}
	if !ctx.cfg.DownloadFolders {
		segs = segs[len(segs)-1:]
	}
	segs[len(segs)-1] += ext

	return filepath.Join(append([]string{root}, segs...)...)
}

// ── Manager ───────────────────────────────────────────────────────────────────

type Downloader struct {
	mu      sync.Mutex
	items   []*Download
	seq     int
	running bool
	cancel  map[int]bool

	prog *tea.Program
}

func NewDownloader() *Downloader {
	return &Downloader{cancel: map[int]bool{}}
}

func (d *Downloader) Attach(p *tea.Program) { d.prog = p }

func (d *Downloader) emit(msg tea.Msg) {
	if d.prog != nil {
		d.prog.Send(msg)
	}
}

// Add queues a download. Returns false if that file is already queued, running
// or fully downloaded.
func (d *Downloader) Add(label, url, path string) (string, bool) {
	if fi, err := os.Stat(path); err == nil && fi.Size() > 0 {
		return "already downloaded", false
	}

	d.mu.Lock()
	for _, it := range d.items {
		if it.Path == path && (it.State == DLQueued || it.State == DLActive) {
			d.mu.Unlock()
			return "already in the queue", false
		}
	}

	d.seq++
	dl := &Download{ID: d.seq, Label: label, URL: url, Path: path, State: DLQueued}
	d.items = append(d.items, dl)

	start := !d.running
	if start {
		d.running = true
	}
	d.mu.Unlock()

	if start {
		go d.worker()
	}
	return "queued " + label, true
}

func (d *Downloader) Snapshot() []Download {
	d.mu.Lock()
	defer d.mu.Unlock()

	out := make([]Download, len(d.items))
	for i, it := range d.items {
		out[i] = *it
	}
	return out
}

// Active returns the download in progress, if any.
func (d *Downloader) Active() (Download, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, it := range d.items {
		if it.State == DLActive {
			return *it, true
		}
	}
	return Download{}, false
}

func (d *Downloader) Pending() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	n := 0
	for _, it := range d.items {
		if it.State == DLQueued || it.State == DLActive {
			n++
		}
	}
	return n
}

// Cancel stops an active download or drops a queued one. The partial file is
// left in place so it can be resumed later.
func (d *Downloader) Cancel(id int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, it := range d.items {
		if it.ID != id {
			continue
		}
		switch it.State {
		case DLQueued:
			it.State = DLCancelled
		case DLActive:
			d.cancel[id] = true
		}
		return
	}
}

// Clear removes finished entries from the list.
func (d *Downloader) Clear() {
	d.mu.Lock()
	defer d.mu.Unlock()

	kept := d.items[:0]
	for _, it := range d.items {
		if it.State == DLQueued || it.State == DLActive {
			kept = append(kept, it)
		}
	}
	d.items = kept
}

func (d *Downloader) next() *Download {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, it := range d.items {
		if it.State == DLQueued {
			it.State = DLActive
			return it
		}
	}
	d.running = false
	return nil
}

func (d *Downloader) worker() {
	for {
		dl := d.next()
		if dl == nil {
			return
		}

		err := d.fetch(dl)

		d.mu.Lock()
		cancelled := d.cancel[dl.ID]
		delete(d.cancel, dl.ID)
		switch {
		case cancelled:
			dl.State = DLCancelled
		case err != nil:
			dl.State, dl.Err = DLFailed, err
		default:
			dl.State = DLDone
		}
		label, state := dl.Label, dl.State
		d.mu.Unlock()

		if state != DLCancelled {
			d.emit(DownloadDoneMsg{Label: label, Err: err})
		}
		d.emit(DownloadTickMsg{})
	}
}

func (d *Downloader) fetch(dl *Download) error {
	if err := os.MkdirAll(filepath.Dir(dl.Path), 0755); err != nil {
		return err
	}

	part := partPath(dl.Path)
	var offset int64

	// Resume only when the sidecar agrees the partial came from this URL.
	if pi, ok := readPartInfo(dl.Path); ok && pi.URL == dl.URL {
		if fi, err := os.Stat(part); err == nil {
			offset = fi.Size()
		}
	} else {
		os.Remove(part)
		os.Remove(infoPath(dl.Path))
	}

	req, err := http.NewRequest("GET", dl.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", downloadUA)
	req.Header.Set("Accept", "*/*")
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	res, err := downloadClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	switch res.StatusCode {
	case http.StatusOK:
		offset = 0 // server ignored the range, start over
	case http.StatusPartialContent:
		// resuming
	case http.StatusForbidden, http.StatusUnauthorized:
		// Debrid links are time-limited; a stale one looks exactly like this.
		return fmt.Errorf("%s — the link may have expired, press R on the stream list to refetch", res.Status)
	default:
		return fmt.Errorf("server said %s", res.Status)
	}

	total := res.ContentLength
	if total > 0 {
		total += offset
	}

	d.mu.Lock()
	dl.Done, dl.Total, dl.Resume = offset, total, offset > 0
	d.mu.Unlock()
	writePartInfo(dl.Path, partInfo{URL: dl.URL, Total: total})

	flag := os.O_CREATE | os.O_WRONLY
	if offset > 0 {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}
	f, err := os.OpenFile(part, flag, 0644)
	if err != nil {
		return err
	}

	err = d.copy(dl, f, res.Body)
	f.Close()
	if err != nil {
		return err
	}

	// Rename only once the bytes are all there, so a half file never looks
	// like a finished one.
	if err := os.Rename(part, dl.Path); err != nil {
		return err
	}
	os.Remove(infoPath(dl.Path))
	return nil
}

func (d *Downloader) copy(dl *Download, dst io.Writer, src io.Reader) error {
	buf := make([]byte, 256*1024)

	lastEmit := time.Now()
	lastBytes := dl.Done

	for {
		d.mu.Lock()
		stop := d.cancel[dl.ID]
		d.mu.Unlock()
		if stop {
			return nil // partial file and sidecar stay put for a resume
		}

		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return werr
			}
			d.mu.Lock()
			dl.Done += int64(n)
			d.mu.Unlock()
		}

		if since := time.Since(lastEmit); since >= time.Second {
			d.mu.Lock()
			dl.Speed = int64(float64(dl.Done-lastBytes) / since.Seconds())
			lastBytes = dl.Done
			d.mu.Unlock()
			lastEmit = time.Now()
			d.emit(DownloadTickMsg{})
		}

		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

// ── Formatting ────────────────────────────────────────────────────────────────

func fmtBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 3; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

func fmtETA(d Download) string {
	if d.Speed <= 0 || d.Total <= 0 || d.Done >= d.Total {
		return ""
	}
	secs := float64(d.Total-d.Done) / float64(d.Speed)
	return fmtSecs(secs) + " left"
}

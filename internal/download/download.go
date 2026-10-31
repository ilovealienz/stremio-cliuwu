package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"stremio-cliuwu/internal/store"
)

// Downloads run one at a time. Debrid providers cap concurrent connections
// per account, so parallel downloads mostly just make each other slower.

// downloadClient has no overall timeout — the shared stremio.HTTPClient caps requests
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

// errStopped marks a download the user stopped. It has to be distinct from
// both success and failure: returning nil let fetch promote the partial file
// to its final name, which looked like a finished download and destroyed the
// sidecar the resume depends on.
var errStopped = errors.New("stopped")

type State int

const (
	Queued State = iota
	Active
	Done
	Failed
	Cancelled
)

func (s State) String() string {
	switch s {
	case Active:
		return "downloading"
	case Done:
		return "done"
	case Failed:
		return "failed"
	case Cancelled:
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

	State  State
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

type TickMsg struct{}

type DoneMsg struct {
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
	Label string `json:"label,omitempty"`

	// Set by aria2c downloads: the file's name on the server, for resuming
	// with a refetched link, and the bytes done, since its partial file has
	// gaps and its size says nothing.
	Name string `json:"name,omitempty"`
	Done int64  `json:"done,omitempty"`
}

func partPath(final string) string { return final + ".part" }

func infoPath(final string) string { return final + ".part.json" }

// controlPath is aria2c's resume file beside its partial.
func controlPath(final string) string { return final + ".part.aria2" }

// partDone is how much of a partial is downloaded.
func partDone(final string, pi partInfo) int64 {
	if _, err := os.Stat(controlPath(final)); err == nil {
		return pi.Done
	}
	return fileSize(partPath(final))
}

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

// Written atomically for the same reason as the index: a truncated sidecar
// fails to parse, and then both Load and Scan skip the file, leaving the
// .part invisible on disk with nothing to clean it up.
func writePartInfo(final string, pi partInfo) {
	if b, err := json.Marshal(pi); err == nil {
		store.WriteAtomic(infoPath(final), b, 0644)
	}
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

// LibraryPath is where a file from a debrid library entry lands.
//
// These have no meta behind them — no season, no episode, often no clean
// title — so the pack's own folder structure is the best thing to preserve.
// A season pack keeps its layout rather than being flattened into one folder
// of similarly-named files.
func LibraryPath(root, pack, rel string) string {
	if !store.Cfg.DownloadFolders {
		return filepath.Join(root, fitFile(store.SafeName(BaseName(rel))))
	}

	parts := []string{root}
	if pack != "" {
		parts = append(parts, store.FitName(store.SafeName(pack), store.MaxNameBytes))
	}
	segs := strings.Split(rel, "/")
	for i, seg := range segs {
		// "a//b", "./a" and "../a" have nothing to keep, and ".." mustn't
		// climb out of the download folder.
		if seg = strings.TrimSpace(seg); seg == "" || seg == "." || seg == ".." {
			continue
		}
		if i == len(segs)-1 {
			parts = append(parts, fitFile(store.SafeName(seg)))
		} else {
			parts = append(parts, store.FitName(store.SafeName(seg), store.MaxNameBytes))
		}
	}
	return filepath.Join(parts...)
}

// partRoom is what a download's name must leave for its partial files.
// aria2c saves its resume file through a temporary name, the longest of them.
const partRoom = len(".part.aria2__temp")

// fitFile shortens a file name to fit beside its partial files, keeping the
// extension.
func fitFile(name string) string {
	ext := filepath.Ext(name)
	if len(ext) > 10 { // not an extension, just a dot in the name
		ext = ""
	}
	return store.FitName(strings.TrimSuffix(name, ext), store.MaxNameBytes-len(ext)-partRoom) + ext
}

// Path works out where a stream should land.
func Path(root string, t store.StreamTarget, url string) string {
	ext := extFromURL(url)

	var segs []string
	if t.Queue == nil {
		name := t.Meta.Name
		if name == "" {
			name = t.Label
		}
		segs = store.ExpandPattern(store.Cfg.MoviePattern, map[string]string{
			"title": name,
			"year":  t.Meta.Year,
		})
	} else {
		q := t.Queue
		v := q.Episodes[q.Index]
		segs = store.ExpandPattern(store.Cfg.EpisodePattern, map[string]string{
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
	if !store.Cfg.DownloadFolders {
		segs = segs[len(segs)-1:]
	}
	for i := range segs {
		max := store.MaxNameBytes
		if i == len(segs)-1 {
			max -= len(ext) + partRoom
		}
		segs[i] = store.FitName(segs[i], max)
	}
	segs[len(segs)-1] += ext

	return filepath.Join(append([]string{root}, segs...)...)
}

// ── Index ─────────────────────────────────────────────────────────────────────

// downloads.json records what's been downloaded and what hasn't.
//
// The sidecars beside the files are still the authority for resuming — they
// carry the URL, so a link that's been reissued can't be spliced onto old
// bytes. This is a cache of that, so startup reads one small file instead of
// walking a media folder that might be enormous or on a network mount.
// Rescanning stays available when the two disagree.

// downloadRecord is deliberately thin. Anything derivable from disk isn't
// stored, because two copies of the same fact can disagree and then something
// has to win. Path is the reason the index exists; Label is the one thing
// disk can't supply, since a finished download has no sidecar left.
type downloadRecord struct {
	Path  string `json:"path"`
	Label string `json:"label"`
	At    int64  `json:"at,omitempty"`
}

type downloadIndex struct {
	Version int              `json:"version"`
	Items   []downloadRecord `json:"items"`
}

// Load restores the queue from the index, checking each entry against disk.
func (d *Downloader) Load() {
	b, err := os.ReadFile(store.DownloadsFile())
	if err != nil {
		return
	}
	var idx downloadIndex
	if json.Unmarshal(b, &idx) != nil {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	for _, r := range idx.Items {
		dl := &Download{Label: r.Label, Path: r.Path}

		// Disk decides the state. An index that says "active" just means the
		// last run was killed mid-download.
		if size := fileSize(r.Path); size > 0 {
			dl.State, dl.Total, dl.Done = Done, size, size
		} else {
			part := fileSize(partPath(r.Path))
			if part == 0 {
				continue // nothing on disk any more
			}

			// The sidecar is the authority for resuming: without its URL
			// there's nothing safe to continue from.
			pi, ok := readPartInfo(r.Path)
			if !ok || pi.URL == "" {
				continue
			}
			dl.State, dl.Resume = Cancelled, true
			dl.URL, dl.Total, dl.Done = pi.URL, pi.Total, partDone(r.Path, pi)
			if dl.Label == "" {
				dl.Label = pi.Label
			}
		}

		d.seq++
		dl.ID = d.seq
		d.items = append(d.items, dl)
	}
}

func fileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// save writes the index. Takes a snapshot under the lock, writes outside it.
func (d *Downloader) save() {
	d.mu.Lock()
	now := time.Now().Unix()
	idx := downloadIndex{Version: 1, Items: make([]downloadRecord, 0, len(d.items))}
	for _, it := range d.items {
		idx.Items = append(idx.Items, downloadRecord{
			Path: it.Path, Label: it.Label, At: now,
		})
	}
	d.mu.Unlock()

	if b, err := json.Marshal(idx); err == nil {
		store.WriteAtomic(store.DownloadsFile(), b, 0644)
	}
}

// ── Manager ───────────────────────────────────────────────────────────────────

type Downloader struct {
	mu      sync.Mutex
	items   []*Download
	seq     int
	running bool
	cancel  map[int]bool

	// Aborts the in-flight request. Polling a flag between reads only notices
	// a stop once more bytes arrive, so a stalled connection ignored it.
	abort map[int]context.CancelFunc

	// Removed while active: the worker deletes the files once it has stopped.
	purge map[int]string

	prog *tea.Program
}

func New() *Downloader {
	return &Downloader{cancel: map[int]bool{}, abort: map[int]context.CancelFunc{}, purge: map[int]string{}}
}

// deleteFiles removes a download's partial file, sidecar and aria2c's
// resume file.
func deleteFiles(path string) {
	os.Remove(infoPath(path))
	os.Remove(partPath(path))
	os.Remove(controlPath(path))
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

	// One entry per path, whatever state it's in. Matching only queued and
	// active meant re-queueing a stopped or failed episode appended a second
	// row pointing at the same part file, and whichever ran second decided
	// what the bytes were — the other row's progress was a fiction.
	for _, it := range d.items {
		if it.Path != path {
			continue
		}
		if it.State == Queued || it.State == Active {
			d.mu.Unlock()
			return "already in the queue", false
		}

		// Stopped, failed, or finished with the file since deleted. Take the
		// fresh url: the old one may well have expired, and fetch compares it
		// against the sidecar before trusting the partial bytes.
		//
		// Safe to write these without racing fetch, which reads URL and Label
		// unlocked: the branch above returns early for a queued or active
		// entry, so nothing here is touching one a worker holds.
		it.URL = url
		if label != "" {
			it.Label = label
		}
		it.State, it.Err, it.Speed = Queued, nil, 0

		// Read anything the return value needs while the lock is still held.
		queued := it.Label
		start := !d.running
		if start {
			d.running = true
		}
		d.mu.Unlock()

		if start {
			go d.worker()
		}
		d.save()
		return "queued " + queued, true
	}

	d.seq++
	dl := &Download{ID: d.seq, Label: label, URL: url, Path: path, State: Queued}
	d.items = append(d.items, dl)

	start := !d.running
	if start {
		d.running = true
	}
	d.mu.Unlock()

	if start {
		go d.worker()
	}
	d.save()
	return "queued " + label, true
}

// Scan picks up unfinished downloads left on disk.
//
// The queue lives in memory, so without this a restart loses track of what was
// part-downloaded — the bytes are still there, but you'd have to remember
// which episode it was and navigate back to it to resume. The sidecar already
// records the URL and size for resumption; carrying the label too makes the
// files self-describing, and the disk stays the single source of truth rather
// than a queue file that can drift from it.
func (d *Downloader) Scan(root string) int {
	if root == "" {
		return 0
	}

	found := 0
	filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".part.json") {
			return nil
		}

		final := strings.TrimSuffix(path, ".part.json")

		d.mu.Lock()
		known := false
		for _, it := range d.items {
			if it.Path == final {
				known = true
				break
			}
		}
		d.mu.Unlock()
		if known {
			return nil // a running one writes its sidecar before its data
		}

		if _, err := os.Stat(partPath(final)); err != nil {
			os.Remove(path) // sidecar with no data beside it
			return nil
		}
		pi, ok := readPartInfo(final)
		if !ok || pi.URL == "" {
			return nil
		}

		label := pi.Label
		if label == "" {
			label = BaseName(final)
		}
		d.mu.Lock()
		d.seq++
		d.items = append(d.items, &Download{
			ID: d.seq, Label: label, URL: pi.URL, Path: final,
			Total: pi.Total, Done: partDone(final, pi),
			State: Cancelled, Resume: true,
		})
		d.mu.Unlock()
		found++
		return nil
	})

	if found > 0 {
		d.save()
	}
	return found
}

// Resume re-queues a stopped or failed download.
func (d *Downloader) Resume(id int) bool {
	d.mu.Lock()
	var start, found bool
	for _, it := range d.items {
		if it.ID != id {
			continue
		}
		if it.State != Cancelled && it.State != Failed {
			d.mu.Unlock()
			return false
		}
		found = true
		it.State, it.Err = Queued, nil
		start = !d.running
		if start {
			d.running = true
		}
		break
	}
	d.mu.Unlock()
	if !found {
		return false
	}

	if start {
		go d.worker()
	}
	d.save()
	return true
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
		if it.State == Active {
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
		if it.State == Queued || it.State == Active {
			n++
		}
	}
	return n
}

// Cancel stops an active download or drops a queued one. The partial file is
// left in place so it can be resumed later; Remove is the one that deletes.
//
// Reports whether there was anything to stop, so the caller doesn't claim to
// have cancelled a download that already finished.
func (d *Downloader) Cancel(id int) bool {
	defer d.save()

	d.mu.Lock()
	defer d.mu.Unlock()

	for _, it := range d.items {
		if it.ID != id {
			continue
		}
		switch it.State {
		case Queued:
			it.State = Cancelled
		case Active:
			d.cancel[id] = true
			if abort := d.abort[id]; abort != nil {
				abort()
			}
		default:
			return false // already finished, failed or stopped
		}
		// A rate from the moment it stopped isn't current any more.
		it.Speed = 0
		return true
	}
	return false
}

// Remove drops an entry and deletes its partial file and sidecar.
//
// Clearing the list alone wasn't enough: Scan rebuilds an entry from any
// sidecar it finds, so a row removed while its files were still on disk came
// straight back on the next scan.
func (d *Downloader) Remove(id int) bool {
	d.mu.Lock()

	var path string
	found, active := false, false
	kept := d.items[:0]
	for _, it := range d.items {
		if it.ID != id {
			kept = append(kept, it)
			continue
		}
		found, path = true, it.Path
		if it.State == Active {
			active = true
			d.cancel[id] = true
			d.purge[id] = path
			if abort := d.abort[id]; abort != nil {
				abort()
			}
		}
	}
	if found {
		clear(d.items[len(kept):])
		d.items = kept
	}
	d.mu.Unlock()

	if !found {
		return false
	}

	// An active download can still write its files until it stops, so its
	// worker deletes them.
	if !active {
		deleteFiles(path)
	}
	d.save()
	// Deliberately no emit here. A confirm screen runs its onYes inline from
	// Update, on the ui goroutine, and Send blocks until the event loop reads
	// the channel — which it can't while it's inside Update. The caller
	// returns a TickMsg as a command instead.
	return true
}

// Clear removes completed entries from the list.
//
// Only Done: a stopped or failed download still has a part file and sidecar
// beside it, and dropping the row without deleting those just means Scan
// rebuilds it. Use Remove for those, which deletes both.
func (d *Downloader) Clear() int {
	defer d.save()

	d.mu.Lock()
	defer d.mu.Unlock()

	kept := d.items[:0]
	for _, it := range d.items {
		if it.State != Done {
			kept = append(kept, it)
		}
	}
	gone := len(d.items) - len(kept)
	// Filtering in place leaves the dropped pointers in the tail of the
	// backing array, where the collector can still see them.
	clear(d.items[len(kept):])
	d.items = kept
	return gone
}

func (d *Downloader) next() *Download {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, it := range d.items {
		if it.State == Queued {
			it.State = Active
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
		cancelled := d.cancel[dl.ID] || errors.Is(err, errStopped)
		purge, removed := d.purge[dl.ID]
		delete(d.cancel, dl.ID)
		delete(d.abort, dl.ID)
		delete(d.purge, dl.ID)
		switch {
		case cancelled:
			dl.State = Cancelled
		case err != nil:
			dl.State, dl.Err = Failed, err
		default:
			dl.State = Done
		}
		label, state := dl.Label, dl.State
		d.mu.Unlock()

		if removed {
			deleteFiles(purge)
		}
		d.save()
		if state != Cancelled {
			d.emit(DoneMsg{Label: label, Err: err})
		}
		d.emit(TickMsg{})
	}
}

func (d *Downloader) fetch(dl *Download) error {
	if err := os.MkdirAll(filepath.Dir(dl.Path), 0755); err != nil {
		return err
	}
	if store.Cfg.Aria2 {
		bin, err := aria2Path()
		if err != nil {
			return err
		}
		return d.fetchAria2(dl, bin)
	}

	part := partPath(dl.Path)
	var offset int64

	// aria2c's partial has gaps, so only it can carry on from one.
	if _, err := os.Stat(controlPath(dl.Path)); err == nil {
		deleteFiles(dl.Path)
	}

	// Resume only when the sidecar agrees the partial came from this URL.
	if pi, ok := readPartInfo(dl.Path); ok && pi.URL == dl.URL {
		if fi, err := os.Stat(part); err == nil {
			offset = fi.Size()
		}
	} else {
		os.Remove(part)
		os.Remove(infoPath(dl.Path))
	}

	// A cancel has to abort the request itself. Checking a flag between reads
	// only takes effect once the next read returns, so a stalled transfer sat
	// there ignoring it.
	rctx, abort := context.WithCancel(context.Background())
	defer abort()
	d.mu.Lock()
	d.abort[dl.ID] = abort
	stopped := d.cancel[dl.ID]
	d.mu.Unlock()
	if stopped {
		return errStopped
	}

	req, err := http.NewRequestWithContext(rctx, "GET", dl.URL, nil)
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
		var ue *url.Error
		if errors.As(err, &ue) {
			return ue.Err // the URL can carry a debrid key
		}
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

	// ContentLength is -1 when the server doesn't say. Carrying that through
	// made Total negative on the next resume, which turned the progress bar
	// and the ETA into nonsense. Zero means unknown everywhere else.
	total := res.ContentLength
	switch {
	case total > 0:
		total += offset
	default:
		total = 0
	}

	d.mu.Lock()
	dl.Done, dl.Total, dl.Resume = offset, total, offset > 0
	d.mu.Unlock()

	// The size is known now, and nothing has been written yet — running out
	// midway surfaces as a short write that says nothing about the cause.
	// Ahead of the sidecar too, so a refusal doesn't leave one behind with no
	// part file beside it.
	if need := total - offset; need > 0 {
		dir := filepath.Dir(dl.Path)
		if avail, ok := freeSpace(dir); ok && avail < uint64(need) {
			return fmt.Errorf("not enough space in %s — %s free, needs %s",
				dir, FmtBytes(int64(avail)), FmtBytes(need))
		}
	}

	writePartInfo(dl.Path, partInfo{URL: dl.URL, Total: total, Label: dl.Label})

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

	// A clean EOF doesn't mean a complete file — a dropped connection ends the
	// same way. Without this the short file got renamed into place and then
	// looked finished to Add, to Load and on screen, with the sidecar deleted
	// so it couldn't be resumed either.
	d.mu.Lock()
	got, want := dl.Done, dl.Total
	d.mu.Unlock()
	if want > 0 && got < want {
		return fmt.Errorf("connection ended early at %s of %s — enter to resume",
			FmtBytes(got), FmtBytes(want))
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

	// Every other access to Done is under the lock, and the race detector
	// counts this one even though fetch set it before starting this goroutine.
	d.mu.Lock()
	lastBytes := dl.Done
	d.mu.Unlock()

	for {
		d.mu.Lock()
		stop := d.cancel[dl.ID]
		d.mu.Unlock()
		if stop {
			return errStopped // partial file and sidecar stay put for a resume
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
			d.emit(TickMsg{})
		}

		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			// Our own abort surfaces here as a read error. It isn't a failure,
			// and reporting it as one would mark the download failed and show
			// the cancellation as the reason.
			d.mu.Lock()
			stopped := d.cancel[dl.ID]
			d.mu.Unlock()
			if stopped || errors.Is(rerr, context.Canceled) {
				return errStopped
			}
			return rerr
		}
	}
}

package player

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"stremio-cliuwu/internal/download"
	"stremio-cliuwu/internal/store"
	"stremio-cliuwu/internal/stremio"
)

// The old client used `loadfile <url> append-play`, which meant mpv owned a
// playlist and we had to reverse-engineer "what's playing" by asking mpv for
// its current path and matching it against a URL→videoID map. That map is gone.
//
// With `replace` there is exactly one file loaded at any moment, so the
// currently playing item is just p.now. Everything downstream — position
// saving, watched marking, the status bar — reads one field instead of doing
// prefix matching against a map that could go stale.
//
// The one thing we lose is mpv's own playlist advance. Rather than silently
// picking a stream for the next episode, the player emits EpisodeEndedMsg and
// the TUI opens that episode's stream list — see EpisodeEndedMsg below.

func socketPath() string {
	if runtime.GOOS == "windows" {
		return `\\.\pipe\stremio-cliuwu`
	}
	// The runtime dir is per user; /tmp is shared, so it's only the fallback.
	if dir := os.Getenv("XDG_RUNTIME_DIR"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "stremio-cliuwu.sock")
	}
	return "/tmp/stremio-cliuwu.sock"
}

var errNoMpv = errors.New("mpv is not running")

// ── Messages into the Bubble Tea program ──────────────────────────────────────

type StateMsg struct{ State State }

type NoticeMsg struct{ Text string }

type ErrMsg struct{ Err error }

// EpisodeEndedMsg fires when a queued episode plays to its end. The player
// deliberately does not pick the next stream itself: it can't tell a cached
// debrid result from one that needs downloading, and a blind pick that fails
// leaves you with an error and no list to retry from. The TUI opens the next
// episode's stream picker instead.
type EpisodeEndedMsg struct{ Prev PlayRequest }

// PrefetchNextMsg fires once the current episode is far enough through that
// the next one is a safe bet. Waiting for EOF meant the stream list only
// started loading after playback had already stopped; this way the next
// episode's streams are resolved and on screen while you're still watching.
type PrefetchNextMsg struct{ Prev PlayRequest }

// ErrorClipMsg fires when a file too short to be the episode or film ends.
// Addons answer a stream that isn't cached or has gone with a short status
// clip; it isn't recorded and nothing moves on.
type ErrorClipMsg struct{ Prev PlayRequest }

// minFileLen is the shortest file taken to be what you picked. The longest
// addon status clips (AIOStreams) run 120 s.
const minFileLen = 180

// isErrorClip reports whether a file of dur seconds is an addon's status clip.
// Library files are the content itself, so they're never one.
func isErrorClip(req *PlayRequest, dur float64) bool {
	return req != nil && req.MediaType != "other" && dur > 0 && dur < minFileLen
}

// record saves progress for req, unless there's nothing to save or the file
// is a status clip.
func record(req *PlayRequest, st State) {
	if req == nil || st.Duration <= 0 || st.Pos <= 0 || isErrorClip(req, st.Duration) {
		return
	}
	store.UpdatePosition(req.VideoID, st.Pos, st.Duration)
}

// How far before the end to look ahead is worked out from the runtime — see
// store.PrefetchLead. A flat fraction fired 37 minutes early on a long film.

type State struct {
	Alive     bool
	Loading   bool
	Buffering bool
	Paused    bool
	Label     string // our label, e.g. "Frieren · S01E04"
	NextLabel string // queued to follow this one, empty if nothing is
	Title     string // mpv's media-title
	VideoID   string
	Pos       float64
	Duration  float64
	QueuePos  int
	QueueLen  int
	ErrorClip bool // the file is an addon's status clip, not the video
}

func (s State) Frac() float64 {
	if s.Duration <= 0 {
		return 0
	}
	return s.Pos / s.Duration
}

func (s State) Percent() float64 { return s.Frac() * 100 }

// ── Play request ──────────────────────────────────────────────────────────────

type PlayRequest struct {
	VideoID   string
	FetchID   string // the id stream addons were asked for
	MediaType string
	Label     string
	URL       string
	Resume    float64
	Entry     store.HistoryEntry // written to history when playback starts
	Queue     *stremio.EpQueue   // non-nil for series, drives autoplay
	Addons    []stremio.Addon    // needed to resolve the next episode

	// Carried from the chosen stream so the subtitle picker can ask for
	// this exact file rather than the title in general.
	VideoHash string
	VideoSize int64
	Filename  string
	Subs      []stremio.Subtitle // shipped with the stream itself

	Auto bool // queued by autoplay rather than by hand

	// How the stream was picked, so autoplay can pick the next one the same way.
	BingeGroup string
	Release    string // file or release name
	Filter     string // the stream list's filter text
	AddonTab   string // the addon tab, "" for all
}

// ── Player ────────────────────────────────────────────────────────────────────

type Player struct {
	cfg  store.Config
	prog *tea.Program

	mu      sync.Mutex
	conn    net.Conn
	pending map[uint32]chan map[string]any
	now     *PlayRequest
	state   State
	seq     uint32

	wmu sync.Mutex // serialises writes to conn

	queued     *PlayRequest // plays automatically when the current file ends
	noAuto     bool         // up next was cleared by hand; autoplay leaves it empty
	ended      bool         // the current file played to the end with nothing queued
	replacing  bool         // suppress the end-file/stop that a replace generates
	prefetched bool         // next episode already surfaced for this request
	historied  bool         // written to history; waits until the file proves not to be a status clip
	clip       bool         // the file was found to be a status clip
	seekTo     float64      // pending resume seek, applied on file-loaded
	lastSave   time.Time    // throttles history writes
	lastEmit   int          // last whole second pushed to the UI

	start func(PlayRequest) // plays a request off the calling goroutine; tests swap it
}

func New(cfg store.Config) *Player {
	p := &Player{cfg: cfg, pending: map[uint32]chan map[string]any{}}
	p.start = func(req PlayRequest) {
		p.sendAsync(func() {
			if msg := p.play(req); msg != nil {
				p.emit(msg)
			}
		})
	}
	return p
}

func (p *Player) Attach(prog *tea.Program) { p.prog = prog }

func (p *Player) SetConfig(cfg store.Config) { p.mu.Lock(); p.cfg = cfg; p.mu.Unlock() }

func (p *Player) emit(msg tea.Msg) {
	if p.prog != nil {
		p.prog.Send(msg)
	}
}

func (p *Player) State() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// Queue holds a stream to play when the current one ends. Queueing again
// replaces what was there — you're changing your mind about what's next, not
// building a list.
//
// Neither this nor Unqueue emits: they're called from a screen's Update, and
// Program.Send writes to an unbuffered channel drained by the very event loop
// that is running Update — so emitting from there blocks waiting for itself.
// Callers return playerStateCmd() instead, letting the runtime deliver it.
//
// If the current file has already ended there's nothing to queue behind, so
// it plays straight away and reports started.
func (p *Player) Queue(req PlayRequest) (started bool) {
	p.mu.Lock()
	req.Auto = false
	if p.ended && p.now != nil {
		p.ended = false
		p.mu.Unlock()
		p.start(req)
		return true
	}
	p.queued = &req
	p.state.NextLabel = req.Label
	p.mu.Unlock()
	return false
}

// QueueAuto is Queue for autoplay. It only fills an empty slot, only while
// the episode it was picked for is current, and not once you've cleared the
// slot yourself. If that episode has already ended it plays straight away,
// and started reports so.
func (p *Player) QueueAuto(req PlayRequest, after string) (ok, started bool) {
	p.mu.Lock()
	if p.now == nil || p.now.VideoID != after || p.queued != nil || p.noAuto {
		p.mu.Unlock()
		return false, false
	}
	req.Auto = true
	if p.ended {
		p.ended = false
		p.mu.Unlock()
		p.start(req)
		return true, true
	}
	p.queued = &req
	p.state.NextLabel = req.Label
	p.mu.Unlock()
	return true, false
}

// ClearAuto empties up next if autoplay filled it.
func (p *Player) ClearAuto() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.queued != nil && p.queued.Auto {
		p.queued = nil
		p.state.NextLabel = ""
	}
}

func (p *Player) Unqueue() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.queued = nil
	p.noAuto = true
	p.state.NextLabel = ""
}

// AddSubtitle loads a subtitle track into the running file and selects it.
//
// mpv fetches the URL itself, so nothing is downloaded here — which also
// means a dead link fails inside mpv rather than anywhere we can report.
func (p *Player) AddSubtitle(url, title, lang string) tea.Cmd {
	return func() tea.Msg {
		if title == "" {
			title = "subtitle"
		}

		// Generous: mpv downloads the file before it answers, and some
		// subtitle hosts are slow enough that four seconds is a false
		// negative rather than a failure.
		_, err := p.commandWait(30*time.Second, "sub-add", url, "select", title, lang)
		return SubtitleAddedMsg{URL: url, Title: title, Err: err}
	}
}

// SubtitleAddedMsg reports whether mpv actually took the track, so the picker
// can mark the one that's really playing rather than the one you last pressed.
type SubtitleAddedMsg struct {
	URL   string
	Title string
	Err   error
}

// Queued returns what's lined up, if anything.
func (p *Player) Queued() *PlayRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.queued
}

func (p *Player) Now() *PlayRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.now
}

// ── Connection ────────────────────────────────────────────────────────────────

func (p *Player) connect() error {
	p.mu.Lock()
	live := p.conn != nil
	p.mu.Unlock()
	if live {
		return nil
	}

	c := ipcDial()
	if c == nil {
		if err := p.spawn(); err != nil {
			return err
		}
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			if c = ipcDial(); c != nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if c == nil {
			return errors.New("mpv started but its IPC socket never appeared")
		}
	}

	p.mu.Lock()
	p.conn = c
	p.state.Alive = true
	p.mu.Unlock()

	go p.readLoop(c)
	p.observe()
	return nil
}

func (p *Player) spawn() error {
	if runtime.GOOS != "windows" {
		os.Remove(socketPath())
	}
	p.mu.Lock()
	cfg := p.cfg
	p.mu.Unlock()

	bin := cfg.MpvPath
	if bin == "" {
		bin = "mpv"
	}

	args := []string{
		"--idle=yes",
		"--force-window=yes",
		"--keep-open=no",
		// --no-terminal is not optional here: we run inside the alt screen,
		// and anything mpv prints to our tty shreds the TUI.
		"--no-terminal",
		"--input-ipc-server=" + socketPath(),
	}
	if cfg.SubtitleLang != "" {
		args = append(args, "--slang="+cfg.SubtitleLang)
	}

	cmd := exec.Command(bin, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("couldn't start mpv (%s): %w", bin, err)
	}
	go cmd.Wait() // don't leave a zombie
	return nil
}

func (p *Player) readLoop(c net.Conn) {
	r := bufio.NewReader(c)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var m map[string]any
			if json.Unmarshal(line, &m) == nil {
				p.dispatch(m)
			}
		}
		if err != nil {
			break
		}
	}
	p.gone()
}

func (p *Player) gone() {
	p.mu.Lock()
	if p.conn != nil {
		p.conn.Close()
		p.conn = nil
	}
	for id, ch := range p.pending {
		close(ch)
		delete(p.pending, id)
	}
	now := p.now
	st := p.state
	p.now = nil
	p.state = State{}

	// Nothing left to follow. Left set, it would suppress the next-episode
	// picker for the rest of the session — Queued() being non-nil is what
	// tells the app a choice has already been made.
	p.queued = nil
	p.mu.Unlock()

	record(now, st)
	p.emit(StateMsg{})
}

// sendAsync runs IPC work off the read loop.
//
// dispatch is called synchronously from readLoop, and command() waits for a
// reply that only readLoop can deliver — so calling it from an event handler
// deadlocks until the timeout fires. The command still reaches mpv, which is
// why this looked like it worked; what it actually did was stall every
// property update for four seconds after each file load.
func (p *Player) sendAsync(fn func()) { go fn() }

// mpvTitle is what mpv should show as the media title.
//
// Empty leaves mpv to its own behaviour: the filename from the url path, or
// whatever the server says in Content-Disposition, falling back to the url
// itself. For a debrid link that is usually the release name.
func mpvTitle(req PlayRequest) string {
	e := req.Entry

	mode := store.Cfg.MpvTitle
	if mode == "" || mode == "default" {
		return ""
	}

	// A library file has no metadata behind it. Its episode number is the
	// file's position in the pack, recorded so history can tell the files
	// apart, and its title is the filename — so there is nothing to format
	// or to pattern against, in either mode.
	if e.Type == "other" {
		if t := strings.TrimSpace(e.EpTitle); t != "" {
			return t
		}
		return e.Name
	}

	switch mode {
	case "formatted":
		if e.Episode > 0 {
			out := fmt.Sprintf("%s - %s", e.Name, stremio.FmtEp(e.Season, e.Episode, e.VideoID))
			// Only when the addon has a real one. Some fill the field with
			// "Episode #29.1", which says nothing the number has not.
			if t := strings.TrimSpace(e.EpTitle); t != "" && !strings.HasPrefix(t, "Episode #") {
				out += " - " + t
			}
			return out
		}
		if e.Year != "" {
			return fmt.Sprintf("%s (%s)", e.Name, e.Year)
		}
		return e.Name
	}

	// A pattern, using the same placeholders as the download filenames. The
	// segments are joined back with a space: a title is one line, and the
	// separators only mean folders when a path is being built.
	vals := map[string]string{
		"title": e.Name,
		"year":  e.Year,
		"show":  e.Name,
	}
	if e.Episode > 0 {
		vals["season"] = fmt.Sprintf("%02d", e.Season)
		vals["episode"] = fmt.Sprintf("%02d", e.Episode)
		if e.EpTitle != "" {
			vals["title"] = e.EpTitle
		}
	}
	return strings.Join(store.ExpandPattern(mode, vals), " ")
}

// command sends an mpv IPC command and waits for the matching response.
// command sends an mpv IPC command and waits the default time for a reply.
func (p *Player) command(args ...any) (map[string]any, error) {
	return p.commandWait(4*time.Second, args...)
}

// commandWait is command with its own deadline.
//
// Most commands are instant — a seek, a property set. sub-add is not: mpv
// fetches the file before replying, and a slow subtitle host takes longer
// than any sane default. Reporting failure at four seconds meant saying a
// subtitle hadn't loaded while mpv was still loading it.
func (p *Player) commandWait(wait time.Duration, args ...any) (map[string]any, error) {
	p.mu.Lock()
	c := p.conn
	if c == nil {
		p.mu.Unlock()
		return nil, errNoMpv
	}
	id := atomic.AddUint32(&p.seq, 1)
	ch := make(chan map[string]any, 1)
	p.pending[id] = ch
	p.mu.Unlock()

	payload, _ := json.Marshal(map[string]any{"command": args, "request_id": id})
	payload = append(payload, '\n')

	p.wmu.Lock()
	_, err := c.Write(payload)
	p.wmu.Unlock()
	if err != nil {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return nil, err
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, errNoMpv
		}
		if e, _ := resp["error"].(string); e != "success" {
			return resp, fmt.Errorf("mpv: %s", e)
		}
		return resp, nil
	case <-time.After(wait):
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return nil, errors.New("mpv did not respond")
	}
}

func (p *Player) observe() {
	props := []string{"time-pos", "duration", "pause", "media-title", "paused-for-cache"}
	for i, prop := range props {
		p.command("observe_property", i+1, prop)
	}
}

// ── Event dispatch ────────────────────────────────────────────────────────────

func (p *Player) dispatch(m map[string]any) {
	if rid, ok := m["request_id"].(float64); ok {
		p.mu.Lock()
		ch := p.pending[uint32(rid)]
		delete(p.pending, uint32(rid))
		p.mu.Unlock()
		if ch != nil {
			ch <- m
		}
		return
	}

	switch ev, _ := m["event"].(string); ev {
	case "property-change":
		name, _ := m["name"].(string)
		p.onProperty(name, m["data"])
	case "file-loaded":
		p.onFileLoaded()
	case "end-file":
		reason, _ := m["reason"].(string)
		p.onEndFile(reason)
	case "shutdown":
		p.gone()
	}
}

func (p *Player) onProperty(name string, data any) {
	p.mu.Lock()
	switch name {
	case "time-pos":
		if f, ok := data.(float64); ok {
			p.state.Pos = f
		}
	case "duration":
		if f, ok := data.(float64); ok {
			p.state.Duration = f
		}
	case "pause":
		p.state.Paused, _ = data.(bool)
	case "paused-for-cache":
		p.state.Buffering, _ = data.(bool)
	case "media-title":
		if s, ok := data.(string); ok {
			p.state.Title = stremio.CleanTitle(s)
		}
	}

	p.state.ErrorClip = isErrorClip(p.now, p.state.Duration)
	if p.state.ErrorClip && !p.replacing {
		p.markErrorClip()
	}
	addHist := p.takeHistory()
	histMax := p.cfg.HistoryMax
	st := p.state
	now := p.now

	// Throttle history writes to once a second.
	save := now != nil && st.Duration > 0 && st.Pos > 0 && time.Since(p.lastSave) >= time.Second
	if save {
		p.lastSave = time.Now()
	}

	// Look ahead once we're far enough in.
	prefetch := false
	if now != nil && !p.prefetched && (p.cfg.AutoNext || p.cfg.Autoplay) &&
		now.Queue.HasNext() && st.Duration > 0 && !st.ErrorClip &&
		st.Duration-st.Pos <= store.PrefetchLead(st.Duration) {
		p.prefetched = true
		prefetch = true
	}

	// Only repaint when the displayed second actually changes, otherwise mpv's
	// property firehose turns into a Bubble Tea message firehose.
	sec := int(st.Pos)
	repaint := sec != p.lastEmit || name != "time-pos"
	if repaint {
		p.lastEmit = sec
	}
	p.mu.Unlock()

	if addHist {
		store.AddHistory(now.Entry, histMax)
	}
	if save {
		go record(now, st)
	}
	if prefetch && now != nil {
		p.emit(PrefetchNextMsg{Prev: *now})
	}
	if repaint {
		p.emit(StateMsg{State: st})
	}
}

// takeHistory reports whether the playing file should be written to history
// now: once its length is known and it isn't a status clip. Callers hold the
// lock.
func (p *Player) takeHistory() bool {
	if p.historied || p.replacing || p.now == nil || p.now.Entry.ID == "" ||
		p.state.Duration <= 0 || p.state.ErrorClip {
		return false
	}
	p.historied = true
	return true
}

func (p *Player) onFileLoaded() {
	p.mu.Lock()
	p.replacing = false
	p.state.Loading = false
	seek := p.seekTo
	p.seekTo = 0
	// The duration can arrive before the file counts as loaded.
	if p.state.ErrorClip {
		p.markErrorClip()
	}
	addHist := p.takeHistory()
	histMax := p.cfg.HistoryMax
	now := p.now
	st := p.state
	p.mu.Unlock()

	if addHist {
		store.AddHistory(now.Entry, histMax)
	}

	// Off the read loop — see sendAsync. Calling command() here blocked the
	// reader for the full timeout, twice on a resume, and no property updates
	// were processed in the meantime.
	p.sendAsync(func() {
		// Only into a file known to be long enough: an error clip plays from
		// the start, and a seek past the end closes the file at once.
		if seek > 5 {
			if d := p.duration(); d > 0 && !isErrorClip(now, d) && seek < d {
				p.command("seek", seek, "absolute")
			}
		}
		// Loading a new file while paused leaves mpv paused, so you'd have to
		// go and hit play yourself. Picking a stream means you want it to play.
		p.command("set_property", "pause", false)
	})

	p.emit(StateMsg{State: st})
}

func (p *Player) onEndFile(reason string) {
	p.mu.Lock()
	// A `loadfile ... replace` makes mpv emit end-file/stop for the outgoing
	// file. That is bookkeeping, not the end of playback — ignore it.
	if p.replacing {
		p.mu.Unlock()
		return
	}
	now := p.now
	st := p.state
	autoNext := p.cfg.AutoNext || p.cfg.Autoplay
	prefetched := p.prefetched
	queued := p.queued
	p.mu.Unlock()

	if now == nil {
		return
	}
	// Ended by itself or closed by you, a status clip goes no further.
	if (reason == "eof" || reason == "quit") && isErrorClip(now, st.Duration) {
		p.mu.Lock()
		p.markErrorClip()
		p.state.Loading = false
		p.mu.Unlock()
		p.emit(ErrorClipMsg{Prev: *now})
		p.emit(StateMsg{State: p.State()})
		return
	}

	switch reason {
	case "eof":
		// mpv also reports a dropped connection as the end of the file.
		if st.Duration <= 0 || !store.IsWatchedAt(st.Pos, st.Duration) {
			record(now, st)
			p.endEarly(*now, st.Pos)
			return
		}

		// Watched in full — pin it at 100% so history is unambiguous.
		store.UpdatePosition(now.VideoID, st.Duration, st.Duration)

		// A queued stream beats everything: you've already chosen, so don't
		// open a picker or announce anything, just play it. Off the read loop,
		// since play() sends a command and this handler runs inside it.
		//
		// Cleared before the play, not by it. play() nils the field, but it
		// runs on another goroutine — until it does, the slot still holds this
		// request, and a second end-file would hand the same one over again.
		// Read again under the lock so an autoplay pick can't land in between.
		p.mu.Lock()
		queued = p.queued
		if queued != nil {
			p.queued = nil
			p.state.NextLabel = ""
		} else {
			p.ended = true
		}
		p.mu.Unlock()

		if queued != nil {
			p.start(*queued)
			return
		}

		if autoNext && now.Queue.HasNext() && !prefetched {
			p.emit(EpisodeEndedMsg{Prev: *now})
			return
		}
		if prefetched {
			return // the next episode's stream list is already open
		}
		p.emit(NoticeMsg{Text: "finished — " + now.Label})
	case "error":
		// Deliberately doesn't skip ahead: a failure here means this stream is
		// bad, so what you want is a different stream for the same episode,
		// which is where you already are.
		p.emit(ErrMsg{Err: errors.New("mpv couldn't play that stream — R to refetch, or pick another")})
	default:
		record(now, st)
	}

	p.mu.Lock()
	p.state.Loading = false
	p.mu.Unlock()
}

// endEarly stops on a file that ended well short of its end, so nothing
// moves on and the position is there to resume from.
func (p *Player) endEarly(req PlayRequest, pos float64) {
	p.mu.Lock()
	p.queued = nil
	p.noAuto = true // an autoplay pick still on its way mustn't start
	p.state.NextLabel = ""
	p.ended = true
	p.state.Loading = false
	p.mu.Unlock()
	p.emit(ErrMsg{Err: fmt.Errorf("playback stopped early at %s — pick a stream to resume", download.FmtSecs(pos))})
	p.emit(StateMsg{State: p.State()}) // screens re-read the saved position
}

// markErrorClip drops anything queued behind a status clip, once per file.
// Callers hold the lock.
func (p *Player) markErrorClip() {
	if p.clip {
		return
	}
	p.clip = true
	p.queued = nil
	p.noAuto = true // an autoplay pick still on its way mustn't start
	p.state.NextLabel = ""
	p.ended = true // nothing worth waiting for: n plays straight away
}

// duration asks mpv for the file's length, 0 while it doesn't know.
func (p *Player) duration() float64 {
	m, err := p.command("get_property", "duration")
	if err != nil {
		return 0
	}
	d, _ := m["data"].(float64)
	return d
}

// ── Public controls ───────────────────────────────────────────────────────────

// Play returns a tea.Cmd so screens can fire it without blocking the UI.
func (p *Player) Play(req PlayRequest) tea.Cmd {
	return func() tea.Msg { return p.play(req) }
}

func (p *Player) play(req PlayRequest) tea.Msg {
	if err := p.connect(); err != nil {
		return ErrMsg{Err: err}
	}

	// Flush the outgoing item's position before we lose track of it.
	p.mu.Lock()
	prev, prevSt := p.now, p.state
	p.replacing = true
	p.prefetched = false
	p.historied = false
	p.clip = false
	p.queued = nil // choosing something now supersedes whatever was lined up
	p.noAuto = false
	p.ended = false
	p.now = &req
	p.seekTo = req.Resume
	p.state = State{
		Alive:   true,
		Loading: true,
		Label:   req.Label,
		VideoID: req.VideoID,
	}
	if req.Queue != nil {
		p.state.QueuePos = req.Queue.Index + 1
		p.state.QueueLen = len(req.Queue.Episodes)
	}
	st := p.state
	p.mu.Unlock()

	record(prev, prevSt)

	// Set before the load so mpv has it for the first frame. It is a global
	// property rather than a per-file one, so it survives the loadfile.
	if title := mpvTitle(req); title != "" {
		p.command("set_property", "force-media-title", title)
	} else {
		p.command("set_property", "force-media-title", "")
	}

	if _, err := p.command("loadfile", req.URL, "replace"); err != nil {
		p.mu.Lock()
		p.replacing = false
		p.state.Loading = false
		p.mu.Unlock()
		return ErrMsg{Err: err}
	}
	return StateMsg{State: st}
}

// Stop quits mpv outright. `stop` alone leaves the window sitting there when
// the user's mpv.conf sets idle=yes, which looks like the key did nothing.
func (p *Player) Stop() tea.Cmd {
	return func() tea.Msg {
		p.mu.Lock()
		now, st := p.now, p.state
		p.now = nil
		p.state = State{Alive: st.Alive}
		p.mu.Unlock()

		record(now, st)
		p.command("quit")
		return StateMsg{}
	}
}

// Quit flushes position and tells mpv to exit.
func (p *Player) Quit() {
	p.Shutdown()
	p.command("quit")
}

// Shutdown flushes the current position without touching mpv.
func (p *Player) Shutdown() {
	p.mu.Lock()
	now, st := p.now, p.state
	p.mu.Unlock()
	record(now, st)
}

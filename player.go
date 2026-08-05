package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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
	return "/tmp/stremio-cliuwu.sock"
}

var errNoMpv = errors.New("mpv is not running")

// ── Messages into the Bubble Tea program ──────────────────────────────────────

type PlayerStateMsg struct{ State PlayerState }
type PlayerNoticeMsg struct{ Text string }
type PlayerErrMsg struct{ Err error }

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

// prefetchAt is the fraction of an episode after which we look ahead.
const prefetchAt = 0.75

type PlayerState struct {
	Alive     bool
	Loading   bool
	Buffering bool
	Paused    bool
	Label     string // our label, e.g. "Frieren · S01E04"
	Title     string // mpv's media-title
	VideoID   string
	Pos       float64
	Duration  float64
	QueuePos  int
	QueueLen  int
}

func (s PlayerState) Frac() float64 {
	if s.Duration <= 0 {
		return 0
	}
	return s.Pos / s.Duration
}

func (s PlayerState) Percent() float64 { return s.Frac() * 100 }

// ── Play request ──────────────────────────────────────────────────────────────

type PlayRequest struct {
	VideoID   string
	MediaType string
	Label     string
	URL       string
	Resume    float64
	Entry     HistoryEntry // written to history when playback starts
	Queue     *EpQueue     // non-nil for series, drives autoplay
	Addons    []Addon      // needed to resolve the next episode
}

// ── Player ────────────────────────────────────────────────────────────────────

type Player struct {
	cfg  AppConfig
	prog *tea.Program

	mu      sync.Mutex
	conn    net.Conn
	pending map[uint32]chan map[string]any
	now     *PlayRequest
	state   PlayerState
	seq     uint32

	wmu sync.Mutex // serialises writes to conn

	replacing  bool    // suppress the end-file/stop that a replace generates
	prefetched bool    // next episode already surfaced for this request
	seekTo     float64 // pending resume seek, applied on file-loaded
	lastSave  time.Time // throttles history writes
	lastEmit  int       // last whole second pushed to the UI
}

func NewPlayer(cfg AppConfig) *Player {
	return &Player{cfg: cfg, pending: map[uint32]chan map[string]any{}}
}

func (p *Player) Attach(prog *tea.Program) { p.prog = prog }
func (p *Player) SetConfig(cfg AppConfig)  { p.mu.Lock(); p.cfg = cfg; p.mu.Unlock() }

func (p *Player) emit(msg tea.Msg) {
	if p.prog != nil {
		p.prog.Send(msg)
	}
}

func (p *Player) State() PlayerState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
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
	p.state = PlayerState{}
	p.mu.Unlock()

	if now != nil && st.Duration > 0 && st.Pos > 0 {
		UpdatePosition(now.VideoID, st.Pos, st.Duration, st.Percent())
	}
	p.emit(PlayerStateMsg{})
}

// command sends an mpv IPC command and waits for the matching response.
func (p *Player) command(args ...any) (map[string]any, error) {
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
	case <-time.After(4 * time.Second):
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
			p.state.Title = cleanTitle(s)
		}
	}

	st := p.state
	now := p.now

	// Throttle history writes to once a second.
	save := now != nil && st.Duration > 0 && st.Pos > 0 && time.Since(p.lastSave) >= time.Second
	if save {
		p.lastSave = time.Now()
	}

	// Look ahead once we're far enough in.
	prefetch := false
	if now != nil && !p.prefetched && p.cfg.AutoNext &&
		now.Queue.HasNext() && st.Duration > 0 && st.Frac() >= prefetchAt {
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
	videoID := ""
	if now != nil {
		videoID = now.VideoID
	}
	p.mu.Unlock()

	if save {
		go UpdatePosition(videoID, st.Pos, st.Duration, st.Percent())
	}
	if prefetch && now != nil {
		p.emit(PrefetchNextMsg{Prev: *now})
	}
	if repaint {
		p.emit(PlayerStateMsg{State: st})
	}
}

func (p *Player) onFileLoaded() {
	p.mu.Lock()
	p.replacing = false
	p.state.Loading = false
	seek := p.seekTo
	p.seekTo = 0
	st := p.state
	p.mu.Unlock()

	if seek > 5 {
		p.command("seek", seek, "absolute")
	}
	// Loading a new file while paused leaves mpv paused, so you'd have to go
	// and hit play yourself. Picking a stream means you want it to play.
	p.command("set_property", "pause", false)
	p.emit(PlayerStateMsg{State: st})
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
	autoNext := p.cfg.AutoNext
	prefetched := p.prefetched
	p.mu.Unlock()

	if now == nil {
		return
	}

	switch reason {
	case "eof":
		// Watched in full — pin it at 100% so history is unambiguous.
		UpdatePosition(now.VideoID, st.Duration, st.Duration, 100)
		if autoNext && now.Queue.HasNext() && !prefetched {
			p.emit(EpisodeEndedMsg{Prev: *now})
			return
		}
		if prefetched {
			return // the next episode's stream list is already open
		}
		p.emit(PlayerNoticeMsg{Text: "finished — " + now.Label})
	case "error":
		p.emit(PlayerErrMsg{Err: errors.New("mpv failed to play that stream")})
	default:
		if st.Duration > 0 && st.Pos > 0 {
			UpdatePosition(now.VideoID, st.Pos, st.Duration, st.Percent())
		}
	}

	p.mu.Lock()
	p.state.Loading = false
	p.mu.Unlock()
}

// ── Public controls ───────────────────────────────────────────────────────────

// Play returns a tea.Cmd so screens can fire it without blocking the UI.
func (p *Player) Play(req PlayRequest) tea.Cmd {
	return func() tea.Msg { return p.play(req) }
}

func (p *Player) play(req PlayRequest) tea.Msg {
	if err := p.connect(); err != nil {
		return PlayerErrMsg{Err: err}
	}

	// Flush the outgoing item's position before we lose track of it.
	p.mu.Lock()
	prev, prevSt := p.now, p.state
	p.replacing = true
	p.prefetched = false
	p.now = &req
	p.seekTo = req.Resume
	p.state = PlayerState{
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
	histMax := p.cfg.HistoryMax
	p.mu.Unlock()

	if prev != nil && prevSt.Duration > 0 && prevSt.Pos > 0 {
		UpdatePosition(prev.VideoID, prevSt.Pos, prevSt.Duration, prevSt.Percent())
	}
	if req.Entry.ID != "" {
		AddHistory(req.Entry, histMax)
	}

	if _, err := p.command("loadfile", req.URL, "replace"); err != nil {
		p.mu.Lock()
		p.replacing = false
		p.state.Loading = false
		p.mu.Unlock()
		return PlayerErrMsg{Err: err}
	}
	return PlayerStateMsg{State: st}
}

// Stop quits mpv outright. `stop` alone leaves the window sitting there when
// the user's mpv.conf sets idle=yes, which looks like the key did nothing.
func (p *Player) Stop() tea.Cmd {
	return func() tea.Msg {
		p.mu.Lock()
		now, st := p.now, p.state
		p.now = nil
		p.state = PlayerState{Alive: st.Alive}
		p.mu.Unlock()

		if now != nil && st.Duration > 0 && st.Pos > 0 {
			UpdatePosition(now.VideoID, st.Pos, st.Duration, st.Percent())
		}
		p.command("quit")
		return PlayerStateMsg{}
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
	if now != nil && st.Duration > 0 && st.Pos > 0 {
		UpdatePosition(now.VideoID, st.Pos, st.Duration, st.Percent())
	}
}

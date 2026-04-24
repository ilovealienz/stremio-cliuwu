package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/term"
)

// ── Socket ────────────────────────────────────────────────────────────────────

func socketPath() string {
	if runtime.GOOS == "windows" {
		return `\\.\pipe\stremio-cliuwu`
	}
	return "/tmp/stremio-cliuwu.sock"
}

var (
	ipcMu  sync.Mutex
	ipcSeq uint32
)

func ipcDial() net.Conn {
	network := "unix"
	if runtime.GOOS == "windows" {
		network = "pipe"
	}
	c, err := net.DialTimeout(network, socketPath(), 400*time.Millisecond)
	if err != nil {
		return nil
	}
	return c
}

func ipcSend(conn net.Conn, cmd []any) map[string]any {
	id := atomic.AddUint32(&ipcSeq, 1)
	msg, _ := json.Marshal(map[string]any{"command": cmd, "request_id": id})
	msg = append(msg, '\n')
	conn.SetDeadline(time.Now().Add(1500 * time.Millisecond))
	if _, err := conn.Write(msg); err != nil {
		return nil
	}
	var buf []byte
	tmp := make([]byte, 512)
	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		for {
			idx := strings.Index(string(buf), "\n")
			if idx < 0 {
				break
			}
			line := buf[:idx]
			buf = buf[idx+1:]
			var resp map[string]any
			if json.Unmarshal(line, &resp) == nil {
				if rid, ok := resp["request_id"].(float64); ok && uint32(rid) == id {
					return resp
				}
			}
		}
		if err != nil {
			break
		}
	}
	return nil
}

// ipcCmd opens a fresh connection and sends one command. Used for one-off commands.
func ipcCmd(cmd []any) bool {
	conn := ipcDial()
	if conn == nil {
		return false
	}
	defer conn.Close()
	resp := ipcSend(conn, cmd)
	return resp != nil && resp["error"] == "success"
}

func mpvAlive() bool {
	c := ipcDial()
	if c == nil {
		return false
	}
	c.Close()
	return true
}

// ── Launch ────────────────────────────────────────────────────────────────────

var mpvProc *os.Process

func mpvBinary(cfg AppConfig) string {
	if cfg.MpvPath != "" {
		return cfg.MpvPath
	}
	return "mpv"
}

func launchMpv(cfg AppConfig, streamURL string) error {
	if runtime.GOOS != "windows" {
		os.Remove(socketPath())
	}
	args := []string{
		streamURL,
		"--idle=yes",
		"--force-window=yes",
		"--really-quiet",
		"--input-ipc-server=" + socketPath(),
		"--term-status-msg=",
	}
	if cfg.SubtitleLang != "" {
		args = append(args, "--slang="+cfg.SubtitleLang)
	}
	cmd := exec.Command(mpvBinary(cfg), args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return err
	}
	mpvProc = cmd.Process

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if mpvAlive() {
			statusLines = 2
			StatusStart()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("mpv started but socket never appeared")
}

// PlayStream sends a stream to mpv — appends if running, launches if not.
func PlayStream(cfg AppConfig, streamURL string) error {
	if mpvAlive() {
		conn := ipcDial()
		if conn != nil {
			defer conn.Close()
			resp := ipcSend(conn, []any{"loadfile", streamURL, "append-play"})
			if resp != nil && resp["error"] == "success" {
				statusLines = 2
				StatusStart()
				return nil
			}
		}
	}
	return launchMpv(cfg, streamURL)
}


// ── Status ────────────────────────────────────────────────────────────────────

var (
	lastStatus     MpvStatus
	currentVideoID string // videoID being tracked for position saving
)

// SetCurrentVideo tells the tracker which videoID is playing.
func SetCurrentVideo(id string) {
	ipcMu.Lock()
	currentVideoID = id
	ipcMu.Unlock()
}

func fetchStatus() MpvStatus {
	conn := ipcDial()
	if conn == nil {
		if lastStatus.Alive {
			lastStatus = MpvStatus{}
			statusLines = 0
		}
		return MpvStatus{}
	}
	defer conn.Close()

	get := func(prop string) any {
		resp := ipcSend(conn, []any{"get_property", prop})
		if resp == nil || resp["error"] != "success" {
			return nil
		}
		return resp["data"]
	}

	toF := func(v any) float64 {
		if v == nil {
			return 0
		}
		f, _ := v.(float64)
		return f
	}
	toB := func(v any) bool {
		b, _ := v.(bool)
		return b
	}
	toS := func(v any) string {
		s, _ := v.(string)
		return s
	}

	st := MpvStatus{
		Alive:    true,
		Pos:      toF(get("time-pos")),
		Duration: toF(get("duration")),
		Percent:  toF(get("percent-pos")),
		Cache:    toF(get("cache-buffering-state")),
		Paused:   toB(get("pause")),
		Title:    toS(get("media-title")),
	}
	if st.Duration == 0 && lastStatus.Duration > 0 {
		st.Duration = lastStatus.Duration
		st.Percent = lastStatus.Percent
	}
	lastStatus = st
	statusLines = 2
	return st
}

func cleanTitle(s string) string {
	if s == "" {
		return ""
	}
	once, err := url.QueryUnescape(s)
	if err == nil {
		s = once
	}
	twice, err := url.QueryUnescape(s)
	if err == nil {
		s = twice
	}
	for _, ext := range []string{".mkv", ".mp4", ".avi", ".m4v", ".ts", ".mov", ".wmv"} {
		if strings.HasSuffix(strings.ToLower(s), ext) {
			s = s[:len(s)-len(ext)]
			break
		}
	}
	return strings.TrimSpace(s)
}

func fmtSecs(s float64) string {
	if s < 0 {
		s = 0
	}
	t := int(s)
	h, m, sec := t/3600, (t%3600)/60, t%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%02d:%02d", m, sec)
}

func buildBar(st MpvStatus) string {
	if !st.Alive {
		return ""
	}
	title := ""
	if t := cleanTitle(st.Title); t != "" {
		maxW := tw() - 38
		if maxW < 10 {
			maxW = 10
		}
		runes := []rune(t)
		if len(runes) > maxW {
			t = string(runes[:maxW]) + "…"
		}
		title = grey(t) + "  "
	}
	pause := ""
	if st.Paused {
		pause = yell(" [paused]")
	}
	return fmt.Sprintf("%s%s%s / %s (%.0f%%)%s  Cache: %.0fs",
		title, accent("▶ "),
		bold(fmtSecs(st.Pos)),
		fmtSecs(st.Duration),
		st.Percent, pause, st.Cache,
	)
}

// ── Status bar ticker ─────────────────────────────────────────────────────────

var (
	statusLines int
	tickStop    chan struct{}
	tickDone    chan struct{}
)

func StatusStart() {
	if tickStop != nil {
		return
	}
	tickStop = make(chan struct{})
	tickDone = make(chan struct{})
	go func() {
		defer close(tickDone)
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-tickStop:
				return
			case <-t.C:
				if statusLines > 0 {
					rewriteBar()
				}
			}
		}
	}()
}

func StatusStop() {
	if tickStop != nil {
		close(tickStop)
		<-tickDone
		tickStop = nil
		tickDone = nil
	}
}

func rewriteBar() {
	st := fetchStatus()

	// Save position to history every tick
	if st.Alive && currentVideoID != "" && st.Duration > 0 {
		go UpdatePosition(currentVideoID, st.Pos, st.Duration, st.Percent)
	}

	bar := buildBar(st)
	if bar == "" {
		return
	}
	_, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || h < 4 {
		return
	}
	w := tw()
	fmt.Print("\0337")
	fmt.Printf("\033[%d;1H\033[2K\r%s\033[K", h-1, grey(strings.Repeat("─", w)))
	fmt.Printf("\033[%d;1H\033[2K\r  %s\033[K", h, bar)
	fmt.Print("\0338")
}

package poster

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKittyDetect(t *testing.T) {
	bin := t.TempDir()
	tmux := filepath.Join(bin, "tmux")
	fake := func(answer string) {
		os.WriteFile(tmux, []byte("#!/bin/sh\necho '"+answer+"'\n"), 0755)
	}
	t.Setenv("PATH", bin)
	cases := []struct {
		name string
		env  map[string]string
		tmux string
		want bool
	}{
		{"kitty", map[string]string{"TERM": "xterm-kitty"}, "", true},
		{"ghostty names itself", map[string]string{"TERM_PROGRAM": "ghostty"}, "", true},
		{"wezterm from kitty", map[string]string{"TERM_PROGRAM": "WezTerm", "KITTY_WINDOW_ID": "1"}, "", false},
		{"tmux in kitty", map[string]string{"TMUX": "x", "TERM_PROGRAM": "tmux", "TERM": "tmux-256color"}, "xterm-kitty:xterm-kitty", true},
		{"tmux in alacritty, stale kitty id", map[string]string{"TMUX": "x", "TERM_PROGRAM": "tmux", "KITTY_WINDOW_ID": "3"}, "alacritty:alacritty", false},
		{"tmux over ssh", map[string]string{"TMUX": "x", "TERM_PROGRAM": "tmux", "SSH_TTY": "/dev/pts/1"}, "xterm-kitty:xterm-kitty", false},
		{"old tmux with no answer", map[string]string{"TMUX": "x", "TERM_PROGRAM": "tmux", "TERM": "xterm-kitty"}, ":", true},
		{"ghostty opened from a tmux shell", map[string]string{"TMUX": "x", "TERM_PROGRAM": "ghostty", "TERM": "xterm-ghostty"}, "alacritty:alacritty", true},
		{"plain xterm", map[string]string{"TERM": "xterm-256color"}, "", false},
	}
	for _, c := range cases {
		for _, k := range []string{"TMUX", "TERM", "TERM_PROGRAM", "KITTY_WINDOW_ID", "SSH_TTY", "SSH_CONNECTION"} {
			t.Setenv(k, c.env[k])
		}
		os.Remove(tmux)
		if c.tmux != "" {
			fake(c.tmux)
		}
		if got := kittyDetect(); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestOpenURLWebOnly(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "/home/x/run.sh", "smb://host/share", "javascript:alert(1)", "C:\\x.exe"} {
		if OpenURL(u) == nil {
			t.Errorf("%q was opened", u)
		}
	}
}

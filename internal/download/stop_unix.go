//go:build !windows

package download

import (
	"os"
	"syscall"
)

// terminate asks a process to exit cleanly.
func terminate(p *os.Process) bool { return p.Signal(syscall.SIGTERM) == nil }

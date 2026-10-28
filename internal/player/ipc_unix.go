//go:build !windows

package player

import (
	"net"
	"time"
)

func ipcDial() net.Conn {
	c, err := net.DialTimeout("unix", socketPath(), 400*time.Millisecond)
	if err != nil {
		return nil
	}
	return c
}

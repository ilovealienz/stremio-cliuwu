//go:build windows

package main

import (
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

func ipcDial() net.Conn {
	timeout := 400 * time.Millisecond
	conn, err := winio.DialPipe(socketPath(), &timeout)
	if err != nil {
		return nil
	}
	return conn
}

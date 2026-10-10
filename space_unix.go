//go:build !windows

package main

import "golang.org/x/sys/unix"

// freeSpace reports the bytes available at dir, and whether the figure could
// be obtained at all. A false means don't guess — skip the check rather than
// refuse a download over a number we don't have.
//
// Bavail rather than Bfree: ext4 and friends reserve a slice of the disk for
// root, and an unprivileged process can't write into it, so Bfree would
// promise space that isn't actually there.
func freeSpace(dir string) (uint64, bool) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, false
	}
	// Bsize is int64 on linux and uint32 on darwin; the conversion covers both.
	return st.Bavail * uint64(st.Bsize), true
}

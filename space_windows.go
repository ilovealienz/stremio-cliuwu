//go:build windows

package main

import "golang.org/x/sys/windows"

// freeSpace reports the bytes available at dir, and whether the figure could
// be obtained at all. See the unix build for why a false means skip the check.
func freeSpace(dir string) (uint64, bool) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, false
	}
	// The first out-parameter is what this user may write, which is the one
	// that matters when a quota is in force.
	var avail, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &total, &totalFree); err != nil {
		return 0, false
	}
	return avail, true
}

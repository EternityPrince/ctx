//go:build linux || freebsd || openbsd || netbsd || dragonfly || solaris

package app

import (
	"os"
	"syscall"
)

func travelPeakRSSBytes(state *os.ProcessState) int64 {
	if state == nil {
		return 0
	}
	usage, ok := state.SysUsage().(*syscall.Rusage)
	if !ok || usage == nil || usage.Maxrss <= 0 {
		return 0
	}
	return usage.Maxrss * 1024
}

//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly && !solaris && !windows

package app

import "os"

func travelPeakRSSBytes(state *os.ProcessState) int64 {
	return 0
}

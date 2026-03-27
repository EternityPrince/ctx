//go:build !darwin && !linux && !windows

package app

import "fmt"

func newNativeWatchBackend(root string) (watchBackend, error) {
	_ = root
	return nil, fmt.Errorf("native watch backend unavailable")
}

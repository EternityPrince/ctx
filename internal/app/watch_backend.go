package app

import "time"

func newWatchBackend(root string) (watchBackend, error) {
	backend, err := newNativeWatchBackend(root)
	if err == nil {
		return backend, nil
	}
	return &pollWatchBackend{mode: "poll(fallback)"}, nil
}

type pollWatchBackend struct {
	mode string
}

func (b *pollWatchBackend) Mode() string {
	if b.mode != "" {
		return b.mode
	}
	return "poll"
}

func (b *pollWatchBackend) EventDriven() bool { return false }

func (b *pollWatchBackend) Wait(timeout time.Duration) (watchWake, error) {
	if timeout > 0 {
		time.Sleep(timeout)
	}
	return watchWake{Triggered: true, Reason: "poll"}, nil
}

func (b *pollWatchBackend) Close() error { return nil }

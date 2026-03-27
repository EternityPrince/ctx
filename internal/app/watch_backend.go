package app

import "time"

func newWatchBackend(root string) (watchBackend, error) {
	backend, err := newNativeWatchBackend(root)
	if err == nil {
		return backend, nil
	}
	return &pollWatchBackend{}, nil
}

type pollWatchBackend struct{}

func (b *pollWatchBackend) Mode() string { return "poll" }

func (b *pollWatchBackend) Wait(timeout time.Duration) (watchWake, error) {
	if timeout > 0 {
		time.Sleep(timeout)
	}
	return watchWake{Triggered: true, Reason: "poll"}, nil
}

func (b *pollWatchBackend) Close() error { return nil }

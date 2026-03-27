//go:build darwin

package app

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"syscall"
	"time"

	"github.com/vladimirkasterin/ctx/internal/filter"
)

type darwinWatchBackend struct {
	root string
	kq   int
	fds  []int
}

func newNativeWatchBackend(root string) (watchBackend, error) {
	kq, err := syscall.Kqueue()
	if err != nil {
		return nil, fmt.Errorf("create kqueue watcher: %w", err)
	}
	backend := &darwinWatchBackend{root: root, kq: kq}
	if err := backend.rebuild(); err != nil {
		_ = backend.Close()
		return nil, err
	}
	return backend, nil
}

func (b *darwinWatchBackend) Mode() string { return "events+kqueue" }

func (b *darwinWatchBackend) EventDriven() bool { return true }

func (b *darwinWatchBackend) Wait(timeout time.Duration) (watchWake, error) {
	events := make([]syscall.Kevent_t, 16)
	var ts *syscall.Timespec
	if timeout > 0 {
		value := syscall.NsecToTimespec(timeout.Nanoseconds())
		ts = &value
	}

	n, err := syscall.Kevent(b.kq, nil, events, ts)
	if err != nil {
		if err == syscall.EINTR {
			return watchWake{Triggered: false, Reason: "interrupt"}, nil
		}
		return watchWake{}, fmt.Errorf("wait for filesystem events: %w", err)
	}
	if n == 0 {
		return watchWake{Triggered: false, Reason: "timeout"}, nil
	}
	if err := b.rebuild(); err != nil {
		return watchWake{}, err
	}
	return watchWake{Triggered: true, Reason: "event"}, nil
}

func (b *darwinWatchBackend) Close() error {
	b.closeFDs()
	if b.kq > 0 {
		if err := syscall.Close(b.kq); err != nil {
			return fmt.Errorf("close kqueue watcher: %w", err)
		}
		b.kq = 0
	}
	return nil
}

func (b *darwinWatchBackend) rebuild() error {
	b.closeFDs()

	walker, err := filter.NewWalker(b.root, false, "index")
	if err != nil {
		return err
	}

	changes := make([]syscall.Kevent_t, 0, 64)
	err = filepath.WalkDir(b.root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return filter.HandleWalkError(path, walkErr)
		}

		relPath := ""
		if path != b.root {
			value, err := filepath.Rel(b.root, path)
			if err != nil {
				return fmt.Errorf("make relative watch path: %w", err)
			}
			relPath = filepath.ToSlash(value)
		}

		if !entry.IsDir() {
			return nil
		}
		if path != b.root {
			if skip, _, err := walker.ShouldSkipDirectory(path, relPath, entry.Name()); err != nil {
				return err
			} else if skip {
				return filepath.SkipDir
			}
		}

		fd, err := syscall.Open(path, syscall.O_EVTONLY, 0)
		if err != nil {
			return fmt.Errorf("open watch directory %s: %w", path, err)
		}
		b.fds = append(b.fds, fd)
		changes = append(changes, syscall.Kevent_t{
			Ident:  uint64(fd),
			Filter: syscall.EVFILT_VNODE,
			Flags:  syscall.EV_ADD | syscall.EV_CLEAR,
			Fflags: syscall.NOTE_WRITE | syscall.NOTE_DELETE | syscall.NOTE_RENAME | syscall.NOTE_EXTEND | syscall.NOTE_ATTRIB,
		})
		return nil
	})
	if err != nil {
		b.closeFDs()
		return err
	}
	if len(changes) == 0 {
		return nil
	}
	if _, err := syscall.Kevent(b.kq, changes, nil, nil); err != nil {
		b.closeFDs()
		return fmt.Errorf("register kqueue directories: %w", err)
	}
	return nil
}

func (b *darwinWatchBackend) closeFDs() {
	for _, fd := range b.fds {
		_ = syscall.Close(fd)
	}
	b.fds = nil
}

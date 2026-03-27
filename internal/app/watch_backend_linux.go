//go:build linux

package app

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"github.com/vladimirkasterin/ctx/internal/filter"
)

const linuxWatchMask = syscall.IN_ATTRIB |
	syscall.IN_CLOSE_WRITE |
	syscall.IN_CREATE |
	syscall.IN_DELETE |
	syscall.IN_DELETE_SELF |
	syscall.IN_MODIFY |
	syscall.IN_MOVE_SELF |
	syscall.IN_MOVED_FROM |
	syscall.IN_MOVED_TO

type linuxWatchBackend struct {
	root string
	fd   int
	wds  map[int]string
}

func newNativeWatchBackend(root string) (watchBackend, error) {
	fd, err := syscall.InotifyInit()
	if err != nil {
		return nil, fmt.Errorf("create inotify watcher: %w", err)
	}
	backend := &linuxWatchBackend{
		root: root,
		fd:   fd,
		wds:  make(map[int]string),
	}
	if err := backend.rebuild(); err != nil {
		_ = backend.Close()
		return nil, err
	}
	return backend, nil
}

func (b *linuxWatchBackend) Mode() string { return "events+inotify" }

func (b *linuxWatchBackend) EventDriven() bool { return true }

func (b *linuxWatchBackend) Wait(timeout time.Duration) (watchWake, error) {
	ready, err := waitReadableFD(b.fd, timeout)
	if err != nil {
		return watchWake{}, fmt.Errorf("wait for inotify events: %w", err)
	}
	if !ready {
		return watchWake{Triggered: false, Reason: "timeout"}, nil
	}

	buf := make([]byte, 16*1024)
	n, err := syscall.Read(b.fd, buf)
	if err != nil {
		if err == syscall.EINTR {
			return watchWake{Triggered: false, Reason: "interrupt"}, nil
		}
		return watchWake{}, fmt.Errorf("read inotify events: %w", err)
	}
	if n == 0 {
		return watchWake{Triggered: false, Reason: "timeout"}, nil
	}
	if err := b.rebuild(); err != nil {
		return watchWake{}, err
	}
	return watchWake{Triggered: true, Reason: "event"}, nil
}

func (b *linuxWatchBackend) Close() error {
	b.clearWatches()
	if b.fd > 0 {
		if err := syscall.Close(b.fd); err != nil {
			return fmt.Errorf("close inotify watcher: %w", err)
		}
		b.fd = 0
	}
	return nil
}

func (b *linuxWatchBackend) rebuild() error {
	b.clearWatches()

	walker, err := filter.NewWalker(b.root, false, "index")
	if err != nil {
		return err
	}
	return filepath.WalkDir(b.root, func(path string, entry fs.DirEntry, walkErr error) error {
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

		wd, err := syscall.InotifyAddWatch(b.fd, path, linuxWatchMask)
		if err != nil {
			return fmt.Errorf("watch directory %s: %w", path, err)
		}
		b.wds[wd] = path
		return nil
	})
}

func (b *linuxWatchBackend) clearWatches() {
	for wd := range b.wds {
		_, _ = syscall.InotifyRmWatch(b.fd, uint32(wd))
	}
	b.wds = make(map[int]string)
}

func waitReadableFD(fd int, timeout time.Duration) (bool, error) {
	var readSet syscall.FdSet
	fdSet(fd, &readSet)

	var tv *syscall.Timeval
	if timeout > 0 {
		value := syscall.NsecToTimeval(timeout.Nanoseconds())
		tv = &value
	}

	n, err := syscall.Select(fd+1, &readSet, nil, nil, tv)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func fdSet(fd int, set *syscall.FdSet) {
	if fd < 0 {
		return
	}
	bitsPerWord := int(unsafe.Sizeof(set.Bits[0]) * 8)
	index := fd / bitsPerWord
	if index < 0 || index >= len(set.Bits) {
		return
	}
	set.Bits[index] |= 1 << (uint(fd) % uint(bitsPerWord))
}

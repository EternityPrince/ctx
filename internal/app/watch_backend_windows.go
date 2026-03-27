//go:build windows

package app

import (
	"fmt"
	"syscall"
	"time"
)

const windowsWatchMask = syscall.FILE_NOTIFY_CHANGE_FILE_NAME |
	syscall.FILE_NOTIFY_CHANGE_DIR_NAME |
	syscall.FILE_NOTIFY_CHANGE_ATTRIBUTES |
	syscall.FILE_NOTIFY_CHANGE_SIZE |
	syscall.FILE_NOTIFY_CHANGE_LAST_WRITE |
	syscall.FILE_NOTIFY_CHANGE_CREATION

const windowsErrorInvalidHandle = syscall.Errno(6)

type windowsWatchBackend struct {
	handle syscall.Handle
	events chan watchWake
	errs   chan error
	done   chan struct{}
}

func newNativeWatchBackend(root string) (watchBackend, error) {
	ptr, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return nil, fmt.Errorf("encode watch root: %w", err)
	}
	handle, err := syscall.CreateFile(
		ptr,
		syscall.FILE_LIST_DIRECTORY,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("open directory watcher: %w", err)
	}

	backend := &windowsWatchBackend{
		handle: handle,
		events: make(chan watchWake, 8),
		errs:   make(chan error, 1),
		done:   make(chan struct{}),
	}
	go backend.readLoop()
	return backend, nil
}

func (b *windowsWatchBackend) Mode() string { return "events+rdcw" }

func (b *windowsWatchBackend) EventDriven() bool { return true }

func (b *windowsWatchBackend) Wait(timeout time.Duration) (watchWake, error) {
	if timeout <= 0 {
		select {
		case wake := <-b.events:
			return wake, nil
		case err := <-b.errs:
			return watchWake{}, err
		case <-b.done:
			return watchWake{Triggered: false, Reason: "interrupt"}, nil
		}
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case wake := <-b.events:
		return wake, nil
	case err := <-b.errs:
		return watchWake{}, err
	case <-timer.C:
		return watchWake{Triggered: false, Reason: "timeout"}, nil
	case <-b.done:
		return watchWake{Triggered: false, Reason: "interrupt"}, nil
	}
}

func (b *windowsWatchBackend) Close() error {
	select {
	case <-b.done:
		return nil
	default:
		close(b.done)
	}
	if b.handle != 0 {
		if err := syscall.CloseHandle(b.handle); err != nil && err != windowsErrorInvalidHandle {
			return fmt.Errorf("close windows watcher: %w", err)
		}
		b.handle = 0
	}
	return nil
}

func (b *windowsWatchBackend) readLoop() {
	buf := make([]byte, 64*1024)
	for {
		select {
		case <-b.done:
			return
		default:
		}

		var read uint32
		err := syscall.ReadDirectoryChanges(
			b.handle,
			&buf[0],
			uint32(len(buf)),
			true,
			windowsWatchMask,
			&read,
			nil,
			0,
		)
		if err != nil {
			if b.isClosing() || err == windowsErrorInvalidHandle || err == syscall.ERROR_OPERATION_ABORTED {
				return
			}
			select {
			case b.errs <- fmt.Errorf("read windows filesystem events: %w", err):
			default:
			}
			return
		}
		if read == 0 {
			continue
		}
		select {
		case b.events <- watchWake{Triggered: true, Reason: "event"}:
		default:
		}
	}
}

func (b *windowsWatchBackend) isClosing() bool {
	select {
	case <-b.done:
		return true
	default:
		return false
	}
}

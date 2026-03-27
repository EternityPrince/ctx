package app

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/vladimirkasterin/ctx/internal/cli"
)

type fakeWatchBackend struct {
	mode   string
	wakes  []watchWake
	hook   func(call int)
	calls  int
	closed bool
}

func (b *fakeWatchBackend) Mode() string { return b.mode }

func (b *fakeWatchBackend) Wait(timeout time.Duration) (watchWake, error) {
	_ = timeout
	b.calls++
	if b.hook != nil {
		b.hook(b.calls)
	}
	if len(b.wakes) == 0 {
		return watchWake{Triggered: false, Reason: "timeout"}, nil
	}
	wake := b.wakes[0]
	b.wakes = b.wakes[1:]
	return wake, nil
}

func (b *fakeWatchBackend) Close() error {
	b.closed = true
	return nil
}

func TestRunWatchCycleBootstrapsAndThenIdlesFromCache(t *testing.T) {
	t.Setenv("CTX_HOME", t.TempDir())

	root := t.TempDir()
	writeProjectStateFixture(t, root, "Cargo.toml", "[package]\nname = \"watch-demo\"\nedition = \"2021\"\n")
	writeProjectStateFixture(t, root, "src/lib.rs", "pub fn run() {}\n")

	first, err := runWatchCycle(root)
	if err != nil {
		t.Fatalf("runWatchCycle bootstrap returned error: %v", err)
	}
	if first.Action != "bootstrap" || first.Snapshot.ID == 0 {
		t.Fatalf("expected bootstrap action, got %+v", first)
	}

	second, err := runWatchCycle(root)
	if err != nil {
		t.Fatalf("runWatchCycle idle returned error: %v", err)
	}
	if second.Action != "idle" || !second.Plan.CacheHit || second.Plan.Changes.Count() != 0 {
		t.Fatalf("expected cached idle plan after bootstrap, got %+v", second)
	}
}

func TestRunWatchAppliesIncrementalUpdate(t *testing.T) {
	t.Setenv("CTX_HOME", t.TempDir())

	root := t.TempDir()
	writeProjectStateFixture(t, root, "Cargo.toml", "[package]\nname = \"watch-demo\"\nedition = \"2021\"\n")
	writeProjectStateFixture(t, root, "src/lib.rs", "pub fn run() {}\n")

	backend := &fakeWatchBackend{
		mode:  "events",
		wakes: []watchWake{{Triggered: true, Reason: "event"}, {Triggered: false, Reason: "timeout"}},
		hook: func(call int) {
			if call == 1 {
				writeProjectStateFixture(t, root, "src/lib.rs", "pub fn run() {\n    helper();\n}\n\nfn helper() {}\n")
			}
		},
	}
	var out bytes.Buffer
	if err := runWatchLoop(cli.Command{
		Name:          "watch",
		Root:          root,
		OutputMode:    cli.OutputHuman,
		WatchInterval: time.Millisecond,
		WatchCycles:   2,
		Explain:       true,
	}, &out, backend); err != nil {
		t.Fatalf("runWatchLoop returned error: %v", err)
	}
	text := stripANSICodes(out.String())
	for _, expected := range []string{"Watching ", "mode=events", "action=bootstrap", "action=update", "cache_hit=", "Watch complete: cycles=2"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected %q in watch output, got:\n%s", expected, text)
		}
	}
}

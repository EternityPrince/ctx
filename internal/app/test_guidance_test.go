package app

import (
	"strings"
	"testing"

	"github.com/vladimirkasterin/ctx/internal/core"
)

func TestBuildSymbolTestGuidancePrefersDirectThenNearbyTests(t *testing.T) {
	state := openIndexedAppState(t, map[string]string{
		"go.mod": "module example.com/testplan\n\ngo 1.26\n",
		"pkg/service.go": `package pkg

func Run() int {
	return helper()
}

func helper() int {
	return 1
}
`,
		"pkg/service_test.go": `package pkg

import "testing"

func TestRun(t *testing.T) {
	if Run() == 0 {
		t.Fatal("unexpected zero")
	}
}

func TestHelper(t *testing.T) {
	if helper() == 0 {
		t.Fatal("unexpected zero")
	}
}
`,
		"api/handler.go": `package api

import "example.com/testplan/pkg"

func Handle() int {
	return pkg.Run()
}
`,
		"api/handler_test.go": `package api

import "testing"

func TestHandle(t *testing.T) {
	if Handle() == 0 {
		t.Fatal("unexpected zero")
	}
}
`,
	})

	matches, err := state.Store.FindSymbols("Run")
	if err != nil {
		t.Fatalf("FindSymbols returned error: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected Run symbol match")
	}
	view, err := state.Store.LoadSymbolView(matches[0].SymbolKey)
	if err != nil {
		t.Fatalf("LoadSymbolView returned error: %v", err)
	}

	guidance, err := buildSymbolTestGuidance(state.Store, view, 6)
	if err != nil {
		t.Fatalf("buildSymbolTestGuidance returned error: %v", err)
	}
	if len(guidance.ReadBefore) < 2 {
		t.Fatalf("expected direct and nearby tests, got %+v", guidance.ReadBefore)
	}
	if guidance.ReadBefore[0].Name != "TestRun" {
		t.Fatalf("expected direct TestRun to be first, got %+v", guidance.ReadBefore)
	}
	if guidance.Signal != "direct=1 strong | nearby=2" {
		t.Fatalf("unexpected guidance signal: %q", guidance.Signal)
	}

	joined := guidance.ReadBefore[1].Why + " | " + guidance.ReadBefore[2].Why
	if !strings.Contains(joined, "covers caller Handle") || !strings.Contains(joined, "covers callee helper") {
		t.Fatalf("expected nearby caller/callee guidance, got %+v", guidance.ReadBefore)
	}
}

func TestBuildReportTestWatchFindsRecentWeakTestAreas(t *testing.T) {
	t.Setenv("CTX_HOME", t.TempDir())

	root := t.TempDir()
	writeProjectStateFixture(t, root, "go.mod", "module example.com/watch\n\ngo 1.26\n")
	writeProjectStateFixture(t, root, "pkg/service.go", `package pkg

func Run() int { return 1 }
`)
	writeProjectStateFixture(t, root, "pkg/service_test.go", `package pkg

import "testing"

func TestRun(t *testing.T) {
	if Run() == 0 {
		t.Fatal("unexpected zero")
	}
}
`)
	writeProjectStateFixture(t, root, "api/handler.go", `package api

import "example.com/watch/pkg"

func Handle() int { return pkg.Run() }
`)

	state, err := openProjectState(root)
	if err != nil {
		t.Fatalf("openProjectState returned error: %v", err)
	}
	if _, committed, err := projectService.ApplySnapshot(state, "index", "initial", false); err != nil {
		_ = state.Close()
		t.Fatalf("ApplySnapshot initial returned error: %v", err)
	} else if !committed {
		_ = state.Close()
		t.Fatal("expected initial snapshot to commit")
	}
	if err := state.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	writeProjectStateFixture(t, root, "api/handler.go", `package api

import "example.com/watch/pkg"

func Handle() int {
	value := pkg.Run()
	if value == 0 {
		return 1
	}
	return value
}
`)

	refreshed, err := openProjectState(root)
	if err != nil {
		t.Fatalf("openProjectState refresh returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = refreshed.Close()
	})
	if _, committed, err := projectService.ApplySnapshot(refreshed, "update", "change handler", false); err != nil {
		t.Fatalf("ApplySnapshot update returned error: %v", err)
	} else if !committed {
		t.Fatal("expected update snapshot to commit")
	}

	report, err := refreshed.Store.LoadReportView(8)
	if err != nil {
		t.Fatalf("LoadReportView returned error: %v", err)
	}
	watch, err := buildReportTestWatch(refreshed.Store, report)
	if err != nil {
		t.Fatalf("buildReportTestWatch returned error: %v", err)
	}
	if len(watch.WeakChangedAreas) == 0 {
		t.Fatalf("expected weak changed areas, got %+v", watch)
	}
	if watch.WeakChangedAreas[0].FilePath != "api/handler.go" {
		t.Fatalf("expected api/handler.go as weak changed area, got %+v", watch.WeakChangedAreas)
	}
	if !strings.Contains(watch.WeakChangedAreas[0].Risk, "recent+weak-tests") {
		t.Fatalf("expected recent weak-test risk, got %+v", watch.WeakChangedAreas[0])
	}
}

func openIndexedAppState(t *testing.T, files map[string]string) core.ProjectState {
	t.Helper()
	t.Setenv("CTX_HOME", t.TempDir())

	root := t.TempDir()
	for relPath, content := range files {
		writeProjectStateFixture(t, root, relPath, content)
	}

	state, err := openProjectState(root)
	if err != nil {
		t.Fatalf("openProjectState returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = state.Close()
	})
	if _, committed, err := projectService.ApplySnapshot(state, "index", "test guidance", false); err != nil {
		t.Fatalf("ApplySnapshot returned error: %v", err)
	} else if !committed {
		t.Fatal("expected snapshot to be committed")
	}
	return state
}

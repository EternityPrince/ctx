package core_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/vladimirkasterin/ctx/internal/adapter"
	"github.com/vladimirkasterin/ctx/internal/core"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

func TestApplySnapshotIndexesPythonFileSymbolsInsideGoProject(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for mixed project indexing tests")
	}

	t.Setenv("CTX_HOME", t.TempDir())

	root := t.TempDir()
	writeServiceFixture(t, root, "go.mod", "module example.com/mixed\n\ngo 1.26\n")
	writeServiceFixture(t, root, "main.go", "package main\n\nfunc main() {}\n")
	writeServiceFixture(t, root, "internal/adapter/python/runtime/__init__.py", "")
	writeServiceFixture(t, root, "internal/adapter/python/runtime/analyzer.py", `class Analyzer:
    def run(self) -> int:
        return helper()


def helper() -> int:
    return 1
`)

	service := core.NewService(adapter.NewAdapter())
	state, err := service.OpenProject(root)
	if err != nil {
		t.Fatalf("OpenProject returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = state.Close()
	})

	if _, committed, err := service.ApplySnapshot(state, "index", "test mixed project", false); err != nil {
		t.Fatalf("ApplySnapshot returned error: %v", err)
	} else if !committed {
		t.Fatal("expected snapshot to be committed")
	}

	symbols, err := state.Store.LoadFileSymbols("internal/adapter/python/runtime/analyzer.py")
	if err != nil {
		t.Fatalf("LoadFileSymbols returned error: %v", err)
	}
	if len(symbols) == 0 {
		t.Fatal("expected python file symbols to be indexed")
	}

	assertIndexedSymbol(t, symbols, "internal.adapter.python.runtime.analyzer.Analyzer", "class")
	assertIndexedSymbol(t, symbols, "internal.adapter.python.runtime.analyzer.Analyzer.run", "method")
	assertIndexedSymbol(t, symbols, "internal.adapter.python.runtime.analyzer.helper", "func")
}

func TestApplySnapshotReturnsCurrentSnapshotWhenProjectIsUnchanged(t *testing.T) {
	t.Setenv("CTX_HOME", t.TempDir())

	root := t.TempDir()
	writeServiceFixture(t, root, "go.mod", "module example.com/stable\n\ngo 1.26\n")
	writeServiceFixture(t, root, "main.go", "package main\n\nfunc main() {}\n")

	service := core.NewService(adapter.NewAdapter())
	state, err := service.OpenProject(root)
	if err != nil {
		t.Fatalf("OpenProject returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = state.Close()
	})

	first, committed, err := service.ApplySnapshot(state, "index", "initial", false)
	if err != nil {
		t.Fatalf("ApplySnapshot returned error: %v", err)
	}
	if !committed {
		t.Fatal("expected initial snapshot to be committed")
	}

	if err := state.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	reopened, err := service.OpenProject(root)
	if err != nil {
		t.Fatalf("OpenProject second pass returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = reopened.Close()
	})

	second, committed, err := service.ApplySnapshot(reopened, "index", "noop", false)
	if err != nil {
		t.Fatalf("ApplySnapshot second pass returned error: %v", err)
	}
	if committed {
		t.Fatal("did not expect unchanged project to commit a new snapshot")
	}
	if second.ID != first.ID {
		t.Fatalf("expected unchanged project to return current snapshot %d, got %d", first.ID, second.ID)
	}
}

func TestPlanUsesChangeCacheForUnchangedAndRepeatedChangedState(t *testing.T) {
	t.Setenv("CTX_HOME", t.TempDir())

	root := t.TempDir()
	writeServiceFixture(t, root, "go.mod", "module example.com/cache\n\ngo 1.26\n")
	writeServiceFixture(t, root, "main.go", "package main\n\nfunc main() {}\n")

	service := core.NewService(adapter.NewAdapter())
	state, err := service.OpenProject(root)
	if err != nil {
		t.Fatalf("OpenProject returned error: %v", err)
	}
	if _, committed, err := service.ApplySnapshot(state, "index", "initial", false); err != nil {
		_ = state.Close()
		t.Fatalf("ApplySnapshot returned error: %v", err)
	} else if !committed {
		_ = state.Close()
		t.Fatal("expected initial snapshot to be committed")
	}
	if err := state.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	reopened, err := service.OpenProject(root)
	if err != nil {
		t.Fatalf("OpenProject unchanged returned error: %v", err)
	}
	noopPlan := service.Plan(reopened, false)
	if !noopPlan.CacheHit || noopPlan.Changes.Count() != 0 {
		_ = reopened.Close()
		t.Fatalf("expected unchanged plan to come from cache, got %+v", noopPlan)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("Close unchanged state returned error: %v", err)
	}

	writeServiceFixture(t, root, "main.go", "package main\n\nfunc main() {\n\thelper()\n}\n\nfunc helper() {}\n")
	changed, err := service.OpenProject(root)
	if err != nil {
		t.Fatalf("OpenProject changed returned error: %v", err)
	}
	firstPlan := service.Plan(changed, false)
	if firstPlan.CacheHit {
		_ = changed.Close()
		t.Fatalf("expected first changed plan to be computed, got %+v", firstPlan)
	}
	secondPlan := service.Plan(changed, false)
	if !secondPlan.CacheHit || secondPlan.Changes.Count() == 0 {
		_ = changed.Close()
		t.Fatalf("expected repeated changed plan to hit cache, got %+v", secondPlan)
	}
	if err := changed.Close(); err != nil {
		t.Fatalf("Close changed state returned error: %v", err)
	}
}

func writeServiceFixture(t *testing.T, root, relPath, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertIndexedSymbol(t *testing.T, symbols []storage.SymbolMatch, qname, kind string) {
	t.Helper()
	for _, symbol := range symbols {
		if symbol.QName == qname {
			if symbol.Kind != kind {
				t.Fatalf("unexpected symbol kind for %s: got %q want %q", qname, symbol.Kind, kind)
			}
			return
		}
	}
	t.Fatalf("expected symbol %s in %+v", qname, symbols)
}

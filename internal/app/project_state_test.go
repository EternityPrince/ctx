package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/vladimirkasterin/ctx/internal/cli"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

func TestOpenPreparedProjectStateAutoRefreshesPythonChangesInMixedProject(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for mixed project app tests")
	}

	t.Setenv("CTX_HOME", t.TempDir())

	root := t.TempDir()
	writeProjectStateFixture(t, root, "go.mod", "module example.com/mixed\n\ngo 1.26\n")
	writeProjectStateFixture(t, root, "main.go", "package main\n\nfunc main() {}\n")
	writeProjectStateFixture(t, root, "pkg/service.py", "def run() -> int:\n    return 1\n")

	state, err := openProjectState(root)
	if err != nil {
		t.Fatalf("openProjectState returned error: %v", err)
	}
	if _, committed, err := projectService.ApplySnapshot(state, "index", "initial", false); err != nil {
		_ = state.Close()
		t.Fatalf("ApplySnapshot returned error: %v", err)
	} else if !committed {
		_ = state.Close()
		t.Fatal("expected initial snapshot to be committed")
	}
	if err := state.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	writeProjectStateFixture(t, root, "pkg/service.py", "def run() -> int:\n    return normalize()\n\n\ndef normalize() -> int:\n    return 1\n")

	refreshed, err := openPreparedProjectState(cli.Command{Name: "shell", Root: root})
	if err != nil {
		t.Fatalf("openPreparedProjectState returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = refreshed.Close()
	})

	symbols, err := refreshed.Store.LoadFileSymbols("pkg/service.py")
	if err != nil {
		t.Fatalf("LoadFileSymbols returned error: %v", err)
	}
	assertStoredSymbol(t, symbols, "pkg.service.run", "func")
	assertStoredSymbol(t, symbols, "pkg.service.normalize", "func")
}

func TestOpenPreparedProjectStateAutoRefreshesRustChanges(t *testing.T) {
	t.Setenv("CTX_HOME", t.TempDir())

	root := t.TempDir()
	writeProjectStateFixture(t, root, "Cargo.toml", "[package]\nname = \"demo\"\nedition = \"2021\"\n")
	writeProjectStateFixture(t, root, "src/lib.rs", "pub fn run() {}\n")

	state, err := openProjectState(root)
	if err != nil {
		t.Fatalf("openProjectState returned error: %v", err)
	}
	if _, committed, err := projectService.ApplySnapshot(state, "index", "initial", false); err != nil {
		_ = state.Close()
		t.Fatalf("ApplySnapshot returned error: %v", err)
	} else if !committed {
		_ = state.Close()
		t.Fatal("expected initial snapshot to be committed")
	}
	if err := state.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	writeProjectStateFixture(t, root, "src/lib.rs", "pub fn run() {\n    helper();\n}\n\nfn helper() {}\n")

	refreshed, err := openPreparedProjectState(cli.Command{Name: "shell", Root: root})
	if err != nil {
		t.Fatalf("openPreparedProjectState returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = refreshed.Close()
	})

	symbols, err := refreshed.Store.LoadFileSymbols("src/lib.rs")
	if err != nil {
		t.Fatalf("LoadFileSymbols returned error: %v", err)
	}
	assertStoredSymbol(t, symbols, "demo::run", "func")
	assertStoredSymbol(t, symbols, "demo::helper", "func")
}

func writeProjectStateFixture(t *testing.T, root, relPath, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertStoredSymbol(t *testing.T, symbols []storage.SymbolMatch, qname, kind string) {
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

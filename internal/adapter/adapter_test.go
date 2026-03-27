package adapter

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/project"
)

func TestAnalyzeIndexesPythonSymbolsInsideGoProject(t *testing.T) {
	requirePython3(t)

	root := t.TempDir()
	writeAdapterFixture(t, root, "go.mod", "module example.com/mixed\n\ngo 1.26\n")
	writeAdapterFixture(t, root, "main.go", "package main\n\nfunc main() {}\n")
	writeAdapterFixture(t, root, "internal/tools/__init__.py", "")
	writeAdapterFixture(t, root, "internal/tools/runner.py", `class Service:
    def run(self) -> int:
        return helper()


def helper() -> int:
    return 1
`)

	info, err := project.Resolve(root)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if info.Language != "go" {
		t.Fatalf("expected go project, got %q", info.Language)
	}

	adapter := NewAdapter()
	scanned, err := adapter.Scan(root)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if !containsScanPath(scanned, "internal/tools/runner.py") {
		t.Fatalf("expected python file to be scanned, got %+v", scanned)
	}

	result, err := adapter.Analyze(info, codebase.ScanMap(scanned), nil)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	assertSymbolKind(t, result.Symbols, "example.com/mixed.main", "func")
	assertSymbolKind(t, result.Symbols, "internal.tools.runner.Service", "class")
	assertSymbolKind(t, result.Symbols, "internal.tools.runner.Service.run", "method")
	assertSymbolKind(t, result.Symbols, "internal.tools.runner.helper", "func")
}

func TestAnalyzeFiltersToPythonPackageInsideGoProject(t *testing.T) {
	requirePython3(t)

	root := t.TempDir()
	writeAdapterFixture(t, root, "go.mod", "module example.com/mixed\n\ngo 1.26\n")
	writeAdapterFixture(t, root, "main.go", "package main\n\nfunc main() {}\n")
	writeAdapterFixture(t, root, "scripts/tool.py", "def execute() -> int:\n    return 1\n")

	info, err := project.Resolve(root)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	adapter := NewAdapter()
	scanned, err := adapter.Scan(root)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	result, err := adapter.Analyze(info, codebase.ScanMap(scanned), []string{"scripts"})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if len(result.Packages) != 1 || result.Packages[0].ImportPath != "scripts" {
		t.Fatalf("expected only python package in filtered result, got %+v", result.Packages)
	}
	assertSymbolKind(t, result.Symbols, "scripts.tool.execute", "func")
	if hasQName(result.Symbols, "example.com/mixed.main") {
		t.Fatalf("did not expect go symbols in python-only filtered result: %+v", result.Symbols)
	}
}

func TestAnalyzeIndexesRustSymbolsInsideGoProject(t *testing.T) {
	root := t.TempDir()
	writeAdapterFixture(t, root, "go.mod", "module example.com/mixed\n\ngo 1.26\n")
	writeAdapterFixture(t, root, "main.go", "package main\n\nfunc main() {}\n")
	writeAdapterFixture(t, root, "tools/rust-worker/Cargo.toml", "[package]\nname = \"worker\"\nedition = \"2021\"\n")
	writeAdapterFixture(t, root, "tools/rust-worker/src/lib.rs", `pub struct Worker;

impl Worker {
    pub fn run(&self) {}
}
`)

	info, err := project.Resolve(root)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if info.Language != "go" {
		t.Fatalf("expected go project at repository root, got %q", info.Language)
	}

	adapter := NewAdapter()
	scanned, err := adapter.Scan(root)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if !containsScanPath(scanned, "tools/rust-worker/src/lib.rs") {
		t.Fatalf("expected rust file to be scanned, got %+v", scanned)
	}

	result, err := adapter.Analyze(info, codebase.ScanMap(scanned), nil)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	assertSymbolKind(t, result.Symbols, "example.com/mixed.main", "func")
	assertSymbolKind(t, result.Symbols, "worker::Worker", "struct")
	assertSymbolKind(t, result.Symbols, "worker::Worker::run", "method")
}

func requirePython3(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for mixed adapter tests")
	}
}

func writeAdapterFixture(t *testing.T, root, relPath, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func containsScanPath(scanned []codebase.ScanFile, relPath string) bool {
	for _, file := range scanned {
		if file.RelPath == relPath {
			return true
		}
	}
	return false
}

func assertSymbolKind(t *testing.T, symbols []codebase.SymbolFact, qname, kind string) {
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

func hasQName(symbols []codebase.SymbolFact, qname string) bool {
	for _, symbol := range symbols {
		if symbol.QName == qname {
			return true
		}
	}
	return false
}

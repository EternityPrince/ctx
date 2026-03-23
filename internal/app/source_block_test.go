package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

func TestSymbolBlockBoundsForGoFunction(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "service.go")
	if err := os.WriteFile(path, []byte("package demo\n\n// Run executes work.\nfunc Run() {\n\tprintln(\"ok\")\n}\n"), 0o644); err != nil {
		t.Fatalf("write go source: %v", err)
	}

	_, start, end, _, err := symbolBlockBounds(root, storage.SymbolMatch{
		FilePath: "service.go",
		Name:     "Run",
		Kind:     "func",
		Line:     4,
	})
	if err != nil {
		t.Fatalf("symbolBlockBounds returned error: %v", err)
	}
	if start != 3 || end != 6 {
		t.Fatalf("unexpected go symbol range: start=%d end=%d", start, end)
	}
}

func TestSymbolBlockBoundsForPythonMethod(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for python source block tests")
	}

	root := t.TempDir()
	path := filepath.Join(root, "service.py")
	if err := os.WriteFile(path, []byte("class Service:\n    def run(self):\n        return self.normalize()\n\n    def normalize(self):\n        return 1\n"), 0o644); err != nil {
		t.Fatalf("write python source: %v", err)
	}

	_, start, end, _, err := symbolBlockBounds(root, storage.SymbolMatch{
		FilePath: "service.py",
		Name:     "normalize",
		Kind:     "method",
		Receiver: "Service",
		Line:     5,
	})
	if err != nil {
		t.Fatalf("symbolBlockBounds returned error: %v", err)
	}
	if start != 5 || end != 6 {
		t.Fatalf("unexpected python symbol range: start=%d end=%d", start, end)
	}
}

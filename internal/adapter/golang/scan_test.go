package golang

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanSkipsHiddenDirectories(t *testing.T) {
	root := t.TempDir()

	mustWriteScanFile(t, filepath.Join(root, "go.mod"), "module example.com/project\n\ngo 1.26\n")
	mustWriteScanFile(t, filepath.Join(root, "main.go"), "package main\n")
	mustWriteScanFile(t, filepath.Join(root, ".local", "share", "nvim", "lazy", "fixture.go"), "package fixture\n")

	adapter := NewAdapter()
	files, err := adapter.Scan(root)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	for _, file := range files {
		if file.RelPath == ".local/share/nvim/lazy/fixture.go" {
			t.Fatalf("expected hidden directory file to be skipped, got %+v", file)
		}
	}
	if len(files) != 2 {
		t.Fatalf("expected go.mod and main.go only, got %d files", len(files))
	}
}

func mustWriteScanFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}

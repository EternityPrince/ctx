package python

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLocateSymbolBlockSupportsDeclaredSourceEncoding(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for python adapter tests")
	}

	root := t.TempDir()
	content := append(
		[]byte("# -*- coding: latin-1 -*-\n\n"),
		[]byte("def saludo() -> str:\n    return \"")...,
	)
	content = append(content, 0xf1)
	content = append(content, []byte("\"\n")...)
	writePythonFixtureBytes(t, root, "encoded.py", content)

	path := filepath.Join(root, "encoded.py")
	start, end, err := LocateSymbolBlock(path, "saludo", "func", "", 3)
	if err != nil {
		t.Fatalf("LocateSymbolBlock returned error: %v", err)
	}
	if start != 3 || end != 4 {
		t.Fatalf("unexpected block range: got %d-%d want 3-4", start, end)
	}
}

package filter

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestSkipDirectoryIgnoresConfiguredAbsolutePath(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	trashPath := filepath.Join(homeDir, ".Trash")
	if skip, reason := SkipDirectory(trashPath, ".Trash", true); !skip || reason != "ignored directory path" {
		t.Fatalf("expected configured absolute path to be skipped, got skip=%v reason=%q", skip, reason)
	}
}

func TestSkipDirectoryIgnoresConfiguredAbsoluteSubtree(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	moduleCachePath := filepath.Join(homeDir, "go", "pkg", "mod", "example.com", "mod@v1.0.0")
	if skip, reason := SkipDirectory(moduleCachePath, "mod@v1.0.0", true); !skip || reason != "ignored directory path" {
		t.Fatalf("expected configured subtree to be skipped, got skip=%v reason=%q", skip, reason)
	}
}

func TestHandleWalkErrorSkipsPermissionDenied(t *testing.T) {
	if got := HandleWalkError("/restricted", fs.ErrPermission); !errors.Is(got, filepath.SkipDir) {
		t.Fatalf("expected SkipDir for fs.ErrPermission, got %v", got)
	}
	if got := HandleWalkError("/restricted", os.ErrPermission); !errors.Is(got, filepath.SkipDir) {
		t.Fatalf("expected SkipDir for os.ErrPermission, got %v", got)
	}
}

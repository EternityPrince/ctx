package collector

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vladimirkasterin/ctx/internal/config"
)

func TestCollectBuildsSnapshotWithStats(t *testing.T) {
	root := t.TempDir()

	mustMkdirAll(t, filepath.Join(root, "pkg", "nested"))
	mustWriteFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")
	mustWriteFile(t, filepath.Join(root, "pkg", "nested", "helper.go"), "package nested\n\nconst Name = \"ctx\"\n")
	mustWriteFile(t, filepath.Join(root, ".env"), "SECRET=1\n")
	mustWriteFile(t, filepath.Join(root, "README.md"), "# title\n")

	snapshot, err := Collect(config.Options{
		Root:        root,
		Extensions:  []string{".go", ".md"},
		MaxFileSize: 1024,
	})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	if snapshot.Stats.FilesIncluded != 3 {
		t.Fatalf("expected 3 included files, got %d", snapshot.Stats.FilesIncluded)
	}
	if snapshot.Stats.FilesSkipped != 1 {
		t.Fatalf("expected 1 skipped file, got %d", snapshot.Stats.FilesSkipped)
	}
	if snapshot.Stats.TotalLines != 7 {
		t.Fatalf("expected total lines to be 7, got %d", snapshot.Stats.TotalLines)
	}
	if len(snapshot.Directories) != 2 {
		t.Fatalf("expected 2 collected directories, got %d", len(snapshot.Directories))
	}
	if snapshot.Stats.LargestFile.Path != "pkg/nested/helper.go" && snapshot.Stats.LargestFile.Path != "main.go" {
		t.Fatalf("unexpected largest file path: %s", snapshot.Stats.LargestFile.Path)
	}
}

func TestCollectSkipsBinaryAndLargeFiles(t *testing.T) {
	root := t.TempDir()

	mustWriteFile(t, filepath.Join(root, "main.go"), "package main\n")
	mustWriteFile(t, filepath.Join(root, "huge.txt"), "1234567890")
	if err := os.WriteFile(filepath.Join(root, "image.bin"), []byte{0x00, 0x01, 0x02}, 0o644); err != nil {
		t.Fatalf("write binary file: %v", err)
	}

	snapshot, err := Collect(config.Options{
		Root:        root,
		MaxFileSize: 5,
	})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	if snapshot.Stats.FilesIncluded != 0 {
		t.Fatalf("expected no included files, got %d", snapshot.Stats.FilesIncluded)
	}
	if snapshot.Stats.FilesSkipped != 3 {
		t.Fatalf("expected 3 skipped files, got %d", snapshot.Stats.FilesSkipped)
	}
	if len(snapshot.Stats.SkipReasons) == 0 {
		t.Fatal("expected skip reasons to be populated")
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}

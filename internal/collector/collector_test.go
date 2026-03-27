package collector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vladimirkasterin/ctx/internal/config"
	"github.com/vladimirkasterin/ctx/internal/model"
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

func TestCollectIncludesRustSourcesAndSkipsRustBuildArtifacts(t *testing.T) {
	root := t.TempDir()

	mustWriteFile(t, filepath.Join(root, "Cargo.toml"), "[package]\nname = \"collector-demo\"\nedition = \"2021\"\n")
	mustWriteFile(t, filepath.Join(root, "src", "lib.rs"), "pub fn run() {}\n")
	mustWriteFile(t, filepath.Join(root, "tests", "integration.rs"), "#[test]\nfn integration() {}\n")
	mustWriteFile(t, filepath.Join(root, "examples", "demo.rs"), "fn main() {}\n")
	mustWriteFile(t, filepath.Join(root, "target", "debug", "build.rs"), "fn generated() {}\n")
	mustWriteFile(t, filepath.Join(root, ".cargo", "config.toml"), "[build]\ntarget-dir = \"target\"\n")
	mustWriteFile(t, filepath.Join(root, ".cargo", "registry", "index", "ignored.rs"), "fn ignored() {}\n")

	snapshot, err := Collect(config.Options{
		Root:          root,
		IncludeHidden: true,
		MaxFileSize:   1024,
	})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	if !hasCollectedFile(snapshot, "src/lib.rs") || !hasCollectedFile(snapshot, "tests/integration.rs") || !hasCollectedFile(snapshot, "examples/demo.rs") {
		t.Fatalf("expected rust source files to be included, got %+v", snapshot.Files)
	}
	if hasCollectedFile(snapshot, "target/debug/build.rs") {
		t.Fatalf("did not expect rust target artifact in snapshot, got %+v", snapshot.Files)
	}
	if hasCollectedFile(snapshot, ".cargo/registry/index/ignored.rs") {
		t.Fatalf("did not expect cargo registry cache file in snapshot, got %+v", snapshot.Files)
	}
	if !hasCollectedFile(snapshot, ".cargo/config.toml") {
		t.Fatalf("expected useful cargo config to remain visible, got %+v", snapshot.Files)
	}
	if !hasSkipReason(snapshot, "rust build directory") || !hasSkipReason(snapshot, "rust cargo registry cache") {
		t.Fatalf("expected rust-specific skip reasons, got %+v", snapshot.Stats.SkipReasons)
	}
}

func TestCollectSkipsEmptyAndWhitespaceOnlyFiles(t *testing.T) {
	root := t.TempDir()

	mustWriteFile(t, filepath.Join(root, "main.go"), "package main\n")
	mustWriteFile(t, filepath.Join(root, "empty.txt"), "")
	mustWriteFile(t, filepath.Join(root, "blank.md"), " \n\t\n")

	snapshot, err := Collect(config.Options{Root: root})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	if snapshot.Stats.FilesIncluded != 1 {
		t.Fatalf("expected only one included file, got %d", snapshot.Stats.FilesIncluded)
	}
	if hasCollectedFile(snapshot, "empty.txt") || hasCollectedFile(snapshot, "blank.md") {
		t.Fatalf("did not expect empty files in snapshot, got %+v", snapshot.Files)
	}
	if !hasSkipReason(snapshot, "empty file") {
		t.Fatalf("expected empty file skip reason, got %+v", snapshot.Stats.SkipReasons)
	}
}

func TestCollectHonorsIgnoreFiles(t *testing.T) {
	root := t.TempDir()

	mustWriteFile(t, filepath.Join(root, ".gitignore"), "*.txt\n")
	mustWriteFile(t, filepath.Join(root, ".ctxignore"), "!keep.txt\n")
	mustWriteFile(t, filepath.Join(root, "ignored.txt"), "ignore me\n")
	mustWriteFile(t, filepath.Join(root, "keep.txt"), "keep me\n")
	mustWriteFile(t, filepath.Join(root, "main.go"), "package main\n")

	snapshot, err := Collect(config.Options{Root: root, IncludeHidden: true})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	if hasCollectedFile(snapshot, "ignored.txt") {
		t.Fatalf("did not expect ignored.txt in snapshot, got %+v", snapshot.Files)
	}
	if !hasCollectedFile(snapshot, "keep.txt") {
		t.Fatalf("expected keep.txt to be restored by .ctxignore, got %+v", snapshot.Files)
	}
	if !hasSkipReason(snapshot, "ignored by .gitignore") {
		t.Fatalf("expected .gitignore skip reason, got %+v", snapshot.Stats.SkipReasons)
	}
}

func TestCollectHonorsCtxConfigExcludeAndExplainDecisions(t *testing.T) {
	root := t.TempDir()

	mustWriteFile(t, filepath.Join(root, ".ctxconfig"), "[dump]\nexclude_paths = generated/**\ninclude_paths = generated/keep.txt\n")
	mustWriteFile(t, filepath.Join(root, "generated", "drop.txt"), "drop me\n")
	mustWriteFile(t, filepath.Join(root, "generated", "keep.txt"), "keep me\n")
	mustWriteFile(t, filepath.Join(root, "main.go"), "package main\n")

	snapshot, err := Collect(config.Options{Root: root, Explain: true})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	if hasCollectedFile(snapshot, "generated/drop.txt") {
		t.Fatalf("did not expect generated/drop.txt in snapshot, got %+v", snapshot.Files)
	}
	if !hasCollectedFile(snapshot, "generated/keep.txt") {
		t.Fatalf("expected generated/keep.txt to be restored by .ctxconfig, got %+v", snapshot.Files)
	}
	if !hasDecision(snapshot, "generated/drop.txt", false, "ignored by .ctxconfig [dump]") {
		t.Fatalf("expected explain decision for skipped file, got %+v", snapshot.Decisions)
	}
	if !hasDecision(snapshot, "generated/keep.txt", true, "passed filters") {
		t.Fatalf("expected explain decision for included file, got %+v", snapshot.Decisions)
	}
}

func TestCollectSkipsGeneratedMinifiedAndLowValueByDefault(t *testing.T) {
	root := t.TempDir()

	mustWriteFile(t, filepath.Join(root, "main.go"), "package main\n")
	mustWriteFile(t, filepath.Join(root, "generated.go"), "// Code generated by mockgen. DO NOT EDIT.\npackage generated\n")
	mustWriteFile(t, filepath.Join(root, "bundle.min.js"), strings.Repeat("const a=1;", 300))
	mustWriteFile(t, filepath.Join(root, "debug.log"), "line 1\n")

	snapshot, err := Collect(config.Options{Root: root})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	if hasCollectedFile(snapshot, "generated.go") || hasCollectedFile(snapshot, "bundle.min.js") || hasCollectedFile(snapshot, "debug.log") {
		t.Fatalf("did not expect generated/minified/artifact files in snapshot, got %+v", snapshot.Files)
	}
	for _, reason := range []string{"generated file", "minified file", "low-value artifact"} {
		if !hasSkipReason(snapshot, reason) {
			t.Fatalf("expected skip reason %q, got %+v", reason, snapshot.Stats.SkipReasons)
		}
	}
}

func TestCollectIncludeFlagsRestoreFilteredDumpFiles(t *testing.T) {
	root := t.TempDir()

	mustWriteFile(t, filepath.Join(root, "empty.txt"), "")
	mustWriteFile(t, filepath.Join(root, "generated.go"), "// Code generated by tool. DO NOT EDIT.\npackage generated\n")
	mustWriteFile(t, filepath.Join(root, "bundle.min.js"), strings.Repeat("const a=1;", 300))
	mustWriteFile(t, filepath.Join(root, "debug.log"), "line 1\n")

	snapshot, err := Collect(config.Options{
		Root:             root,
		KeepEmpty:        true,
		IncludeGenerated: true,
		IncludeMinified:  true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	for _, relPath := range []string{"empty.txt", "generated.go", "bundle.min.js", "debug.log"} {
		if !hasCollectedFile(snapshot, relPath) {
			t.Fatalf("expected %s in snapshot with include flags, got %+v", relPath, snapshot.Files)
		}
	}
}

func hasCollectedFile(snapshot *model.Snapshot, relPath string) bool {
	for _, file := range snapshot.Files {
		if file.RelativePath == relPath {
			return true
		}
	}
	return false
}

func hasSkipReason(snapshot *model.Snapshot, reason string) bool {
	for _, metric := range snapshot.Stats.SkipReasons {
		if metric.Name == reason {
			return true
		}
	}
	return false
}

func hasDecision(snapshot *model.Snapshot, relPath string, included bool, reason string) bool {
	for _, decision := range snapshot.Decisions {
		if decision.Path == relPath && decision.Included == included && decision.Reason == reason {
			return true
		}
	}
	return false
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}

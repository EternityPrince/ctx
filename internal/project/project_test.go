package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindModuleRootStopsAtRepositoryBoundary(t *testing.T) {
	tempDir := t.TempDir()
	repoRoot := filepath.Join(tempDir, "repo")
	start := filepath.Join(repoRoot, "nested", "pkg")

	mustMkdirAll(t, filepath.Join(repoRoot, ".git"))
	mustMkdirAll(t, start)
	mustWriteFile(t, filepath.Join(tempDir, "go.mod"), "module example.com/outside\n\ngo 1.25\n")

	_, _, err := findModuleRoot(start)
	if err == nil {
		t.Fatal("expected repository-bounded lookup to reject parent go.mod")
	}
	if !strings.Contains(err.Error(), "repository root") {
		t.Fatalf("expected repository boundary error, got %v", err)
	}
}

func TestFindModuleRootUsesNestedModuleInsideRepository(t *testing.T) {
	tempDir := t.TempDir()
	repoRoot := filepath.Join(tempDir, "repo")
	moduleRoot := filepath.Join(repoRoot, "services", "api")
	start := filepath.Join(moduleRoot, "internal", "pkg")

	mustMkdirAll(t, filepath.Join(repoRoot, ".git"))
	mustMkdirAll(t, start)
	mustWriteFile(t, filepath.Join(moduleRoot, "go.mod"), "module example.com/api\n\ngo 1.25\n")

	gotRoot, gotGoMod, err := findModuleRoot(start)
	if err != nil {
		t.Fatalf("findModuleRoot returned error: %v", err)
	}
	if gotRoot != moduleRoot {
		t.Fatalf("expected module root %q, got %q", moduleRoot, gotRoot)
	}
	if gotGoMod != filepath.Join(moduleRoot, "go.mod") {
		t.Fatalf("expected go.mod path inside module, got %q", gotGoMod)
	}
}

func TestFindModuleRootFallsBackToAncestorWithoutRepository(t *testing.T) {
	tempDir := t.TempDir()
	moduleRoot := filepath.Join(tempDir, "workspace")
	start := filepath.Join(moduleRoot, "nested", "pkg")

	mustMkdirAll(t, start)
	mustWriteFile(t, filepath.Join(moduleRoot, "go.mod"), "module example.com/workspace\n\ngo 1.25\n")

	gotRoot, gotGoMod, err := findModuleRoot(start)
	if err != nil {
		t.Fatalf("findModuleRoot returned error: %v", err)
	}
	if gotRoot != moduleRoot {
		t.Fatalf("expected module root %q, got %q", moduleRoot, gotRoot)
	}
	if gotGoMod != filepath.Join(moduleRoot, "go.mod") {
		t.Fatalf("expected go.mod path %q, got %q", filepath.Join(moduleRoot, "go.mod"), gotGoMod)
	}
}

func TestResolveDetectsPythonProjectFromPyProject(t *testing.T) {
	tempDir := t.TempDir()
	projectRoot := filepath.Join(tempDir, "repo")
	start := filepath.Join(projectRoot, "pkg", "nested")

	mustMkdirAll(t, start)
	mustWriteFile(t, filepath.Join(projectRoot, "pyproject.toml"), "[project]\nname = \"demo-project\"\nrequires-python = \">=3.11\"\n")
	mustWriteFile(t, filepath.Join(projectRoot, "pkg", "__init__.py"), "")
	mustWriteFile(t, filepath.Join(projectRoot, "pkg", "nested", "service.py"), "def run():\n    return 1\n")

	info, err := Resolve(start)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if info.Root != projectRoot {
		t.Fatalf("expected python root %q, got %q", projectRoot, info.Root)
	}
	if info.Language != "python" {
		t.Fatalf("expected python language, got %q", info.Language)
	}
	if info.ModulePath != "demo_project" {
		t.Fatalf("expected normalized project name, got %q", info.ModulePath)
	}
	if info.GoVersion != ">=3.11" {
		t.Fatalf("expected requires-python version, got %q", info.GoVersion)
	}
}

func TestFindPythonProjectRootFallsBackToRepositoryCandidate(t *testing.T) {
	tempDir := t.TempDir()
	repoRoot := filepath.Join(tempDir, "repo")
	start := filepath.Join(repoRoot, "pkg", "nested")

	mustMkdirAll(t, filepath.Join(repoRoot, ".git"))
	mustMkdirAll(t, start)
	mustWriteFile(t, filepath.Join(repoRoot, "pkg", "__init__.py"), "")
	mustWriteFile(t, filepath.Join(repoRoot, "pkg", "nested", "service.py"), "def run():\n    return 1\n")

	root, name, version, err := findPythonProjectRoot(start)
	if err != nil {
		t.Fatalf("findPythonProjectRoot returned error: %v", err)
	}
	if root != repoRoot {
		t.Fatalf("expected repository root %q, got %q", repoRoot, root)
	}
	if name != "repo" {
		t.Fatalf("expected fallback project name %q, got %q", "repo", name)
	}
	if version != "" {
		t.Fatalf("expected empty python version, got %q", version)
	}
}

func TestParsePyProjectSupportsPoetryNameFallback(t *testing.T) {
	tempDir := t.TempDir()
	pyprojectPath := filepath.Join(tempDir, "pyproject.toml")
	mustWriteFile(t, pyprojectPath, "[tool.poetry]\nname = \"poetry-demo\"\n")

	name, version, err := parsePyProject(pyprojectPath)
	if err != nil {
		t.Fatalf("parsePyProject returned error: %v", err)
	}
	if name != "poetry_demo" {
		t.Fatalf("expected poetry project name %q, got %q", "poetry_demo", name)
	}
	if version != "" {
		t.Fatalf("expected empty version, got %q", version)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

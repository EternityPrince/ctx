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

func TestResolveDoesNotCreateStoreLayout(t *testing.T) {
	tempDir := t.TempDir()
	storeRoot := filepath.Join(tempDir, "ctx-home")
	root := filepath.Join(tempDir, "workspace")

	t.Setenv("CTX_HOME", storeRoot)
	mustMkdirAll(t, root)
	mustWriteFile(t, filepath.Join(root, "go.mod"), "module example.com/workspace\n\ngo 1.25\n")

	info, err := Resolve(root)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if info.ID == "" {
		t.Fatal("expected resolved project id")
	}
	if _, err := os.Stat(storeRoot); !os.IsNotExist(err) {
		t.Fatalf("Resolve should not create CTX_HOME, stat err=%v", err)
	}
}

func TestResolveDetectsRustProjectFromCargoToml(t *testing.T) {
	tempDir := t.TempDir()
	projectRoot := filepath.Join(tempDir, "rust-demo")
	start := filepath.Join(projectRoot, "src")

	mustMkdirAll(t, start)
	mustWriteFile(t, filepath.Join(projectRoot, "Cargo.toml"), "[package]\nname = \"rust-demo\"\nedition = \"2021\"\n")
	mustWriteFile(t, filepath.Join(projectRoot, "src", "lib.rs"), "pub fn run() {}\n")

	info, err := Resolve(start)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if info.Root != projectRoot {
		t.Fatalf("expected rust root %q, got %q", projectRoot, info.Root)
	}
	if info.Language != "rust" {
		t.Fatalf("expected rust language, got %q", info.Language)
	}
	if info.ModulePath != "rust-demo" {
		t.Fatalf("expected crate name %q, got %q", "rust-demo", info.ModulePath)
	}
	if info.GoVersion != "2021" {
		t.Fatalf("expected rust edition %q, got %q", "2021", info.GoVersion)
	}
}

func TestResolveUsesRustWorkspaceRootFromMemberPath(t *testing.T) {
	tempDir := t.TempDir()
	workspaceRoot := filepath.Join(tempDir, "workspace")
	memberRoot := filepath.Join(workspaceRoot, "crates", "api")
	start := filepath.Join(memberRoot, "src")

	mustMkdirAll(t, start)
	mustWriteFile(t, filepath.Join(workspaceRoot, "Cargo.toml"), "[workspace]\nmembers = [\"crates/*\"]\n\n[workspace.package]\nedition = \"2021\"\n")
	mustWriteFile(t, filepath.Join(memberRoot, "Cargo.toml"), "[package]\nname = \"api\"\nedition = \"2024\"\n")
	mustWriteFile(t, filepath.Join(memberRoot, "src", "lib.rs"), "pub fn run() {}\n")

	info, err := Resolve(start)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if info.Root != workspaceRoot {
		t.Fatalf("expected workspace root %q, got %q", workspaceRoot, info.Root)
	}
	if info.Language != "rust" {
		t.Fatalf("expected rust language, got %q", info.Language)
	}
	if info.ModulePath != filepath.Base(workspaceRoot) {
		t.Fatalf("expected workspace fallback name %q, got %q", filepath.Base(workspaceRoot), info.ModulePath)
	}
	if info.GoVersion != "2021" {
		t.Fatalf("expected workspace edition %q, got %q", "2021", info.GoVersion)
	}
}

func TestResolvePrefersNestedRustProjectOverAncestorGoModule(t *testing.T) {
	tempDir := t.TempDir()
	repoRoot := filepath.Join(tempDir, "repo")
	rustRoot := filepath.Join(repoRoot, "tools", "worker")
	start := filepath.Join(rustRoot, "src")

	mustMkdirAll(t, start)
	mustWriteFile(t, filepath.Join(repoRoot, "go.mod"), "module example.com/repo\n\ngo 1.26\n")
	mustWriteFile(t, filepath.Join(rustRoot, "Cargo.toml"), "[package]\nname = \"worker\"\nedition = \"2021\"\n")
	mustWriteFile(t, filepath.Join(rustRoot, "src", "main.rs"), "fn main() {}\n")

	info, err := Resolve(start)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if info.Root != rustRoot {
		t.Fatalf("expected nested rust root %q, got %q", rustRoot, info.Root)
	}
	if info.Language != "rust" {
		t.Fatalf("expected rust language, got %q", info.Language)
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

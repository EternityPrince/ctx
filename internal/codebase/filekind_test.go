package codebase

import "testing"

func TestPythonImportHelpersRespectSrcLayoutAndInitFiles(t *testing.T) {
	if got := PythonPackageImportPath("demo", "src/pkg/service.py"); got != "pkg" {
		t.Fatalf("unexpected package import path: %q", got)
	}
	if got := PythonModuleImportPath("demo", "src/pkg/service.py"); got != "pkg.service" {
		t.Fatalf("unexpected module import path: %q", got)
	}
	if got := PythonPackageImportPath("demo", "pkg/__init__.py"); got != "pkg" {
		t.Fatalf("unexpected package import path for __init__: %q", got)
	}
}

func TestFileKindHelpersRecognizePythonAndGoTests(t *testing.T) {
	cases := []struct {
		path        string
		goFile      bool
		goTest      bool
		pythonFile  bool
		pythonTest  bool
		rustFile    bool
		rustTest    bool
		indexedFile bool
	}{
		{path: "pkg/service.go", goFile: true, indexedFile: true},
		{path: "pkg/service_test.go", goFile: true, goTest: true, indexedFile: true},
		{path: "pkg/service.py", pythonFile: true, indexedFile: true},
		{path: "tests/test_service.py", pythonFile: true, pythonTest: true, indexedFile: true},
		{path: "pkg/conftest.py", pythonFile: true, pythonTest: true, indexedFile: true},
		{path: "src/lib.rs", rustFile: true, indexedFile: true},
		{path: "tests/integration.rs", rustFile: true, rustTest: true, indexedFile: true},
		{path: "README.md"},
	}

	for _, tc := range cases {
		if got := IsGoFile(tc.path); got != tc.goFile {
			t.Fatalf("IsGoFile(%q) = %v, want %v", tc.path, got, tc.goFile)
		}
		if got := IsGoTestFile(tc.path); got != tc.goTest {
			t.Fatalf("IsGoTestFile(%q) = %v, want %v", tc.path, got, tc.goTest)
		}
		if got := IsPythonFile(tc.path); got != tc.pythonFile {
			t.Fatalf("IsPythonFile(%q) = %v, want %v", tc.path, got, tc.pythonFile)
		}
		if got := IsPythonTestFile(tc.path); got != tc.pythonTest {
			t.Fatalf("IsPythonTestFile(%q) = %v, want %v", tc.path, got, tc.pythonTest)
		}
		if got := IsRustFile(tc.path); got != tc.rustFile {
			t.Fatalf("IsRustFile(%q) = %v, want %v", tc.path, got, tc.rustFile)
		}
		if got := IsRustTestFile(tc.path); got != tc.rustTest {
			t.Fatalf("IsRustTestFile(%q) = %v, want %v", tc.path, got, tc.rustTest)
		}
		if got := IsIndexedSourceFile(tc.path); got != tc.indexedFile {
			t.Fatalf("IsIndexedSourceFile(%q) = %v, want %v", tc.path, got, tc.indexedFile)
		}
	}
}

func TestIsGoProjectFile(t *testing.T) {
	for _, name := range []string{"go.mod", "go.sum"} {
		if !IsGoProjectFile(name) {
			t.Fatalf("expected %q to be recognized as go project file", name)
		}
	}
	if IsGoProjectFile("pyproject.toml") {
		t.Fatal("did not expect pyproject.toml to be recognized as go project file")
	}
}

func TestIsPythonProjectFile(t *testing.T) {
	for _, name := range []string{"pyproject.toml", "requirements.txt", "setup.py", "setup.cfg", "Pipfile", "poetry.lock"} {
		if !IsPythonProjectFile(name) {
			t.Fatalf("expected %q to be recognized as python project file", name)
		}
	}
	if IsPythonProjectFile("go.mod") {
		t.Fatal("did not expect go.mod to be recognized as python project file")
	}
}

func TestIsRustProjectFile(t *testing.T) {
	for _, name := range []string{"Cargo.toml", "Cargo.lock"} {
		if !IsRustProjectFile(name) {
			t.Fatalf("expected %q to be recognized as rust project file", name)
		}
	}
	if IsRustProjectFile("pyproject.toml") {
		t.Fatal("did not expect pyproject.toml to be recognized as rust project file")
	}
}

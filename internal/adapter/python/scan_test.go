package python

import "testing"

func TestScanRecognizesPythonProjectFilesAndTests(t *testing.T) {
	root := t.TempDir()
	writePythonFixture(t, root, "pyproject.toml", "[project]\nname = \"scan-demo\"\n")
	writePythonFixture(t, root, "app/main.py", "def run():\n    return 1\n")
	writePythonFixture(t, root, "tests/test_main.py", "def test_run():\n    assert True\n")
	writePythonFixture(t, root, ".venv/lib/ignored.py", "def hidden():\n    return 0\n")

	adapter := NewAdapter()
	files, err := adapter.Scan(root)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	byPath := make(map[string]struct {
		isTest    bool
		isModule  bool
		packageID string
	})
	for _, file := range files {
		byPath[file.RelPath] = struct {
			isTest    bool
			isModule  bool
			packageID string
		}{
			isTest:    file.IsTest,
			isModule:  file.IsModule,
			packageID: file.PackageImportPath,
		}
	}

	if _, ok := byPath[".venv/lib/ignored.py"]; ok {
		t.Fatal("expected hidden virtualenv file to be skipped")
	}
	if value, ok := byPath["pyproject.toml"]; !ok || !value.isModule {
		t.Fatalf("expected pyproject.toml to be scanned as module file, got %+v", value)
	}
	if value, ok := byPath["tests/test_main.py"]; !ok || !value.isTest {
		t.Fatalf("expected tests/test_main.py to be marked as test file, got %+v", value)
	}
	if value, ok := byPath["app/main.py"]; !ok || value.isTest {
		t.Fatalf("expected app/main.py to be scanned as non-test source, got %+v", value)
	}
}

func TestScanDerivesPythonPackagesFromConfiguredSourceRoot(t *testing.T) {
	root := t.TempDir()
	writePythonFixture(t, root, "pyproject.toml", "[project]\nname = \"scan-demo\"\n\n[tool.setuptools.packages.find]\nwhere = [\"lib\"]\n")
	writePythonFixture(t, root, "lib/pkg/__init__.py", "")
	writePythonFixture(t, root, "lib/pkg/service.py", "def run():\n    return 1\n")

	adapter := NewAdapter()
	files, err := adapter.Scan(root)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	for _, file := range files {
		if file.RelPath == "lib/pkg/service.py" && file.PackageImportPath != "pkg" {
			t.Fatalf("expected configured source root to strip lib/, got %+v", file)
		}
	}
}

func TestScanHonorsCtxIgnoreOverride(t *testing.T) {
	root := t.TempDir()
	writePythonFixture(t, root, ".gitignore", "*.py\n")
	writePythonFixture(t, root, ".ctxignore", "!app/main.py\n")
	writePythonFixture(t, root, "pyproject.toml", "[project]\nname = \"scan-demo\"\n")
	writePythonFixture(t, root, "app/main.py", "def run():\n    return 1\n")
	writePythonFixture(t, root, "app/ignored.py", "def ignored():\n    return 0\n")

	adapter := NewAdapter()
	files, err := adapter.Scan(root)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	foundMain := false
	for _, file := range files {
		if file.RelPath == "app/main.py" {
			foundMain = true
		}
		if file.RelPath == "app/ignored.py" {
			t.Fatalf("expected gitignored file to be skipped, got %+v", file)
		}
	}
	if !foundMain {
		t.Fatalf("expected .ctxignore to restore app/main.py, got %+v", files)
	}
}

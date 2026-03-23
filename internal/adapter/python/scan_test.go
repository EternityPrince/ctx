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
		isTest   bool
		isModule bool
	})
	for _, file := range files {
		byPath[file.RelPath] = struct {
			isTest   bool
			isModule bool
		}{
			isTest:   file.IsTest,
			isModule: file.IsModule,
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

package codebase

import (
	"path/filepath"
	"strings"
)

func IsGoFile(path string) bool {
	return strings.HasSuffix(normalizePath(path), ".go")
}

func IsGoProjectFile(path string) bool {
	switch baseName(path) {
	case "go.mod", "go.sum":
		return true
	default:
		return false
	}
}

func IsGoTestFile(path string) bool {
	return strings.HasSuffix(baseName(path), "_test.go")
}

func IsPythonFile(path string) bool {
	return strings.HasSuffix(normalizePath(strings.ToLower(path)), ".py")
}

func IsPythonTestFile(path string) bool {
	if !IsPythonFile(path) {
		return false
	}
	base := baseName(path)
	if base == "conftest.py" {
		return true
	}
	if strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") || strings.HasSuffix(base, "_tests.py") {
		return true
	}

	path = normalizePath(path)
	return strings.HasPrefix(path, "tests/") || strings.Contains(path, "/tests/")
}

func IsPythonProjectFile(name string) bool {
	switch name {
	case "pyproject.toml", "requirements.txt", "setup.py", "setup.cfg", "Pipfile", "poetry.lock":
		return true
	default:
		return false
	}
}

func IsRustFile(path string) bool {
	return strings.HasSuffix(normalizePath(strings.ToLower(path)), ".rs")
}

func IsRustProjectFile(path string) bool {
	switch baseName(path) {
	case "Cargo.toml", "Cargo.lock":
		return true
	default:
		return false
	}
}

func IsRustTestFile(path string) bool {
	if !IsRustFile(path) {
		return false
	}
	path = normalizePath(path)
	if strings.HasPrefix(path, "tests/") || strings.Contains(path, "/tests/") {
		return true
	}
	base := baseName(path)
	return strings.HasSuffix(base, "_test.rs")
}

func IsIndexedSourceFile(path string) bool {
	return IsGoFile(path) || IsPythonFile(path) || IsRustFile(path)
}

func normalizePath(path string) string {
	return filepath.ToSlash(strings.TrimSpace(path))
}

func baseName(path string) string {
	path = normalizePath(path)
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

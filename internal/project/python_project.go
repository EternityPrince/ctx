package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/codebase"
)

func findPythonProjectRoot(start string) (string, string, string, error) {
	boundary, hasBoundary := findRepositoryRoot(start)
	current := start

	bestRoot := ""
	bestName := ""
	bestVersion := ""
	for {
		detected, name, version, err := detectPythonProject(current)
		if err != nil {
			return "", "", "", err
		}
		if detected {
			bestRoot = current
			if name != "" {
				bestName = name
			}
			if version != "" {
				bestVersion = version
			}
		}

		if hasBoundary && current == boundary {
			break
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	if bestRoot == "" {
		if hasBoundary {
			return "", "", "", fmt.Errorf("python project markers not found between %s and repository root %s", start, boundary)
		}
		return "", "", "", errors.New("python project markers not found from the provided path")
	}

	if bestName == "" {
		bestName = normalizePythonProjectName(filepath.Base(bestRoot))
	}
	return bestRoot, bestName, bestVersion, nil
}

func detectPythonProject(dir string) (bool, string, string, error) {
	pyprojectPath := filepath.Join(dir, "pyproject.toml")
	if info, err := os.Stat(pyprojectPath); err == nil && !info.IsDir() {
		name, version, err := parsePyProject(pyprojectPath)
		if err != nil {
			return false, "", "", err
		}
		return true, name, version, nil
	}

	for _, marker := range []string{"setup.py", "setup.cfg", "requirements.txt", "Pipfile"} {
		if info, err := os.Stat(filepath.Join(dir, marker)); err == nil && !info.IsDir() {
			return true, normalizePythonProjectName(filepath.Base(dir)), "", nil
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, "", "", fmt.Errorf("read python project dir %s: %w", dir, err)
	}

	hasPackageDir := false
	hasPythonFile := false
	hasSrcLayout := false

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			if strings.HasPrefix(name, ".") {
				continue
			}
			if name == "src" {
				srcLayout, err := directoryHasPythonEntries(filepath.Join(dir, name))
				if err != nil {
					return false, "", "", err
				}
				hasSrcLayout = hasSrcLayout || srcLayout
				continue
			}
			if _, err := os.Stat(filepath.Join(dir, name, "__init__.py")); err == nil {
				hasPackageDir = true
			}
			continue
		}

		if codebase.IsPythonFile(name) {
			hasPythonFile = true
		}
	}

	if hasPackageDir || hasPythonFile || hasSrcLayout {
		return true, normalizePythonProjectName(filepath.Base(dir)), "", nil
	}
	return false, "", "", nil
}

func directoryHasPythonEntries(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("read python source dir %s: %w", dir, err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			if strings.HasPrefix(name, ".") {
				continue
			}
			if _, err := os.Stat(filepath.Join(dir, name, "__init__.py")); err == nil {
				return true, nil
			}
			continue
		}
		if codebase.IsPythonFile(name) {
			return true, nil
		}
	}
	return false, nil
}

func parsePyProject(path string) (string, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("read pyproject.toml: %w", err)
	}

	section := ""
	var name string
	var version string

	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			continue
		}

		key, value, ok := splitAssignment(line)
		if !ok {
			continue
		}

		switch section {
		case "project":
			switch key {
			case "name":
				name = normalizePythonProjectName(trimQuoted(value))
			case "requires-python":
				version = trimQuoted(value)
			}
		case "tool.poetry":
			if key == "name" && name == "" {
				name = normalizePythonProjectName(trimQuoted(value))
			}
		}
	}

	return name, version, nil
}

func splitAssignment(line string) (string, string, bool) {
	index := strings.Index(line, "=")
	if index <= 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:index])
	value := strings.TrimSpace(line[index+1:])
	if key == "" || value == "" {
		return "", "", false
	}
	if comment := strings.Index(value, "#"); comment >= 0 {
		value = strings.TrimSpace(value[:comment])
	}
	return key, value, true
}

func trimQuoted(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"'`)
	return strings.TrimSpace(value)
}

func normalizePythonProjectName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	return value
}

package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func findModuleRoot(start string) (string, string, error) {
	boundary, hasBoundary := findRepositoryRoot(start)
	current := start
	for {
		goModPath := filepath.Join(current, "go.mod")
		if _, err := os.Stat(goModPath); err == nil {
			return current, goModPath, nil
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

	if hasBoundary {
		return "", "", fmt.Errorf("go.mod not found between %s and repository root %s", start, boundary)
	}
	return "", "", errors.New("go.mod not found from the provided path")
}

func parseGoMod(path string) (string, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("read go.mod: %w", err)
	}

	var modulePath string
	var goVersion string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if strings.HasPrefix(line, "module ") {
			modulePath = strings.TrimSpace(strings.TrimPrefix(line, "module "))
			continue
		}
		if strings.HasPrefix(line, "go ") {
			goVersion = strings.TrimSpace(strings.TrimPrefix(line, "go "))
		}
	}

	if modulePath == "" {
		return "", "", errors.New("module path not found in go.mod")
	}
	return modulePath, goVersion, nil
}

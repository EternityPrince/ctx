package project

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const envHome = "CTX_HOME"

type Info struct {
	Root       string
	ModulePath string
	GoVersion  string
	ID         string
	DBPath     string
}

func Resolve(path string) (Info, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return Info{}, fmt.Errorf("resolve path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return Info{}, fmt.Errorf("stat path: %w", err)
	}
	if !info.IsDir() {
		absPath = filepath.Dir(absPath)
	}

	moduleRoot, goModPath, err := findModuleRoot(absPath)
	if err != nil {
		return Info{}, err
	}

	modulePath, goVersion, err := parseGoMod(goModPath)
	if err != nil {
		return Info{}, err
	}

	projectID := ProjectID(moduleRoot)
	dbPath, err := DBPath(projectID)
	if err != nil {
		return Info{}, err
	}

	return Info{
		Root:       moduleRoot,
		ModulePath: modulePath,
		GoVersion:  goVersion,
		ID:         projectID,
		DBPath:     dbPath,
	}, nil
}

func ProjectID(root string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(root)))
	return hex.EncodeToString(sum[:8])
}

func StoreRoot() (string, error) {
	if value := strings.TrimSpace(os.Getenv(envHome)); value != "" {
		if err := os.MkdirAll(value, 0o755); err != nil {
			return "", fmt.Errorf("create CTX_HOME: %w", err)
		}
		return value, nil
	}

	configRoot, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}

	root := filepath.Join(configRoot, "ctx")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create store root: %w", err)
	}
	return root, nil
}

func ProjectsRoot() (string, error) {
	storeRoot, err := StoreRoot()
	if err != nil {
		return "", err
	}

	root := filepath.Join(storeRoot, "projects")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create projects root: %w", err)
	}
	return root, nil
}

func DBPath(projectID string) (string, error) {
	projectsRoot, err := ProjectsRoot()
	if err != nil {
		return "", err
	}

	projectDir := filepath.Join(projectsRoot, projectID)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return "", fmt.Errorf("create project dir: %w", err)
	}
	return filepath.Join(projectDir, "index.sqlite"), nil
}

func ListDBPaths() ([]string, error) {
	projectsRoot, err := ProjectsRoot()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(projectsRoot)
	if err != nil {
		return nil, fmt.Errorf("read projects root: %w", err)
	}

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dbPath := filepath.Join(projectsRoot, entry.Name(), "index.sqlite")
		if _, err := os.Stat(dbPath); err == nil {
			paths = append(paths, dbPath)
		}
	}
	return paths, nil
}

func ProjectDir(projectID string) (string, error) {
	projectsRoot, err := ProjectsRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(projectsRoot, projectID), nil
}

func findModuleRoot(start string) (string, string, error) {
	current := start
	for {
		goModPath := filepath.Join(current, "go.mod")
		if _, err := os.Stat(goModPath); err == nil {
			return current, goModPath, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
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

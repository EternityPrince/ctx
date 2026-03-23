package project

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const envHome = "CTX_HOME"

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

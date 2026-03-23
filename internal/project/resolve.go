package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Info struct {
	Root       string
	ModulePath string
	GoVersion  string
	Language   string
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

	if projectInfo, err := resolveGoProject(absPath); err == nil {
		return projectInfo, nil
	} else if pyInfo, pyErr := resolvePythonProject(absPath); pyErr == nil {
		return pyInfo, nil
	} else {
		return Info{}, errors.Join(err, pyErr)
	}
}

func resolveGoProject(path string) (Info, error) {
	moduleRoot, goModPath, err := findModuleRoot(path)
	if err != nil {
		return Info{}, err
	}

	modulePath, goVersion, err := parseGoMod(goModPath)
	if err != nil {
		return Info{}, err
	}

	return buildInfo(moduleRoot, modulePath, goVersion, "go")
}

func resolvePythonProject(path string) (Info, error) {
	root, modulePath, version, err := findPythonProjectRoot(path)
	if err != nil {
		return Info{}, err
	}
	return buildInfo(root, modulePath, version, "python")
}

func buildInfo(root, modulePath, version, language string) (Info, error) {
	projectID := ProjectID(root)
	dbPath, err := DBPath(projectID)
	if err != nil {
		return Info{}, err
	}

	return Info{
		Root:       root,
		ModulePath: modulePath,
		GoVersion:  version,
		Language:   language,
		ID:         projectID,
		DBPath:     dbPath,
	}, nil
}

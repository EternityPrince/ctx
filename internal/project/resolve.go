package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type Info struct {
	Root       string
	ModulePath string
	GoVersion  string
	Language   string
	ID         string
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

	type candidate struct {
		info     Info
		priority int
	}

	resolvers := []struct {
		priority int
		resolve  func(string) (Info, error)
	}{
		{priority: 0, resolve: resolveGoProject},
		{priority: 1, resolve: resolveRustProject},
		{priority: 2, resolve: resolvePythonProject},
	}

	candidates := make([]candidate, 0, len(resolvers))
	errs := make([]error, 0, len(resolvers))
	for _, resolver := range resolvers {
		projectInfo, resolveErr := resolver.resolve(absPath)
		if resolveErr != nil {
			errs = append(errs, resolveErr)
			continue
		}
		candidates = append(candidates, candidate{
			info:     projectInfo,
			priority: resolver.priority,
		})
	}
	if len(candidates) == 0 {
		return Info{}, errors.Join(errs...)
	}

	sort.Slice(candidates, func(i, j int) bool {
		leftDepth := pathDepth(candidates[i].info.Root)
		rightDepth := pathDepth(candidates[j].info.Root)
		if leftDepth == rightDepth {
			return candidates[i].priority < candidates[j].priority
		}
		return leftDepth > rightDepth
	})
	return candidates[0].info, nil
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

func resolveRustProject(path string) (Info, error) {
	root, modulePath, version, err := findRustProjectRoot(path)
	if err != nil {
		return Info{}, err
	}
	return buildInfo(root, modulePath, version, "rust")
}

func buildInfo(root, modulePath, version, language string) (Info, error) {
	return Info{
		Root:       root,
		ModulePath: modulePath,
		GoVersion:  version,
		Language:   language,
		ID:         ProjectID(root),
	}, nil
}

func pathDepth(path string) int {
	clean := filepath.Clean(path)
	depth := 0
	for {
		parent := filepath.Dir(clean)
		if parent == clean {
			break
		}
		depth++
		clean = parent
	}
	return depth
}

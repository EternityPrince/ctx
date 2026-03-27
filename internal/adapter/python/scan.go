package python

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/filter"
)

func (a *Adapter) Scan(root string) ([]codebase.ScanFile, error) {
	files := make([]codebase.ScanFile, 0, 128)
	walker, err := filter.NewWalker(root, false, "index")
	if err != nil {
		return nil, err
	}

	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return filter.HandleWalkError(path, walkErr)
		}

		relPath := ""
		if path != root {
			value, err := filepath.Rel(root, path)
			if err != nil {
				return fmt.Errorf("make relative path: %w", err)
			}
			relPath = filepath.ToSlash(value)
		}

		if entry.IsDir() {
			if path != root {
				if skip, _, err := walker.ShouldSkipDirectory(path, relPath, entry.Name()); err != nil {
					return err
				} else if skip {
					return filepath.SkipDir
				}
			}
			return nil
		}

		name := entry.Name()
		if skip, _, err := walker.ShouldSkipFile(path, relPath, name); err != nil {
			return err
		} else if skip {
			return nil
		}
		isPython := codebase.IsPythonFile(name)
		isModule := codebase.IsPythonProjectFile(name)
		if !isPython && !isModule {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read file %s: %w", path, err)
		}

		identity := ""
		semanticMeta := ""
		if isModule {
			meta := codebase.ParsePythonProjectManifestMeta(name, data)
			semanticMeta = codebase.EncodeManifestMeta(meta)
			if name == "pyproject.toml" || name == "setup.cfg" || name == "setup.py" {
				identity = meta.Name
			}
		}
		sum := sha256.Sum256(data)
		files = append(files, codebase.ScanFile{
			AbsPath:      path,
			RelPath:      relPath,
			Identity:     identity,
			SemanticMeta: semanticMeta,
			Hash:         hex.EncodeToString(sum[:]),
			SizeBytes:    int64(len(data)),
			IsTest:       isPython && codebase.IsPythonTestFile(relPath),
			IsModule:     isModule,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sourceRoots := pythonSourceRootsFromScanFiles(files)
	for idx := range files {
		if !codebase.IsPythonFile(files[idx].RelPath) {
			continue
		}
		files[idx].PackageImportPath = codebase.PythonPackageImportPathWithRoots("", files[idx].RelPath, sourceRoots)
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].RelPath < files[j].RelPath
	})
	return files, nil
}

func pythonSourceRootsFromScanFiles(files []codebase.ScanFile) []string {
	roots := []string{"src"}
	for _, file := range files {
		if !file.IsModule {
			continue
		}
		meta := codebase.DecodeManifestMeta(file.SemanticMeta)
		roots = append(roots, meta.PackageRoots...)
	}
	return stablePythonRoots(roots)
}

func stablePythonRoots(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	roots := make([]string, 0, len(values))
	for _, value := range values {
		value = filepath.ToSlash(value)
		value = strings.TrimSpace(value)
		value = strings.TrimPrefix(value, "./")
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		roots = append(roots, value)
	}
	sort.Strings(roots)
	return roots
}

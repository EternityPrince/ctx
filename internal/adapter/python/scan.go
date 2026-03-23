package python

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/filter"
)

func (a *Adapter) Scan(root string) ([]codebase.ScanFile, error) {
	files := make([]codebase.ScanFile, 0, 128)

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return filter.HandleWalkError(path, walkErr)
		}

		if entry.IsDir() {
			if path != root {
				if skip, _ := filter.SkipDirectory(path, entry.Name(), false); skip {
					return filepath.SkipDir
				}
			}
			return nil
		}

		name := entry.Name()
		isPython := codebase.IsPythonFile(name)
		isModule := codebase.IsPythonProjectFile(name)
		if !isPython && !isModule {
			return nil
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("make relative path: %w", err)
		}
		relPath = filepath.ToSlash(relPath)

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read file %s: %w", path, err)
		}

		sum := sha256.Sum256(data)
		files = append(files, codebase.ScanFile{
			AbsPath:   path,
			RelPath:   relPath,
			Hash:      hex.EncodeToString(sum[:]),
			SizeBytes: int64(len(data)),
			IsTest:    isPython && codebase.IsPythonTestFile(relPath),
			IsModule:  isModule,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].RelPath < files[j].RelPath
	})
	return files, nil
}

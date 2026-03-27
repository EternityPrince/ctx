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

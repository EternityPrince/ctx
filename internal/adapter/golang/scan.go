package golang

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
	files := make([]codebase.ScanFile, 0, 64)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return filter.HandleWalkError(path, walkErr)
		}

		if d.IsDir() {
			if path != root {
				if skip, _ := filter.SkipDirectory(path, d.Name(), false); skip {
					return filepath.SkipDir
				}
			}
			return nil
		}

		name := d.Name()
		isGo := strings.HasSuffix(name, ".go")
		isModule := name == "go.mod" || name == "go.sum"
		if !isGo && !isModule {
			return nil
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("make relative path: %w", err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read file %s: %w", path, err)
		}

		sum := sha256.Sum256(data)
		files = append(files, codebase.ScanFile{
			AbsPath:   path,
			RelPath:   filepath.ToSlash(relPath),
			Hash:      hex.EncodeToString(sum[:]),
			SizeBytes: int64(len(data)),
			IsGo:      isGo,
			IsTest:    isGo && strings.HasSuffix(name, "_test.go"),
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

func findScanFile(scanned []codebase.ScanFile, relPath string) *codebase.ScanFile {
	for i := range scanned {
		if scanned[i].RelPath == relPath {
			return &scanned[i]
		}
	}
	return nil
}

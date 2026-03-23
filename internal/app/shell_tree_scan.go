package app

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/filter"
	"github.com/vladimirkasterin/ctx/internal/text"
)

func scanProjectTree(root string) ([]string, []shellScannedFile, error) {
	directories := make([]string, 0, 64)
	files := make([]shellScannedFile, 0, 128)

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return filter.HandleWalkError(path, walkErr)
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("make relative path: %w", err)
		}
		if relPath == "." {
			return nil
		}

		relPath = filepath.ToSlash(relPath)
		name := entry.Name()
		if entry.IsDir() {
			if skip, _ := filter.SkipDirectory(path, name, false); skip {
				return filepath.SkipDir
			}
			directories = append(directories, relPath)
			return nil
		}

		if strings.HasPrefix(name, ".") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("read file info: %w", err)
		}

		lineCount := 0
		data, err := os.ReadFile(path)
		if err == nil && !filter.IsLikelyBinary(data) {
			lineCount, _ = text.CountLines(text.NormalizeNewlines(string(data)))
		}

		files = append(files, shellScannedFile{
			Path:      relPath,
			SizeBytes: info.Size(),
			LineCount: lineCount,
			IsTest:    codebase.IsGoTestFile(relPath) || codebase.IsPythonTestFile(relPath),
		})
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return directories, files, nil
}

func directoryLineTotals(scannedFiles []shellScannedFile) map[string]int {
	totals := map[string]int{"": 0}
	for _, file := range scannedFiles {
		dir := filepath.ToSlash(filepath.Dir(file.Path))
		if dir == "." {
			dir = ""
		}
		for {
			totals[dir] += file.LineCount
			if dir == "" {
				break
			}
			parent := filepath.ToSlash(filepath.Dir(dir))
			if parent == "." {
				parent = ""
			}
			dir = parent
		}
	}
	return totals
}

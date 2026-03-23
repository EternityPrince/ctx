package adapter

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/vladimirkasterin/ctx/internal/codebase"
)

func hasGoProject(root string) bool {
	if info, err := os.Stat(filepath.Join(root, "go.mod")); err == nil && !info.IsDir() {
		return true
	}
	return false
}

func mergeScannedFiles(parts ...[]codebase.ScanFile) []codebase.ScanFile {
	byPath := make(map[string]codebase.ScanFile)
	for _, files := range parts {
		for _, file := range files {
			current, ok := byPath[file.RelPath]
			if !ok {
				byPath[file.RelPath] = file
				continue
			}
			if file.IsGo {
				current.IsGo = true
			}
			current.IsTest = current.IsTest || file.IsTest
			current.IsModule = current.IsModule || file.IsModule
			if current.AbsPath == "" {
				current.AbsPath = file.AbsPath
			}
			if current.Hash == "" {
				current.Hash = file.Hash
			}
			if current.SizeBytes == 0 {
				current.SizeBytes = file.SizeBytes
			}
			byPath[file.RelPath] = current
		}
	}

	merged := make([]codebase.ScanFile, 0, len(byPath))
	for _, file := range byPath {
		merged = append(merged, file)
	}
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].RelPath < merged[j].RelPath
	})
	return merged
}

func filterScannedFiles(scanned []codebase.ScanFile, keep func(codebase.ScanFile) bool) []codebase.ScanFile {
	filtered := make([]codebase.ScanFile, 0, len(scanned))
	for _, file := range scanned {
		if keep(file) {
			filtered = append(filtered, file)
		}
	}
	return filtered
}

func filterScannedMap(scanned map[string]codebase.ScanFile, keep func(codebase.ScanFile) bool) map[string]codebase.ScanFile {
	filtered := make(map[string]codebase.ScanFile)
	for relPath, file := range scanned {
		if keep(file) {
			filtered[relPath] = file
		}
	}
	return filtered
}

func filterPreviousFiles(previous map[string]codebase.PreviousFile, keep func(codebase.PreviousFile) bool) map[string]codebase.PreviousFile {
	filtered := make(map[string]codebase.PreviousFile)
	for relPath, file := range previous {
		if keep(file) {
			filtered[relPath] = file
		}
	}
	return filtered
}

func hasPreviousFiles(previous map[string]codebase.PreviousFile, keep func(codebase.PreviousFile) bool) bool {
	for _, file := range previous {
		if keep(file) {
			return true
		}
	}
	return false
}

func isGoScanFile(file codebase.ScanFile) bool {
	return codebase.IsGoFile(file.RelPath) || codebase.IsGoProjectFile(file.RelPath)
}

func isPythonScanFile(file codebase.ScanFile) bool {
	return codebase.IsPythonFile(file.RelPath) || codebase.IsPythonProjectFile(filepath.Base(file.RelPath))
}

func isGoPreviousFile(file codebase.PreviousFile) bool {
	return codebase.IsGoFile(file.RelPath) || codebase.IsGoProjectFile(file.RelPath)
}

func isPythonPreviousFile(file codebase.PreviousFile) bool {
	return codebase.IsPythonFile(file.RelPath) || codebase.IsPythonProjectFile(filepath.Base(file.RelPath))
}

func packageSets(modulePath string, scanned map[string]codebase.ScanFile) (map[string]struct{}, map[string]struct{}) {
	goPackages := make(map[string]struct{})
	pythonPackages := make(map[string]struct{})
	for _, file := range scanned {
		switch {
		case codebase.IsGoFile(file.RelPath):
			goPackages[codebase.PackageImportPath(modulePath, file.RelPath)] = struct{}{}
		case codebase.IsPythonFile(file.RelPath):
			pythonPackages[codebase.PackageImportPath(modulePath, file.RelPath)] = struct{}{}
		}
	}
	return goPackages, pythonPackages
}

func splitPatterns(patterns []string, goPackages, pythonPackages map[string]struct{}) ([]string, []string) {
	if len(patterns) == 0 {
		return nil, nil
	}

	var goPatterns []string
	var pythonPatterns []string
	for _, pattern := range patterns {
		if _, ok := goPackages[pattern]; ok {
			goPatterns = append(goPatterns, pattern)
		}
		if _, ok := pythonPackages[pattern]; ok {
			pythonPatterns = append(pythonPatterns, pattern)
		}
	}
	sort.Strings(goPatterns)
	sort.Strings(pythonPatterns)
	return goPatterns, pythonPatterns
}

func shouldAnalyze(enabled bool, scanned map[string]codebase.ScanFile, patterns, filteredPatterns []string) bool {
	if !enabled || len(scanned) == 0 {
		return false
	}
	if len(patterns) == 0 {
		return true
	}
	return len(filteredPatterns) > 0
}

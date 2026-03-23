package python

import (
	"sort"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/codebase"
)

func sortedScanPaths(scanned map[string]codebase.ScanFile) []string {
	paths := make([]string, 0, len(scanned))
	for relPath := range scanned {
		paths = append(paths, relPath)
	}
	sort.Strings(paths)
	return paths
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func findScanFile(scanned []codebase.ScanFile, relPath string) *codebase.ScanFile {
	for i := range scanned {
		if scanned[i].RelPath == relPath {
			return &scanned[i]
		}
	}
	return nil
}

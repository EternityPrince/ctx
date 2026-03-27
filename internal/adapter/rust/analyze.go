package rust

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/project"
)

func (a *Adapter) Analyze(info project.Info, scanned map[string]codebase.ScanFile, patterns []string) (*codebase.Result, error) {
	result := codebase.NewResult(info.Root, info.ModulePath, info.GoVersion)

	patternSet := make(map[string]struct{}, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		patternSet[pattern] = struct{}{}
		result.ImpactedPackage[pattern] = struct{}{}
	}

	parsedFiles := make([]rustParsedScanFile, 0, len(scanned))
	rustSources := 0
	for _, relPath := range sortedScanPaths(scanned) {
		file := scanned[relPath]
		if !codebase.IsRustFile(file.RelPath) {
			continue
		}
		rustSources++

		data, err := os.ReadFile(file.AbsPath)
		if err != nil {
			return nil, fmt.Errorf("read rust source %s: %w", file.AbsPath, err)
		}

		parsedFiles = append(parsedFiles, rustParsedScanFile{
			File:   file,
			Parsed: parseRustFile(string(data), file.RelPath, codebase.ScanPackageImportPath(info.ModulePath, file)),
		})
	}

	if rustSources == 0 && len(patternSet) == 0 {
		return nil, fmt.Errorf("no Rust source files found")
	}

	indexes := buildRustIndexes(parsedFiles)
	packageFacts := make(map[string]*codebase.PackageFact)
	packageFiles := make(map[string]map[string]struct{})

	for _, entry := range parsedFiles {
		filePkg := codebase.ScanPackageImportPath(info.ModulePath, entry.File)
		if !selectedRustPackage(patternSet, filePkg) {
			continue
		}
		if filePkg != "" {
			result.ImpactedPackage[filePkg] = struct{}{}
			addRustPackageFact(packageFacts, packageFiles, filePkg, entry.File.RelPath)
		}

		result.Files = append(result.Files, codebase.FileFact{
			RelPath:           entry.File.RelPath,
			PackageImportPath: filePkg,
			Hash:              entry.File.Hash,
			SizeBytes:         entry.File.SizeBytes,
			IsTest:            entry.File.IsTest,
		})

		for _, symbol := range entry.Parsed.Symbols {
			result.Symbols = append(result.Symbols, symbol.Fact)
		}
		for _, test := range entry.Parsed.Tests {
			result.Tests = append(result.Tests, test)
		}
	}

	for _, pkg := range packageFacts {
		result.Packages = append(result.Packages, *pkg)
	}

	result.Dependencies = buildRustDependencies(parsedFiles, indexes, patternSet)
	refs, calls, links := buildRustRelationships(parsedFiles, indexes, patternSet)
	sortRustSemantics(refs, calls)
	result.References = refs
	result.Calls = calls
	result.TestLinks = links

	codebase.SortResult(result)
	return result, nil
}

func sortedScanPaths(scanned map[string]codebase.ScanFile) []string {
	paths := make([]string, 0, len(scanned))
	for relPath := range scanned {
		paths = append(paths, relPath)
	}
	sort.Strings(paths)
	return paths
}

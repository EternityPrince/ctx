package python

import (
	"fmt"

	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/project"
)

func (a *Adapter) Analyze(info project.Info, scanned map[string]codebase.ScanFile, patterns []string) (*codebase.Result, error) {
	input := analyzerInput{
		Root:        info.Root,
		ProjectName: info.ModulePath,
		SourceRoots: pythonSourceRootsFromScannedMap(scanned),
		Patterns:    dedupeStrings(patterns),
		Files:       make([]analyzerInputFile, 0, len(scanned)),
	}

	for _, relPath := range sortedScanPaths(scanned) {
		file := scanned[relPath]
		if !codebase.IsPythonFile(file.RelPath) {
			continue
		}
		input.Files = append(input.Files, analyzerInputFile{
			AbsPath: file.AbsPath,
			RelPath: file.RelPath,
			IsTest:  file.IsTest,
		})
	}
	if len(input.Files) == 0 {
		return nil, fmt.Errorf("no Python source files found")
	}

	output, err := runAnalyzer(input)
	if err != nil {
		return nil, err
	}

	result := codebase.NewResult(info.Root, info.ModulePath, info.GoVersion)
	result.Packages = output.Packages
	result.Files = output.Files
	result.Symbols = output.Symbols
	result.Dependencies = output.Dependencies
	result.References = output.References
	result.Calls = output.Calls
	result.Flows = output.Flows
	result.Tests = output.Tests
	result.TestLinks = output.TestLinks

	impacted := output.ImpactedPackages
	if len(impacted) == 0 {
		if len(input.Patterns) > 0 {
			impacted = input.Patterns
		} else {
			for _, pkg := range output.Packages {
				impacted = append(impacted, pkg.ImportPath)
			}
		}
	}
	for _, pkg := range dedupeStrings(impacted) {
		result.ImpactedPackage[pkg] = struct{}{}
	}

	codebase.SortResult(result)
	return result, nil
}

func pythonSourceRootsFromScannedMap(scanned map[string]codebase.ScanFile) []string {
	files := make([]codebase.ScanFile, 0, len(scanned))
	for _, file := range scanned {
		files = append(files, file)
	}
	return pythonSourceRootsFromScanFiles(files)
}

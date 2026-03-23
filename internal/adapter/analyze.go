package adapter

import (
	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/project"
)

func (a *Adapter) Analyze(info project.Info, scanned map[string]codebase.ScanFile, patterns []string) (*codebase.Result, error) {
	goScanned := filterScannedMap(scanned, isGoScanFile)
	pythonScanned := filterScannedMap(scanned, isPythonScanFile)
	goPackages, pythonPackages := packageSets(info.ModulePath, scanned)
	goPatterns, pythonPatterns := splitPatterns(patterns, goPackages, pythonPackages)

	result := codebase.NewResult(info.Root, info.ModulePath, info.GoVersion)

	if shouldAnalyze(hasGoProject(info.Root), goScanned, patterns, goPatterns) {
		goResult, err := a.goAdapter.Analyze(info, goScanned, goPatterns)
		if err != nil {
			return nil, err
		}
		codebase.MergeResult(result, goResult)
	}

	if shouldAnalyze(true, pythonScanned, patterns, pythonPatterns) {
		pythonResult, err := a.pythonAdapter.Analyze(info, pythonScanned, pythonPatterns)
		if err != nil {
			return nil, err
		}
		codebase.MergeResult(result, pythonResult)
	}

	for _, pkg := range patterns {
		result.ImpactedPackage[pkg] = struct{}{}
	}
	codebase.SortResult(result)
	return result, nil
}

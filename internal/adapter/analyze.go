package adapter

import (
	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/project"
)

func (a *Adapter) Analyze(info project.Info, scanned map[string]codebase.ScanFile, patterns []string) (*codebase.Result, error) {
	goScanned := filterScannedMap(scanned, isGoScanFile)
	pythonScanned := filterScannedMap(scanned, isPythonScanFile)
	rustScanned := filterScannedMap(scanned, isRustScanFile)
	goPackages, pythonPackages, rustPackages := packageSets(info.ModulePath, scanned)
	goPatterns, pythonPatterns, rustPatterns := splitPatterns(patterns, goPackages, pythonPackages, rustPackages)

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

	if shouldAnalyze(true, rustScanned, patterns, rustPatterns) {
		rustResult, err := a.rustAdapter.Analyze(info, rustScanned, rustPatterns)
		if err != nil {
			return nil, err
		}
		codebase.MergeResult(result, rustResult)
	}

	for _, pkg := range patterns {
		result.ImpactedPackage[pkg] = struct{}{}
	}
	codebase.SortResult(result)
	return result, nil
}

package golang

import (
	"sort"

	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/project"
)

type Adapter struct{}

func NewAdapter() *Adapter {
	return &Adapter{}
}

func (a *Adapter) DetectChanges(info project.Info, scanned []codebase.ScanFile, previous map[string]codebase.PreviousFile) codebase.ChangePlan {
	changes := codebase.Diff(scanned, previous)
	if len(previous) == 0 {
		return codebase.ChangePlan{
			Changes:     changes,
			FullReindex: true,
		}
	}

	impacted := make(map[string]struct{})
	fullReindex := false

	changedPaths := append(append([]string{}, changes.Added...), changes.Changed...)
	for _, relPath := range changedPaths {
		file := findScanFile(scanned, relPath)
		if file == nil {
			continue
		}
		if file.IsModule {
			fullReindex = true
			continue
		}

		pkg := codebase.PackageImportPath(info.ModulePath, file.RelPath)
		if prev, ok := previous[file.RelPath]; ok && prev.PackageImportPath != "" {
			pkg = prev.PackageImportPath
		}
		if pkg != "" {
			impacted[pkg] = struct{}{}
		}
	}

	for _, relPath := range changes.Deleted {
		prev, ok := previous[relPath]
		if !ok {
			continue
		}
		if prev.PackageImportPath == "" {
			fullReindex = true
			continue
		}
		impacted[prev.PackageImportPath] = struct{}{}
	}

	impactedPackages := make([]string, 0, len(impacted))
	for pkg := range impacted {
		impactedPackages = append(impactedPackages, pkg)
	}
	sort.Strings(impactedPackages)

	return codebase.ChangePlan{
		Changes:          changes,
		ImpactedPackages: impactedPackages,
		FullReindex:      fullReindex,
	}
}

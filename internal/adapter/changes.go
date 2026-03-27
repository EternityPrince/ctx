package adapter

import (
	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/project"
)

func (a *Adapter) DetectChanges(info project.Info, scanned []codebase.ScanFile, previous map[string]codebase.PreviousFile) codebase.ChangePlan {
	changes := codebase.Diff(scanned, previous)
	if len(previous) == 0 {
		return codebase.ChangePlan{
			Changes:     changes,
			FullReindex: true,
		}
	}

	plans := make([]codebase.ChangePlan, 0, 3)

	goScanned := filterScannedFiles(scanned, isGoScanFile)
	if hasGoProject(info.Root) || len(goScanned) > 0 || hasPreviousFiles(previous, isGoPreviousFile) {
		plans = append(plans, a.goAdapter.DetectChanges(info, goScanned, filterPreviousFiles(previous, isGoPreviousFile)))
	}

	pythonScanned := filterScannedFiles(scanned, isPythonScanFile)
	if len(pythonScanned) > 0 || hasPreviousFiles(previous, isPythonPreviousFile) {
		plans = append(plans, a.pythonAdapter.DetectChanges(info, pythonScanned, filterPreviousFiles(previous, isPythonPreviousFile)))
	}

	rustScanned := filterScannedFiles(scanned, isRustScanFile)
	if len(rustScanned) > 0 || hasPreviousFiles(previous, isRustPreviousFile) {
		plans = append(plans, a.rustAdapter.DetectChanges(info, rustScanned, filterPreviousFiles(previous, isRustPreviousFile)))
	}

	return codebase.MergeChangePlans(changes, plans...)
}

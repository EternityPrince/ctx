package codebase

import "strings"

type projectFileDecision struct {
	Handled          bool
	NoOp             bool
	FullReindex      bool
	ImpactedPackages []string
	Reasons          []string
}

type projectFilePolicy func(relPath string, current *ScanFile, previous *PreviousFile, allPackages []string) projectFileDecision

func DetectGoChanges(modulePath string, scanned []ScanFile, previous map[string]PreviousFile) ChangePlan {
	return detectChangesWithPolicy(modulePath, scanned, previous, goProjectFileDecision)
}

func DetectPythonChanges(modulePath string, scanned []ScanFile, previous map[string]PreviousFile) ChangePlan {
	return detectChangesWithPolicy(modulePath, scanned, previous, pythonProjectFileDecision)
}

func DetectRustChanges(modulePath string, scanned []ScanFile, previous map[string]PreviousFile) ChangePlan {
	return detectChangesWithPolicy(modulePath, scanned, previous, rustProjectFileDecision)
}

func detectChangesWithPolicy(modulePath string, scanned []ScanFile, previous map[string]PreviousFile, policy projectFilePolicy) ChangePlan {
	changes := Diff(scanned, previous)
	if len(previous) == 0 {
		return ChangePlan{
			Changes:     changes,
			FullReindex: true,
			Reason:      "no previous snapshot",
		}
	}

	current := ScanMap(scanned)
	allPackages := changePlanPackages(scanned, previous)
	impacted := make(map[string]struct{})
	fullReindex := false
	reasons := make([]string, 0, 2)

	changedPaths := make([]string, 0, len(changes.Added)+len(changes.Changed))
	changedPaths = append(changedPaths, changes.Added...)
	changedPaths = append(changedPaths, changes.Changed...)
	for _, relPath := range changedPaths {
		file, ok := current[relPath]
		if !ok {
			continue
		}

		var prev *PreviousFile
		if previousFile, exists := previous[relPath]; exists {
			prev = &previousFile
		}
		if decision := policy(relPath, &file, prev, allPackages); decision.Handled {
			fullReindex = fullReindex || decision.FullReindex
			reasons = append(reasons, decision.Reasons...)
			for _, pkg := range decision.ImpactedPackages {
				if strings.TrimSpace(pkg) != "" {
					impacted[pkg] = struct{}{}
				}
			}
			if decision.NoOp || decision.FullReindex || len(decision.ImpactedPackages) > 0 {
				continue
			}
		}

		pkg := ScanPackageImportPath(modulePath, file)
		if prev != nil && strings.TrimSpace(prev.PackageImportPath) != "" {
			pkg = strings.TrimSpace(prev.PackageImportPath)
		}
		if pkg != "" {
			impacted[pkg] = struct{}{}
			reasons = append(reasons, "package-scoped changes")
		}
	}

	for _, relPath := range changes.Deleted {
		prev, ok := previous[relPath]
		if !ok {
			continue
		}
		if decision := policy(relPath, nil, &prev, allPackages); decision.Handled {
			fullReindex = fullReindex || decision.FullReindex
			reasons = append(reasons, decision.Reasons...)
			for _, pkg := range decision.ImpactedPackages {
				if strings.TrimSpace(pkg) != "" {
					impacted[pkg] = struct{}{}
				}
			}
			if decision.NoOp || decision.FullReindex || len(decision.ImpactedPackages) > 0 {
				continue
			}
		}

		if prev.PackageImportPath == "" {
			fullReindex = true
			reasons = append(reasons, "deleted file with unknown package: "+relPath)
			continue
		}
		impacted[prev.PackageImportPath] = struct{}{}
		reasons = append(reasons, "package-scoped changes")
	}

	if len(reasons) == 0 {
		switch {
		case fullReindex:
			reasons = append(reasons, "full reindex required")
		case len(impacted) > 0:
			reasons = append(reasons, "package-scoped changes")
		default:
			reasons = append(reasons, "no indexed file changes")
		}
	}

	return ChangePlan{
		Changes:          changes,
		ImpactedPackages: sortedSetKeys(impacted),
		FullReindex:      fullReindex,
		Reason:           joinPlanReasons(reasons),
	}
}

func changePlanPackages(scanned []ScanFile, previous map[string]PreviousFile) []string {
	packages := make(map[string]struct{})
	for _, file := range scanned {
		if value := strings.TrimSpace(file.PackageImportPath); value != "" {
			packages[value] = struct{}{}
		}
	}
	for _, file := range previous {
		if value := strings.TrimSpace(file.PackageImportPath); value != "" {
			packages[value] = struct{}{}
		}
	}
	return sortedSetKeys(packages)
}

func goProjectFileDecision(relPath string, current *ScanFile, previous *PreviousFile, allPackages []string) projectFileDecision {
	switch baseName(relPath) {
	case "go.sum":
		return projectFileDecision{Handled: true, NoOp: true, Reasons: []string{"dependency checksum file changed: " + relPath}}
	case "go.mod":
		currentModule := ""
		if current != nil {
			currentModule = strings.TrimSpace(current.Identity)
		}
		previousModule := ""
		if previous != nil {
			previousModule = strings.TrimSpace(previous.Identity)
		}
		switch {
		case current == nil:
			return projectFileDecision{Handled: true, FullReindex: true, Reasons: []string{"go.mod deleted: " + relPath}}
		case currentModule == "" || previousModule == "":
			return projectFileDecision{Handled: true, FullReindex: true, Reasons: []string{"go.mod changed without stable module identity: " + relPath}}
		case currentModule != previousModule:
			return projectFileDecision{Handled: true, FullReindex: true, Reasons: []string{"module path changed in go.mod: " + previousModule + " -> " + currentModule}}
		default:
			return projectFileDecision{Handled: true, NoOp: true, Reasons: []string{"go.mod changed without module path move: " + currentModule}}
		}
	}
	return projectFileDecision{}
}

func pythonProjectFileDecision(relPath string, current *ScanFile, previous *PreviousFile, allPackages []string) projectFileDecision {
	if !IsPythonProjectFile(baseName(relPath)) {
		return projectFileDecision{}
	}
	return projectFileDecision{
		Handled: true,
		NoOp:    true,
		Reasons: []string{"python project metadata changed without source graph impact: " + relPath},
	}
}

func rustProjectFileDecision(relPath string, current *ScanFile, previous *PreviousFile, allPackages []string) projectFileDecision {
	switch baseName(relPath) {
	case "Cargo.lock":
		return projectFileDecision{Handled: true, NoOp: true, Reasons: []string{"dependency lockfile changed: " + relPath}}
	case "Cargo.toml":
		currentIdentity := ""
		if current != nil {
			currentIdentity = strings.TrimSpace(current.Identity)
		}
		previousIdentity := ""
		if previous != nil {
			previousIdentity = strings.TrimSpace(previous.Identity)
		}
		switch {
		case current == nil:
			return projectFileDecision{Handled: true, FullReindex: true, Reasons: []string{"Cargo.toml deleted: " + relPath}}
		case currentIdentity == "" || previousIdentity == "":
			return projectFileDecision{Handled: true, FullReindex: true, Reasons: []string{"workspace or unstable crate manifest changed: " + relPath}}
		case currentIdentity != previousIdentity:
			return projectFileDecision{Handled: true, FullReindex: true, Reasons: []string{"manifest identity changed in Cargo.toml: " + previousIdentity + " -> " + currentIdentity}}
		case strings.HasPrefix(currentIdentity, "rust:workspace:"):
			return projectFileDecision{Handled: true, NoOp: true, Reasons: []string{"workspace manifest changed without workspace identity move: " + currentIdentity}}
		default:
			return projectFileDecision{Handled: true, NoOp: true, Reasons: []string{"crate manifest changed without crate identity move: " + currentIdentity}}
		}
	}
	return projectFileDecision{}
}

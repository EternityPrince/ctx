package codebase

import "strings"

type projectFileDecision struct {
	Handled          bool
	NoOp             bool
	FullReindex      bool
	ImpactedPackages []string
	Reasons          []string
	ManifestChange   *ManifestDelta
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
	manifestChanges := make([]ManifestDelta, 0, 2)

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
			if decision.ManifestChange != nil {
				manifestChanges = append(manifestChanges, *decision.ManifestChange)
			}
			for _, pkg := range decision.ImpactedPackages {
				if strings.TrimSpace(pkg) != "" {
					impacted[pkg] = struct{}{}
				}
			}
			continue
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
			if decision.ManifestChange != nil {
				manifestChanges = append(manifestChanges, *decision.ManifestChange)
			}
			for _, pkg := range decision.ImpactedPackages {
				if strings.TrimSpace(pkg) != "" {
					impacted[pkg] = struct{}{}
				}
			}
			continue
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
		ManifestChanges:  manifestChanges,
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
		return projectFileDecision{
			Handled: true,
			NoOp:    true,
			Reasons: []string{"dependency checksum file changed: " + relPath},
			ManifestChange: &ManifestDelta{
				RelPath: relPath,
				Kind:    "go.sum",
				Impact:  "noop",
				Details: []string{"dependency checksum file changed without source-graph impact"},
			},
		}
	case "go.mod":
		currentMeta := decodeCurrentManifestMeta(current)
		previousMeta := decodePreviousManifestMeta(previous)
		delta := ManifestDelta{RelPath: relPath, Kind: "go.mod"}
		switch {
		case current == nil:
			delta.Impact = "full"
			delta.Details = []string{"go.mod deleted"}
			return projectFileDecision{Handled: true, FullReindex: true, Reasons: []string{"go.mod deleted: " + relPath}, ManifestChange: &delta}
		case currentMeta.Module == "" || previousMeta.Module == "":
			delta.Impact = "full"
			delta.Details = []string{"go.mod changed without stable module identity"}
			return projectFileDecision{Handled: true, FullReindex: true, Reasons: []string{"go.mod changed without stable module identity: " + relPath}, ManifestChange: &delta}
		case currentMeta.Module != previousMeta.Module:
			delta.Impact = "full"
			delta.PrevValue = previousMeta.Module
			delta.CurValue = currentMeta.Module
			delta.Details = []string{"module path changed"}
			return projectFileDecision{Handled: true, FullReindex: true, Reasons: []string{"module path changed in go.mod: " + previousMeta.Module + " -> " + currentMeta.Module}, ManifestChange: &delta}
		}
		if added, removed := diffManifestList(previousMeta.LocalDeps, currentMeta.LocalDeps); len(added) > 0 || len(removed) > 0 {
			delta.Impact = "package"
			delta.Packages = append([]string(nil), allPackages...)
			delta.Details = summarizeManifestListDelta("local replace targets changed", added, removed)
			return projectFileDecision{
				Handled:          true,
				ImpactedPackages: append([]string(nil), allPackages...),
				Reasons:          []string{"go.mod local replace targets changed: " + relPath},
				ManifestChange:   &delta,
			}
		}
		if currentMeta.GoVersion != previousMeta.GoVersion && currentMeta.GoVersion != "" && previousMeta.GoVersion != "" {
			delta.Impact = "package"
			delta.Packages = append([]string(nil), allPackages...)
			delta.PrevValue = previousMeta.GoVersion
			delta.CurValue = currentMeta.GoVersion
			delta.Details = []string{"go version changed"}
			return projectFileDecision{
				Handled:          true,
				ImpactedPackages: append([]string(nil), allPackages...),
				Reasons:          []string{"go version changed in go.mod: " + previousMeta.GoVersion + " -> " + currentMeta.GoVersion},
				ManifestChange:   &delta,
			}
		}
		if added, removed := diffManifestList(previousMeta.ExternalDeps, currentMeta.ExternalDeps); len(added) > 0 || len(removed) > 0 {
			delta.Impact = "noop"
			delta.Details = summarizeManifestListDelta("dependency requirements changed without local package move", added, removed)
			return projectFileDecision{
				Handled:        true,
				NoOp:           true,
				Reasons:        []string{"go.mod dependency requirements changed without local package move: " + currentMeta.Module},
				ManifestChange: &delta,
			}
		}
		delta.Impact = "noop"
		delta.Details = []string{"go.mod changed without semantic package impact"}
		return projectFileDecision{Handled: true, NoOp: true, Reasons: []string{"go.mod changed without module path move: " + currentMeta.Module}, ManifestChange: &delta}
	}
	return projectFileDecision{}
}

func pythonProjectFileDecision(relPath string, current *ScanFile, previous *PreviousFile, allPackages []string) projectFileDecision {
	base := baseName(relPath)
	if !IsPythonProjectFile(base) {
		return projectFileDecision{}
	}
	currentMeta := decodeCurrentManifestMeta(current)
	previousMeta := decodePreviousManifestMeta(previous)
	delta := ManifestDelta{
		RelPath: relPath,
		Kind:    base,
		Impact:  "noop",
	}

	if added, removed := diffManifestList(previousMeta.PackageRoots, currentMeta.PackageRoots); len(added) > 0 || len(removed) > 0 {
		delta.Impact = "full"
		delta.Details = summarizeManifestListDelta("python package roots changed", added, removed)
		return projectFileDecision{
			Handled:        true,
			FullReindex:    true,
			Reasons:        []string{"python package roots changed: " + relPath},
			ManifestChange: &delta,
		}
	}

	if currentMeta.RequiresPython != previousMeta.RequiresPython && currentMeta.RequiresPython != "" && previousMeta.RequiresPython != "" {
		delta.Impact = "package"
		delta.Packages = append([]string(nil), allPackages...)
		delta.Details = []string{"python runtime requirement changed"}
		delta.PrevValue = previousMeta.RequiresPython
		delta.CurValue = currentMeta.RequiresPython
		return projectFileDecision{
			Handled:          true,
			ImpactedPackages: append([]string(nil), allPackages...),
			Reasons:          []string{"python runtime requirement changed: " + relPath},
			ManifestChange:   &delta,
		}
	}

	if added, removed := diffManifestList(previousMeta.LocalDeps, currentMeta.LocalDeps); len(added) > 0 || len(removed) > 0 {
		delta.Impact = "package"
		delta.Packages = append([]string(nil), allPackages...)
		delta.Details = summarizeManifestListDelta("local python dependencies changed", added, removed)
		return projectFileDecision{
			Handled:          true,
			ImpactedPackages: append([]string(nil), allPackages...),
			Reasons:          []string{"local python dependencies changed: " + relPath},
			ManifestChange:   &delta,
		}
	}

	switch base {
	case "pyproject.toml", "setup.cfg", "setup.py":
		switch {
		case current == nil:
			delta.Impact = "full"
			delta.Details = []string{base + " deleted"}
			return projectFileDecision{Handled: true, FullReindex: true, Reasons: []string{base + " deleted: " + relPath}, ManifestChange: &delta}
		case previous == nil:
			delta.Impact = "full"
			delta.Details = []string{base + " added"}
			return projectFileDecision{Handled: true, FullReindex: true, Reasons: []string{base + " added: " + relPath}, ManifestChange: &delta}
		case currentMeta.Name != previousMeta.Name && currentMeta.Name != "" && previousMeta.Name != "":
			delta.Details = []string{"project metadata name changed without import-path derivation change"}
			delta.PrevValue = previousMeta.Name
			delta.CurValue = currentMeta.Name
			return projectFileDecision{
				Handled:        true,
				NoOp:           true,
				Reasons:        []string{"python project metadata changed without source graph impact: " + relPath},
				ManifestChange: &delta,
			}
		default:
			if added, removed := diffManifestList(previousMeta.ExternalDeps, currentMeta.ExternalDeps); len(added) > 0 || len(removed) > 0 {
				delta.Details = summarizeManifestListDelta("external python dependencies changed without local graph impact", added, removed)
			} else {
				delta.Details = []string{"python project metadata changed without source-graph impact"}
			}
		}
	case "requirements.txt", "Pipfile", "poetry.lock":
		if added, removed := diffManifestList(previousMeta.ExternalDeps, currentMeta.ExternalDeps); len(added) > 0 || len(removed) > 0 {
			delta.Details = summarizeManifestListDelta("python dependency set changed without local graph impact", added, removed)
		} else {
			delta.Details = []string{"python dependency metadata changed without source-graph impact"}
		}
	default:
		delta.Details = []string{"python project metadata changed without source-graph impact"}
	}
	return projectFileDecision{
		Handled:        true,
		NoOp:           true,
		Reasons:        []string{"python project metadata changed without source graph impact: " + relPath},
		ManifestChange: &delta,
	}
}

func rustProjectFileDecision(relPath string, current *ScanFile, previous *PreviousFile, allPackages []string) projectFileDecision {
	switch baseName(relPath) {
	case "Cargo.lock":
		return projectFileDecision{
			Handled: true,
			NoOp:    true,
			Reasons: []string{"dependency lockfile changed: " + relPath},
			ManifestChange: &ManifestDelta{
				RelPath: relPath,
				Kind:    "Cargo.lock",
				Impact:  "noop",
				Details: []string{"dependency lockfile changed without source-graph impact"},
			},
		}
	case "Cargo.toml":
		currentIdentity := currentIdentity(current)
		previousIdentity := previousIdentity(previous)
		currentMeta := decodeCurrentManifestMeta(current)
		previousMeta := decodePreviousManifestMeta(previous)
		delta := ManifestDelta{RelPath: relPath, Kind: "Cargo.toml"}
		switch {
		case current == nil:
			delta.Impact = "full"
			delta.Details = []string{"Cargo.toml deleted"}
			return projectFileDecision{Handled: true, FullReindex: true, Reasons: []string{"Cargo.toml deleted: " + relPath}, ManifestChange: &delta}
		case currentIdentity == "" || previousIdentity == "":
			delta.Impact = "full"
			delta.Details = []string{"manifest changed without stable identity"}
			return projectFileDecision{Handled: true, FullReindex: true, Reasons: []string{"workspace or unstable crate manifest changed: " + relPath}, ManifestChange: &delta}
		case currentIdentity != previousIdentity:
			delta.Impact = "full"
			delta.PrevValue = previousIdentity
			delta.CurValue = currentIdentity
			delta.Details = []string{"manifest identity changed"}
			return projectFileDecision{Handled: true, FullReindex: true, Reasons: []string{"manifest identity changed in Cargo.toml: " + previousIdentity + " -> " + currentIdentity}, ManifestChange: &delta}
		}

		if added, removed := diffManifestList(previousMeta.WorkspaceMembers, currentMeta.WorkspaceMembers); len(added) > 0 || len(removed) > 0 {
			delta.Impact = "full"
			delta.Details = summarizeManifestListDelta("workspace members changed", added, removed)
			return projectFileDecision{Handled: true, FullReindex: true, Reasons: []string{"workspace members changed in Cargo.toml: " + currentIdentity}, ManifestChange: &delta}
		}
		if added, removed := diffManifestList(previousMeta.WorkspaceExclude, currentMeta.WorkspaceExclude); len(added) > 0 || len(removed) > 0 {
			delta.Impact = "full"
			delta.Details = summarizeManifestListDelta("workspace excludes changed", added, removed)
			return projectFileDecision{Handled: true, FullReindex: true, Reasons: []string{"workspace exclude rules changed in Cargo.toml: " + currentIdentity}, ManifestChange: &delta}
		}
		if added, removed := diffManifestList(previousMeta.LocalDeps, currentMeta.LocalDeps); len(added) > 0 || len(removed) > 0 {
			packages := rustManifestImpactPackages(currentIdentity, allPackages)
			delta.Impact = "package"
			delta.Packages = packages
			delta.Details = summarizeManifestListDelta("local cargo dependencies changed", added, removed)
			return projectFileDecision{
				Handled:          true,
				ImpactedPackages: packages,
				Reasons:          []string{"local cargo dependencies changed in Cargo.toml: " + currentIdentity},
				ManifestChange:   &delta,
			}
		}
		if added, removed := diffManifestList(previousMeta.Features, currentMeta.Features); len(added) > 0 || len(removed) > 0 {
			packages := rustManifestImpactPackages(currentIdentity, allPackages)
			delta.Impact = "package"
			delta.Packages = packages
			delta.Details = summarizeManifestListDelta("crate or workspace features changed", added, removed)
			return projectFileDecision{
				Handled:          true,
				ImpactedPackages: packages,
				Reasons:          []string{"cargo feature surface changed in Cargo.toml: " + currentIdentity},
				ManifestChange:   &delta,
			}
		}
		if currentMeta.Edition != previousMeta.Edition && currentMeta.Edition != "" && previousMeta.Edition != "" {
			packages := rustManifestImpactPackages(currentIdentity, allPackages)
			delta.Impact = "package"
			delta.Packages = packages
			delta.PrevValue = previousMeta.Edition
			delta.CurValue = currentMeta.Edition
			delta.Details = []string{"rust edition changed"}
			return projectFileDecision{
				Handled:          true,
				ImpactedPackages: packages,
				Reasons:          []string{"rust edition changed in Cargo.toml: " + previousMeta.Edition + " -> " + currentMeta.Edition},
				ManifestChange:   &delta,
			}
		}
		if added, removed := diffManifestList(previousMeta.ExternalDeps, currentMeta.ExternalDeps); len(added) > 0 || len(removed) > 0 {
			delta.Impact = "noop"
			delta.Details = summarizeManifestListDelta("external cargo dependencies changed without local graph move", added, removed)
			return projectFileDecision{
				Handled:        true,
				NoOp:           true,
				Reasons:        []string{"external cargo dependencies changed without local graph move: " + currentIdentity},
				ManifestChange: &delta,
			}
		}
		if strings.HasPrefix(currentIdentity, "rust:workspace:") {
			delta.Impact = "noop"
			delta.Details = []string{"workspace manifest changed without semantic package impact"}
			return projectFileDecision{Handled: true, NoOp: true, Reasons: []string{"workspace manifest changed without workspace identity move: " + currentIdentity}, ManifestChange: &delta}
		}
		delta.Impact = "noop"
		delta.Details = []string{"crate manifest changed without semantic package impact"}
		return projectFileDecision{Handled: true, NoOp: true, Reasons: []string{"crate manifest changed without crate identity move: " + currentIdentity}, ManifestChange: &delta}
	}
	return projectFileDecision{}
}

func decodeCurrentManifestMeta(file *ScanFile) ManifestMeta {
	if file == nil {
		return ManifestMeta{}
	}
	meta := DecodeManifestMeta(file.SemanticMeta)
	return fallbackManifestMeta(meta, file.Identity)
}

func decodePreviousManifestMeta(file *PreviousFile) ManifestMeta {
	if file == nil {
		return ManifestMeta{}
	}
	meta := DecodeManifestMeta(file.SemanticMeta)
	return fallbackManifestMeta(meta, file.Identity)
}

func currentIdentity(file *ScanFile) string {
	if file == nil {
		return ""
	}
	return strings.TrimSpace(file.Identity)
}

func previousIdentity(file *PreviousFile) string {
	if file == nil {
		return ""
	}
	return strings.TrimSpace(file.Identity)
}

func summarizeManifestListDelta(prefix string, added, removed []string) []string {
	details := []string{prefix}
	if len(added) > 0 {
		details = append(details, "added "+strings.Join(added, ", "))
	}
	if len(removed) > 0 {
		details = append(details, "removed "+strings.Join(removed, ", "))
	}
	return details
}

func rustManifestImpactPackages(identity string, allPackages []string) []string {
	identity = strings.TrimSpace(identity)
	switch {
	case strings.HasPrefix(identity, "rust:workspace:"):
		return append([]string(nil), allPackages...)
	case strings.HasPrefix(identity, "rust:crate:"):
		name := strings.TrimSpace(strings.TrimPrefix(identity, "rust:crate:"))
		if name != "" {
			return []string{name}
		}
	}
	return nil
}

func fallbackManifestMeta(meta ManifestMeta, identity string) ManifestMeta {
	identity = strings.TrimSpace(identity)
	if meta.Module == "" && identity != "" && !strings.HasPrefix(identity, "rust:") {
		meta.Module = identity
	}
	if meta.Name == "" && strings.HasPrefix(identity, "rust:crate:") {
		meta.Name = strings.TrimSpace(strings.TrimPrefix(identity, "rust:crate:"))
	}
	if meta.Kind == "" && strings.HasPrefix(identity, "rust:workspace:") {
		meta.Kind = "cargo-workspace"
	}
	if meta.Kind == "" && strings.HasPrefix(identity, "rust:crate:") {
		meta.Kind = "cargo-crate"
	}
	return meta
}

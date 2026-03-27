package codebase

import "strings"

func DetectPackageChanges(modulePath string, scanned []ScanFile, previous map[string]PreviousFile) ChangePlan {
	changes := Diff(scanned, previous)
	if len(previous) == 0 {
		return ChangePlan{
			Changes:     changes,
			FullReindex: true,
			Reason:      "no previous snapshot",
		}
	}

	current := ScanMap(scanned)
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
		if file.IsModule {
			fullReindex = true
			reasons = append(reasons, "module file changed: "+relPath)
			continue
		}

		pkg := ScanPackageImportPath(modulePath, file)
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
			reasons = append(reasons, "deleted file with unknown package: "+relPath)
			continue
		}
		impacted[prev.PackageImportPath] = struct{}{}
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

func MergeChangePlans(changes ChangeSet, plans ...ChangePlan) ChangePlan {
	impacted := make(map[string]struct{})
	fullReindex := false
	reasons := make([]string, 0, len(plans))
	manifestChanges := make([]ManifestDelta, 0, len(plans))

	for _, plan := range plans {
		fullReindex = fullReindex || plan.FullReindex
		for _, pkg := range plan.ImpactedPackages {
			impacted[pkg] = struct{}{}
		}
		manifestChanges = append(manifestChanges, plan.ManifestChanges...)
		if value := strings.TrimSpace(plan.Reason); value != "" {
			reasons = append(reasons, value)
		}
	}

	if len(reasons) == 0 {
		switch {
		case fullReindex:
			reasons = append(reasons, "full reindex required")
		case changes.Count() > 0:
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

func joinPlanReasons(values []string) string {
	if len(values) == 0 {
		return ""
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
	return strings.Join(result, "; ")
}

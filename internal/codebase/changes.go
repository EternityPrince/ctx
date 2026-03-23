package codebase

func DetectPackageChanges(modulePath string, scanned []ScanFile, previous map[string]PreviousFile) ChangePlan {
	changes := Diff(scanned, previous)
	if len(previous) == 0 {
		return ChangePlan{
			Changes:     changes,
			FullReindex: true,
		}
	}

	current := ScanMap(scanned)
	impacted := make(map[string]struct{})
	fullReindex := false

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
			continue
		}

		pkg := PackageImportPath(modulePath, file.RelPath)
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

	return ChangePlan{
		Changes:          changes,
		ImpactedPackages: sortedSetKeys(impacted),
		FullReindex:      fullReindex,
	}
}

func MergeChangePlans(changes ChangeSet, plans ...ChangePlan) ChangePlan {
	impacted := make(map[string]struct{})
	fullReindex := false

	for _, plan := range plans {
		fullReindex = fullReindex || plan.FullReindex
		for _, pkg := range plan.ImpactedPackages {
			impacted[pkg] = struct{}{}
		}
	}

	return ChangePlan{
		Changes:          changes,
		ImpactedPackages: sortedSetKeys(impacted),
		FullReindex:      fullReindex,
	}
}

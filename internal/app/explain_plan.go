package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/core"
)

func buildChangePlanExplain(state core.ProjectState, plan codebase.ChangePlan) (explainSection, error) {
	currentByPath := codebase.ScanMap(state.Scanned)
	section := explainSection{
		Title: "Explain",
		Facts: []explainFact{
			{Key: "Strategy", Value: describeChangePlanStrategy(plan)},
			{Key: "Reason", Value: blankIf(strings.TrimSpace(plan.Reason), "none")},
			{Key: "Change cache", Value: fmt.Sprintf("%s (%s)", yesNo(plan.CacheHit), shortFingerprint(plan.Fingerprint))},
			{Key: "Changes", Value: fmt.Sprintf("added=%d changed=%d deleted=%d", len(plan.Changes.Added), len(plan.Changes.Changed), len(plan.Changes.Deleted))},
			{Key: "Package scope", Value: fmt.Sprintf("direct=%d expanded=%d reused=%d (%d%%)", plan.Metrics.DirectPackageCount, plan.Metrics.ExpandedPackageCount, plan.Metrics.ReusedPackageCount, plan.Metrics.ReusePercent)},
			{Key: "Precision", Value: "manifest-aware invalidation + reverse-dependency expansion; dynamic runtime paths may be missing"},
		},
	}

	if plan.Changes.Count() > 0 {
		items := make([]explainItem, 0, plan.Changes.Count())
		for _, relPath := range plan.Changes.Added {
			items = append(items, explainItem{Label: fmt.Sprintf("%s [added]", relPath), Details: []string{explainChangedPath(relPath, currentByPath, state.Previous)}})
		}
		for _, relPath := range plan.Changes.Changed {
			items = append(items, explainItem{Label: fmt.Sprintf("%s [changed]", relPath), Details: []string{explainChangedPath(relPath, currentByPath, state.Previous)}})
		}
		for _, relPath := range plan.Changes.Deleted {
			items = append(items, explainItem{Label: fmt.Sprintf("%s [deleted]", relPath), Details: []string{explainDeletedPath(relPath, state.Previous)}})
		}
		section.Groups = append(section.Groups, explainGroup{Title: "Changed paths", Items: items})
	}

	if len(plan.ImpactedPackages) > 0 {
		items := make([]explainItem, 0, len(plan.ImpactedPackages))
		for _, pkg := range plan.ImpactedPackages {
			items = append(items, explainItem{Label: pkg})
		}
		section.Groups = append(section.Groups, explainGroup{Title: "Directly impacted packages", Items: items})
	}

	if len(plan.ManifestChanges) > 0 {
		items := make([]explainItem, 0, len(plan.ManifestChanges))
		for _, delta := range plan.ManifestChanges {
			details := append([]string{}, delta.Details...)
			if delta.PrevValue != "" || delta.CurValue != "" {
				details = append(details, fmt.Sprintf("prev=%q current=%q", delta.PrevValue, delta.CurValue))
			}
			if len(delta.Packages) > 0 {
				details = append(details, fmt.Sprintf("packages=%s", strings.Join(delta.Packages, ", ")))
			}
			items = append(items, explainItem{
				Label:   fmt.Sprintf("%s [%s/%s]", delta.RelPath, delta.Kind, delta.Impact),
				Details: details,
			})
		}
		section.Groups = append(section.Groups, explainGroup{Title: "Manifest semantics", Items: items})
	}

	current, ok, err := state.Store.CurrentSnapshot()
	if err != nil {
		return explainSection{}, err
	}
	if ok && !plan.FullReindex && len(plan.ImpactedPackages) > 0 {
		reverse, err := state.Store.ReverseDependencies(current.ID, plan.ImpactedPackages)
		if err != nil {
			return explainSection{}, err
		}
		if len(reverse) > 0 {
			items := make([]explainItem, 0, len(reverse))
			for _, pkg := range reverse {
				items = append(items, explainItem{Label: pkg, Details: []string{"reverse package dependency in local graph"}})
			}
			section.Groups = append(section.Groups, explainGroup{Title: "Expanded via reverse deps", Items: items})
		}
	}

	return section, nil
}

func describeChangePlanStrategy(plan codebase.ChangePlan) string {
	switch {
	case plan.FullReindex:
		return "full reindex"
	case plan.Changes.Count() == 0:
		return "no-op"
	default:
		return blankIf(plan.Metrics.Mode, "incremental update")
	}
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func explainChangedPath(relPath string, current map[string]codebase.ScanFile, previous map[string]codebase.PreviousFile) string {
	if file, ok := current[relPath]; ok {
		if file.IsModule {
			switch {
			case filepath.Base(relPath) == "go.sum":
				return "dependency checksum file -> no reindex"
			case filepath.Base(relPath) == "Cargo.lock":
				return "dependency lockfile -> no reindex"
			case codebase.IsPythonProjectFile(filepath.Base(relPath)):
				return "python project metadata -> manifest-aware invalidation"
			case strings.TrimSpace(file.Identity) != "":
				return "project manifest for " + strings.TrimSpace(file.Identity) + " -> manifest-aware invalidation"
			default:
				return "project/workspace manifest -> manifest-aware invalidation"
			}
		}
		if pkg := strings.TrimSpace(file.PackageImportPath); pkg != "" {
			return "package " + pkg
		}
	}
	if prev, ok := previous[relPath]; ok && strings.TrimSpace(prev.PackageImportPath) != "" {
		return "package " + strings.TrimSpace(prev.PackageImportPath)
	}
	return "package unknown -> conservative handling"
}

func explainDeletedPath(relPath string, previous map[string]codebase.PreviousFile) string {
	switch {
	case filepath.Base(relPath) == "go.sum":
		return "dependency checksum file -> no reindex"
	case filepath.Base(relPath) == "Cargo.lock":
		return "dependency lockfile -> no reindex"
	case codebase.IsPythonProjectFile(filepath.Base(relPath)):
		return "python project metadata -> manifest-aware invalidation"
	}
	if prev, ok := previous[relPath]; ok {
		if pkg := strings.TrimSpace(prev.Identity); pkg != "" && (codebase.IsGoProjectFile(relPath) || codebase.IsRustProjectFile(relPath)) {
			return "project manifest for " + pkg + " -> manifest-aware invalidation"
		}
		if pkg := strings.TrimSpace(prev.PackageImportPath); pkg != "" {
			if codebase.IsGoProjectFile(relPath) || codebase.IsRustProjectFile(relPath) {
				return "project manifest for " + pkg + " -> manifest-aware invalidation"
			}
			return "package " + pkg
		}
	}
	return "package unknown -> full reindex"
}

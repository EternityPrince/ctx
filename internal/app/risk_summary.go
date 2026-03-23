package app

import (
	"fmt"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

func joinCompactRisk(parts []string) string {
	if len(parts) == 0 {
		return "contained"
	}
	seen := make(map[string]struct{}, len(parts))
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		filtered = append(filtered, part)
	}
	if len(filtered) == 0 {
		return "contained"
	}
	return strings.Join(filtered, " | ")
}

func symbolSearchRiskSummary(symbol storage.SymbolMatch) string {
	return joinCompactRisk(symbolRiskNotes(
		symbol.CallerCount,
		symbol.CalleeCount,
		symbol.ReferenceCount,
		0,
		symbol.TestCount,
		symbol.ReversePackageDeps,
		symbol.PackageImportance,
	))
}

func symbolViewRiskSummary(view storage.SymbolView) string {
	return joinCompactRisk(symbolRiskNotes(
		len(view.Callers),
		len(view.Callees),
		len(view.ReferencesIn),
		len(view.ReferencesOut),
		len(view.Tests),
		len(view.Package.ReverseDeps),
		view.Symbol.PackageImportance,
	))
}

func symbolViewRiskWithFileSummary(view storage.SymbolView, summary storage.FileSummary, hotScore int, recentChanged bool) string {
	parts := symbolRiskNotes(
		len(view.Callers),
		len(view.Callees),
		len(view.ReferencesIn),
		len(view.ReferencesOut),
		len(view.Tests),
		len(view.Package.ReverseDeps),
		view.Symbol.PackageImportance,
	)
	if fileRisk := fileRiskSummary(summary, hotScore, recentChanged); fileRisk != "contained" {
		parts = append(parts, "file="+strings.ReplaceAll(fileRisk, " | ", ", "))
	}
	return joinCompactRisk(parts)
}

func rankedSymbolRiskSummary(value storage.RankedSymbol) string {
	parts := []string{}
	impact := impactLabel(value.CallerCount, value.ReferenceCount, value.TestCount, value.ReversePackageDeps)
	if impact != "low" {
		parts = append(parts, "blast="+impact)
	}
	if value.CallerCount > 0 && value.CalleeCount > 0 {
		parts = append(parts, "seam")
	}
	if value.Score >= 18 {
		parts = append(parts, "hotspot")
	}
	if value.TestCount == 0 && value.CallerCount+value.ReferenceCount+value.ReversePackageDeps >= 3 {
		parts = append(parts, "tests=thin")
	}
	if value.ReversePackageDeps >= 3 {
		parts = append(parts, fmt.Sprintf("pkg-rdeps=%d", value.ReversePackageDeps))
	}
	if value.MethodCount >= 3 {
		parts = append(parts, "hub-type")
	}
	return joinCompactRisk(parts)
}

func rankedPackageRiskSummary(value storage.RankedPackage) string {
	return joinCompactRisk(packageRiskNotes(
		value.Summary.FileCount,
		value.Summary.SymbolCount,
		value.Summary.TestCount,
		value.LocalDepCount,
		value.ReverseDepCount,
	))
}

func packageSummaryRiskSummary(summary storage.PackageSummary) string {
	return joinCompactRisk(packageRiskNotes(
		summary.FileCount,
		summary.SymbolCount,
		summary.TestCount,
		len(summary.LocalDeps),
		len(summary.ReverseDeps),
	))
}

func impactRiskSummary(callers, refs, tests, reverseDeps int) string {
	return joinCompactRisk(symbolRiskNotes(callers, 0, refs, 0, tests, reverseDeps, 0))
}

func fileRiskSummary(summary storage.FileSummary, hotScore int, recentChanged bool) string {
	return joinCompactRisk(fileRiskNotes(summary, hotScore, recentChanged))
}

func symbolRiskNotes(callers, callees, refsIn, refsOut, tests, reverseDeps, packageImportance int) []string {
	parts := []string{}
	impact := impactLabel(callers, refsIn, tests, reverseDeps)
	if impact != "low" {
		parts = append(parts, "blast="+impact)
	}
	if (callers > 0 && callees > 0) || (refsIn > 0 && refsOut > 0) {
		parts = append(parts, "seam")
	}
	if reverseDeps >= 3 {
		parts = append(parts, fmt.Sprintf("pkg-rdeps=%d", reverseDeps))
	}
	if tests == 0 && callers+refsIn+reverseDeps >= 3 {
		parts = append(parts, "tests=thin")
	}
	if packageImportance >= 6 || callers+refsIn+reverseDeps >= 8 {
		parts = append(parts, "hotspot")
	}
	return parts
}

func packageRiskNotes(files, symbols, tests, localDeps, reverseDeps int) []string {
	parts := []string{}
	if reverseDeps >= 3 {
		parts = append(parts, fmt.Sprintf("rdeps=%d", reverseDeps))
	}
	if localDeps > 0 && reverseDeps > 0 {
		parts = append(parts, "seam")
	}
	if tests == 0 && (reverseDeps >= 2 || symbols >= 6) {
		parts = append(parts, "tests=thin")
	}
	if files >= 5 || symbols >= 10 {
		parts = append(parts, "wide-surface")
	}
	return parts
}

func fileRiskNotes(summary storage.FileSummary, hotScore int, recentChanged bool) []string {
	parts := []string{}
	weakTests := false
	if summary.RelevantSymbolCount > 0 {
		coverage := coveragePercent(summary.TestLinkedSymbolCount, summary.RelevantSymbolCount)
		weakTests = summary.RelatedTestCount == 0 || coverage < 35
	}

	switch {
	case hotScore >= 36:
		parts = append(parts, "hotspot")
	case hotScore >= 18 || summary.RelevantSymbolCount >= 5:
		parts = append(parts, "dense-logic")
	}

	switch {
	case recentChanged && weakTests:
		parts = append(parts, "recent+weak-tests")
	case recentChanged:
		parts = append(parts, "recent")
	case weakTests:
		parts = append(parts, "weak-test-link")
	}
	return parts
}

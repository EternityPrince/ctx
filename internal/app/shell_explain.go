package app

import (
	"fmt"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

func (s *shellSession) renderShellExplain(section explainSection) error {
	return renderHumanExplainSection(s.stdout, s.palette, section)
}

func (s *shellSession) buildShellFileExplain(summary storage.FileSummary, focusView *storage.SymbolView, riskSummary string, hotspots []string, focusLabel string) explainSection {
	packageName := shortenQName(s.info.ModulePath, summary.PackageImportPath)
	if packageName == "" && focusView != nil {
		packageName = shortenQName(s.info.ModulePath, focusView.Symbol.PackageImportPath)
	}
	if packageName == "" {
		packageName = "unknown"
	}

	surface := reportImportance(summary.QualityScore)
	reach := fmt.Sprintf("reverse_deps=%d", summary.ReversePackageDeps)
	if focusView != nil {
		surface = impactLabel(len(focusView.Callers), len(focusView.ReferencesIn), len(focusView.Tests), len(focusView.Package.ReverseDeps))
		reach = fmt.Sprintf("local_deps=%d reverse_deps=%d", len(focusView.Package.LocalDeps), len(focusView.Package.ReverseDeps))
	}

	notes := append([]string{}, summary.QualityWhy...)
	if focusLabel != "" {
		notes = append(notes, "focus symbol: "+focusLabel)
	}
	if len(hotspots) > 0 {
		notes = append(notes, "hotspots: "+strings.Join(hotspots, ", "))
	}

	return explainSection{
		Title: "Explain",
		Facts: []explainFact{
			{Key: "Surface", Value: surface},
			{Key: "Package", Value: packageName},
			{Key: "Shape", Value: fmt.Sprintf("symbols=%d funcs=%d methods=%d types=%d", summary.SymbolCount, summary.FuncCount, summary.MethodCount, summary.StructCount)},
			{Key: "Test map", Value: fmt.Sprintf("declared=%d related=%d linked=%d/%d", summary.DeclaredTestCount, summary.RelatedTestCount, summary.TestLinkedSymbolCount, max(summary.RelevantSymbolCount, 0))},
			{Key: "Quality", Value: fmt.Sprintf("%s (score=%d)", reportImportance(summary.QualityScore), summary.QualityScore)},
			{Key: "Reach", Value: reach},
			{Key: "Risk", Value: riskSummary},
			{Key: "Footprint", Value: shellHumanSize(summary.SizeBytes)},
			{Key: "Precision", Value: "file importance is inferred from indexed symbols, graph signals, tests, and package reach"},
		},
		Notes: notes,
	}
}

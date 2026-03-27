package app

import (
	"fmt"
	"io"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

func renderHumanSymbolView(stdout io.Writer, projectRoot, modulePath string, view storage.SymbolView, guidance symbolTestGuidance, explain bool) error {
	p := newPalette()
	impact := impactLabel(len(view.Callers), len(view.ReferencesIn), len(view.Tests), len(view.Package.ReverseDeps))

	if _, err := fmt.Fprintf(
		stdout,
		"%s\n%s %s %s %s\n\n",
		p.rule("Symbol"),
		p.title("CTX Symbol"),
		p.kindBadge(view.Symbol.Kind),
		p.accent(shortenQName(modulePath, view.Symbol.QName)),
		p.badge(impact),
	); err != nil {
		return err
	}

	if err := renderHumanDeclaration(stdout, p, projectRoot, view.Symbol); err != nil {
		return err
	}
	if explain {
		if err := renderHumanExplainSection(stdout, p, buildSymbolExplain(view, guidance)); err != nil {
			return err
		}
	}
	if len(view.QualityWhy) > 0 {
		if _, err := fmt.Fprintf(stdout, "%s\n  %s %d\n  %s %s\n\n", p.section("Quality"), p.label("Score:"), view.QualityScore, p.label("Why:"), strings.Join(view.QualityWhy, "; ")); err != nil {
			return err
		}
	}
	if err := renderHumanSource(stdout, p, projectRoot, view.Symbol.FilePath, view.Symbol.Line); err != nil {
		return err
	}
	if err := renderHumanPackageSummary(stdout, p, modulePath, view.Package); err != nil {
		return err
	}
	if err := renderHumanRelatedSymbols(stdout, p, projectRoot, modulePath, "Direct Callers", view.Callers, 6, true); err != nil {
		return err
	}
	if err := renderHumanRelatedSymbols(stdout, p, projectRoot, modulePath, "Direct Callees", view.Callees, 8, true); err != nil {
		return err
	}
	if err := renderHumanReferences(stdout, p, projectRoot, modulePath, "References In", view.ReferencesIn, 6); err != nil {
		return err
	}
	if err := renderHumanReferences(stdout, p, projectRoot, modulePath, "References Out", view.ReferencesOut, 8); err != nil {
		return err
	}
	if err := renderHumanTestGuidance(stdout, p, projectRoot, guidance, 8); err != nil {
		return err
	}
	if err := renderHumanSiblingSymbols(stdout, p, projectRoot, modulePath, "Related Symbols", view.Siblings, 8); err != nil {
		return err
	}

	return renderHumanImpactSummary(
		stdout,
		p,
		impact,
		len(view.Callers),
		len(view.ReferencesIn),
		len(view.Tests),
		len(view.Package.ReverseDeps),
	)
}

func renderAISymbolView(stdout io.Writer, modulePath string, view storage.SymbolView, guidance symbolTestGuidance, explain bool) error {
	if _, err := fmt.Fprintf(
		stdout,
		"symbol q=%s kind=%s file=%s:%d package=%s impact=%s quality_score=%d callers=%d refs_in=%d refs_out=%d tests=%d rdeps=%d test_signal=%q\n",
		shortenQName(modulePath, view.Symbol.QName),
		view.Symbol.Kind,
		view.Symbol.FilePath,
		view.Symbol.Line,
		shortenQName(modulePath, view.Symbol.PackageImportPath),
		impactLabel(len(view.Callers), len(view.ReferencesIn), len(view.Tests), len(view.Package.ReverseDeps)),
		view.QualityScore,
		len(view.Callers),
		len(view.ReferencesIn),
		len(view.ReferencesOut),
		len(view.Tests),
		len(view.Package.ReverseDeps),
		guidance.Signal,
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "sig=%q\n", displaySignature(view.Symbol)); err != nil {
		return err
	}
	if view.Symbol.Receiver != "" {
		if _, err := fmt.Fprintf(stdout, "receiver=%q\n", view.Symbol.Receiver); err != nil {
			return err
		}
	}
	if view.Symbol.Doc != "" {
		if _, err := fmt.Fprintf(stdout, "doc=%q\n", oneLine(view.Symbol.Doc)); err != nil {
			return err
		}
	}
	if len(view.QualityWhy) > 0 {
		if _, err := fmt.Fprintf(stdout, "quality_why=%q\n", strings.Join(view.QualityWhy, " | ")); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(
		stdout,
		"pkg files=%d symbols=%d tests=%d deps=%d rdeps=%d\n",
		view.Package.FileCount,
		view.Package.SymbolCount,
		view.Package.TestCount,
		len(view.Package.LocalDeps),
		len(view.Package.ReverseDeps),
	); err != nil {
		return err
	}
	if explain {
		if err := renderAIExplainSection(stdout, "explain", buildSymbolExplain(view, guidance)); err != nil {
			return err
		}
	}
	if err := renderAIRelatedSymbols(stdout, modulePath, "callers", view.Callers, 6, true); err != nil {
		return err
	}
	if err := renderAIRelatedSymbols(stdout, modulePath, "callees", view.Callees, 8, true); err != nil {
		return err
	}
	if err := renderAIReferences(stdout, modulePath, "refs_in", view.ReferencesIn, 6); err != nil {
		return err
	}
	if err := renderAIReferences(stdout, modulePath, "refs_out", view.ReferencesOut, 8); err != nil {
		return err
	}
	if err := renderAITests(stdout, "tests", guidance.ReadBefore, 8); err != nil {
		return err
	}
	return renderAISiblings(stdout, modulePath, "related", view.Siblings, 8)
}

func renderHumanImpactView(stdout io.Writer, projectRoot, modulePath string, view storage.ImpactView, guidance symbolTestGuidance, depth int, explain bool) error {
	p := newPalette()
	impact := impactLabel(len(view.DirectCallers), len(view.TransitiveCallers), len(view.Tests), len(view.Package.ReverseDeps))

	if _, err := fmt.Fprintf(
		stdout,
		"%s\n%s %s %s %s\n\n",
		p.rule("Impact"),
		p.title("CTX Impact"),
		p.kindBadge(view.Target.Kind),
		p.accent(shortenQName(modulePath, view.Target.QName)),
		p.badge(impact),
	); err != nil {
		return err
	}

	if err := renderHumanDeclaration(stdout, p, projectRoot, view.Target); err != nil {
		return err
	}
	if explain {
		if err := renderHumanExplainSection(stdout, p, buildImpactExplain(modulePath, view, guidance, depth)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(
		stdout,
		"%s\n  %s %s\n  %s %d\n  %s %d\n  %s %d\n  %s %d\n  %s %d\n  %s %d\n  %s %d\n  %s %d\n  %s %d\n  %s %s\n  %s static graph plus co-change heuristics; dynamic dispatch and non-indexed runtime paths may be missing\n\n",
		p.section("Impact Summary"),
		p.label("Surface:"),
		p.badge(impact),
		p.label("Direct callers:"),
		len(view.DirectCallers),
		p.label("Transitive callers:"),
		len(view.TransitiveCallers),
		p.label("Inbound refs:"),
		len(view.InboundRefs),
		p.label("Caller packages:"),
		len(view.CallerPackages),
		p.label("Reference packages:"),
		len(view.ReferencePackages),
		p.label("Blast packages:"),
		len(view.BlastPackages),
		p.label("Blast files:"),
		len(view.BlastFiles),
		p.label("Related tests:"),
		len(view.Tests),
		p.label("Reverse package deps:"),
		len(view.Package.ReverseDeps),
		p.label("Test signal:"),
		guidance.Signal,
		p.label("Precision:"),
	); err != nil {
		return err
	}
	if view.HasRecentDelta {
		if err := renderHumanRecentImpactDelta(stdout, p, modulePath, view.RecentDelta); err != nil {
			return err
		}
	}

	if err := renderHumanRelatedSymbols(stdout, p, projectRoot, modulePath, "Direct Callers", view.DirectCallers, 8, true); err != nil {
		return err
	}
	if err := renderHumanImpactNodes(stdout, p, projectRoot, modulePath, fmt.Sprintf("Transitive Callers (depth<=%d)", depth), view.TransitiveCallers, 10); err != nil {
		return err
	}
	if err := renderHumanReferences(stdout, p, projectRoot, modulePath, "Inbound References", view.InboundRefs, 8); err != nil {
		return err
	}
	if len(view.CallerPackageReasons) > 0 {
		if err := renderHumanPackageReasons(stdout, p, modulePath, "Caller Packages", view.CallerPackageReasons, 12); err != nil {
			return err
		}
	} else if err := renderHumanStringList(stdout, p, "Caller Packages", shortenValues(modulePath, view.CallerPackages), 12); err != nil {
		return err
	}
	if len(view.ReferencePackageReasons) > 0 {
		if err := renderHumanPackageReasons(stdout, p, modulePath, "Reference Packages", view.ReferencePackageReasons, 12); err != nil {
			return err
		}
	} else if err := renderHumanStringList(stdout, p, "Reference Packages", shortenValues(modulePath, view.ReferencePackages), 12); err != nil {
		return err
	}
	if len(view.BlastPackageReasons) > 0 {
		if err := renderHumanPackageReasons(stdout, p, modulePath, "Blast Packages", view.BlastPackageReasons, 16); err != nil {
			return err
		}
	} else if err := renderHumanStringList(stdout, p, "Blast Packages", shortenValues(modulePath, view.BlastPackages), 16); err != nil {
		return err
	}
	if len(view.BlastFileReasons) > 0 {
		if err := renderHumanFileReasons(stdout, p, "Blast Files", view.BlastFileReasons, 16); err != nil {
			return err
		}
	} else if err := renderHumanStringList(stdout, p, "Blast Files", view.BlastFiles, 16); err != nil {
		return err
	}
	if err := renderHumanCoChangeItems(stdout, p, "Empirical Packages", view.EmpiricalPackages, 8); err != nil {
		return err
	}
	if err := renderHumanCoChangeItems(stdout, p, "Empirical Files", view.EmpiricalFiles, 8); err != nil {
		return err
	}
	if err := renderHumanTestGuidance(stdout, p, projectRoot, guidance, 8); err != nil {
		return err
	}
	return renderHumanPackageSummary(stdout, p, modulePath, view.Package)
}

func renderAIImpactView(stdout io.Writer, modulePath string, view storage.ImpactView, guidance symbolTestGuidance, depth int, explain bool) error {
	if _, err := fmt.Fprintf(
		stdout,
		"impact q=%s kind=%s depth=%d surface=%s direct_callers=%d transitive=%d refs_in=%d caller_packages=%d ref_packages=%d blast_packages=%d blast_files=%d tests=%d rdeps=%d test_signal=%q\n",
		shortenQName(modulePath, view.Target.QName),
		view.Target.Kind,
		depth,
		impactLabel(len(view.DirectCallers), len(view.TransitiveCallers), len(view.Tests), len(view.Package.ReverseDeps)),
		len(view.DirectCallers),
		len(view.TransitiveCallers),
		len(view.InboundRefs),
		len(view.CallerPackages),
		len(view.ReferencePackages),
		len(view.BlastPackages),
		len(view.BlastFiles),
		len(view.Tests),
		len(view.Package.ReverseDeps),
		guidance.Signal,
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "sig=%q\n", displaySignature(view.Target)); err != nil {
		return err
	}
	if explain {
		if err := renderAIExplainSection(stdout, "explain", buildImpactExplain(modulePath, view, guidance, depth)); err != nil {
			return err
		}
	} else {
		if view.HasRecentDelta {
			if _, err := fmt.Fprintf(stdout, "recent_delta=%q\n", formatSymbolImpactDelta(modulePath, view.RecentDelta)); err != nil {
				return err
			}
		}
		if len(view.ExpansionWhy) > 0 {
			if _, err := fmt.Fprintf(stdout, "expansion_why=%q\n", strings.Join(view.ExpansionWhy, " | ")); err != nil {
				return err
			}
		}
	}
	if err := renderAIRelatedSymbols(stdout, modulePath, "direct_callers", view.DirectCallers, 8, true); err != nil {
		return err
	}
	if err := renderAIImpactNodes(stdout, modulePath, "transitive_callers", view.TransitiveCallers, 10); err != nil {
		return err
	}
	if err := renderAIReferences(stdout, modulePath, "refs_in", view.InboundRefs, 8); err != nil {
		return err
	}
	if err := renderAIStringList(stdout, "caller_packages", shortenValues(modulePath, view.CallerPackages), 12); err != nil {
		return err
	}
	if err := renderAIPackageReasons(stdout, modulePath, "caller_package_reasons", view.CallerPackageReasons, 12); err != nil {
		return err
	}
	if err := renderAIStringList(stdout, "reference_packages", shortenValues(modulePath, view.ReferencePackages), 12); err != nil {
		return err
	}
	if err := renderAIPackageReasons(stdout, modulePath, "reference_package_reasons", view.ReferencePackageReasons, 12); err != nil {
		return err
	}
	if err := renderAIStringList(stdout, "blast_packages", shortenValues(modulePath, view.BlastPackages), 16); err != nil {
		return err
	}
	if err := renderAIPackageReasons(stdout, modulePath, "blast_package_reasons", view.BlastPackageReasons, 16); err != nil {
		return err
	}
	if err := renderAIStringList(stdout, "blast_files", view.BlastFiles, 16); err != nil {
		return err
	}
	if err := renderAIFileReasons(stdout, "blast_file_reasons", view.BlastFileReasons, 16); err != nil {
		return err
	}
	if err := renderAICoChangeItems(stdout, "empirical_packages", view.EmpiricalPackages, 8); err != nil {
		return err
	}
	if err := renderAICoChangeItems(stdout, "empirical_files", view.EmpiricalFiles, 8); err != nil {
		return err
	}
	if err := renderAITests(stdout, "tests", guidance.ReadBefore, 8); err != nil {
		return err
	}
	return nil
}

func buildSymbolExplain(view storage.SymbolView, guidance symbolTestGuidance) explainSection {
	notes := append([]string{}, view.QualityWhy...)
	notes = append(notes, "static graph plus indexed refs/tests; dynamic dispatch and runtime-only edges may be missing")
	return explainSection{
		Title: "Explain",
		Facts: []explainFact{
			{Key: "Surface", Value: impactLabel(len(view.Callers), len(view.ReferencesIn), len(view.Tests), len(view.Package.ReverseDeps))},
			{Key: "Quality score", Value: fmt.Sprintf("%d", view.QualityScore)},
			{Key: "Test signal", Value: guidance.Signal},
			{Key: "Precision", Value: "call/ref/test evidence comes from indexed graph and source-level heuristics"},
		},
		Notes: notes,
	}
}

func buildImpactExplain(modulePath string, view storage.ImpactView, guidance symbolTestGuidance, depth int) explainSection {
	notes := append([]string{}, view.ExpansionWhy...)
	notes = append(notes, fmt.Sprintf("transitive callers are expanded up to depth=%d", depth))
	return explainSection{
		Title: "Explain",
		Facts: []explainFact{
			{Key: "Surface", Value: impactLabel(len(view.DirectCallers), len(view.TransitiveCallers), len(view.Tests), len(view.Package.ReverseDeps))},
			{Key: "Blast radius", Value: fmt.Sprintf("packages=%d files=%d tests=%d", len(view.BlastPackages), len(view.BlastFiles), len(view.Tests))},
			{Key: "Recent delta", Value: blankIf(formatImpactRecentDeltaMaybe(modulePath, view), "none")},
			{Key: "Test signal", Value: guidance.Signal},
			{Key: "Precision", Value: "static graph + co-change + recent symbol delta; runtime-only dispatch may be missing"},
		},
		Notes: notes,
	}
}

func formatImpactRecentDeltaMaybe(modulePath string, view storage.ImpactView) string {
	if !view.HasRecentDelta {
		return ""
	}
	return formatSymbolImpactDelta(modulePath, view.RecentDelta)
}

func formatCoChangeItem(value storage.CoChangeItem) string {
	label := strings.TrimSpace(value.Label)
	switch {
	case value.Count > 0 && value.Frequency > 0:
		return fmt.Sprintf("%s (count=%d freq=%.2f)", label, value.Count, value.Frequency)
	case value.Count > 0:
		return fmt.Sprintf("%s (count=%d)", label, value.Count)
	case value.Frequency > 0:
		return fmt.Sprintf("%s (freq=%.2f)", label, value.Frequency)
	default:
		return label
	}
}

func renderHumanRecentImpactDelta(stdout io.Writer, p palette, modulePath string, delta storage.SymbolImpactDelta) error {
	if _, err := fmt.Fprintf(stdout, "%s\n  %s %s\n", p.section("Recent Change"), p.label("Delta:"), formatSymbolImpactDelta(modulePath, delta)); err != nil {
		return err
	}
	for _, why := range delta.Why {
		if _, err := fmt.Fprintf(stdout, "  %s %s\n", p.label("why:"), why); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

func formatSymbolImpactDelta(modulePath string, delta storage.SymbolImpactDelta) string {
	parts := make([]string, 0, 10)
	if delta.Status != "" {
		parts = append(parts, delta.Status)
	}
	if delta.ContractChanged {
		parts = append(parts, "contract")
	}
	if delta.Moved {
		parts = append(parts, "moved")
	}
	if delta.AddedCallers+delta.RemovedCallers > 0 {
		parts = append(parts, fmt.Sprintf("callers +%d/-%d", delta.AddedCallers, delta.RemovedCallers))
	}
	if delta.AddedCallees+delta.RemovedCallees > 0 {
		parts = append(parts, fmt.Sprintf("callees +%d/-%d", delta.AddedCallees, delta.RemovedCallees))
	}
	if delta.AddedRefsIn+delta.RemovedRefsIn > 0 {
		parts = append(parts, fmt.Sprintf("refs_in +%d/-%d", delta.AddedRefsIn, delta.RemovedRefsIn))
	}
	if delta.AddedRefsOut+delta.RemovedRefsOut > 0 {
		parts = append(parts, fmt.Sprintf("refs_out +%d/-%d", delta.AddedRefsOut, delta.RemovedRefsOut))
	}
	if delta.AddedTests+delta.RemovedTests > 0 {
		parts = append(parts, fmt.Sprintf("tests +%d/-%d", delta.AddedTests, delta.RemovedTests))
	}
	if delta.BlastRadius > 0 {
		parts = append(parts, fmt.Sprintf("blast=%d", delta.BlastRadius))
	}
	if len(parts) == 0 {
		return shortenQName(modulePath, delta.QName)
	}
	return shortenQName(modulePath, delta.QName) + " [" + strings.Join(parts, ", ") + "]"
}

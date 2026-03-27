package app

import (
	"fmt"
	"io"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

func renderHumanSymbolView(stdout io.Writer, projectRoot, modulePath string, view storage.SymbolView, guidance symbolTestGuidance) error {
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

func renderAISymbolView(stdout io.Writer, modulePath string, view storage.SymbolView, guidance symbolTestGuidance) error {
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

func renderHumanImpactView(stdout io.Writer, projectRoot, modulePath string, view storage.ImpactView, guidance symbolTestGuidance, depth int) error {
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
	if _, err := fmt.Fprintf(
		stdout,
		"%s\n  %s %s\n  %s %d\n  %s %d\n  %s %d\n  %s %d\n  %s %d\n  %s %s\n  %s static graph with heuristics; dynamic dispatch paths may be missing\n\n",
		p.section("Impact Summary"),
		p.label("Surface:"),
		p.badge(impact),
		p.label("Direct callers:"),
		len(view.DirectCallers),
		p.label("Transitive callers:"),
		len(view.TransitiveCallers),
		p.label("Caller packages:"),
		len(view.CallerPackages),
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

	if err := renderHumanRelatedSymbols(stdout, p, projectRoot, modulePath, "Direct Callers", view.DirectCallers, 8, true); err != nil {
		return err
	}
	if err := renderHumanImpactNodes(stdout, p, projectRoot, modulePath, fmt.Sprintf("Transitive Callers (depth<=%d)", depth), view.TransitiveCallers, 10); err != nil {
		return err
	}
	if err := renderHumanStringList(stdout, p, "Caller Packages", shortenValues(modulePath, view.CallerPackages), 12); err != nil {
		return err
	}
	if err := renderHumanTestGuidance(stdout, p, projectRoot, guidance, 8); err != nil {
		return err
	}
	return renderHumanPackageSummary(stdout, p, modulePath, view.Package)
}

func renderAIImpactView(stdout io.Writer, modulePath string, view storage.ImpactView, guidance symbolTestGuidance, depth int) error {
	if _, err := fmt.Fprintf(
		stdout,
		"impact q=%s kind=%s depth=%d surface=%s direct_callers=%d transitive=%d caller_packages=%d tests=%d rdeps=%d test_signal=%q\n",
		shortenQName(modulePath, view.Target.QName),
		view.Target.Kind,
		depth,
		impactLabel(len(view.DirectCallers), len(view.TransitiveCallers), len(view.Tests), len(view.Package.ReverseDeps)),
		len(view.DirectCallers),
		len(view.TransitiveCallers),
		len(view.CallerPackages),
		len(view.Tests),
		len(view.Package.ReverseDeps),
		guidance.Signal,
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "sig=%q\n", displaySignature(view.Target)); err != nil {
		return err
	}
	if err := renderAIRelatedSymbols(stdout, modulePath, "direct_callers", view.DirectCallers, 8, true); err != nil {
		return err
	}
	if err := renderAIImpactNodes(stdout, modulePath, "transitive_callers", view.TransitiveCallers, 10); err != nil {
		return err
	}
	if err := renderAIStringList(stdout, "caller_packages", shortenValues(modulePath, view.CallerPackages), 12); err != nil {
		return err
	}
	if err := renderAITests(stdout, "tests", guidance.ReadBefore, 8); err != nil {
		return err
	}
	return nil
}

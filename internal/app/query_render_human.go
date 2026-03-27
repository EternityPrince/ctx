package app

import (
	"fmt"
	"io"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

func renderHumanPackages(stdout io.Writer, p palette, modulePath, title string, values []storage.RankedPackage, explain bool) error {
	if _, err := fmt.Fprintf(stdout, "%s\n", p.section(title)); err != nil {
		return err
	}
	if len(values) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(
			stdout,
			"  %s %s\n    %s files=%d symbols=%d tests=%d deps=%d rdeps=%d score=%d\n    %s %s\n",
			p.badge(reportImportance(value.Score)),
			shortenQName(modulePath, value.Summary.ImportPath),
			p.label("metrics:"),
			value.Summary.FileCount,
			value.Summary.SymbolCount,
			value.Summary.TestCount,
			value.LocalDepCount,
			value.ReverseDepCount,
			value.Score,
			p.label("risk:"),
			rankedPackageRiskSummary(value),
		); err != nil {
			return err
		}
		if explain {
			for _, why := range value.QualityWhy {
				if _, err := fmt.Fprintf(stdout, "    %s %s\n", p.label("quality:"), why); err != nil {
					return err
				}
			}
			if err := renderHumanProvenance(stdout, p, modulePath, value.Provenance); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

func renderHumanRankedSymbols(stdout io.Writer, p palette, modulePath, title string, values []storage.RankedSymbol, explain bool) error {
	if _, err := fmt.Fprintf(stdout, "%s\n", p.section(title)); err != nil {
		return err
	}
	if len(values) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(
			stdout,
			"  %s %s %s\n    %s\n    %s %s:%d\n    %s callers=%d callees=%d refs=%d tests=%d methods=%d rdeps=%d score=%d\n    %s %s\n",
			p.badge(reportImportance(value.Score)),
			p.kindBadge(value.Symbol.Kind),
			shortenQName(modulePath, value.Symbol.QName),
			styleHumanSignature(p, displaySignature(value.Symbol)),
			p.label("declared:"),
			value.Symbol.FilePath,
			value.Symbol.Line,
			p.label("metrics:"),
			value.CallerCount,
			value.CalleeCount,
			value.ReferenceCount,
			value.TestCount,
			value.MethodCount,
			value.ReversePackageDeps,
			value.Score,
			p.label("risk:"),
			rankedSymbolRiskSummary(value),
		); err != nil {
			return err
		}
		if explain {
			for _, why := range value.QualityWhy {
				if _, err := fmt.Fprintf(stdout, "    %s %s\n", p.label("quality:"), why); err != nil {
					return err
				}
			}
			if err := renderHumanProvenance(stdout, p, modulePath, value.Provenance); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

func renderHumanRankedFiles(stdout io.Writer, p palette, title string, values []storage.RankedFile, explain bool) error {
	if _, err := fmt.Fprintf(stdout, "%s\n", p.section(title)); err != nil {
		return err
	}
	if len(values) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, value := range values {
		symbols := strings.Join(limitStrings(value.TopSymbols, 4), ", ")
		if symbols == "" {
			symbols = "none"
		}
		if _, err := fmt.Fprintf(
			stdout,
			"  %s %s\n    %s calls=%d refs=%d tests=%d rdeps=%d relevant=%d score=%d\n    %s symbols=%s\n",
			p.badge(reportImportance(value.Score)),
			value.Summary.FilePath,
			p.label("metrics:"),
			value.Summary.InboundCallCount,
			value.Summary.InboundReferenceCount,
			value.Summary.RelatedTestCount,
			value.Summary.ReversePackageDeps,
			value.Summary.RelevantSymbolCount,
			value.Score,
			p.label("surface:"),
			symbols,
		); err != nil {
			return err
		}
		if explain {
			for _, why := range value.QualityWhy {
				if _, err := fmt.Fprintf(stdout, "    %s %s\n", p.label("why:"), why); err != nil {
					return err
				}
			}
		}
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

func renderHumanDeclaration(stdout io.Writer, p palette, projectRoot string, symbol storage.SymbolMatch) error {
	if _, err := fmt.Fprintf(stdout, "%s\n", p.section("Declaration")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		stdout,
		"  %s %s\n  %s %s\n  %s %s\n",
		p.label("Signature:"),
		styleHumanSignature(p, displaySignature(symbol)),
		p.label("Package:"),
		symbol.PackageImportPath,
		p.label("File:"),
		symbolRangeDisplay(projectRoot, symbol),
	); err != nil {
		return err
	}
	if symbol.Kind != "" {
		if _, err := fmt.Fprintf(stdout, "  %s %s\n", p.label("Kind:"), symbol.Kind); err != nil {
			return err
		}
	}
	if symbol.Receiver != "" {
		if _, err := fmt.Fprintf(stdout, "  %s %s\n", p.label("Receiver:"), symbol.Receiver); err != nil {
			return err
		}
	}
	if symbol.Doc != "" {
		if _, err := fmt.Fprintf(stdout, "  %s %s\n", p.label("Doc:"), oneLine(symbol.Doc)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

func renderHumanSource(stdout io.Writer, p palette, projectRoot, relPath string, line int) error {
	excerpt, err := readSourceExcerpt(projectRoot, relPath, line, 2, 5)
	if err != nil || excerpt == "" {
		return nil
	}
	if _, err := fmt.Fprintf(stdout, "%s\n%s\n\n", p.section("Source"), excerpt); err != nil {
		return err
	}
	return nil
}

func renderHumanPackageSummary(stdout io.Writer, p palette, modulePath string, summary storage.PackageSummary) error {
	if _, err := fmt.Fprintf(
		stdout,
		"%s\n  %s %s\n  %s %s\n  %s files=%d symbols=%d tests=%d\n",
		p.section("Package Context"),
		p.label("Package:"),
		shortenQName(modulePath, summary.ImportPath),
		p.label("Dir:"),
		summary.DirPath,
		p.label("Inventory:"),
		summary.FileCount,
		summary.SymbolCount,
		summary.TestCount,
	); err != nil {
		return err
	}
	if len(summary.LocalDeps) > 0 {
		if _, err := fmt.Fprintf(stdout, "  %s %s\n", p.label("Local deps:"), strings.Join(shortenValues(modulePath, limitStrings(summary.LocalDeps, 6)), ", ")); err != nil {
			return err
		}
	}
	if len(summary.ReverseDeps) > 0 {
		if _, err := fmt.Fprintf(stdout, "  %s %s\n", p.label("Reverse deps:"), strings.Join(shortenValues(modulePath, limitStrings(summary.ReverseDeps, 6)), ", ")); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(stdout, "  %s %s\n", p.label("Risk:"), packageSummaryRiskSummary(summary)); err != nil {
		return err
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

func renderHumanRelatedSymbols(stdout io.Writer, p palette, projectRoot, modulePath, title string, values []storage.RelatedSymbolView, limit int, showUseSite bool) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d)\n", p.section(title), len(values)); err != nil {
		return err
	}
	if len(values) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, value := range values[:min(limit, len(values))] {
		if _, err := fmt.Fprintf(stdout, "  %s %s\n", p.kindBadge(value.Symbol.Kind), shortenQName(modulePath, value.Symbol.QName)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(stdout, "    %s\n", styleHumanSignature(p, displaySignature(value.Symbol))); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(stdout, "    %s %s\n", p.label("declared:"), symbolRangeDisplay(projectRoot, value.Symbol)); err != nil {
			return err
		}
		if showUseSite {
			if _, err := fmt.Fprintf(stdout, "    %s %s:%d", p.label("use:"), value.UseFilePath, value.UseLine); err != nil {
				return err
			}
			if value.Relation != "" {
				if _, err := fmt.Fprintf(stdout, " [%s]", value.Relation); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(stdout); err != nil {
				return err
			}
			if snippet := sourceLineSnippet(projectRoot, value.UseFilePath, value.UseLine); snippet != "" {
				if _, err := fmt.Fprintf(stdout, "    %s %s\n", p.label("context:"), snippet); err != nil {
					return err
				}
			}
			if value.Why != "" {
				if _, err := fmt.Fprintf(stdout, "    %s %s\n", p.label("why:"), value.Why); err != nil {
					return err
				}
			}
		}
	}
	return renderMoreLine(stdout, len(values), limit)
}

func renderHumanReferences(stdout io.Writer, p palette, projectRoot, modulePath, title string, values []storage.RefView, limit int) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d)\n", p.section(title), len(values)); err != nil {
		return err
	}
	if len(values) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, value := range values[:min(limit, len(values))] {
		if _, err := fmt.Fprintf(stdout, "  %s %s\n", p.kindBadge(value.Symbol.Kind), shortenQName(modulePath, value.Symbol.QName)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(stdout, "    %s\n", styleHumanSignature(p, displaySignature(value.Symbol))); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(stdout, "    %s %s\n", p.label("declared:"), symbolRangeDisplay(projectRoot, value.Symbol)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(stdout, "    %s %s:%d [%s]\n", p.label("ref:"), value.UseFilePath, value.UseLine, value.Kind); err != nil {
			return err
		}
		if value.Why != "" {
			if _, err := fmt.Fprintf(stdout, "    %s %s\n", p.label("why:"), value.Why); err != nil {
				return err
			}
		}
		if snippet := sourceLineSnippet(projectRoot, value.UseFilePath, value.UseLine); snippet != "" {
			if _, err := fmt.Fprintf(stdout, "    %s %s\n", p.label("context:"), snippet); err != nil {
				return err
			}
		}
	}
	return renderMoreLine(stdout, len(values), limit)
}

func renderHumanTests(stdout io.Writer, p palette, projectRoot, title string, tests []storage.TestView, limit int) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d)\n", p.section(title), len(tests)); err != nil {
		return err
	}
	if len(tests) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, test := range tests[:min(limit, len(tests))] {
		if _, err := fmt.Fprintf(stdout, "  %s %s\n", p.kindBadge(test.Kind), test.Name); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(stdout, "    %s:%d %s\n", test.FilePath, test.Line, formatTestRelationLabel(test)); err != nil {
			return err
		}
		if test.Why != "" {
			if _, err := fmt.Fprintf(stdout, "    %s %s\n", p.label("why:"), test.Why); err != nil {
				return err
			}
		}
		if snippet := sourceLineSnippet(projectRoot, test.FilePath, test.Line); snippet != "" {
			if _, err := fmt.Fprintf(stdout, "    %s %s\n", p.label("context:"), snippet); err != nil {
				return err
			}
		}
	}
	return renderMoreLine(stdout, len(tests), limit)
}

func renderHumanTestGuidance(stdout io.Writer, p palette, projectRoot string, guidance symbolTestGuidance, limit int) error {
	if _, err := fmt.Fprintf(
		stdout,
		"%s\n  %s %s\n",
		p.section("Test Signal"),
		p.label("Coverage posture:"),
		guidance.Signal,
	); err != nil {
		return err
	}
	if guidance.ImportantWarning != "" {
		if _, err := fmt.Fprintf(stdout, "  %s %s\n", p.label("Warning:"), guidance.ImportantWarning); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(stdout); err != nil {
		return err
	}
	return renderHumanTests(stdout, p, projectRoot, "Tests To Read Before Change", guidance.ReadBefore, limit)
}

func renderHumanProvenance(stdout io.Writer, p palette, modulePath string, items []storage.ProvenanceItem) error {
	for _, item := range items {
		label := item.Label
		switch item.Kind {
		case "call", "ref", "method", "symbol", "reverse_dep":
			label = shortenQName(modulePath, item.Label)
		}
		if _, err := fmt.Fprintf(stdout, "    %s %s", p.label("why:"), item.Why); err != nil {
			return err
		}
		if label != "" {
			if _, err := fmt.Fprintf(stdout, " -> %s", label); err != nil {
				return err
			}
		}
		if item.FilePath != "" && item.Line > 0 {
			if _, err := fmt.Fprintf(stdout, " @ %s:%d", item.FilePath, item.Line); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(stdout); err != nil {
			return err
		}
	}
	return nil
}

func renderHumanSiblingSymbols(stdout io.Writer, p palette, projectRoot, modulePath, title string, symbols []storage.SymbolMatch, limit int) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d)\n", p.section(title), len(symbols)); err != nil {
		return err
	}
	if len(symbols) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, symbol := range symbols[:min(limit, len(symbols))] {
		if _, err := fmt.Fprintf(stdout, "  %s %s\n", p.kindBadge(symbol.Kind), shortenQName(modulePath, symbol.QName)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(stdout, "    %s\n", styleHumanSignature(p, displaySignature(symbol))); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(stdout, "    %s\n", symbolRangeDisplay(projectRoot, symbol)); err != nil {
			return err
		}
	}
	return renderMoreLine(stdout, len(symbols), limit)
}

func renderHumanImpactNodes(stdout io.Writer, p palette, projectRoot, modulePath, title string, values []storage.ImpactNode, limit int) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d)\n", p.section(title), len(values)); err != nil {
		return err
	}
	if len(values) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, value := range values[:min(limit, len(values))] {
		if _, err := fmt.Fprintf(stdout, "  %s %s\n", p.kindBadge(value.Symbol.Kind), shortenQName(modulePath, value.Symbol.QName)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(stdout, "    %s\n", styleHumanSignature(p, displaySignature(value.Symbol))); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(stdout, "    %s %s  %s %d\n", p.label("declared:"), symbolRangeDisplay(projectRoot, value.Symbol), p.label("depth:"), value.Depth); err != nil {
			return err
		}
	}
	return renderMoreLine(stdout, len(values), limit)
}

func renderHumanStringList(stdout io.Writer, p palette, title string, values []string, limit int) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d)\n", p.section(title), len(values)); err != nil {
		return err
	}
	if len(values) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, value := range values[:min(limit, len(values))] {
		if _, err := fmt.Fprintf(stdout, "  %s\n", value); err != nil {
			return err
		}
	}
	return renderMoreLine(stdout, len(values), limit)
}

func renderHumanPackageReasons(stdout io.Writer, p palette, modulePath, title string, values []storage.ImpactPackageReason, limit int) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d)\n", p.section(title), len(values)); err != nil {
		return err
	}
	if len(values) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, value := range values[:min(limit, len(values))] {
		if _, err := fmt.Fprintf(stdout, "  %s\n", shortenQName(modulePath, value.PackageImportPath)); err != nil {
			return err
		}
		for _, why := range value.Why {
			if _, err := fmt.Fprintf(stdout, "    %s %s\n", p.label("why:"), why); err != nil {
				return err
			}
		}
	}
	return renderMoreLine(stdout, len(values), limit)
}

func renderHumanFileReasons(stdout io.Writer, p palette, title string, values []storage.ImpactFileReason, limit int) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d)\n", p.section(title), len(values)); err != nil {
		return err
	}
	if len(values) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, value := range values[:min(limit, len(values))] {
		if _, err := fmt.Fprintf(stdout, "  %s\n", value.FilePath); err != nil {
			return err
		}
		for _, why := range value.Why {
			if _, err := fmt.Fprintf(stdout, "    %s %s\n", p.label("why:"), why); err != nil {
				return err
			}
		}
	}
	return renderMoreLine(stdout, len(values), limit)
}

func renderHumanCoChangeItems(stdout io.Writer, p palette, title string, values []storage.CoChangeItem, limit int) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d)\n", p.section(title), len(values)); err != nil {
		return err
	}
	if len(values) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, value := range values[:min(limit, len(values))] {
		if _, err := fmt.Fprintf(stdout, "  %s\n", formatCoChangeItem(value)); err != nil {
			return err
		}
	}
	return renderMoreLine(stdout, len(values), limit)
}

func renderHumanImpactSummary(stdout io.Writer, p palette, impact string, callers, refs, tests, reverseDeps int) error {
	if _, err := fmt.Fprintf(
		stdout,
		"%s\n  %s %s\n  %s %d\n  %s %d\n  %s %d\n  %s %d\n  %s %s\n\n",
		p.section("Impact Surface"),
		p.label("Surface:"),
		p.badge(impact),
		p.label("Direct callers:"),
		callers,
		p.label("Inbound refs:"),
		refs,
		p.label("Related tests:"),
		tests,
		p.label("Reverse package deps:"),
		reverseDeps,
		p.label("Risk:"),
		impactRiskSummary(callers, refs, tests, reverseDeps),
	); err != nil {
		return err
	}
	return nil
}

func renderHumanWeakChangedAreas(stdout io.Writer, p palette, modulePath, title string, values []weakChangedArea) error {
	if _, err := fmt.Fprintf(stdout, "%s\n", p.section(title)); err != nil {
		return err
	}
	if len(values) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, value := range values {
		coverage := "n/a"
		if value.CoveragePercent >= 0 {
			coverage = fmt.Sprintf("%d%%", value.CoveragePercent)
		}
		if _, err := fmt.Fprintf(
			stdout,
			"  %s\n    %s package=%s tests=%d linked=%d coverage=%s hot=%d\n",
			value.FilePath,
			p.label("signals:"),
			shortenQName(modulePath, value.PackageImportPath),
			value.RelatedTestCount,
			value.TestLinkedCount,
			coverage,
			value.HotScore,
		); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(stdout, "    %s %s\n", p.label("risk:"), value.Risk); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

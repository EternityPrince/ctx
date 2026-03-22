package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

func resolveSingleSymbolQuery(stdout io.Writer, modulePath string, store *storage.Store, query string) (storage.SymbolMatch, bool, error) {
	matches, err := store.FindSymbols(query)
	if err != nil {
		return storage.SymbolMatch{}, false, err
	}
	if len(matches) == 0 {
		_, err := fmt.Fprintf(stdout, "No symbol matches for %q\n", query)
		return storage.SymbolMatch{}, false, err
	}
	if len(matches) == 1 {
		return matches[0], true, nil
	}

	if _, err := fmt.Fprintf(stdout, "Ambiguous symbol query %q. Candidates:\n", query); err != nil {
		return storage.SymbolMatch{}, false, err
	}
	for _, match := range matches {
		if _, err := fmt.Fprintf(
			stdout,
			"  %s\n    %s\n    %s:%d\n",
			shortenQName(modulePath, match.QName),
			displaySignature(match),
			match.FilePath,
			match.Line,
		); err != nil {
			return storage.SymbolMatch{}, false, err
		}
	}
	return storage.SymbolMatch{}, false, nil
}

func renderHumanReport(stdout io.Writer, projectRoot, modulePath string, status storage.ProjectStatus, view storage.ReportView) error {
	p := newPalette()

	if _, err := fmt.Fprintf(stdout, "%s\n%s\n", p.rule("Project Report"), p.title("CTX Project Report")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		stdout,
		"%s %s\n%s %s\n%s %d (%s)\n%s packages=%d files=%d symbols=%d refs=%d calls=%d tests=%d\n%s %d\n%s callers + refs + tests + reverse deps\n\n",
		p.label("Root:"),
		projectRoot,
		p.label("Module:"),
		modulePath,
		p.label("Snapshot:"),
		view.Snapshot.ID,
		view.Snapshot.CreatedAt.Format(timeFormat),
		p.label("Inventory:"),
		view.Snapshot.TotalPackages,
		view.Snapshot.TotalFiles,
		view.Snapshot.TotalSymbols,
		view.Snapshot.TotalRefs,
		view.Snapshot.TotalCalls,
		view.Snapshot.TotalTests,
		p.label("Changed now:"),
		status.ChangedNow,
		p.label("Importance model:"),
	); err != nil {
		return err
	}

	if err := renderHumanPackages(stdout, p, modulePath, "Key Packages", view.TopPackages); err != nil {
		return err
	}
	if err := renderHumanRankedSymbols(stdout, p, modulePath, "Critical Functions and Methods", view.TopFunctions); err != nil {
		return err
	}
	if err := renderHumanRankedSymbols(stdout, p, modulePath, "Important Types", view.TopTypes); err != nil {
		return err
	}

	_, err := fmt.Fprintf(stdout, "%s `ctx symbol <name>` for a full graph slice.\n", p.label("Next step:"))
	return err
}

func renderAIReport(stdout io.Writer, modulePath string, status storage.ProjectStatus, view storage.ReportView) error {
	if _, err := fmt.Fprintf(
		stdout,
		"report module=%s snapshot=%d snapshot_at=%s changed_now=%d packages=%d files=%d symbols=%d refs=%d calls=%d tests=%d\n",
		modulePath,
		view.Snapshot.ID,
		view.Snapshot.CreatedAt.Format(timeFormat),
		status.ChangedNow,
		view.Snapshot.TotalPackages,
		view.Snapshot.TotalFiles,
		view.Snapshot.TotalSymbols,
		view.Snapshot.TotalRefs,
		view.Snapshot.TotalCalls,
		view.Snapshot.TotalTests,
	); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(stdout, "top_packages=%d\n", len(view.TopPackages)); err != nil {
		return err
	}
	for _, item := range view.TopPackages {
		if _, err := fmt.Fprintf(
			stdout,
			"pkg q=%s files=%d symbols=%d tests=%d deps=%d rdeps=%d score=%d importance=%s\n",
			shortenQName(modulePath, item.Summary.ImportPath),
			item.Summary.FileCount,
			item.Summary.SymbolCount,
			item.Summary.TestCount,
			item.LocalDepCount,
			item.ReverseDepCount,
			item.Score,
			reportImportance(item.Score),
		); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(stdout, "top_functions=%d\n", len(view.TopFunctions)); err != nil {
		return err
	}
	for _, item := range view.TopFunctions {
		if _, err := fmt.Fprintf(
			stdout,
			"fn q=%s sig=%q file=%s:%d callers=%d refs=%d tests=%d rdeps=%d score=%d importance=%s\n",
			shortenQName(modulePath, item.Symbol.QName),
			displaySignature(item.Symbol),
			item.Symbol.FilePath,
			item.Symbol.Line,
			item.CallerCount,
			item.ReferenceCount,
			item.TestCount,
			item.ReversePackageDeps,
			item.Score,
			reportImportance(item.Score),
		); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(stdout, "top_types=%d\n", len(view.TopTypes)); err != nil {
		return err
	}
	for _, item := range view.TopTypes {
		if _, err := fmt.Fprintf(
			stdout,
			"type q=%s kind=%s sig=%q file=%s:%d refs=%d tests=%d methods=%d rdeps=%d score=%d importance=%s\n",
			shortenQName(modulePath, item.Symbol.QName),
			item.Symbol.Kind,
			displaySignature(item.Symbol),
			item.Symbol.FilePath,
			item.Symbol.Line,
			item.ReferenceCount,
			item.TestCount,
			item.MethodCount,
			item.ReversePackageDeps,
			item.Score,
			reportImportance(item.Score),
		); err != nil {
			return err
		}
	}
	return nil
}

func renderHumanSymbolView(stdout io.Writer, projectRoot, modulePath string, view storage.SymbolView) error {
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
	if err := renderHumanTests(stdout, p, projectRoot, "Related Tests", view.Tests, 8); err != nil {
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

func renderAISymbolView(stdout io.Writer, modulePath string, view storage.SymbolView) error {
	if _, err := fmt.Fprintf(
		stdout,
		"symbol q=%s kind=%s file=%s:%d package=%s impact=%s callers=%d refs_in=%d refs_out=%d tests=%d rdeps=%d\n",
		shortenQName(modulePath, view.Symbol.QName),
		view.Symbol.Kind,
		view.Symbol.FilePath,
		view.Symbol.Line,
		shortenQName(modulePath, view.Symbol.PackageImportPath),
		impactLabel(len(view.Callers), len(view.ReferencesIn), len(view.Tests), len(view.Package.ReverseDeps)),
		len(view.Callers),
		len(view.ReferencesIn),
		len(view.ReferencesOut),
		len(view.Tests),
		len(view.Package.ReverseDeps),
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
	if err := renderAITests(stdout, "tests", view.Tests, 8); err != nil {
		return err
	}
	return renderAISiblings(stdout, modulePath, "related", view.Siblings, 8)
}

func renderHumanImpactView(stdout io.Writer, projectRoot, modulePath string, view storage.ImpactView, depth int) error {
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
		"%s\n  %s %s\n  %s %d\n  %s %d\n  %s %d\n  %s %d\n  %s %d\n  %s static graph only; interface/reflection paths may be missing\n\n",
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
	if err := renderHumanTests(stdout, p, projectRoot, "Related Tests", view.Tests, 8); err != nil {
		return err
	}
	return renderHumanPackageSummary(stdout, p, modulePath, view.Package)
}

func renderAIImpactView(stdout io.Writer, modulePath string, view storage.ImpactView, depth int) error {
	if _, err := fmt.Fprintf(
		stdout,
		"impact q=%s kind=%s depth=%d surface=%s direct_callers=%d transitive=%d caller_packages=%d tests=%d rdeps=%d\n",
		shortenQName(modulePath, view.Target.QName),
		view.Target.Kind,
		depth,
		impactLabel(len(view.DirectCallers), len(view.TransitiveCallers), len(view.Tests), len(view.Package.ReverseDeps)),
		len(view.DirectCallers),
		len(view.TransitiveCallers),
		len(view.CallerPackages),
		len(view.Tests),
		len(view.Package.ReverseDeps),
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
	if err := renderAITests(stdout, "tests", view.Tests, 8); err != nil {
		return err
	}
	return nil
}

func renderHumanPackages(stdout io.Writer, p palette, modulePath, title string, values []storage.RankedPackage) error {
	if _, err := fmt.Fprintf(stdout, "%s\n", p.section(title)); err != nil {
		return err
	}
	if len(values) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(
			stdout,
			"  %s %s\n    %s files=%d symbols=%d tests=%d deps=%d rdeps=%d score=%d\n",
			p.badge(reportImportance(value.Score)),
			shortenQName(modulePath, value.Summary.ImportPath),
			p.label("metrics:"),
			value.Summary.FileCount,
			value.Summary.SymbolCount,
			value.Summary.TestCount,
			value.LocalDepCount,
			value.ReverseDepCount,
			value.Score,
		); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

func renderHumanRankedSymbols(stdout io.Writer, p palette, modulePath, title string, values []storage.RankedSymbol) error {
	if _, err := fmt.Fprintf(stdout, "%s\n", p.section(title)); err != nil {
		return err
	}
	if len(values) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(
			stdout,
			"  %s %s %s\n    %s\n    %s %s:%d\n    %s callers=%d refs=%d tests=%d methods=%d rdeps=%d score=%d\n",
			p.badge(reportImportance(value.Score)),
			p.kindBadge(value.Symbol.Kind),
			shortenQName(modulePath, value.Symbol.QName),
			styleHumanSignature(p, displaySignature(value.Symbol)),
			p.label("declared:"),
			value.Symbol.FilePath,
			value.Symbol.Line,
			p.label("metrics:"),
			value.CallerCount,
			value.ReferenceCount,
			value.TestCount,
			value.MethodCount,
			value.ReversePackageDeps,
			value.Score,
		); err != nil {
			return err
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
		if _, err := fmt.Fprintf(stdout, "    %s:%d [%s/%s]\n", test.FilePath, test.Line, test.LinkKind, test.Confidence); err != nil {
			return err
		}
		if snippet := sourceLineSnippet(projectRoot, test.FilePath, test.Line); snippet != "" {
			if _, err := fmt.Fprintf(stdout, "    %s %s\n", p.label("context:"), snippet); err != nil {
				return err
			}
		}
	}
	return renderMoreLine(stdout, len(tests), limit)
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

func renderHumanImpactSummary(stdout io.Writer, p palette, impact string, callers, refs, tests, reverseDeps int) error {
	if _, err := fmt.Fprintf(
		stdout,
		"%s\n  %s %s\n  %s %d\n  %s %d\n  %s %d\n  %s %d\n\n",
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
	); err != nil {
		return err
	}
	return nil
}

func styleHumanSignature(p palette, value string) string {
	return p.wrap("1;37", value)
}

func sourceLineSnippet(projectRoot, relPath string, line int) string {
	excerpt, err := readSourceExcerpt(projectRoot, relPath, line, 0, 0)
	if err != nil || excerpt == "" {
		return ""
	}
	parts := strings.Split(strings.TrimSpace(excerpt), "|")
	if len(parts) == 0 {
		return strings.TrimSpace(excerpt)
	}
	return strings.TrimSpace(parts[len(parts)-1])
}

func renderAIRelatedSymbols(stdout io.Writer, modulePath, title string, values []storage.RelatedSymbolView, limit int, showUseSite bool) error {
	if _, err := fmt.Fprintf(stdout, "%s=%d\n", title, len(values)); err != nil {
		return err
	}
	for _, value := range values[:min(limit, len(values))] {
		if _, err := fmt.Fprintf(
			stdout,
			"- q=%s sig=%q decl=%s:%d",
			shortenQName(modulePath, value.Symbol.QName),
			displaySignature(value.Symbol),
			value.Symbol.FilePath,
			value.Symbol.Line,
		); err != nil {
			return err
		}
		if showUseSite {
			if _, err := fmt.Fprintf(stdout, " use=%s:%d", value.UseFilePath, value.UseLine); err != nil {
				return err
			}
			if value.Relation != "" {
				if _, err := fmt.Fprintf(stdout, " rel=%s", value.Relation); err != nil {
					return err
				}
			}
		}
		if _, err := fmt.Fprintln(stdout); err != nil {
			return err
		}
	}
	return renderMoreLine(stdout, len(values), limit)
}

func renderAIReferences(stdout io.Writer, modulePath, title string, values []storage.RefView, limit int) error {
	if _, err := fmt.Fprintf(stdout, "%s=%d\n", title, len(values)); err != nil {
		return err
	}
	for _, value := range values[:min(limit, len(values))] {
		if _, err := fmt.Fprintf(
			stdout,
			"- q=%s kind=%s sig=%q decl=%s:%d ref=%s:%d\n",
			shortenQName(modulePath, value.Symbol.QName),
			value.Kind,
			displaySignature(value.Symbol),
			value.Symbol.FilePath,
			value.Symbol.Line,
			value.UseFilePath,
			value.UseLine,
		); err != nil {
			return err
		}
	}
	return renderMoreLine(stdout, len(values), limit)
}

func renderAITests(stdout io.Writer, title string, tests []storage.TestView, limit int) error {
	if _, err := fmt.Fprintf(stdout, "%s=%d\n", title, len(tests)); err != nil {
		return err
	}
	for _, test := range tests[:min(limit, len(tests))] {
		if _, err := fmt.Fprintf(stdout, "- name=%s file=%s:%d link=%s conf=%s\n", test.Name, test.FilePath, test.Line, test.LinkKind, test.Confidence); err != nil {
			return err
		}
	}
	return renderMoreLine(stdout, len(tests), limit)
}

func renderAISiblings(stdout io.Writer, modulePath, title string, symbols []storage.SymbolMatch, limit int) error {
	if _, err := fmt.Fprintf(stdout, "%s=%d\n", title, len(symbols)); err != nil {
		return err
	}
	for _, symbol := range symbols[:min(limit, len(symbols))] {
		if _, err := fmt.Fprintf(stdout, "- q=%s sig=%q file=%s:%d\n", shortenQName(modulePath, symbol.QName), displaySignature(symbol), symbol.FilePath, symbol.Line); err != nil {
			return err
		}
	}
	return renderMoreLine(stdout, len(symbols), limit)
}

func renderAIImpactNodes(stdout io.Writer, modulePath, title string, values []storage.ImpactNode, limit int) error {
	if _, err := fmt.Fprintf(stdout, "%s=%d\n", title, len(values)); err != nil {
		return err
	}
	for _, value := range values[:min(limit, len(values))] {
		if _, err := fmt.Fprintf(stdout, "- q=%s sig=%q decl=%s:%d depth=%d\n", shortenQName(modulePath, value.Symbol.QName), displaySignature(value.Symbol), value.Symbol.FilePath, value.Symbol.Line, value.Depth); err != nil {
			return err
		}
	}
	return renderMoreLine(stdout, len(values), limit)
}

func renderAIStringList(stdout io.Writer, title string, values []string, limit int) error {
	if _, err := fmt.Fprintf(stdout, "%s=%d\n", title, len(values)); err != nil {
		return err
	}
	for _, value := range values[:min(limit, len(values))] {
		if _, err := fmt.Fprintf(stdout, "- %s\n", value); err != nil {
			return err
		}
	}
	return renderMoreLine(stdout, len(values), limit)
}

func renderHumanEmpty(stdout io.Writer, p palette) error {
	_, err := fmt.Fprintf(stdout, "  %s\n\n", p.muted("none"))
	return err
}

func renderMoreLine(stdout io.Writer, total, limit int) error {
	if total > limit {
		if _, err := fmt.Fprintf(stdout, "  ... and %d more\n", total-limit); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

func readSourceExcerpt(projectRoot, relPath string, focusLine, before, after int) (string, error) {
	path := filepath.Join(projectRoot, filepath.FromSlash(relPath))
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if focusLine <= 0 || focusLine > len(lines) {
		return "", nil
	}

	start := max(1, focusLine-before)
	end := min(len(lines), focusLine+after)
	var builder strings.Builder
	for line := start; line <= end; line++ {
		marker := " "
		if line == focusLine {
			marker = ">"
		}
		builder.WriteString(fmt.Sprintf("  %s %4d | %s\n", marker, line, lines[line-1]))
	}
	return strings.TrimRight(builder.String(), "\n"), nil
}

func shortenValues(modulePath string, values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, shortenQName(modulePath, value))
	}
	return result
}

func impactLabel(callers, refs, tests, reverseDeps int) string {
	score := callers + refs + tests + reverseDeps
	switch {
	case score >= 12:
		return "high"
	case score >= 5:
		return "medium"
	default:
		return "low"
	}
}

func reportImportance(score int) string {
	switch {
	case score >= 18:
		return "high"
	case score >= 7:
		return "medium"
	default:
		return "low"
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func limitStrings(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

func displaySignature(symbol storage.SymbolMatch) string {
	switch symbol.Kind {
	case "func":
		if strings.HasPrefix(symbol.Signature, "func(") {
			return "func " + symbol.Name + symbol.Signature[len("func"):]
		}
	case "method":
		if strings.HasPrefix(symbol.Signature, "func(") {
			if symbol.Receiver != "" {
				return "func (" + symbol.Receiver + ") " + symbol.Name + symbol.Signature[len("func"):]
			}
			return "func " + symbol.Name + symbol.Signature[len("func"):]
		}
	case "struct", "interface", "type", "alias":
		if symbol.Signature != "" && symbol.Signature != symbol.Name {
			if strings.HasPrefix(symbol.Signature, "type ") {
				return symbol.Signature
			}
			return "type " + symbol.Name + " " + symbol.Signature
		}
		if symbol.Name != "" {
			return "type " + symbol.Name
		}
	}
	if symbol.Signature != "" {
		return symbol.Signature
	}
	return symbol.Name
}

type palette struct {
	enabled bool
}

func newPalette() palette {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return palette{}
	}
	return palette{enabled: true}
}

func (p palette) title(value string) string   { return p.wrap("1;36", value) }
func (p palette) section(value string) string { return p.wrap("1;34", value) }
func (p palette) label(value string) string   { return p.wrap("0;37", value) }
func (p palette) accent(value string) string  { return p.wrap("1;33", value) }
func (p palette) muted(value string) string   { return p.wrap("0;90", value) }
func (p palette) kind(kind string) string {
	switch kind {
	case "func":
		return p.wrap("1;36", strings.ToUpper(kind))
	case "method":
		return p.wrap("1;34", strings.ToUpper(kind))
	case "struct":
		return p.wrap("1;35", strings.ToUpper(kind))
	case "interface":
		return p.wrap("1;32", strings.ToUpper(kind))
	case "type", "alias":
		return p.wrap("1;33", strings.ToUpper(kind))
	case "test", "benchmark", "fuzz":
		return p.wrap("1;32", strings.ToUpper(kind))
	default:
		if kind == "" {
			return p.wrap("1;37", "SYMBOL")
		}
		return p.wrap("1;37", strings.ToUpper(kind))
	}
}

func (p palette) kindBadge(kind string) string {
	return "[" + p.kind(kind) + "]"
}

func (p palette) rule(title string) string {
	base := "========================================================================"
	if title == "" {
		return p.wrap("0;90", base)
	}
	prefix := "== " + title + " "
	if len(prefix) >= len(base) {
		return p.wrap("0;90", prefix)
	}
	return p.wrap("0;90", prefix+strings.Repeat("=", len(base)-len(prefix)))
}

func (p palette) badge(value string) string {
	switch value {
	case "high":
		return p.wrap("1;31", strings.ToUpper(value))
	case "medium":
		return p.wrap("1;33", strings.ToUpper(value))
	case "low":
		return p.wrap("1;32", strings.ToUpper(value))
	default:
		return p.wrap("1;35", strings.ToUpper(value))
	}
}

func (p palette) wrap(code, value string) string {
	if !p.enabled || value == "" {
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

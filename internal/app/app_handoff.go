package app

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/cli"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

type handoffTarget struct {
	Kind     string
	Symbol   storage.SymbolMatch
	Package  storage.PackageMatch
	FilePath string
}

type handoffPackageView struct {
	History    storage.PackageHistoryView
	CoChange   storage.CoChangeView
	Tests      []storage.TestView
	TopFiles   []string
	ReadFirst  []string
	Checklist  []string
	EventLines []string
	Risk       string
}

type handoffFileView struct {
	Summary      storage.FileSummary
	SymbolLines  []string
	SurfaceLines []string
	Tests        []storage.TestView
	ReadFirst    []string
	Checklist    []string
	Risk         string
}

func runHandoff(command cli.Command, stdout io.Writer) error {
	state, err := openPreparedProjectState(command)
	if err != nil {
		return err
	}
	defer state.Close()

	if _, ok, err := ensureIndexedSnapshot(stdout, state); err != nil || !ok {
		return err
	}

	fileSummaries, err := state.Store.LoadFileSummaries()
	if err != nil {
		return err
	}
	target, found, err := resolveHandoffTarget(stdout, state.Info.Root, state.Info.ModulePath, state.Store, fileSummaries, command.Scope, command.Query)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}

	switch target.Kind {
	case "package":
		view, err := buildPackageHandoffView(state.Info.ModulePath, state.Store, target.Package.ImportPath, command.Limit)
		if err != nil {
			return err
		}
		switch command.OutputMode {
		case cli.OutputAI:
			return renderAIPackageHandoff(stdout, state.Info.ModulePath, view, command.Limit, command.Explain)
		default:
			return renderHumanPackageHandoff(stdout, state.Info.Root, state.Info.ModulePath, view, command.Limit, command.Explain)
		}
	case "file":
		view, err := buildFileHandoffView(state.Info.ModulePath, state.Store, target.FilePath, command.Limit)
		if err != nil {
			return err
		}
		switch command.OutputMode {
		case cli.OutputAI:
			return renderAIFileHandoff(stdout, state.Info.ModulePath, view, command.Limit, command.Explain)
		default:
			return renderHumanFileHandoff(stdout, state.Info.Root, state.Info.ModulePath, view, command.Limit, command.Explain)
		}
	default:
		trace, err := buildTraceView(state.Info.ModulePath, state.Store, target.Symbol.SymbolKey, 3, command.Limit)
		if err != nil {
			return err
		}
		trace.Flow = buildTraceFlow(state.Info.Root, state.Info.ModulePath, trace.Symbol.Symbol, trace.Symbol.Callers, trace.Symbol.Callees, trace.Symbol.ReferencesOut, trace.Symbol.Flow)
		switch command.OutputMode {
		case cli.OutputAI:
			return renderAISymbolHandoff(stdout, state.Info.ModulePath, trace, command.Limit, command.Explain)
		default:
			return renderHumanSymbolHandoff(stdout, state.Info.Root, state.Info.ModulePath, trace, command.Limit, command.Explain)
		}
	}
}

func resolveHandoffTarget(stdout io.Writer, root, modulePath string, store *storage.Store, summaries map[string]storage.FileSummary, scope, query string) (handoffTarget, bool, error) {
	scope = strings.ToLower(strings.TrimSpace(scope))
	switch scope {
	case "file":
		relPath, found, err := resolveIndexedFileQuery(root, query, summaries)
		if err != nil {
			_, printErr := fmt.Fprintf(stdout, "%s\n", err.Error())
			return handoffTarget{}, false, printErr
		}
		if !found {
			_, err := fmt.Fprintf(stdout, "No indexed file matches for %q\n", query)
			return handoffTarget{}, false, err
		}
		return handoffTarget{Kind: "file", FilePath: relPath}, true, nil
	case "package":
		match, found, err := resolveSinglePackageQuery(stdout, modulePath, store, query)
		if err != nil || !found {
			return handoffTarget{}, found, err
		}
		return handoffTarget{Kind: "package", Package: match}, true, nil
	case "symbol":
		match, found, err := resolveSingleSymbolQuery(stdout, modulePath, store, query)
		if err != nil || !found {
			return handoffTarget{}, found, err
		}
		return handoffTarget{Kind: "symbol", Symbol: match}, true, nil
	default:
		relPath, found, err := resolveIndexedFileQuery(root, query, summaries)
		if err == nil && found {
			return handoffTarget{Kind: "file", FilePath: relPath}, true, nil
		}
		if err != nil {
			_, printErr := fmt.Fprintf(stdout, "%s\n", err.Error())
			return handoffTarget{}, false, printErr
		}

		symbols, err := store.FindSymbols(query)
		if err != nil {
			return handoffTarget{}, false, err
		}
		if len(symbols) == 1 {
			return handoffTarget{Kind: "symbol", Symbol: symbols[0]}, true, nil
		}

		packages, err := store.FindPackages(query)
		if err != nil {
			return handoffTarget{}, false, err
		}
		if len(packages) == 1 {
			return handoffTarget{Kind: "package", Package: packages[0]}, true, nil
		}

		switch {
		case len(symbols) > 1:
			return handoffTarget{}, false, printAmbiguousSymbolMatches(stdout, modulePath, query, symbols)
		case len(packages) > 1:
			return handoffTarget{}, false, printAmbiguousPackageMatches(stdout, modulePath, query, packages)
		default:
			_, err := fmt.Fprintf(stdout, "No handoff matches for %q\n", query)
			return handoffTarget{}, false, err
		}
	}
}

func printAmbiguousSymbolMatches(stdout io.Writer, modulePath, query string, matches []storage.SymbolMatch) error {
	if _, err := fmt.Fprintf(stdout, "Ambiguous symbol query %q. Candidates:\n", query); err != nil {
		return err
	}
	for _, match := range matches {
		reason := ""
		if match.SearchKind != "" {
			reason = fmt.Sprintf(" [%s]", match.SearchKind)
		}
		if _, err := fmt.Fprintf(
			stdout,
			"  %s%s\n    %s\n    %s:%d\n    why: %s\n",
			shortenQName(modulePath, match.QName),
			reason,
			displaySignature(match),
			match.FilePath,
			match.Line,
			describeSymbolSearchWhy(match),
		); err != nil {
			return err
		}
	}
	return nil
}

func printAmbiguousPackageMatches(stdout io.Writer, modulePath, query string, matches []storage.PackageMatch) error {
	if _, err := fmt.Fprintf(stdout, "Ambiguous package query %q. Candidates:\n", query); err != nil {
		return err
	}
	for _, match := range matches {
		reason := ""
		if match.SearchKind != "" {
			reason = fmt.Sprintf(" [%s]", match.SearchKind)
		}
		if _, err := fmt.Fprintf(stdout, "  %s%s\n    dir: %s\n", shortenQName(modulePath, match.ImportPath), reason, match.DirPath); err != nil {
			return err
		}
	}
	return nil
}

func buildPackageHandoffView(modulePath string, store *storage.Store, importPath string, limit int) (handoffPackageView, error) {
	if limit <= 0 {
		limit = 6
	}
	history, err := store.PackageHistory(importPath, max(limit, 6))
	if err != nil {
		return handoffPackageView{}, err
	}
	cochange, err := store.PackageCoChange(importPath, max(limit, 6))
	if err != nil {
		return handoffPackageView{}, err
	}
	tests, err := store.LoadPackageTests(importPath, max(limit+2, 8))
	if err != nil {
		return handoffPackageView{}, err
	}
	summaries, err := store.LoadFileSummaries()
	if err != nil {
		return handoffPackageView{}, err
	}
	riskCtx, err := loadWorkflowRiskContext(store)
	if err != nil {
		return handoffPackageView{}, err
	}

	files := topFileSummaries(summaries, func(summary storage.FileSummary) bool {
		return summary.PackageImportPath == importPath
	}, max(limit+2, 8))
	fileLines := make([]string, 0, len(files))
	for _, summary := range files[:min(len(files), max(limit, 4))] {
		symbols, err := store.LoadFileSymbols(summary.FilePath)
		if err != nil {
			return handoffPackageView{}, err
		}
		names := make([]string, 0, min(3, len(symbols)))
		for _, symbol := range symbols[:min(3, len(symbols))] {
			names = append(names, shortenQName(modulePath, symbol.QName))
		}
		if len(names) == 0 {
			names = append(names, "no indexed symbols")
		}
		risk := fileRiskSummary(summary, riskCtx.hotScore(summary.FilePath), riskCtx.recentChanged(summary.FilePath))
		fileLines = append(fileLines, fmt.Sprintf("%s [risk=%s score=%d] symbols=%s", summary.FilePath, risk, summary.QualityScore, strings.Join(names, ", ")))
	}

	view := handoffPackageView{
		History:  history,
		CoChange: cochange,
		Tests:    tests,
		TopFiles: fileLines,
		Risk:     packageSummaryRiskSummary(history.Package),
	}
	view.EventLines = buildPackageHistoryLines(history, limit)
	view.ReadFirst = buildPackageReadFirst(modulePath, history.Package, view.TopFiles, tests, cochange)
	view.Checklist = buildPackageChecklist(view)
	return view, nil
}

func buildFileHandoffView(modulePath string, store *storage.Store, filePath string, limit int) (handoffFileView, error) {
	if limit <= 0 {
		limit = 6
	}
	summary, err := store.LoadFileSummary(filePath)
	if err != nil {
		return handoffFileView{}, err
	}
	symbols, err := store.LoadFileSymbols(filePath)
	if err != nil {
		return handoffFileView{}, err
	}
	riskCtx, err := loadWorkflowRiskContext(store)
	if err != nil {
		return handoffFileView{}, err
	}

	type scoredSymbol struct {
		view     storage.SymbolView
		guidance symbolTestGuidance
		score    int
	}
	scored := make([]scoredSymbol, 0, len(symbols))
	for _, symbol := range symbols {
		view, err := store.LoadSymbolView(symbol.SymbolKey)
		if err != nil {
			return handoffFileView{}, err
		}
		guidance, err := buildSymbolTestGuidance(store, view, max(limit, 6))
		if err != nil {
			return handoffFileView{}, err
		}
		score := view.QualityScore + len(view.Callers)*3 + len(view.ReferencesIn)*2 + len(view.Tests)*4
		scored = append(scored, scoredSymbol{view: view, guidance: guidance, score: score})
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		if scored[i].view.Symbol.Line != scored[j].view.Symbol.Line {
			return scored[i].view.Symbol.Line < scored[j].view.Symbol.Line
		}
		return scored[i].view.Symbol.QName < scored[j].view.Symbol.QName
	})

	symbolLines := make([]string, 0, min(len(scored), max(limit, 4)))
	surfaceLines := make([]string, 0, min(len(scored), max(limit, 4)))
	var tests []storage.TestView
	for _, item := range scored[:min(len(scored), max(limit, 4))] {
		symbolLines = append(symbolLines, fmt.Sprintf("%s [%s] %s:%d score=%d", shortenQName(modulePath, item.view.Symbol.QName), item.view.Symbol.Kind, item.view.Symbol.FilePath, item.view.Symbol.Line, item.view.QualityScore))
		surfaceLines = append(surfaceLines, fmt.Sprintf("%s callers=%d refs=%d tests=%d rdeps=%d", shortenQName(modulePath, item.view.Symbol.QName), len(item.view.Callers), len(item.view.ReferencesIn), len(item.guidance.ReadBefore), len(item.view.Package.ReverseDeps)))
		tests = append(tests, item.guidance.ReadBefore...)
	}
	tests = dedupeTestsByBestScore(tests, max(limit+2, 8))
	if len(tests) == 0 && summary.PackageImportPath != "" {
		tests, err = store.LoadPackageTests(summary.PackageImportPath, max(limit+2, 8))
		if err != nil {
			return handoffFileView{}, err
		}
	}

	view := handoffFileView{
		Summary:      summary,
		SymbolLines:  symbolLines,
		SurfaceLines: surfaceLines,
		Tests:        tests,
		Risk:         fileRiskSummary(summary, riskCtx.hotScore(summary.FilePath), riskCtx.recentChanged(summary.FilePath)),
	}
	view.ReadFirst = buildFileReadFirst(modulePath, summary, symbolLines, tests)
	view.Checklist = buildFileChecklist(summary, view.Risk, tests)
	return view, nil
}

func renderHumanSymbolHandoff(stdout io.Writer, projectRoot, modulePath string, view traceView, limit int, explain bool) error {
	p := newPalette()
	surface := impactLabel(len(view.Symbol.Callers), len(view.Symbol.ReferencesIn), len(view.Guidance.ReadBefore), len(view.Symbol.Package.ReverseDeps))

	if _, err := fmt.Fprintf(stdout, "%s\n%s\n\n", p.rule("Handoff"), p.title("CTX Handoff")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		stdout,
		"%s\n  %s symbol\n  %s %s\n  %s %s\n  %s %s\n  %s %s\n\n",
		p.section("Summary"),
		p.label("Scope:"),
		p.label("Target:"),
		shortenQName(modulePath, view.Symbol.Symbol.QName),
		p.label("Surface:"),
		p.badge(surface),
		p.label("Risk:"),
		view.Risk,
		p.label("Test signal:"),
		view.Guidance.Signal,
	); err != nil {
		return err
	}
	if explain {
		if err := renderHumanExplainSection(stdout, p, buildHandoffSymbolExplain(modulePath, view)); err != nil {
			return err
		}
	}
	if err := renderHumanDeclaration(stdout, p, projectRoot, view.Symbol.Symbol); err != nil {
		return err
	}
	if err := renderHumanTraceFlow(stdout, p, view.Flow, max(limit, 6)); err != nil {
		return err
	}
	if err := renderHumanChecklist(stdout, p, "Read First", view.ReadOrder, max(limit, 5)); err != nil {
		return err
	}
	if len(view.Impact.BlastPackageReasons) > 0 {
		if err := renderHumanPackageReasons(stdout, p, modulePath, "Change Carefully: Blast Packages", view.Impact.BlastPackageReasons, max(limit, 6)); err != nil {
			return err
		}
	}
	if len(view.Impact.BlastFileReasons) > 0 {
		if err := renderHumanFileReasons(stdout, p, "Change Carefully: Blast Files", view.Impact.BlastFileReasons, max(limit, 6)); err != nil {
			return err
		}
	}
	if err := renderHumanTestGuidance(stdout, p, projectRoot, view.Guidance, max(limit+2, 8)); err != nil {
		return err
	}
	return renderHumanChecklist(stdout, p, "Review Checklist", view.Checklist, max(limit, 6))
}

func renderAISymbolHandoff(stdout io.Writer, modulePath string, view traceView, limit int, explain bool) error {
	if _, err := fmt.Fprintf(stdout, "handoff scope=symbol target=%s surface=%s risk=%q test_signal=%q\n", shortenQName(modulePath, view.Symbol.Symbol.QName), impactLabel(len(view.Symbol.Callers), len(view.Symbol.ReferencesIn), len(view.Guidance.ReadBefore), len(view.Symbol.Package.ReverseDeps)), view.Risk, view.Guidance.Signal); err != nil {
		return err
	}
	if explain {
		if err := renderAIExplainSection(stdout, "explain", buildHandoffSymbolExplain(modulePath, view)); err != nil {
			return err
		}
	}
	if err := renderAITraceFlow(stdout, view.Flow, max(limit, 6)); err != nil {
		return err
	}
	if err := renderAIChecklist(stdout, "read_first", view.ReadOrder, max(limit, 5)); err != nil {
		return err
	}
	if err := renderAITests(stdout, "tests", view.Guidance.ReadBefore, max(limit+2, 8)); err != nil {
		return err
	}
	if err := renderAIPackageReasons(stdout, modulePath, "blast_packages", view.Impact.BlastPackageReasons, max(limit, 6)); err != nil {
		return err
	}
	return renderAIChecklist(stdout, "review_checklist", view.Checklist, max(limit, 6))
}

func renderHumanPackageHandoff(stdout io.Writer, projectRoot, modulePath string, view handoffPackageView, limit int, explain bool) error {
	p := newPalette()
	if _, err := fmt.Fprintf(stdout, "%s\n%s\n\n", p.rule("Handoff"), p.title("CTX Handoff")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		stdout,
		"%s\n  %s package\n  %s %s\n  %s %s\n  %s files=%d symbols=%d tests=%d\n  %s deps=%d rdeps=%d\n  %s %s\n\n",
		p.section("Summary"),
		p.label("Scope:"),
		p.label("Target:"),
		shortenQName(modulePath, view.History.Package.ImportPath),
		p.label("Dir:"),
		view.History.Package.DirPath,
		p.label("Inventory:"),
		view.History.Package.FileCount,
		view.History.Package.SymbolCount,
		view.History.Package.TestCount,
		p.label("Fan-out:"),
		len(view.History.Package.LocalDeps),
		len(view.History.Package.ReverseDeps),
		p.label("Risk:"),
		view.Risk,
	); err != nil {
		return err
	}
	if explain {
		if err := renderHumanExplainSection(stdout, p, buildHandoffPackageExplain(modulePath, view)); err != nil {
			return err
		}
	}
	if err := renderHumanPackageSummary(stdout, p, modulePath, view.History.Package); err != nil {
		return err
	}
	if err := renderHumanChecklist(stdout, p, "Top Files", view.TopFiles, max(limit, 6)); err != nil {
		return err
	}
	if err := renderHumanCoChangeItems(stdout, p, "Packages That Change Together", view.CoChange.Packages, max(limit, 6)); err != nil {
		return err
	}
	if err := renderHumanCoChangeItems(stdout, p, "Files That Change Together", view.CoChange.Files, max(limit, 6)); err != nil {
		return err
	}
	if err := renderHumanTests(stdout, p, projectRoot, "Tests To Run", view.Tests, max(limit+2, 8)); err != nil {
		return err
	}
	if err := renderHumanChecklist(stdout, p, "Recent Change Events", view.EventLines, max(limit, 6)); err != nil {
		return err
	}
	if err := renderHumanChecklist(stdout, p, "Read First", view.ReadFirst, max(limit, 5)); err != nil {
		return err
	}
	return renderHumanChecklist(stdout, p, "Review Checklist", view.Checklist, max(limit, 6))
}

func renderAIPackageHandoff(stdout io.Writer, modulePath string, view handoffPackageView, limit int, explain bool) error {
	if _, err := fmt.Fprintf(stdout, "handoff scope=package target=%s dir=%s files=%d symbols=%d tests=%d deps=%d rdeps=%d risk=%q\n", shortenQName(modulePath, view.History.Package.ImportPath), view.History.Package.DirPath, view.History.Package.FileCount, view.History.Package.SymbolCount, view.History.Package.TestCount, len(view.History.Package.LocalDeps), len(view.History.Package.ReverseDeps), view.Risk); err != nil {
		return err
	}
	if explain {
		if err := renderAIExplainSection(stdout, "explain", buildHandoffPackageExplain(modulePath, view)); err != nil {
			return err
		}
	}
	if err := renderAIChecklist(stdout, "top_files", view.TopFiles, max(limit, 6)); err != nil {
		return err
	}
	if err := renderAITests(stdout, "tests", view.Tests, max(limit+2, 8)); err != nil {
		return err
	}
	if err := renderAIChecklist(stdout, "read_first", view.ReadFirst, max(limit, 5)); err != nil {
		return err
	}
	return renderAIChecklist(stdout, "review_checklist", view.Checklist, max(limit, 6))
}

func renderHumanFileHandoff(stdout io.Writer, projectRoot, modulePath string, view handoffFileView, limit int, explain bool) error {
	p := newPalette()
	if _, err := fmt.Fprintf(stdout, "%s\n%s\n\n", p.rule("Handoff"), p.title("CTX Handoff")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		stdout,
		"%s\n  %s file\n  %s %s\n  %s %s\n  %s symbols=%d relevant=%d tests=%d\n  %s calls=%d refs=%d\n  %s %s\n\n",
		p.section("Summary"),
		p.label("Scope:"),
		p.label("Target:"),
		view.Summary.FilePath,
		p.label("Package:"),
		shortenQName(modulePath, view.Summary.PackageImportPath),
		p.label("Inventory:"),
		view.Summary.SymbolCount,
		view.Summary.RelevantSymbolCount,
		view.Summary.RelatedTestCount,
		p.label("Surface:"),
		view.Summary.InboundCallCount,
		view.Summary.InboundReferenceCount,
		p.label("Risk:"),
		view.Risk,
	); err != nil {
		return err
	}
	if explain {
		if err := renderHumanExplainSection(stdout, p, buildHandoffFileExplain(modulePath, view)); err != nil {
			return err
		}
	}
	if err := renderHumanChecklist(stdout, p, "Key Symbols", view.SymbolLines, max(limit, 6)); err != nil {
		return err
	}
	if err := renderHumanChecklist(stdout, p, "Nearby Surface", view.SurfaceLines, max(limit, 6)); err != nil {
		return err
	}
	if err := renderHumanTests(stdout, p, projectRoot, "Tests To Run", view.Tests, max(limit+2, 8)); err != nil {
		return err
	}
	if err := renderHumanChecklist(stdout, p, "Read First", view.ReadFirst, max(limit, 5)); err != nil {
		return err
	}
	return renderHumanChecklist(stdout, p, "Review Checklist", view.Checklist, max(limit, 6))
}

func renderAIFileHandoff(stdout io.Writer, modulePath string, view handoffFileView, limit int, explain bool) error {
	if _, err := fmt.Fprintf(stdout, "handoff scope=file target=%s package=%s symbols=%d relevant=%d tests=%d calls=%d refs=%d risk=%q\n", view.Summary.FilePath, shortenQName(modulePath, view.Summary.PackageImportPath), view.Summary.SymbolCount, view.Summary.RelevantSymbolCount, view.Summary.RelatedTestCount, view.Summary.InboundCallCount, view.Summary.InboundReferenceCount, view.Risk); err != nil {
		return err
	}
	if explain {
		if err := renderAIExplainSection(stdout, "explain", buildHandoffFileExplain(modulePath, view)); err != nil {
			return err
		}
	}
	if err := renderAIChecklist(stdout, "key_symbols", view.SymbolLines, max(limit, 6)); err != nil {
		return err
	}
	if err := renderAIChecklist(stdout, "nearby_surface", view.SurfaceLines, max(limit, 6)); err != nil {
		return err
	}
	if err := renderAITests(stdout, "tests", view.Tests, max(limit+2, 8)); err != nil {
		return err
	}
	if err := renderAIChecklist(stdout, "read_first", view.ReadFirst, max(limit, 5)); err != nil {
		return err
	}
	return renderAIChecklist(stdout, "review_checklist", view.Checklist, max(limit, 6))
}

func buildPackageHistoryLines(view storage.PackageHistoryView, limit int) []string {
	lines := make([]string, 0, min(limit, len(view.Events)))
	for _, event := range view.Events[:min(len(view.Events), max(limit, 4))] {
		parts := []string{event.Status}
		if event.FileDelta != 0 {
			parts = append(parts, fmt.Sprintf("files %+d", event.FileDelta))
		}
		if event.SymbolDelta != 0 {
			parts = append(parts, fmt.Sprintf("symbols %+d", event.SymbolDelta))
		}
		if event.TestDelta != 0 {
			parts = append(parts, fmt.Sprintf("tests %+d", event.TestDelta))
		}
		if event.AddedDeps > 0 || event.RemovedDeps > 0 {
			parts = append(parts, fmt.Sprintf("deps +%d/-%d", event.AddedDeps, event.RemovedDeps))
		}
		lines = append(lines, fmt.Sprintf("snapshot %d (%s): %s", event.ToSnapshot.ID, event.ToSnapshot.CreatedAt.Format(timeFormat), strings.Join(parts, " | ")))
	}
	return lines
}

func buildPackageReadFirst(modulePath string, summary storage.PackageSummary, topFiles []string, tests []storage.TestView, cochange storage.CoChangeView) []string {
	items := make([]string, 0, 5)
	if len(topFiles) > 0 {
		items = append(items, "Start with the top-ranked file in the package to anchor the main surface area.")
	}
	if len(summary.ReverseDeps) > 0 {
		items = append(items, fmt.Sprintf("Then inspect reverse dep %s to see who consumes this package.", shortenQName(modulePath, summary.ReverseDeps[0])))
	}
	if len(tests) > 0 {
		items = append(items, fmt.Sprintf("Read test %s at %s:%d before changing behavior.", tests[0].Name, tests[0].FilePath, tests[0].Line))
	}
	if len(cochange.Packages) > 0 {
		items = append(items, fmt.Sprintf("Skim package %s because it often changes together with this area.", shortenQName(modulePath, cochange.Packages[0].Label)))
	}
	if len(items) == 0 {
		items = append(items, "This package is fairly self-contained; start with its entry file and local tests.")
	}
	return items
}

func buildPackageChecklist(view handoffPackageView) []string {
	items := make([]string, 0, 6)
	if len(view.History.Package.ReverseDeps) > 0 {
		items = append(items, "Check reverse deps before making contract changes.")
	}
	if len(view.History.Package.LocalDeps) > 0 {
		items = append(items, "Inspect local deps this package fans out to before moving shared logic.")
	}
	if len(view.Tests) == 0 {
		items = append(items, "Package-level test coverage is thin, so plan extra verification.")
	}
	if len(view.CoChange.Packages) > 0 {
		items = append(items, "Use co-change neighbors as a sanity check for hidden cross-package coupling.")
	}
	if len(view.EventLines) > 0 {
		items = append(items, "Scan recent package history before large refactors.")
	}
	if len(items) == 0 {
		items = append(items, "This package looks contained; verify local tests and proceed file by file.")
	}
	return items
}

func buildFileReadFirst(modulePath string, summary storage.FileSummary, symbolLines []string, tests []storage.TestView) []string {
	items := []string{
		fmt.Sprintf("Start with %s to understand the local control surface.", summary.FilePath),
	}
	if len(symbolLines) > 0 {
		items = append(items, "Then inspect the highest-signal symbol in the file before touching helpers.")
	}
	if len(tests) > 0 {
		items = append(items, fmt.Sprintf("Read test %s at %s:%d before editing behavior.", tests[0].Name, tests[0].FilePath, tests[0].Line))
	}
	if summary.PackageImportPath != "" {
		items = append(items, fmt.Sprintf("Keep the wider package %s in view while editing.", shortenQName(modulePath, summary.PackageImportPath)))
	}
	return items
}

func buildFileChecklist(summary storage.FileSummary, risk string, tests []storage.TestView) []string {
	items := make([]string, 0, 5)
	if summary.InboundCallCount > 0 || summary.InboundReferenceCount > 0 {
		items = append(items, "This file has upstream pressure, so check callers and refs before changing shared symbols.")
	}
	if strings.Contains(risk, "weak-test") || len(tests) == 0 {
		items = append(items, "Test coverage around this file is thin, so plan an explicit verification step.")
	}
	if summary.RelevantSymbolCount > 3 {
		items = append(items, "Multiple relevant symbols live here; edit one cluster at a time.")
	}
	if summary.ChangedRecently {
		items = append(items, "This file changed recently, so watch for half-finished refactors or unstable assumptions.")
	}
	if len(items) == 0 {
		items = append(items, "This file looks contained; read top symbols and proceed.")
	}
	return items
}

func buildHandoffSymbolExplain(modulePath string, view traceView) explainSection {
	section := explainSection{
		Title: "Explain",
		Facts: []explainFact{
			{Key: "Scope", Value: "symbol handoff for " + shortenQName(modulePath, view.Symbol.Symbol.QName)},
			{Key: "Surface", Value: impactLabel(len(view.Symbol.Callers), len(view.Symbol.ReferencesIn), len(view.Guidance.ReadBefore), len(view.Symbol.Package.ReverseDeps))},
			{Key: "Precision", Value: "handoff prioritizes high-signal callers, tests, and blast-radius packages rather than rendering the whole graph"},
		},
		Notes: []string{
			"read-first order favors contract, real callers, and safety tests before widening to blast packages",
		},
	}
	if len(view.ReadOrder) > 0 {
		items := make([]explainItem, 0, min(4, len(view.ReadOrder)))
		for _, item := range view.ReadOrder[:min(4, len(view.ReadOrder))] {
			items = append(items, explainItem{Label: item})
		}
		section.Groups = append(section.Groups, explainGroup{Title: "Read first", Items: items})
	}
	if len(view.Impact.BlastPackageReasons) > 0 {
		items := make([]explainItem, 0, min(4, len(view.Impact.BlastPackageReasons)))
		for _, reason := range view.Impact.BlastPackageReasons[:min(4, len(view.Impact.BlastPackageReasons))] {
			items = append(items, explainItem{Label: shortenQName(modulePath, reason.PackageImportPath), Details: reason.Why})
		}
		section.Groups = append(section.Groups, explainGroup{Title: "Blast packages", Items: items})
	}
	return section
}

func buildHandoffPackageExplain(modulePath string, view handoffPackageView) explainSection {
	section := explainSection{
		Title: "Explain",
		Facts: []explainFact{
			{Key: "Scope", Value: "package handoff for " + shortenQName(modulePath, view.History.Package.ImportPath)},
			{Key: "Surface", Value: fmt.Sprintf("files=%d symbols=%d tests=%d rdeps=%d", view.History.Package.FileCount, view.History.Package.SymbolCount, view.History.Package.TestCount, len(view.History.Package.ReverseDeps))},
			{Key: "Precision", Value: "handoff uses package inventory, reverse deps, package tests, and co-change history; runtime-only wiring may be missing"},
		},
	}
	if len(view.TopFiles) > 0 {
		items := make([]explainItem, 0, min(4, len(view.TopFiles)))
		for _, item := range view.TopFiles[:min(4, len(view.TopFiles))] {
			items = append(items, explainItem{Label: item})
		}
		section.Groups = append(section.Groups, explainGroup{Title: "Top files", Items: items})
	}
	if len(view.CoChange.Packages) > 0 {
		items := make([]explainItem, 0, min(4, len(view.CoChange.Packages)))
		for _, item := range view.CoChange.Packages[:min(4, len(view.CoChange.Packages))] {
			items = append(items, explainItem{Label: shortenQName(modulePath, item.Label), Details: []string{formatCoChangeItem(item)}})
		}
		section.Groups = append(section.Groups, explainGroup{Title: "Change-together packages", Items: items})
	}
	return section
}

func buildHandoffFileExplain(modulePath string, view handoffFileView) explainSection {
	section := explainSection{
		Title: "Explain",
		Facts: []explainFact{
			{Key: "Scope", Value: "file handoff for " + view.Summary.FilePath},
			{Key: "Surface", Value: fmt.Sprintf("symbols=%d relevant=%d calls=%d refs=%d tests=%d", view.Summary.SymbolCount, view.Summary.RelevantSymbolCount, view.Summary.InboundCallCount, view.Summary.InboundReferenceCount, view.Summary.RelatedTestCount)},
			{Key: "Precision", Value: "file handoff ranks local symbols by indexed graph pressure and nearby tests, not by full type-level semantics"},
		},
	}
	if len(view.SymbolLines) > 0 {
		items := make([]explainItem, 0, min(4, len(view.SymbolLines)))
		for _, item := range view.SymbolLines[:min(4, len(view.SymbolLines))] {
			items = append(items, explainItem{Label: item})
		}
		section.Groups = append(section.Groups, explainGroup{Title: "Key symbols", Items: items})
	}
	if len(view.SurfaceLines) > 0 {
		items := make([]explainItem, 0, min(4, len(view.SurfaceLines)))
		for _, item := range view.SurfaceLines[:min(4, len(view.SurfaceLines))] {
			items = append(items, explainItem{Label: item})
		}
		section.Groups = append(section.Groups, explainGroup{Title: "Nearby surface", Items: items})
	}
	return section
}

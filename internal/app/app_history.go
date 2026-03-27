package app

import (
	"fmt"
	"io"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/cli"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

func runHistory(command cli.Command, stdout io.Writer) error {
	state, err := openPreparedProjectState(command)
	if err != nil {
		return err
	}
	defer state.Close()

	switch command.Scope {
	case "package":
		match, found, err := resolveSinglePackageQuery(stdout, state.Info.ModulePath, state.Store, command.Query)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		view, err := state.Store.PackageHistory(match.ImportPath, command.Limit)
		if err != nil {
			return err
		}
		return renderHumanPackageHistory(stdout, state.Info.ModulePath, view, command.Explain)
	default:
		match, found, err := resolveSingleSymbolQuery(stdout, state.Info.ModulePath, state.Store, command.Query)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		view, err := state.Store.SymbolHistory(match.SymbolKey, command.Limit)
		if err != nil {
			return err
		}
		return renderHumanSymbolHistory(stdout, state.Info.Root, state.Info.ModulePath, view, command.Explain)
	}
}

func runCoChange(command cli.Command, stdout io.Writer) error {
	state, err := openPreparedProjectState(command)
	if err != nil {
		return err
	}
	defer state.Close()

	switch command.Scope {
	case "package":
		match, found, err := resolveSinglePackageQuery(stdout, state.Info.ModulePath, state.Store, command.Query)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		view, err := state.Store.PackageCoChange(match.ImportPath, command.Limit)
		if err != nil {
			return err
		}
		return renderHumanCoChange(stdout, state.Info.ModulePath, view, command.Explain)
	default:
		match, found, err := resolveSingleSymbolQuery(stdout, state.Info.ModulePath, state.Store, command.Query)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		view, err := state.Store.SymbolCoChange(match.SymbolKey, command.Limit)
		if err != nil {
			return err
		}
		return renderHumanCoChange(stdout, state.Info.ModulePath, view, command.Explain)
	}
}

func resolveSinglePackageQuery(stdout io.Writer, modulePath string, store *storage.Store, query string) (storage.PackageMatch, bool, error) {
	matches, err := store.FindPackages(query)
	if err != nil {
		return storage.PackageMatch{}, false, err
	}
	if len(matches) == 0 {
		_, err := fmt.Fprintf(stdout, "No package matches for %q\n", query)
		return storage.PackageMatch{}, false, err
	}
	if len(matches) == 1 {
		return matches[0], true, nil
	}

	if _, err := fmt.Fprintf(stdout, "Ambiguous package query %q. Candidates:\n", query); err != nil {
		return storage.PackageMatch{}, false, err
	}
	for _, match := range matches {
		reason := ""
		if match.SearchKind != "" {
			reason = fmt.Sprintf(" [%s]", match.SearchKind)
		}
		if _, err := fmt.Fprintf(stdout, "  %s%s\n    dir: %s\n", shortenQName(modulePath, match.ImportPath), reason, match.DirPath); err != nil {
			return storage.PackageMatch{}, false, err
		}
	}
	return storage.PackageMatch{}, false, nil
}

func renderHumanSymbolHistory(stdout io.Writer, root, modulePath string, view storage.SymbolHistoryView, explain bool) error {
	p := newPalette()
	if _, err := fmt.Fprintf(stdout, "%s\n%s\n\n", p.rule("History"), p.title("CTX History")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		stdout,
		"%s\n  %s symbol\n  %s %s\n  %s %s\n  %s %s\n  %s snapshot %d (%s)\n  %s snapshot %d (%s)\n\n",
		p.section("Summary"),
		p.label("Scope:"),
		p.label("Symbol:"),
		shortenQName(modulePath, view.Symbol.QName),
		p.label("Signature:"),
		displaySignature(view.Symbol),
		p.label("Declared now:"),
		symbolRangeDisplay(root, view.Symbol),
		p.label("Introduced in:"),
		view.IntroducedIn.ID,
		view.IntroducedIn.CreatedAt.Format(timeFormat),
		p.label("Changed since:"),
		view.LastChangedIn.ID,
		view.LastChangedIn.CreatedAt.Format(timeFormat),
	); err != nil {
		return err
	}
	if explain {
		if err := renderHumanExplainSection(stdout, p, buildSymbolHistoryExplain(view)); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(stdout, "%s (%d)\n", p.section("Recent Change Events"), len(view.Events)); err != nil {
		return err
	}
	if len(view.Events) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, event := range view.Events {
		parts := []string{event.Status}
		if event.ContractChanged {
			parts = append(parts, "contract")
		}
		if event.Moved {
			parts = append(parts, "moved")
		}
		if event.AddedCalls > 0 || event.RemovedCalls > 0 {
			parts = append(parts, fmt.Sprintf("calls +%d/-%d", event.AddedCalls, event.RemovedCalls))
		}
		if event.AddedRefs > 0 || event.RemovedRefs > 0 {
			parts = append(parts, fmt.Sprintf("refs +%d/-%d", event.AddedRefs, event.RemovedRefs))
		}
		if event.AddedTests > 0 || event.RemovedTests > 0 {
			parts = append(parts, fmt.Sprintf("tests +%d/-%d", event.AddedTests, event.RemovedTests))
		}
		if _, err := fmt.Fprintf(
			stdout,
			"  [%d] %s\n      %s\n",
			event.ToSnapshot.ID,
			event.ToSnapshot.CreatedAt.Format(timeFormat),
			strings.Join(parts, " | "),
		); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

func renderHumanPackageHistory(stdout io.Writer, modulePath string, view storage.PackageHistoryView, explain bool) error {
	p := newPalette()
	if _, err := fmt.Fprintf(stdout, "%s\n%s\n\n", p.rule("History"), p.title("CTX History")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		stdout,
		"%s\n  %s package\n  %s %s\n  %s %s\n  %s files=%d symbols=%d tests=%d\n  %s snapshot %d (%s)\n  %s snapshot %d (%s)\n\n",
		p.section("Summary"),
		p.label("Scope:"),
		p.label("Package:"),
		shortenQName(modulePath, view.Package.ImportPath),
		p.label("Dir:"),
		view.Package.DirPath,
		p.label("Inventory now:"),
		view.Package.FileCount,
		view.Package.SymbolCount,
		view.Package.TestCount,
		p.label("Introduced in:"),
		view.IntroducedIn.ID,
		view.IntroducedIn.CreatedAt.Format(timeFormat),
		p.label("Changed since:"),
		view.LastChangedIn.ID,
		view.LastChangedIn.CreatedAt.Format(timeFormat),
	); err != nil {
		return err
	}
	if explain {
		if err := renderHumanExplainSection(stdout, p, buildPackageHistoryExplain(view)); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(stdout, "%s (%d)\n", p.section("Recent Change Events"), len(view.Events)); err != nil {
		return err
	}
	if len(view.Events) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, event := range view.Events {
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
		if event.MovedSymbols > 0 {
			parts = append(parts, fmt.Sprintf("moved=%d", event.MovedSymbols))
		}
		if event.ChangedContracts > 0 {
			parts = append(parts, fmt.Sprintf("contracts=%d", event.ChangedContracts))
		}
		if _, err := fmt.Fprintf(
			stdout,
			"  [%d] %s\n      %s\n",
			event.ToSnapshot.ID,
			event.ToSnapshot.CreatedAt.Format(timeFormat),
			strings.Join(parts, " | "),
		); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

func renderHumanCoChange(stdout io.Writer, modulePath string, view storage.CoChangeView, explain bool) error {
	p := newPalette()
	if _, err := fmt.Fprintf(stdout, "%s\n%s\n\n", p.rule("Co-Change"), p.title("CTX Co-Change")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		stdout,
		"%s\n  %s %s\n  %s %s\n",
		p.section("Summary"),
		p.label("Scope:"),
		view.Scope,
		p.label("Anchor:"),
		shortenQName(modulePath, view.Anchor),
	); err != nil {
		return err
	}
	if view.AnchorFile != "" {
		if _, err := fmt.Fprintf(stdout, "  %s %s\n", p.label("Anchor file:"), view.AnchorFile); err != nil {
			return err
		}
	}
	if view.AnchorPackage != "" {
		if _, err := fmt.Fprintf(stdout, "  %s %s\n", p.label("Anchor package:"), shortenQName(modulePath, view.AnchorPackage)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(stdout, "  %s %d snapshot diff(s)\n\n", p.label("Anchor changes observed:"), view.AnchorChangeCount); err != nil {
		return err
	}
	if explain {
		if err := renderHumanExplainSection(stdout, p, buildCoChangeExplain(view)); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(stdout, "%s (%d)\n", p.section("Files That Change Together"), len(view.Files)); err != nil {
		return err
	}
	if len(view.Files) == 0 {
		if err := renderHumanEmpty(stdout, p); err != nil {
			return err
		}
	} else {
		for _, item := range view.Files {
			if _, err := fmt.Fprintf(stdout, "  %s  count=%d  rate=%.0f%%\n", item.Label, item.Count, item.Frequency*100); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintf(stdout, "\n%s (%d)\n", p.section("Packages That Change Together"), len(view.Packages)); err != nil {
		return err
	}
	if len(view.Packages) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, item := range view.Packages {
		if _, err := fmt.Fprintf(stdout, "  %s  count=%d  rate=%.0f%%\n", shortenQName(modulePath, item.Label), item.Count, item.Frequency*100); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

func buildSymbolHistoryExplain(view storage.SymbolHistoryView) explainSection {
	section := explainSection{
		Title: "Explain",
		Facts: []explainFact{
			{Key: "Event source", Value: "stored snapshot diffs for the same qualified symbol"},
			{Key: "Recent events", Value: fmt.Sprintf("%d", len(view.Events))},
			{Key: "Precision", Value: "status/contract/move/call/ref/test deltas come from indexed snapshots only"},
		},
		Notes: []string{
			"contract means the indexed signature changed",
			"moved means file path or declaration line changed for the same symbol",
		},
	}
	if len(view.Events) > 0 {
		items := make([]explainItem, 0, min(5, len(view.Events)))
		for _, event := range view.Events[:min(5, len(view.Events))] {
			details := []string{event.Status}
			if event.ContractChanged {
				details = append(details, "contract changed")
			}
			if event.Moved {
				details = append(details, "declaration moved")
			}
			items = append(items, explainItem{
				Label:   fmt.Sprintf("snapshot %d (%s)", event.ToSnapshot.ID, event.ToSnapshot.CreatedAt.Format(timeFormat)),
				Details: details,
			})
		}
		section.Groups = append(section.Groups, explainGroup{Title: "Recent event hints", Items: items})
	}
	return section
}

func buildPackageHistoryExplain(view storage.PackageHistoryView) explainSection {
	section := explainSection{
		Title: "Explain",
		Facts: []explainFact{
			{Key: "Event source", Value: "stored snapshot diffs for the same package import path"},
			{Key: "Recent events", Value: fmt.Sprintf("%d", len(view.Events))},
			{Key: "Precision", Value: "file/symbol/test/dependency deltas come from indexed package snapshots only"},
		},
		Notes: []string{
			"contracts counts symbols whose indexed signature changed inside the package",
			"moved counts symbols whose declaration location changed between snapshots",
		},
	}
	if len(view.Events) > 0 {
		items := make([]explainItem, 0, min(5, len(view.Events)))
		for _, event := range view.Events[:min(5, len(view.Events))] {
			details := []string{event.Status}
			if event.FileDelta != 0 {
				details = append(details, fmt.Sprintf("files %+d", event.FileDelta))
			}
			if event.SymbolDelta != 0 {
				details = append(details, fmt.Sprintf("symbols %+d", event.SymbolDelta))
			}
			if event.TestDelta != 0 {
				details = append(details, fmt.Sprintf("tests %+d", event.TestDelta))
			}
			items = append(items, explainItem{
				Label:   fmt.Sprintf("snapshot %d (%s)", event.ToSnapshot.ID, event.ToSnapshot.CreatedAt.Format(timeFormat)),
				Details: details,
			})
		}
		section.Groups = append(section.Groups, explainGroup{Title: "Recent event hints", Items: items})
	}
	return section
}

func buildCoChangeExplain(view storage.CoChangeView) explainSection {
	return explainSection{
		Title: "Explain",
		Facts: []explainFact{
			{Key: "Anchor changes", Value: fmt.Sprintf("%d snapshot diff(s)", view.AnchorChangeCount)},
			{Key: "Precision", Value: "co-change is empirical history from stored diffs, not a direct dependency edge"},
		},
		Notes: []string{
			"higher rate means the file/package changed in a larger share of anchor diffs",
			"co-change can reveal operational coupling even when the static graph is sparse",
		},
	}
}

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
		return renderHumanPackageHistory(stdout, state.Info.ModulePath, view)
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
		return renderHumanSymbolHistory(stdout, state.Info.Root, state.Info.ModulePath, view)
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
		return renderHumanCoChange(stdout, state.Info.ModulePath, view)
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
		return renderHumanCoChange(stdout, state.Info.ModulePath, view)
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

func renderHumanSymbolHistory(stdout io.Writer, root, modulePath string, view storage.SymbolHistoryView) error {
	if _, err := fmt.Fprintf(
		stdout,
		"Symbol History\n  Symbol: %s\n  Signature: %s\n  Declared now: %s\n  Introduced in: snapshot %d (%s)\n  Changed since: snapshot %d (%s)\n\n",
		shortenQName(modulePath, view.Symbol.QName),
		displaySignature(view.Symbol),
		symbolRangeDisplay(root, view.Symbol),
		view.IntroducedIn.ID,
		view.IntroducedIn.CreatedAt.Format(timeFormat),
		view.LastChangedIn.ID,
		view.LastChangedIn.CreatedAt.Format(timeFormat),
	); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(stdout, "Recent Change Events (%d):\n", len(view.Events)); err != nil {
		return err
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
	return nil
}

func renderHumanPackageHistory(stdout io.Writer, modulePath string, view storage.PackageHistoryView) error {
	if _, err := fmt.Fprintf(
		stdout,
		"Package History\n  Package: %s\n  Dir: %s\n  Inventory now: files=%d symbols=%d tests=%d\n  Introduced in: snapshot %d (%s)\n  Changed since: snapshot %d (%s)\n\n",
		shortenQName(modulePath, view.Package.ImportPath),
		view.Package.DirPath,
		view.Package.FileCount,
		view.Package.SymbolCount,
		view.Package.TestCount,
		view.IntroducedIn.ID,
		view.IntroducedIn.CreatedAt.Format(timeFormat),
		view.LastChangedIn.ID,
		view.LastChangedIn.CreatedAt.Format(timeFormat),
	); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(stdout, "Recent Change Events (%d):\n", len(view.Events)); err != nil {
		return err
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
	return nil
}

func renderHumanCoChange(stdout io.Writer, modulePath string, view storage.CoChangeView) error {
	if _, err := fmt.Fprintf(
		stdout,
		"Co-Change Analysis\n  Scope: %s\n  Anchor: %s\n",
		view.Scope,
		shortenQName(modulePath, view.Anchor),
	); err != nil {
		return err
	}
	if view.AnchorFile != "" {
		if _, err := fmt.Fprintf(stdout, "  Anchor file: %s\n", view.AnchorFile); err != nil {
			return err
		}
	}
	if view.AnchorPackage != "" {
		if _, err := fmt.Fprintf(stdout, "  Anchor package: %s\n", shortenQName(modulePath, view.AnchorPackage)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(stdout, "  Anchor changes observed: %d snapshot diff(s)\n\n", view.AnchorChangeCount); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(stdout, "Files That Change Together (%d):\n", len(view.Files)); err != nil {
		return err
	}
	for _, item := range view.Files {
		if _, err := fmt.Fprintf(stdout, "  %s  count=%d  rate=%.0f%%\n", item.Label, item.Count, item.Frequency*100); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(stdout, "\nPackages That Change Together (%d):\n", len(view.Packages)); err != nil {
		return err
	}
	for _, item := range view.Packages {
		if _, err := fmt.Fprintf(stdout, "  %s  count=%d  rate=%.0f%%\n", shortenQName(modulePath, item.Label), item.Count, item.Frequency*100); err != nil {
			return err
		}
	}
	return nil
}

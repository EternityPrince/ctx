package app

import (
	"fmt"
	"io"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/cli"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

func runDiff(command cli.Command, stdout io.Writer) error {
	state, err := openPreparedProjectState(command)
	if err != nil {
		return err
	}
	defer state.Close()

	diff, err := state.Store.Diff(command.FromSnapshot, command.ToSnapshot)
	if err != nil {
		return err
	}

	p := newPalette()
	if _, err := fmt.Fprintf(stdout, "%s\n%s\n\n", p.rule("Diff"), p.title("CTX Diff")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		stdout,
		"%s\n  %s %d -> %d\n  %s files +%d ~%d -%d  symbols +%d -%d ~%d  packages ~%d  impacted=%d\n\n",
		p.section("Summary"),
		p.label("Snapshots:"),
		diff.FromSnapshotID,
		diff.ToSnapshotID,
		p.label("Delta:"),
		len(diff.AddedFiles),
		len(diff.ChangedFiles),
		len(diff.DeletedFiles),
		len(diff.AddedSymbols),
		len(diff.RemovedSymbols),
		len(diff.ChangedSymbols),
		len(diff.ChangedPackages),
		len(diff.ImpactedSymbols),
	); err != nil {
		return err
	}
	if command.Explain {
		if err := renderHumanExplainSection(stdout, p, buildDiffExplain(diff)); err != nil {
			return err
		}
	}
	if err := printStringList(stdout, p, "Added Files", diff.AddedFiles); err != nil {
		return err
	}
	if err := printStringList(stdout, p, "Changed Files", diff.ChangedFiles); err != nil {
		return err
	}
	if err := printStringList(stdout, p, "Deleted Files", diff.DeletedFiles); err != nil {
		return err
	}

	if err := printSymbolList(stdout, p, "Added Symbols", diff.AddedSymbols); err != nil {
		return err
	}
	if err := printSymbolList(stdout, p, "Removed Symbols", diff.RemovedSymbols); err != nil {
		return err
	}
	if err := printChangedPackages(stdout, p, diff.ChangedPackages); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(stdout, "%s (%d)\n", p.section("Changed Symbols"), len(diff.ChangedSymbols)); err != nil {
		return err
	}
	for _, symbol := range diff.ChangedSymbols {
		parts := make([]string, 0, 2)
		if symbol.ContractChanged {
			parts = append(parts, "contract")
		}
		if symbol.Moved {
			parts = append(parts, "moved")
		}
		if _, err := fmt.Fprintf(
			stdout,
			"  %s [%s]\n    from: %s  @ %s:%d\n    to:   %s  @ %s:%d\n",
			symbol.QName,
			strings.Join(parts, ", "),
			symbol.FromSignature,
			symbol.FromFilePath,
			symbol.FromLine,
			symbol.ToSignature,
			symbol.ToFilePath,
			symbol.ToLine,
		); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(stdout); err != nil {
		return err
	}
	if err := printCallChanges(stdout, p, "Added Calls", diff.AddedCalls); err != nil {
		return err
	}
	if err := printCallChanges(stdout, p, "Removed Calls", diff.RemovedCalls); err != nil {
		return err
	}
	if err := printRefChanges(stdout, p, "Added Refs", diff.AddedRefs); err != nil {
		return err
	}
	if err := printRefChanges(stdout, p, "Removed Refs", diff.RemovedRefs); err != nil {
		return err
	}
	if err := printTestLinkChanges(stdout, p, "Added Test Links", diff.AddedTestLinks); err != nil {
		return err
	}
	if err := printTestLinkChanges(stdout, p, "Removed Test Links", diff.RemovedTestLinks); err != nil {
		return err
	}
	if err := printPackageDepChanges(stdout, p, "Added Package Deps", diff.AddedPackageDeps); err != nil {
		return err
	}
	if err := printPackageDepChanges(stdout, p, "Removed Package Deps", diff.RemovedPackageDeps); err != nil {
		return err
	}
	if err := printImpactedSymbols(stdout, p, diff.ImpactedSymbols); err != nil {
		return err
	}
	return nil
}

func printStringList(stdout io.Writer, p palette, title string, values []string) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d)\n", p.section(title), len(values)); err != nil {
		return err
	}
	if len(values) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(stdout, "  %s\n", value); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

func printSymbolList(stdout io.Writer, p palette, title string, values []storage.SymbolMatch) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d)\n", p.section(title), len(values)); err != nil {
		return err
	}
	if len(values) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(stdout, "  %s  %s:%d\n", value.QName, value.FilePath, value.Line); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

func printChangedPackages(stdout io.Writer, p palette, values []storage.ChangedPackage) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d)\n", p.section("Changed Packages"), len(values)); err != nil {
		return err
	}
	if len(values) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(
			stdout,
			"  %s [%s]\n    files %d -> %d  symbols %d -> %d  tests %d -> %d  deps %d -> %d  rdeps %d -> %d\n",
			value.ImportPath,
			value.Status,
			value.FromFileCount,
			value.ToFileCount,
			value.FromSymbolCount,
			value.ToSymbolCount,
			value.FromTestCount,
			value.ToTestCount,
			value.FromLocalDepCount,
			value.ToLocalDepCount,
			value.FromReverseDepCount,
			value.ToReverseDepCount,
		); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

func printCallChanges(stdout io.Writer, p palette, title string, values []storage.CallEdgeChange) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d)\n", p.section(title), len(values)); err != nil {
		return err
	}
	if len(values) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(stdout, "  %s -> %s  %s:%d [%s]\n", value.CallerQName, value.CalleeQName, value.FilePath, value.Line, value.Dispatch); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

func printRefChanges(stdout io.Writer, p palette, title string, values []storage.RefChange) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d)\n", p.section(title), len(values)); err != nil {
		return err
	}
	if len(values) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, value := range values {
		from := value.FromQName
		if from == "" {
			from = value.FromPackageImportPath
		}
		if _, err := fmt.Fprintf(stdout, "  %s -> %s  %s:%d [%s]\n", from, value.ToQName, value.FilePath, value.Line, value.Kind); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

func printTestLinkChanges(stdout io.Writer, p palette, title string, values []storage.TestLinkChange) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d)\n", p.section(title), len(values)); err != nil {
		return err
	}
	if len(values) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(stdout, "  %s -> %s  [%s/%s]\n", value.TestName, value.SymbolQName, value.LinkKind, value.Confidence); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

func printPackageDepChanges(stdout io.Writer, p palette, title string, values []storage.PackageDepChange) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d)\n", p.section(title), len(values)); err != nil {
		return err
	}
	if len(values) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(stdout, "  %s -> %s\n", value.FromPackageImportPath, value.ToPackageImportPath); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

func printImpactedSymbols(stdout io.Writer, p palette, values []storage.SymbolImpactDelta) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d)\n", p.section("Impacted Symbols"), len(values)); err != nil {
		return err
	}
	if len(values) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, value := range values {
		parts := make([]string, 0, 6)
		if value.Status != "" {
			parts = append(parts, value.Status)
		}
		if value.ContractChanged {
			parts = append(parts, "contract")
		}
		if value.Moved {
			parts = append(parts, "moved")
		}
		if value.AddedCallers+value.RemovedCallers > 0 {
			parts = append(parts, fmt.Sprintf("callers +%d/-%d", value.AddedCallers, value.RemovedCallers))
		}
		if value.AddedTests+value.RemovedTests > 0 {
			parts = append(parts, fmt.Sprintf("tests +%d/-%d", value.AddedTests, value.RemovedTests))
		}
		if value.BlastRadius > 0 {
			parts = append(parts, fmt.Sprintf("blast=%d", value.BlastRadius))
		}
		if _, err := fmt.Fprintf(stdout, "  %s [%s]\n", value.QName, strings.Join(parts, ", ")); err != nil {
			return err
		}
		for _, why := range value.Why {
			if _, err := fmt.Fprintf(stdout, "    why: %s\n", why); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

func buildDiffExplain(diff storage.DiffView) explainSection {
	section := explainSection{
		Title: "Explain",
		Facts: []explainFact{
			{Key: "Scope", Value: fmt.Sprintf("snapshot %d -> %d", diff.FromSnapshotID, diff.ToSnapshotID)},
			{Key: "Changed files", Value: fmt.Sprintf("added=%d changed=%d deleted=%d", len(diff.AddedFiles), len(diff.ChangedFiles), len(diff.DeletedFiles))},
			{Key: "Changed symbols", Value: fmt.Sprintf("added=%d removed=%d changed=%d impacted=%d", len(diff.AddedSymbols), len(diff.RemovedSymbols), len(diff.ChangedSymbols), len(diff.ImpactedSymbols))},
			{Key: "Precision", Value: "diff is built from stored indexed snapshots; runtime-only edges and non-indexed files are not represented"},
		},
		Notes: []string{
			"changed symbols track signature and location deltas for the same qualified symbol",
			"impacted symbols expand direct changes through callers, refs, tests, and stored blast-radius signals",
		},
	}
	if len(diff.ImpactedSymbols) > 0 {
		items := make([]explainItem, 0, min(6, len(diff.ImpactedSymbols)))
		for _, value := range diff.ImpactedSymbols[:min(6, len(diff.ImpactedSymbols))] {
			details := append([]string{}, value.Why...)
			if len(details) == 0 {
				details = append(details, formatSymbolImpactDelta("", value))
			}
			items = append(items, explainItem{
				Label:   value.QName,
				Details: details,
			})
		}
		section.Groups = append(section.Groups, explainGroup{Title: "High-signal impacted symbols", Items: items})
	}
	return section
}

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

	if _, err := fmt.Fprintf(stdout, "Diff %d -> %d\n", diff.FromSnapshotID, diff.ToSnapshotID); err != nil {
		return err
	}
	if err := printStringList(stdout, "Added files", diff.AddedFiles); err != nil {
		return err
	}
	if err := printStringList(stdout, "Changed files", diff.ChangedFiles); err != nil {
		return err
	}
	if err := printStringList(stdout, "Deleted files", diff.DeletedFiles); err != nil {
		return err
	}

	if err := printSymbolList(stdout, "Added symbols", diff.AddedSymbols); err != nil {
		return err
	}
	if err := printSymbolList(stdout, "Removed symbols", diff.RemovedSymbols); err != nil {
		return err
	}
	if err := printChangedPackages(stdout, diff.ChangedPackages); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(stdout, "Changed symbols (%d):\n", len(diff.ChangedSymbols)); err != nil {
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
	if err := printCallChanges(stdout, "Added calls", diff.AddedCalls); err != nil {
		return err
	}
	if err := printCallChanges(stdout, "Removed calls", diff.RemovedCalls); err != nil {
		return err
	}
	if err := printRefChanges(stdout, "Added refs", diff.AddedRefs); err != nil {
		return err
	}
	if err := printRefChanges(stdout, "Removed refs", diff.RemovedRefs); err != nil {
		return err
	}
	if err := printTestLinkChanges(stdout, "Added test links", diff.AddedTestLinks); err != nil {
		return err
	}
	if err := printTestLinkChanges(stdout, "Removed test links", diff.RemovedTestLinks); err != nil {
		return err
	}
	if err := printPackageDepChanges(stdout, "Added package deps", diff.AddedPackageDeps); err != nil {
		return err
	}
	if err := printPackageDepChanges(stdout, "Removed package deps", diff.RemovedPackageDeps); err != nil {
		return err
	}
	return nil
}

func printStringList(stdout io.Writer, title string, values []string) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d):\n", title, len(values)); err != nil {
		return err
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(stdout, "  %s\n", value); err != nil {
			return err
		}
	}
	return nil
}

func printSymbolList(stdout io.Writer, title string, values []storage.SymbolMatch) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d):\n", title, len(values)); err != nil {
		return err
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(stdout, "  %s  %s:%d\n", value.QName, value.FilePath, value.Line); err != nil {
			return err
		}
	}
	return nil
}

func printChangedPackages(stdout io.Writer, values []storage.ChangedPackage) error {
	if _, err := fmt.Fprintf(stdout, "Changed packages (%d):\n", len(values)); err != nil {
		return err
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
	return nil
}

func printCallChanges(stdout io.Writer, title string, values []storage.CallEdgeChange) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d):\n", title, len(values)); err != nil {
		return err
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(stdout, "  %s -> %s  %s:%d [%s]\n", value.CallerQName, value.CalleeQName, value.FilePath, value.Line, value.Dispatch); err != nil {
			return err
		}
	}
	return nil
}

func printRefChanges(stdout io.Writer, title string, values []storage.RefChange) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d):\n", title, len(values)); err != nil {
		return err
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
	return nil
}

func printTestLinkChanges(stdout io.Writer, title string, values []storage.TestLinkChange) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d):\n", title, len(values)); err != nil {
		return err
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(stdout, "  %s -> %s  [%s/%s]\n", value.TestName, value.SymbolQName, value.LinkKind, value.Confidence); err != nil {
			return err
		}
	}
	return nil
}

func printPackageDepChanges(stdout io.Writer, title string, values []storage.PackageDepChange) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d):\n", title, len(values)); err != nil {
		return err
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(stdout, "  %s -> %s\n", value.FromPackageImportPath, value.ToPackageImportPath); err != nil {
			return err
		}
	}
	return nil
}

package storage

import (
	"database/sql"
	"fmt"

	"github.com/vladimirkasterin/ctx/internal/codebase"
)

func insertFiles(tx *sql.Tx, snapshotID int64, modulePath, root string, scanned []codebase.ScanFile) error {
	stmt, err := tx.Prepare(`
		INSERT INTO files (snapshot_id, rel_path, package_import_path, identity, semantic_meta, content_hash, size_bytes, is_test)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare insert files: %w", err)
	}
	defer stmt.Close()

	for _, file := range scanned {
		pkg := derivePackageForFile(root, modulePath, file)
		if _, err := stmt.Exec(snapshotID, file.RelPath, pkg, file.Identity, file.SemanticMeta, file.Hash, file.SizeBytes, boolInt(file.IsTest)); err != nil {
			return fmt.Errorf("insert file %s: %w", file.RelPath, err)
		}
	}
	return nil
}

func insertPackages(tx *sql.Tx, snapshotID int64, packages []codebase.PackageFact) error {
	stmt, err := tx.Prepare(`
		INSERT INTO packages (snapshot_id, import_path, name, dir_path, file_count)
		VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare insert packages: %w", err)
	}
	defer stmt.Close()

	for _, pkg := range packages {
		if _, err := stmt.Exec(snapshotID, pkg.ImportPath, pkg.Name, pkg.DirPath, pkg.FileCount); err != nil {
			return fmt.Errorf("insert package %s: %w", pkg.ImportPath, err)
		}
	}
	return nil
}

func insertSymbols(tx *sql.Tx, snapshotID int64, symbols []codebase.SymbolFact) error {
	stmt, err := tx.Prepare(`
		INSERT INTO symbols (
			snapshot_id, symbol_key, qname, package_import_path, file_path, name, kind,
			receiver, signature, doc, line, col, exported, is_test
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare insert symbols: %w", err)
	}
	defer stmt.Close()

	for _, symbol := range symbols {
		if _, err := stmt.Exec(
			snapshotID,
			symbol.SymbolKey,
			symbol.QName,
			symbol.PackageImportPath,
			symbol.FilePath,
			symbol.Name,
			symbol.Kind,
			symbol.Receiver,
			symbol.Signature,
			symbol.Doc,
			symbol.Line,
			symbol.Column,
			boolInt(symbol.Exported),
			boolInt(symbol.IsTest),
		); err != nil {
			return fmt.Errorf("insert symbol %s: %w", symbol.SymbolKey, err)
		}
	}
	return nil
}

func insertDependencies(tx *sql.Tx, snapshotID int64, deps []codebase.DependencyFact) error {
	stmt, err := tx.Prepare(`
		INSERT INTO package_deps (snapshot_id, from_package_import_path, to_package_import_path, is_local)
		VALUES (?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare insert deps: %w", err)
	}
	defer stmt.Close()

	for _, dep := range deps {
		if _, err := stmt.Exec(snapshotID, dep.FromPackageImportPath, dep.ToPackageImportPath, boolInt(dep.IsLocal)); err != nil {
			return fmt.Errorf("insert dep %s -> %s: %w", dep.FromPackageImportPath, dep.ToPackageImportPath, err)
		}
	}
	return nil
}

func insertReferences(tx *sql.Tx, snapshotID int64, refs []codebase.ReferenceFact) error {
	stmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO refs (
			snapshot_id, from_package_import_path, from_symbol_key, to_symbol_key, file_path, line, col, kind
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare insert refs: %w", err)
	}
	defer stmt.Close()

	for _, ref := range refs {
		if _, err := stmt.Exec(snapshotID, ref.FromPackageImportPath, ref.FromSymbolKey, ref.ToSymbolKey, ref.FilePath, ref.Line, ref.Column, ref.Kind); err != nil {
			return fmt.Errorf("insert ref %s -> %s: %w", ref.FromSymbolKey, ref.ToSymbolKey, err)
		}
	}
	return nil
}

func insertCalls(tx *sql.Tx, snapshotID int64, calls []codebase.CallFact) error {
	stmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO call_edges (
			snapshot_id, caller_package_import_path, caller_symbol_key, callee_symbol_key, file_path, line, col, dispatch
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare insert calls: %w", err)
	}
	defer stmt.Close()

	for _, call := range calls {
		if _, err := stmt.Exec(snapshotID, call.CallerPackageImportPath, call.CallerSymbolKey, call.CalleeSymbolKey, call.FilePath, call.Line, call.Column, call.Dispatch); err != nil {
			return fmt.Errorf("insert call %s -> %s: %w", call.CallerSymbolKey, call.CalleeSymbolKey, err)
		}
	}
	return nil
}

func insertFlows(tx *sql.Tx, snapshotID int64, flows []codebase.FlowFact) error {
	stmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO flow_edges (
			snapshot_id, owner_package_import_path, owner_symbol_key, file_path, line, col, kind,
			source_kind, source_label, source_symbol_key, target_kind, target_label, target_symbol_key
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare insert flows: %w", err)
	}
	defer stmt.Close()

	for _, flow := range flows {
		if _, err := stmt.Exec(
			snapshotID,
			flow.OwnerPackageImportPath,
			flow.OwnerSymbolKey,
			flow.FilePath,
			flow.Line,
			flow.Column,
			flow.Kind,
			flow.SourceKind,
			flow.SourceLabel,
			flow.SourceSymbolKey,
			flow.TargetKind,
			flow.TargetLabel,
			flow.TargetSymbolKey,
		); err != nil {
			return fmt.Errorf("insert flow %s %s -> %s: %w", flow.OwnerSymbolKey, flow.SourceLabel, flow.TargetLabel, err)
		}
	}
	return nil
}

func insertTests(tx *sql.Tx, snapshotID int64, tests []codebase.TestFact) error {
	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO tests (snapshot_id, test_key, package_import_path, file_path, name, kind, line)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare insert tests: %w", err)
	}
	defer stmt.Close()

	for _, test := range tests {
		if _, err := stmt.Exec(snapshotID, test.TestKey, test.PackageImportPath, test.FilePath, test.Name, test.Kind, test.Line); err != nil {
			return fmt.Errorf("insert test %s: %w", test.TestKey, err)
		}
	}
	return nil
}

func insertTestLinks(tx *sql.Tx, snapshotID int64, links []codebase.TestLinkFact) error {
	stmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO test_links (snapshot_id, test_package_import_path, test_key, symbol_key, link_kind, confidence)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare insert test links: %w", err)
	}
	defer stmt.Close()

	for _, link := range links {
		if _, err := stmt.Exec(snapshotID, link.TestPackageImportPath, link.TestKey, link.SymbolKey, link.LinkKind, link.Confidence); err != nil {
			return fmt.Errorf("insert test link %s -> %s: %w", link.TestKey, link.SymbolKey, err)
		}
	}
	return nil
}

package app

import (
	"fmt"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

func (s *shellSession) showSearchCommand(args []string) error {
	mode, query, err := parseProjectSearchArgs(args, projectSearchModeAll)
	if err != nil {
		return s.printShellError(err)
	}
	return s.showSearch(mode, query)
}

func (s *shellSession) showGrepCommand(args []string) error {
	mode, query, err := parseProjectSearchArgs(args, projectSearchModeRegex)
	if err != nil {
		return s.printShellError(err)
	}
	return s.showSearch(mode, query)
}

func (s *shellSession) showSearch(mode, query string) error {
	results, err := loadProjectSearchResults(s.info.Root, s.store, mode, query, projectSearchLimit)
	if err != nil {
		return s.printShellError(err)
	}

	s.currentMode = "search"
	s.searchScope = results.Mode
	s.searchQuery = results.Query

	if err := s.beginScreen("Search"); err != nil {
		return err
	}
	return s.renderSearchResults(results)
}

func (s *shellSession) showSmartQuery(query string, pushHistory bool) error {
	results, err := loadProjectSearchResults(s.info.Root, s.store, projectSearchModeAll, query, projectSearchLimit)
	if err != nil {
		return s.printShellError(err)
	}
	if len(results.Symbols) == 1 && len(results.Text) == 0 && isStrongSymbolSearchResult(results.Symbols[0]) {
		return s.openSymbolKey(results.Symbols[0].SymbolKey, pushHistory)
	}

	s.currentMode = "search"
	s.searchScope = results.Mode
	s.searchQuery = results.Query
	if err := s.beginScreen("Search"); err != nil {
		return err
	}
	return s.renderSearchResults(results)
}

func (s *shellSession) renderSearchResults(results projectSearchResults) error {
	s.lastTargets = s.lastTargets[:0]

	if _, err := fmt.Fprintf(
		s.stdout,
		"%s\n  %s %q\n  %s %s\n  %s exact > name > prefix > contains > shape > fuzzy, then callers/refs/tests/rdeps/pkg importance\n  %s %s\n  %s symbols=%d  text=%d\n\n",
		s.palette.section("Search"),
		s.palette.label("Query:"),
		results.Query,
		s.palette.label("Mode:"),
		results.Mode,
		s.palette.label("Symbol matching:"),
		s.palette.label("Text matching:"),
		projectSearchTextDescription(results.Mode),
		s.palette.label("Hits:"),
		len(results.Symbols),
		len(results.Text),
	); err != nil {
		return err
	}

	if len(results.Symbols) == 0 && len(results.Text) == 0 {
		if _, err := fmt.Fprintf(
			s.stdout,
			"%s\n  %s\n\n",
			s.palette.section("Results"),
			s.palette.muted("No symbol or text matches found. Try `search text <query>`, `search symbol <query>`, or `grep <regex>`."),
		); err != nil {
			return err
		}
		return nil
	}

	index := 1
	if len(results.Symbols) > 0 {
		if _, err := fmt.Fprintf(s.stdout, "%s (%d)\n", s.palette.section("Symbol Matches"), len(results.Symbols)); err != nil {
			return err
		}
		for _, symbol := range results.Symbols {
			s.lastTargets = append(s.lastTargets, shellTarget{
				Kind:      "symbol",
				SymbolKey: symbol.SymbolKey,
				Label:     shortenQName(s.info.ModulePath, symbol.QName),
				FilePath:  symbol.FilePath,
				Line:      symbol.Line,
			})
			if _, err := fmt.Fprintf(
				s.stdout,
				"  [%d] %s %s  [%s]\n      %s\n      %s:%d\n      %s %s\n      %s %s\n      %s %s\n      %s %s\n",
				index,
				s.palette.kindBadge(symbol.Kind),
				shortenQName(s.info.ModulePath, symbol.QName),
				symbol.SearchKind,
				styleHumanSignature(s.palette, displaySignature(symbol)),
				symbol.FilePath,
				symbol.Line,
				s.palette.label("why:"),
				describeSymbolSearchWhy(symbol),
				s.palette.label("risk:"),
				symbolSearchRiskSummary(symbol),
				s.palette.label("next:"),
				strings.Join(searchResultNextCommands(symbol, index), " | "),
				s.palette.label("lenses:"),
				strings.Join(searchResultLensCommands(symbol, index), " | "),
			); err != nil {
				return err
			}
			index++
		}
		if _, err := fmt.Fprintln(s.stdout); err != nil {
			return err
		}
	}

	if len(results.Text) > 0 {
		if _, err := fmt.Fprintf(
			s.stdout,
			"%s (%d across %d files / %d packages)\n",
			s.palette.section("Text Matches"),
			len(results.Text),
			projectTextDisplayedFileCount(results.TextPackages),
			len(results.TextPackages),
		); err != nil {
			return err
		}
		for _, pkg := range results.TextPackages {
			name := pkg.PackageImportPath
			if name == "" {
				name = "(root)"
			} else {
				name = shortenQName(s.info.ModulePath, pkg.PackageImportPath)
			}
			if _, err := fmt.Fprintf(
				s.stdout,
				"  %s %s\n      %s %s\n",
				s.palette.label("package:"),
				name,
				s.palette.label("why:"),
				pkg.Why,
			); err != nil {
				return err
			}
			for _, file := range pkg.Files {
				if _, err := fmt.Fprintf(
					s.stdout,
					"    %s %s (%d/%d)\n      %s %s\n",
					s.palette.label("file:"),
					file.FilePath,
					len(file.Matches),
					file.TotalMatches,
					s.palette.label("why:"),
					file.Why,
				); err != nil {
					return err
				}
				for _, match := range file.Matches {
					s.lastTargets = append(s.lastTargets, shellTarget{
						Kind:     "location",
						Label:    match.Preview,
						FilePath: match.FilePath,
						Line:     match.Line,
					})
					if _, err := fmt.Fprintf(
						s.stdout,
						"      [%d] %s %s:%d:%d\n          %s\n          %s %s\n",
						index,
						s.palette.badge(strings.ToLower(match.MatchKind)),
						match.FilePath,
						match.Line,
						match.Column,
						match.Preview,
						s.palette.label("next:"),
						textResultNextCommands(index),
					); err != nil {
						return err
					}
					index++
				}
				if file.TotalMatches > len(file.Matches) {
					if _, err := fmt.Fprintf(
						s.stdout,
						"      %s and %d more in this file\n",
						s.palette.muted("..."),
						file.TotalMatches-len(file.Matches),
					); err != nil {
						return err
					}
				}
			}
		}
		if _, err := fmt.Fprintln(s.stdout); err != nil {
			return err
		}
	}

	_, err := fmt.Fprintf(
		s.stdout,
		"%s\n  %s use %s to open a match directly\n  %s use %s, %s, %s, %s, or %s to branch straight from a numbered symbol result\n  %s use %s for symbol-only search, %s for text substring search, or %s for regex search\n  %s use %s to list saved views for the current symbol or %s from a result row\n  %s typing plain text at the prompt now runs this smarter search flow\n\n",
		s.palette.section("Workflow"),
		s.palette.label("Open:"),
		s.palette.accent("open <n>"),
		s.palette.label("Investigate:"),
		s.palette.accent("file <n>"),
		s.palette.accent("callers <n>"),
		s.palette.accent("callees <n>"),
		s.palette.accent("tests <n>"),
		s.palette.accent("impact <n>"),
		s.palette.label("Refine:"),
		s.palette.accent("search symbol <query>"),
		s.palette.accent("search text <query>"),
		s.palette.accent("grep <regex>"),
		s.palette.label("Lenses:"),
		s.palette.accent("lens"),
		s.palette.accent("lens verify <n>"),
		s.palette.label("Tip:"),
	)
	return err
}

func projectSearchTextDescription(mode string) string {
	switch normalizeProjectSearchMode(mode) {
	case projectSearchModeRegex:
		return "regex across indexed files, grouped by package/file and ranked by file/package relevance"
	case projectSearchModeText:
		return "smart-case substring across indexed files, grouped by package/file and ranked by file/package relevance"
	default:
		return "smart-case substring across indexed files, grouped by package/file and ranked by file/package relevance"
	}
}

func isStrongSymbolSearchResult(symbol storage.SymbolMatch) bool {
	switch symbol.SearchKind {
	case "exact", "name", "prefix":
		return true
	default:
		return false
	}
}

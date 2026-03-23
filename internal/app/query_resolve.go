package app

import (
	"fmt"
	"io"

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
			return storage.SymbolMatch{}, false, err
		}
	}
	return storage.SymbolMatch{}, false, nil
}

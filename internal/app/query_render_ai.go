package app

import (
	"fmt"
	"io"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

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
		if value.Why != "" {
			if _, err := fmt.Fprintf(stdout, " why=%q", value.Why); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(stdout); err != nil {
			return err
		}
	}
	return renderMoreLine(stdout, len(values), limit)
}

func renderAIRankedFiles(stdout io.Writer, title string, values []storage.RankedFile, limit int, explain bool) error {
	if _, err := fmt.Fprintf(stdout, "%s=%d\n", title, len(values)); err != nil {
		return err
	}
	for _, value := range values[:min(limit, len(values))] {
		if _, err := fmt.Fprintf(
			stdout,
			"- file=%s calls=%d refs=%d tests=%d rdeps=%d relevant=%d score=%d",
			value.Summary.FilePath,
			value.Summary.InboundCallCount,
			value.Summary.InboundReferenceCount,
			value.Summary.RelatedTestCount,
			value.Summary.ReversePackageDeps,
			value.Summary.RelevantSymbolCount,
			value.Score,
		); err != nil {
			return err
		}
		if explain {
			if _, err := fmt.Fprintf(stdout, " why=%q", strings.Join(value.QualityWhy, " | ")); err != nil {
				return err
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
			"- q=%s kind=%s sig=%q decl=%s:%d ref=%s:%d why=%q\n",
			shortenQName(modulePath, value.Symbol.QName),
			value.Kind,
			displaySignature(value.Symbol),
			value.Symbol.FilePath,
			value.Symbol.Line,
			value.UseFilePath,
			value.UseLine,
			value.Why,
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
		if _, err := fmt.Fprintf(stdout, "- name=%s file=%s:%d rel=%s link=%s conf=%s why=%q\n", test.Name, test.FilePath, test.Line, test.Relation, test.LinkKind, test.Confidence, test.Why); err != nil {
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

func renderAIPackageReasons(stdout io.Writer, modulePath, title string, values []storage.ImpactPackageReason, limit int) error {
	if _, err := fmt.Fprintf(stdout, "%s=%d\n", title, len(values)); err != nil {
		return err
	}
	for _, value := range values[:min(limit, len(values))] {
		if _, err := fmt.Fprintf(stdout, "- package=%s why=%q\n", shortenQName(modulePath, value.PackageImportPath), strings.Join(value.Why, " | ")); err != nil {
			return err
		}
	}
	return renderMoreLine(stdout, len(values), limit)
}

func renderAIFileReasons(stdout io.Writer, title string, values []storage.ImpactFileReason, limit int) error {
	if _, err := fmt.Fprintf(stdout, "%s=%d\n", title, len(values)); err != nil {
		return err
	}
	for _, value := range values[:min(limit, len(values))] {
		if _, err := fmt.Fprintf(stdout, "- file=%s why=%q\n", value.FilePath, strings.Join(value.Why, " | ")); err != nil {
			return err
		}
	}
	return renderMoreLine(stdout, len(values), limit)
}

func renderAICoChangeItems(stdout io.Writer, title string, values []storage.CoChangeItem, limit int) error {
	if _, err := fmt.Fprintf(stdout, "%s=%d\n", title, len(values)); err != nil {
		return err
	}
	for _, value := range values[:min(limit, len(values))] {
		if _, err := fmt.Fprintf(stdout, "- %s\n", formatCoChangeItem(value)); err != nil {
			return err
		}
	}
	return renderMoreLine(stdout, len(values), limit)
}

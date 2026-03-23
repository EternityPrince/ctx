package app

import (
	"fmt"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

func (s *shellSession) listCallers(arg string) error {
	view, err := s.beginTargetSymbolScreen("Direct Callers", arg)
	if err != nil {
		return s.printShellError(err)
	}
	return s.renderRelatedList("Direct Callers", view.Callers)
}

func (s *shellSession) listCallees(arg string) error {
	view, err := s.beginTargetSymbolScreen("Direct Callees", arg)
	if err != nil {
		return s.printShellError(err)
	}
	return s.renderRelatedList("Direct Callees", view.Callees)
}

func (s *shellSession) listRefs(mode, arg string) error {
	view, err := s.targetSymbolView(arg, true)
	if err != nil {
		return s.printShellError(err)
	}

	switch mode {
	case "", "in":
		if err := s.beginScreen("References In"); err != nil {
			return s.printShellError(err)
		}
		if err := s.writeCurrentSymbolSummary(view); err != nil {
			return err
		}
		return s.renderRefList("References In", view.ReferencesIn)
	case "out":
		if err := s.beginScreen("References Out"); err != nil {
			return s.printShellError(err)
		}
		if err := s.writeCurrentSymbolSummary(view); err != nil {
			return err
		}
		return s.renderRefList("References Out", view.ReferencesOut)
	default:
		_, err := fmt.Fprintln(s.stdout, "Usage: refs [in|out]")
		return err
	}
}

func (s *shellSession) listTests(arg string) error {
	view, err := s.beginTargetSymbolScreen("Related Tests", arg)
	if err != nil {
		return s.printShellError(err)
	}
	guidance, err := buildSymbolTestGuidance(s.store, view, shellListLimit)
	if err != nil {
		return err
	}

	s.lastTargets = s.lastTargets[:0]
	if _, err := fmt.Fprintf(
		s.stdout,
		"%s\n  %s %s\n",
		s.palette.section("Test Signal"),
		s.palette.label("Coverage posture:"),
		guidance.Signal,
	); err != nil {
		return err
	}
	if guidance.ImportantWarning != "" {
		if _, err := fmt.Fprintf(s.stdout, "  %s %s\n", s.palette.label("Warning:"), guidance.ImportantWarning); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(s.stdout); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.stdout, "%s (%d)\n", s.palette.section("Tests To Read Before Change"), len(guidance.ReadBefore)); err != nil {
		return err
	}
	if len(guidance.ReadBefore) == 0 {
		_, err := fmt.Fprintln(s.stdout, "  none")
		return err
	}
	for idx, test := range guidance.ReadBefore[:min(shellListLimit, len(guidance.ReadBefore))] {
		s.lastTargets = append(s.lastTargets, shellTarget{
			Kind:     "location",
			Label:    test.Name,
			FilePath: test.FilePath,
			Line:     test.Line,
		})
		if _, err := fmt.Fprintf(
			s.stdout,
			"  [%d] %s %s\n      %s:%d %s\n",
			idx+1,
			s.palette.kindBadge(test.Kind),
			test.Name,
			test.FilePath,
			test.Line,
			formatTestRelationLabel(test),
		); err != nil {
			return err
		}
		if test.Why != "" {
			if _, err := fmt.Fprintf(s.stdout, "      %s %s\n", s.palette.label("why:"), test.Why); err != nil {
				return err
			}
		}
		if snippet := s.previewLine(test.FilePath, test.Line); snippet != "" {
			if _, err := fmt.Fprintf(s.stdout, "      %s\n", snippet); err != nil {
				return err
			}
		}
	}
	if len(guidance.ReadBefore) > shellListLimit {
		if _, err := fmt.Fprintf(s.stdout, "  %s and %d more\n", s.palette.muted("..."), len(guidance.ReadBefore)-shellListLimit); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(s.stdout)
	return err
}

func (s *shellSession) listRelated(arg string) error {
	view, err := s.beginTargetSymbolScreen("Related Symbols", arg)
	if err != nil {
		return s.printShellError(err)
	}

	s.lastTargets = s.lastTargets[:0]
	if _, err := fmt.Fprintf(s.stdout, "%s (%d)\n", s.palette.section("Related Symbols"), len(view.Siblings)); err != nil {
		return err
	}
	if len(view.Siblings) == 0 {
		_, err := fmt.Fprintln(s.stdout, "  none")
		return err
	}
	for idx, symbol := range view.Siblings[:min(shellListLimit, len(view.Siblings))] {
		s.lastTargets = append(s.lastTargets, shellTarget{
			Kind:      "symbol",
			SymbolKey: symbol.SymbolKey,
			Label:     shortenQName(s.info.ModulePath, symbol.QName),
			FilePath:  symbol.FilePath,
			Line:      symbol.Line,
		})
		if _, err := fmt.Fprintf(
			s.stdout,
			"  [%d] %s %s\n      %s\n      %s\n",
			idx+1,
			s.palette.kindBadge(symbol.Kind),
			shortenQName(s.info.ModulePath, symbol.QName),
			styleHumanSignature(s.palette, displaySignature(symbol)),
			symbolRangeDisplay(s.info.Root, symbol),
		); err != nil {
			return err
		}
	}
	if len(view.Siblings) > shellListLimit {
		if _, err := fmt.Fprintf(s.stdout, "  %s and %d more\n", s.palette.muted("..."), len(view.Siblings)-shellListLimit); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(s.stdout)
	return err
}

func (s *shellSession) renderRelatedList(title string, values []storage.RelatedSymbolView) error {
	s.lastTargets = s.lastTargets[:0]
	if _, err := fmt.Fprintf(s.stdout, "%s (%d)\n", s.palette.section(title), len(values)); err != nil {
		return err
	}
	if len(values) == 0 {
		_, err := fmt.Fprintln(s.stdout, "  none")
		return err
	}
	for idx, value := range values[:min(shellListLimit, len(values))] {
		s.lastTargets = append(s.lastTargets, shellTarget{
			Kind:      "symbol",
			SymbolKey: value.Symbol.SymbolKey,
			Label:     shortenQName(s.info.ModulePath, value.Symbol.QName),
			FilePath:  value.UseFilePath,
			Line:      value.UseLine,
		})
		if _, err := fmt.Fprintf(
			s.stdout,
			"  [%d] %s %s\n      %s\n      declared: %s\n      use: %s:%d",
			idx+1,
			s.palette.kindBadge(value.Symbol.Kind),
			shortenQName(s.info.ModulePath, value.Symbol.QName),
			styleHumanSignature(s.palette, displaySignature(value.Symbol)),
			symbolRangeDisplay(s.info.Root, value.Symbol),
			value.UseFilePath,
			value.UseLine,
		); err != nil {
			return err
		}
		if value.Relation != "" {
			if _, err := fmt.Fprintf(s.stdout, " [%s]", value.Relation); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(s.stdout); err != nil {
			return err
		}
		if snippet := s.previewLine(value.UseFilePath, value.UseLine); snippet != "" {
			if _, err := fmt.Fprintf(s.stdout, "      %s\n", snippet); err != nil {
				return err
			}
		}
	}
	if len(values) > shellListLimit {
		if _, err := fmt.Fprintf(s.stdout, "  %s and %d more\n", s.palette.muted("..."), len(values)-shellListLimit); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(s.stdout)
	return err
}

func (s *shellSession) renderRefList(title string, values []storage.RefView) error {
	s.lastTargets = s.lastTargets[:0]
	if _, err := fmt.Fprintf(s.stdout, "%s (%d)\n", s.palette.section(title), len(values)); err != nil {
		return err
	}
	if len(values) == 0 {
		_, err := fmt.Fprintln(s.stdout, "  none")
		return err
	}
	for idx, value := range values[:min(shellListLimit, len(values))] {
		s.lastTargets = append(s.lastTargets, shellTarget{
			Kind:      "symbol",
			SymbolKey: value.Symbol.SymbolKey,
			Label:     shortenQName(s.info.ModulePath, value.Symbol.QName),
			FilePath:  value.UseFilePath,
			Line:      value.UseLine,
		})
		if _, err := fmt.Fprintf(
			s.stdout,
			"  [%d] %s %s\n      %s\n      declared: %s\n      ref: %s:%d [%s]\n",
			idx+1,
			s.palette.kindBadge(value.Symbol.Kind),
			shortenQName(s.info.ModulePath, value.Symbol.QName),
			styleHumanSignature(s.palette, displaySignature(value.Symbol)),
			symbolRangeDisplay(s.info.Root, value.Symbol),
			value.UseFilePath,
			value.UseLine,
			value.Kind,
		); err != nil {
			return err
		}
		if snippet := s.previewLine(value.UseFilePath, value.UseLine); snippet != "" {
			if _, err := fmt.Fprintf(s.stdout, "      %s\n", snippet); err != nil {
				return err
			}
		}
	}
	if len(values) > shellListLimit {
		if _, err := fmt.Fprintf(s.stdout, "  %s and %d more\n", s.palette.muted("..."), len(values)-shellListLimit); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(s.stdout)
	return err
}

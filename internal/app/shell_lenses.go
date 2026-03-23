package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

type shellLens struct {
	Name        string
	Label       string
	Summary     string
	Action      string
	Recommended bool
}

type shellSuggestedStep struct {
	Target  shellTarget
	Label   string
	Summary string
	Score   int
}

func builtinLenses(view storage.SymbolView) []shellLens {
	lenses := []shellLens{
		{Name: "local", Label: "Lens local", Summary: "stay close to the file, siblings, and body", Action: "file"},
		{Name: "incoming", Label: "Lens incoming", Summary: "read upstream through direct callers", Action: "callers"},
		{Name: "outgoing", Label: "Lens outgoing", Summary: "read downstream through callees", Action: "callees"},
		{Name: "verify", Label: "Lens verify", Summary: "switch to related tests and verification context", Action: "tests"},
		{Name: "impact", Label: "Lens impact", Summary: "widen into impact and package blast radius", Action: "impact"},
		{Name: "refs", Label: "Lens refs", Summary: "inspect inbound references and type use sites", Action: "refs_in"},
		{Name: "neighborhood", Label: "Lens neighborhood", Summary: "scan nearby symbols in the same area", Action: "related"},
	}

	best := recommendedLensName(view)
	for idx := range lenses {
		lenses[idx].Recommended = lenses[idx].Name == best
	}
	return lenses
}

func recommendedLensName(view storage.SymbolView) string {
	best := "local"
	bestScore := -1
	candidates := map[string]int{
		"incoming": len(view.Callers)*6 + len(view.ReferencesIn)*2,
		"outgoing": len(view.Callees)*5 + len(view.ReferencesOut)*2,
		"verify":   len(view.Tests) * 8,
		"impact":   len(view.Callers)*5 + len(view.ReferencesIn)*3 + len(view.Tests)*4 + len(view.Package.ReverseDeps)*4,
		"refs":     len(view.ReferencesIn) * 5,
		"local":    len(view.Siblings)*3 + len(view.Tests)*2 + 1,
	}
	for name, score := range candidates {
		if score > bestScore {
			best = name
			bestScore = score
		}
	}
	return best
}

func suggestedStepsForView(view storage.SymbolView) []shellSuggestedStep {
	steps := []shellSuggestedStep{
		{
			Target:  shellTarget{Kind: "action", Action: "file"},
			Label:   "File Journey",
			Summary: "read the local neighborhood in the current file",
			Score:   40 + len(view.Siblings)*3 + len(view.Tests)*2,
		},
		{
			Target:  shellTarget{Kind: "action", Action: "callers"},
			Label:   fmt.Sprintf("Callers (%d)", len(view.Callers)),
			Summary: "move upstream to the symbols that reach this one",
			Score:   len(view.Callers) * 9,
		},
		{
			Target:  shellTarget{Kind: "action", Action: "callees"},
			Label:   fmt.Sprintf("Callees (%d)", len(view.Callees)),
			Summary: "move downstream to the symbols this one uses",
			Score:   len(view.Callees) * 7,
		},
		{
			Target:  shellTarget{Kind: "action", Action: "tests"},
			Label:   fmt.Sprintf("Tests (%d)", len(view.Tests)),
			Summary: "jump into verification context and nearby checks",
			Score:   len(view.Tests) * 10,
		},
		{
			Target:  shellTarget{Kind: "action", Action: "impact"},
			Label:   "Impact",
			Summary: "widen from this symbol into blast radius and caller packages",
			Score:   len(view.Callers)*6 + len(view.ReferencesIn)*4 + len(view.Tests)*5 + len(view.Package.ReverseDeps)*4 + 12,
		},
		{
			Target:  shellTarget{Kind: "action", Action: "related"},
			Label:   fmt.Sprintf("Related (%d)", len(view.Siblings)),
			Summary: "scan sibling entities and nearby routes in the same area",
			Score:   len(view.Siblings)*5 + 6,
		},
		{
			Target:  shellTarget{Kind: "action", Action: "refs_in"},
			Label:   fmt.Sprintf("Refs In (%d)", len(view.ReferencesIn)),
			Summary: "inspect incoming references, type uses, and mentions",
			Score:   len(view.ReferencesIn)*6 + 4,
		},
		{
			Target:  shellTarget{Kind: "action", Action: "refs_out"},
			Label:   fmt.Sprintf("Refs Out (%d)", len(view.ReferencesOut)),
			Summary: "inspect outbound references from this entity",
			Score:   len(view.ReferencesOut)*4 + 2,
		},
		{
			Target:  shellTarget{Kind: "action", Action: "source"},
			Label:   "Source / Body",
			Summary: "open a wider excerpt before committing to a deeper jump",
			Score:   8,
		},
		{
			Target:  shellTarget{Kind: "action", Action: "entity_report"},
			Label:   "Entity Report",
			Summary: "switch to the denser report-style view for this symbol",
			Score:   10,
		},
	}

	sort.Slice(steps, func(i, j int) bool {
		if steps[i].Score != steps[j].Score {
			return steps[i].Score > steps[j].Score
		}
		return steps[i].Label < steps[j].Label
	})
	return steps
}

func searchResultNextCommands(symbol storage.SymbolMatch, index int) []string {
	type candidate struct {
		cmd   string
		score int
	}

	candidates := []candidate{
		{cmd: fmt.Sprintf("file %d", index), score: 20 + symbol.PackageImportance},
		{cmd: fmt.Sprintf("impact %d", index), score: 18 + symbol.CallerCount*4 + symbol.ReferenceCount*2 + symbol.TestCount*3 + symbol.ReversePackageDeps*3},
	}
	if symbol.CallerCount > 0 {
		candidates = append(candidates, candidate{cmd: fmt.Sprintf("callers %d", index), score: 30 + symbol.CallerCount*5})
	}
	if symbol.CalleeCount > 0 {
		candidates = append(candidates, candidate{cmd: fmt.Sprintf("callees %d", index), score: 24 + symbol.CalleeCount*4})
	}
	if symbol.TestCount > 0 || symbol.CallerCount > 0 || symbol.CalleeCount > 0 {
		score := 14 + symbol.TestCount*6 + symbol.CallerCount*2 + symbol.CalleeCount
		candidates = append(candidates, candidate{cmd: fmt.Sprintf("tests %d", index), score: score})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].cmd < candidates[j].cmd
	})

	seen := map[string]struct{}{}
	commands := []string{fmt.Sprintf("open %d", index)}
	for _, candidate := range candidates {
		if _, ok := seen[candidate.cmd]; ok {
			continue
		}
		seen[candidate.cmd] = struct{}{}
		commands = append(commands, candidate.cmd)
		if len(commands) >= 6 {
			break
		}
	}
	return commands
}

func searchResultLensCommands(symbol storage.SymbolMatch, index int) []string {
	commands := []string{fmt.Sprintf("lens local %d", index)}
	if symbol.CallerCount > 0 {
		commands = append(commands, fmt.Sprintf("lens incoming %d", index))
	}
	if symbol.TestCount > 0 || symbol.CallerCount > 0 || symbol.CalleeCount > 0 {
		commands = append(commands, fmt.Sprintf("lens verify %d", index))
	}
	if symbol.CalleeCount > 0 {
		commands = append(commands, fmt.Sprintf("lens outgoing %d", index))
	}
	commands = append(commands, fmt.Sprintf("lens impact %d", index))
	if len(commands) > 3 {
		commands = commands[:3]
	}
	return commands
}

func textResultNextCommands(index int) string {
	return strings.Join([]string{
		fmt.Sprintf("open %d", index),
		fmt.Sprintf("file %d", index),
		fmt.Sprintf("source %d", index),
	}, " | ")
}

func parseLensArgs(args []string) (string, string) {
	if len(args) == 0 {
		return "", ""
	}
	if len(args) == 1 {
		return strings.ToLower(strings.TrimSpace(args[0])), ""
	}
	return strings.ToLower(strings.TrimSpace(args[0])), strings.TrimSpace(args[1])
}

func (s *shellSession) showLens(args []string) error {
	name, targetArg := parseLensArgs(args)
	if name != "" && targetArg == "" {
		if _, ok := s.targetFromArg(name); ok {
			targetArg = name
			name = ""
		}
	}

	view, err := s.targetSymbolView(targetArg, true)
	if err != nil {
		return s.printShellError(err)
	}
	if name != "" {
		return s.applyLens(name, targetArg)
	}

	if err := s.beginScreen("Saved Views / Named Lenses"); err != nil {
		return err
	}
	if err := s.writeCurrentSymbolSummary(view); err != nil {
		return err
	}

	lenses := builtinLenses(view)
	s.lastTargets = s.lastTargets[:0]
	if _, err := fmt.Fprintf(s.stdout, "%s\n", s.palette.section("Saved Views / Named Lenses")); err != nil {
		return err
	}
	for _, lens := range lenses {
		s.lastTargets = append(s.lastTargets, shellTarget{Kind: "action", Action: "lens:" + lens.Name})
		recommended := ""
		if lens.Recommended {
			recommended = "  [" + s.palette.badge("recommended") + "]"
		}
		if _, err := fmt.Fprintf(
			s.stdout,
			"  [%d] %-18s %s%s\n",
			len(s.lastTargets),
			lens.Label,
			lens.Summary,
			recommended,
		); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(
		s.stdout,
		"\n%s use %s, %s, %s, %s, %s, or %s\n\n",
		s.palette.label("Run directly:"),
		s.palette.accent("lens local"),
		s.palette.accent("lens incoming"),
		s.palette.accent("lens outgoing"),
		s.palette.accent("lens verify"),
		s.palette.accent("lens impact"),
		s.palette.accent("lens refs"),
	)
	return err
}

func (s *shellSession) applyLens(name, targetArg string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if _, err := s.targetSymbolView(targetArg, true); err != nil {
		return s.printShellError(err)
	}

	switch name {
	case "local", "file":
		return s.showFileJourney("")
	case "incoming", "upstream", "callers":
		return s.listCallers("")
	case "outgoing", "downstream", "callees":
		return s.listCallees("")
	case "verify", "tests", "test":
		return s.listTests("")
	case "impact", "blast":
		return s.showImpact("")
	case "refs", "references":
		return s.listRefs("in", "")
	case "neighborhood", "related", "nearby":
		return s.listRelated("")
	default:
		return s.printShellError(fmt.Errorf("Unknown lens %q. Use `lens` to list available named views.", name))
	}
}

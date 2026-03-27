package app

import (
	"fmt"
	"strconv"
	"strings"
)

func (s *shellSession) handle(line string) error {
	_, err := s.handleWithStop(line)
	return err
}

func (s *shellSession) handleWithStop(line string) (bool, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false, nil
	}

	cmd := strings.ToLower(fields[0])
	args := fields[1:]

	switch cmd {
	case "help", "?":
		return false, s.printHelp()
	case "quit", "exit", "q":
		return true, nil
	case "home", "main":
		s.resetToHome()
		return false, s.showLanding()
	case "tree":
		return false, s.showTreeCommand(args)
	case "clear":
		if s.walkActive {
			return false, s.showWalk(nil)
		}
		if s.currentMode == "tree" {
			return false, s.showTreeCommand(nil)
		}
		if s.currentMode == "search" && s.searchQuery != "" {
			return false, s.showSearch(s.searchScope, s.searchQuery)
		}
		if s.currentMode == "file" && s.currentFile != "" {
			return false, s.showFileJourney(s.currentFile)
		}
		if s.currentKey != "" {
			return false, s.openSymbolKey(s.currentKey, false)
		}
		return false, s.showLanding()
	case "symbol", "s":
		if len(args) == 0 {
			if s.currentKey == "" {
				_, err := fmt.Fprintln(s.stdout, "No current symbol. Type a symbol name first.")
				return false, err
			}
			return false, s.openSymbolKey(s.currentKey, false)
		}
		return false, s.showSymbol(strings.Join(args, " "), true)
	case "search", "find":
		return false, s.showSearchCommand(args)
	case "grep":
		return false, s.showGrepCommand(args)
	case "impact", "i":
		query := strings.Join(args, " ")
		if query == "" {
			return false, s.showImpact("")
		}
		return false, s.showImpact(query)
	case "callers":
		arg := ""
		if len(args) > 0 {
			arg = args[0]
		}
		return false, s.listCallers(arg)
	case "callees":
		arg := ""
		if len(args) > 0 {
			arg = args[0]
		}
		return false, s.listCallees(arg)
	case "refs":
		mode := ""
		arg := ""
		if len(args) > 0 {
			mode = strings.ToLower(args[0])
			if mode != "in" && mode != "out" {
				arg = args[0]
				mode = ""
			}
		}
		if len(args) > 1 {
			arg = args[1]
		}
		return false, s.listRefs(mode, arg)
	case "tests":
		arg := ""
		if len(args) > 0 {
			arg = args[0]
		}
		return false, s.listTests(arg)
	case "related", "siblings":
		arg := ""
		if len(args) > 0 {
			arg = args[0]
		}
		return false, s.listRelated(arg)
	case "lens", "lenses":
		return false, s.showLens(args)
	case "source", "src", "body":
		if len(args) > 0 && strings.EqualFold(args[0], "full") {
			if len(args) == 1 {
				return false, s.showFullCurrentEntity()
			}
			if len(args) == 2 {
				return false, s.showFullTarget(args[1])
			}
			return false, s.printShellError(fmt.Errorf("Usage: source [n] | source full [n]"))
		}
		if len(args) == 1 {
			return false, s.showSourceTarget(args[0])
		}
		if len(args) > 1 {
			return false, s.printShellError(fmt.Errorf("Usage: source [n] | source full [n]"))
		}
		return false, s.showSource()
	case "file", "files":
		query := ""
		if len(args) > 0 {
			query = strings.Join(args, " ")
		}
		return false, s.showFileJourney(query)
	case "walk":
		return false, s.showWalk(args)
	case "copy", "y":
		arg := ""
		if len(args) > 0 {
			arg = args[0]
		}
		return false, s.copyCurrent(arg)
	case "next":
		if s.walkActive {
			return false, s.walkMove(1)
		}
		if s.currentMode == "tree" {
			return false, s.showTreeCommand([]string{"next"})
		}
		return false, s.printShellError(fmt.Errorf("`next` is available in walk mode and tree mode."))
	case "prev":
		if s.walkActive {
			return false, s.walkMove(-1)
		}
		if s.currentMode == "tree" {
			return false, s.showTreeCommand([]string{"prev"})
		}
		return false, s.printShellError(fmt.Errorf("`prev` is available in walk mode and tree mode."))
	case "leave", "leavewalk", "unwalk":
		if s.walkActive {
			return false, s.exitWalk()
		}
		return false, s.printShellError(fmt.Errorf("Walk mode is not active."))
	case "full":
		if len(args) == 0 {
			return false, s.showFullCurrentEntity()
		}
		if len(args) == 1 {
			return false, s.showFullTarget(args[0])
		}
		return false, s.printShellError(fmt.Errorf("Usage: full [n]"))
	case "open-current":
		if s.walkActive {
			return false, s.openWalkCurrent()
		}
		return false, s.printShellError(fmt.Errorf("`open-current` is available in walk mode."))
	case "open", "o":
		if len(args) != 1 {
			_, err := fmt.Fprintln(s.stdout, "Usage: open <n>")
			return false, err
		}
		return false, s.openIndex(args[0])
	case "back", "b":
		return false, s.back()
	case "forward", "f":
		return false, s.forward()
	case "report":
		return false, s.showContextReport(args)
	case "status":
		return false, s.showStatus()
	case "menu", "m":
		return false, s.showAutoDrive()
	case "auto", "autodrive":
		return false, s.showAutoDrive()
	default:
		if _, err := strconv.Atoi(cmd); err == nil && len(args) == 0 {
			return false, s.openIndex(cmd)
		}
		return false, s.showSmartQuery(line, true)
	}
	return false, nil
}

func (s *shellSession) printHelp() error {
	if err := s.beginScreen("Help"); err != nil {
		return err
	}
	_, err := fmt.Fprintf(
		s.stdout,
		"%s\n  home | main                return to the main menu\n  tree [dirs|hot|next|prev|page <n>|up|root]\n                             browse files, directory summaries with file-type counts, or hot files\n  symbol <query> | s <query> open a symbol journey card\n  search [symbol|text|regex] <query>\n                             fuzzy-search symbols, substring-search text, or run regex across indexed files\n  find <query>               run the combined symbol + text search\n  grep <regex>               search indexed files with a regex\n  file [path|n]              travel through symbols in a file by path or current-list number\n  walk [path|n|next|prev|open|full|exit]\n                             step through entities in the current file\n  menu | m                   show numbered next-step actions for current symbol\n  callers [n]                show direct callers for the current symbol or a listed search item\n  callees [n]                show direct callees for the current symbol or a listed search item\n  refs [in|out] [n]          show references with use-site snippets\n  tests [n]                  show related tests\n  related [n]                show sibling/nearby symbols\n  impact [query|n]           show impact summary for current, named, or listed symbol\n  lens [name] [n]            open a named reading lens like local/incoming/outgoing/verify/impact\n  source [n] | body [n]      show source/body for the current or listed target\n  source full [n] | full [n] show the full current or listed entity body\n                             when you are on a file card, plain full prints the whole file\n  copy [n]                   copy the current or listed target context\n  report [project|risky|seams|hotspots|low-tested|changed-since|entity|file]\n                             show a project report, deterministic report slice, current symbol, or current file report\n  status                     snapshot status\n  open <n>                   open item from the last numbered list\n  back / forward             navigate symbol history\n  clear                      redraw the current screen cleanly\n  quit                       exit the shell\n\n%s\n  after a symbol card, type 1..N to open the suggested next step\n  after a list, use file/callers/callees/tests/impact/lens <name> <n> to branch directly from a search result\n  after a list, type a number to open that item directly\n  after a tree screen, numbering is local to the current window\n  use tree dirs to choose an area first, inspect its file-type mix, open <n> to zoom in, and tree up/root to move back out\n  use tree next / tree prev / tree page <n> inside the current tree view\n  inside walk mode, use next / prev / full / open-current / leave\n  after a file journey, use source <n> to peek a body before opening it or full for the whole file\n  plain text at the prompt now runs the smart search flow instead of exact-symbol-only lookup\n  use report risky / seams / hotspots / low-tested / changed-since for deterministic project slices\n  use report with no args to get a report for the thing you are currently in\n\n",
		s.palette.section("Shell Help"),
		s.palette.section("Number Flow"),
	)
	return err
}

func (s *shellSession) runAction(action string) error {
	if after, ok := strings.CutPrefix(action, "lens:"); ok {
		return s.applyLens(after, "")
	}
	switch action {
	case "file":
		return s.showFileJourney("")
	case "callers":
		return s.listCallers("")
	case "callees":
		return s.listCallees("")
	case "refs_in":
		return s.listRefs("in", "")
	case "refs_out":
		return s.listRefs("out", "")
	case "tests":
		return s.listTests("")
	case "related":
		return s.listRelated("")
	case "impact":
		return s.showImpact("")
	case "source":
		return s.showSource()
	case "copy":
		return s.copyCurrent("")
	case "entity_report":
		return s.showEntityReport("")
	case "tree":
		return s.showTree()
	case "home":
		s.resetToHome()
		return s.showLanding()
	default:
		_, err := fmt.Fprintf(s.stdout, "Unknown action %q\n\n", action)
		return err
	}
}

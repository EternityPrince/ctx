package app

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/cli"
	"github.com/vladimirkasterin/ctx/internal/project"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

type shellSession struct {
	info            project.Info
	store           *storage.Store
	stdout          io.Writer
	palette         palette
	composition     projectComposition
	changedNow      int
	batPath         string
	currentKey      string
	currentQName    string
	currentMode     string
	currentFile     string
	walkActive      bool
	walkSymbols     []storage.SymbolMatch
	walkIndex       int
	treePage        int
	treeMode        string
	treeScope       string
	searchQuery     string
	searchScope     string
	history         []string
	historyIndex    int
	trail           []shellTrailEntry
	trailIndex      int
	lastTargets     []shellTarget
	riskHotScores   map[string]int
	riskRecentFiles map[string]struct{}
}

type shellTrailEntry struct {
	Label string
}

type shellTarget struct {
	Kind      string
	Action    string
	SymbolKey string
	Label     string
	FilePath  string
	Line      int
}

const shellListLimit = 12

func runShellREPL(command cli.Command, stdout io.Writer) error {
	state, err := openPreparedProjectState(command)
	if err != nil {
		return err
	}
	defer state.Close()

	changedNow := projectService.ChangedNow(state)
	status, err := state.Store.Status(changedNow)
	if err != nil {
		return err
	}
	if !status.HasSnapshot {
		_, err := fmt.Fprintf(stdout, "No index snapshot for %s. Run `ctx index %s` first.\n", state.Info.ModulePath, state.Info.Root)
		return err
	}

	session := &shellSession{
		info:         state.Info,
		store:        state.Store,
		stdout:       stdout,
		palette:      newPalette(),
		composition:  summarizeProjectComposition(state.Scanned),
		changedNow:   changedNow,
		batPath:      detectBatPath(),
		historyIndex: -1,
		trailIndex:   -1,
	}

	return session.run(command.Query)
}

func (s *shellSession) run(initialQuery string) error {
	if initialQuery != "" {
		if err := s.showSmartQuery(strings.TrimSpace(initialQuery), true); err != nil {
			return err
		}
	} else {
		if err := s.showLanding(); err != nil {
			return err
		}
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		if _, err := fmt.Fprint(s.stdout, s.prompt()); err != nil {
			return err
		}

		line, err := reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			if err == io.EOF {
				_, writeErr := fmt.Fprintln(s.stdout)
				return writeErr
			}
			return err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if err == io.EOF {
			if err := s.handle(line); err != nil {
				return err
			}
			_, writeErr := fmt.Fprintln(s.stdout)
			return writeErr
		}

		stop, err := s.handleWithStop(line)
		if err != nil {
			return err
		}
		if stop {
			return nil
		}
	}
}

func (s *shellSession) prompt() string {
	switch s.currentMode {
	case "landing":
		return "ctx:home> "
	case "tree":
		return "ctx:tree> "
	case "report":
		return "ctx:report> "
	case "status":
		return "ctx:status> "
	case "search":
		return "ctx:search> "
	case "walk":
		if s.walkActive && len(s.walkSymbols) > 0 && s.walkIndex >= 0 && s.walkIndex < len(s.walkSymbols) {
			return fmt.Sprintf("ctx:walk:%d/%d> ", s.walkIndex+1, len(s.walkSymbols))
		}
		return "ctx:walk> "
	}
	if s.currentMode == "file" && s.currentFile != "" {
		return fmt.Sprintf("ctx:file:%s> ", shortenQName(s.info.ModulePath, s.currentFile))
	}
	if s.currentMode == "symbol" && s.currentQName != "" {
		return fmt.Sprintf("ctx:%s> ", shortenQName(s.info.ModulePath, s.currentQName))
	}
	return "ctx> "
}

func (s *shellSession) beginScreen(title string) error {
	if _, err := fmt.Fprint(s.stdout, "\x1b[2J\x1b[H"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.stdout, "%s\n%s\n", s.palette.rule("CTX Shell"), s.palette.title("CTX Shell · "+title)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.stdout, "%s %s\n%s %s\n", s.palette.label("Project:"), s.info.Root, s.palette.label("Module:"), s.info.ModulePath); err != nil {
		return err
	}
	if s.currentMode == "symbol" && s.currentQName != "" {
		if _, err := fmt.Fprintf(s.stdout, "%s %s\n", s.palette.label("Focus:"), shortenQName(s.info.ModulePath, s.currentQName)); err != nil {
			return err
		}
	}
	if s.currentMode == "file" && s.currentFile != "" {
		if _, err := fmt.Fprintf(s.stdout, "%s %s\n", s.palette.label("File:"), s.currentFile); err != nil {
			return err
		}
	}
	if s.currentMode == "tree" {
		mode := s.treeMode
		if mode == "" {
			mode = shellTreeModeFiles
		}
		scope := s.treeScope
		if scope == "" {
			scope = "."
		}
		if _, err := fmt.Fprintf(s.stdout, "%s %s  %s %s\n", s.palette.label("Tree:"), mode, s.palette.label("Scope:"), scope); err != nil {
			return err
		}
	}
	if s.changedNow > 0 {
		if _, err := fmt.Fprintf(s.stdout, "%s %d file(s) changed since the last snapshot. Use `ctx update %s` when ready.\n", s.palette.label("Index note:"), s.changedNow, s.info.Root); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(
		s.stdout,
		"%s home, tree [dirs|hot|next|prev|page <n>|up|root], symbol <name>, search [symbol|text|regex] <query>, find <query>, grep <regex>, file [path|n], walk [path|n|next|prev], callers [n], callees [n], refs [in|out] [n], tests [n], related [n], impact [query|n], lens [name] [n], source, full, copy\n",
		s.palette.label("Flow:"),
	); err != nil {
		return err
	}
	_, err := fmt.Fprintf(
		s.stdout,
		"%s report [project|risky|seams|hotspots|low-tested|changed-since|entity|file], open <n>, back, forward, clear, quit\n%s\n\n",
		s.palette.label("Control:"),
		s.palette.rule(""),
	)
	if err != nil {
		return err
	}
	s.rememberTrail(title)
	if _, err := fmt.Fprintf(s.stdout, "%s %s\n%s\n\n", s.palette.label("Trail:"), s.renderTrail(6), s.palette.rule("")); err != nil {
		return err
	}
	return err
}

func (s *shellSession) writeCurrentSymbolSummary(view storage.SymbolView) error {
	impact := impactLabel(len(view.Callers), len(view.ReferencesIn), len(view.Tests), len(view.Package.ReverseDeps))
	_, err := fmt.Fprintf(
		s.stdout,
		"%s %s %s\n%s\n%s %s\n\n",
		s.palette.kindBadge(view.Symbol.Kind),
		s.palette.accent(shortenQName(s.info.ModulePath, view.Symbol.QName)),
		s.palette.badge(impact),
		styleHumanSignature(s.palette, displaySignature(view.Symbol)),
		s.palette.label("Declared:"),
		symbolRangeDisplay(s.info.Root, view.Symbol),
	)
	return err
}

func (s *shellSession) showLanding() error {
	s.currentMode = "landing"
	s.currentFile = ""
	if err := s.beginScreen("Project Entry"); err != nil {
		return err
	}
	status, err := s.store.Status(s.changedNow)
	if err != nil {
		return err
	}
	view, err := s.store.LoadReportView(4)
	if err != nil {
		return err
	}
	s.lastTargets = s.lastTargets[:0]
	if _, err := fmt.Fprintf(
		s.stdout,
		"%s\n  %s %d (%s)\n  %s packages=%d files=%d symbols=%d refs=%d calls=%d tests=%d\n  %s %d\n  %s snapshots=%d limit=%s total=%s avg=%s\n\n",
		s.palette.section("Snapshot"),
		s.palette.label("Current:"),
		status.Current.ID,
		status.Current.CreatedAt.Format(timeFormat),
		s.palette.label("Inventory:"),
		status.Current.TotalPackages,
		status.Current.TotalFiles,
		status.Current.TotalSymbols,
		status.Current.TotalRefs,
		status.Current.TotalCalls,
		status.Current.TotalTests,
		s.palette.label("Changed now:"),
		status.ChangedNow,
		s.palette.label("Storage:"),
		status.Storage.SnapshotCount,
		formatSnapshotLimit(status.Storage.SnapshotLimit),
		shellHumanSize(status.Storage.TotalSizeBytes),
		shellHumanSize(status.Storage.AvgSnapshotSizeBytes),
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		s.stdout,
		"%s\n  1. type a symbol name or %s to start from intent, not exact spelling\n  2. type %s to choose an area first, then %s to zoom in\n  3. use %s or %s after opening a symbol or file\n  4. type %s or %s in lists to open things directly\n  5. use %s to come back here anytime\n\n",
		s.palette.section("Quick Start"),
		s.palette.accent("search Login"),
		s.palette.accent("tree dirs"),
		s.palette.accent("open <n>"),
		s.palette.accent("report"),
		s.palette.accent("file <path|n>"),
		s.palette.accent("1..N"),
		s.palette.accent("open <n>"),
		s.palette.accent("home"),
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.stdout, "%s\n", s.palette.section("Project Entry")); err != nil {
		return err
	}
	if len(view.TopPackages) > 0 {
		if _, err := fmt.Fprintf(s.stdout, "  %s ", s.palette.label("Top packages:")); err != nil {
			return err
		}
		for idx, item := range view.TopPackages {
			if idx > 0 {
				if _, err := fmt.Fprint(s.stdout, ", "); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprint(s.stdout, shortenQName(s.info.ModulePath, item.Summary.ImportPath)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(s.stdout); err != nil {
			return err
		}
	}
	if len(view.TopFunctions) > 0 {
		if _, err := fmt.Fprintf(s.stdout, "  %s\n", s.palette.label("Try one of these symbols:")); err != nil {
			return err
		}
		for idx, item := range view.TopFunctions {
			s.lastTargets = append(s.lastTargets, shellTarget{
				Kind:      "symbol",
				SymbolKey: item.Symbol.SymbolKey,
				Label:     shortenQName(s.info.ModulePath, item.Symbol.QName),
				FilePath:  item.Symbol.FilePath,
				Line:      item.Symbol.Line,
			})
			if _, err := fmt.Fprintf(s.stdout, "    %d. %s %s\n", idx+1, s.palette.kindBadge(item.Symbol.Kind), shortenQName(s.info.ModulePath, item.Symbol.QName)); err != nil {
				return err
			}
		}
	}
	hotFiles := s.landingHotFiles(view, 3)
	if len(hotFiles) > 0 {
		if _, err := fmt.Fprintf(s.stdout, "\n  %s\n", s.palette.label("Or travel by file:")); err != nil {
			return err
		}
		for _, target := range hotFiles {
			s.lastTargets = append(s.lastTargets, target)
			if _, err := fmt.Fprintf(s.stdout, "    %d. %s %s\n", len(s.lastTargets), s.palette.section("FILE"), target.FilePath); err != nil {
				return err
			}
		}
	}
	_, err = fmt.Fprintf(s.stdout, "\n%s type a symbol name, `tree`, `file`, `file <path>`, `file <n>`, or just press 1..%d to jump in.\n\n", s.palette.label("Start:"), len(s.lastTargets))
	return err
}

func (s *shellSession) showStatus() error {
	status, err := s.store.Status(s.changedNow)
	if err != nil {
		return err
	}
	s.currentMode = "status"
	if err := s.beginScreen("Status"); err != nil {
		return err
	}
	if !status.HasSnapshot {
		_, err := fmt.Fprintf(s.stdout, "Root: %s\nModule: %s\nSnapshot: none\nChanged now: %d\nStorage: snapshots=%d limit=%s total=%s avg=%s\nDB: %s\n\n", s.info.Root, s.info.ModulePath, s.changedNow, status.Storage.SnapshotCount, formatSnapshotLimit(status.Storage.SnapshotLimit), shellHumanSize(status.Storage.TotalSizeBytes), shellHumanSize(status.Storage.AvgSnapshotSizeBytes), status.Storage.CurrentDBPath)
		return err
	}
	_, err = fmt.Fprintf(
		s.stdout,
		"%s\n  Root: %s\n  Module: %s\n  Snapshot: %d (%s)\n  Inventory: packages=%d files=%d symbols=%d refs=%d calls=%d tests=%d\n  Changed now: %d\n  Storage: snapshots=%d limit=%s total=%s avg=%s\n  DB: %s\n\n",
		s.palette.section("Status"),
		status.RootPath,
		status.ModulePath,
		status.Current.ID,
		status.Current.CreatedAt.Format(timeFormat),
		status.Current.TotalPackages,
		status.Current.TotalFiles,
		status.Current.TotalSymbols,
		status.Current.TotalRefs,
		status.Current.TotalCalls,
		status.Current.TotalTests,
		status.ChangedNow,
		status.Storage.SnapshotCount,
		formatSnapshotLimit(status.Storage.SnapshotLimit),
		shellHumanSize(status.Storage.TotalSizeBytes),
		shellHumanSize(status.Storage.AvgSnapshotSizeBytes),
		status.Storage.CurrentDBPath,
	)
	s.lastTargets = nil
	return err
}

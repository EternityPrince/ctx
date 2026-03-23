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

const (
	tuiModeLanding = "landing"
	tuiModeSearch  = "search"
	tuiModeSymbol  = "symbol"
	tuiModeFile    = "file"
)

type tuiHistoryEntry struct {
	Mode      string
	SymbolKey string
	FilePath  string
	Line      int
}

type tuiSection struct {
	Title string
	Items []tuiItem
	Empty string
}

type tuiItem struct {
	Kind        string
	Title       string
	Subtitle    string
	Detail      string
	Preview     string
	SymbolKey   string
	Symbol      storage.SymbolMatch
	FilePath    string
	Line        int
	CopyText    string
	Relation    string
	Importance  string
	Score       int
	OpenInFile  bool
	PackageName string
}

type tuiModel struct {
	info         project.Info
	store        *storage.Store
	stdout       *os.File
	palette      palette
	changedNow   int
	status       storage.ProjectStatus
	report       storage.ReportView
	initialQuery string
	mode         string
	current      storage.SymbolView
	currentFile  string
	currentLine  int
	fileSymbols  []storage.SymbolMatch
	searchQuery  string
	searchItems  []storage.SymbolMatch
	sections     []tuiSection
	sectionIndex int
	itemIndex    int
	history      []tuiHistoryEntry
	historyIndex int
	showSource   bool
	message      string
	inputMode    string
	inputValue   string
}

func runShell(command cli.Command, stdout io.Writer) error {
	if os.Getenv("CTX_EXPERIMENTAL_TUI") != "1" {
		return runShellREPL(command, stdout)
	}
	if !shouldUseTUI(stdout) {
		tuiDebugf("tui disabled: stdout is not an interactive terminal")
		return runShellREPL(command, stdout)
	}

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

	report, err := state.Store.LoadReportView(8)
	if err != nil {
		return err
	}

	file := stdout.(*os.File)
	model := &tuiModel{
		info:         state.Info,
		store:        state.Store,
		stdout:       file,
		palette:      newPalette(),
		changedNow:   changedNow,
		status:       status,
		report:       report,
		initialQuery: command.Query,
		mode:         tuiModeLanding,
		historyIndex: -1,
	}
	if err := model.loadLanding(); err != nil {
		return err
	}
	if strings.TrimSpace(command.Query) != "" {
		if err := model.search(strings.TrimSpace(command.Query), true); err != nil {
			return err
		}
	}

	return model.run()
}

func (m *tuiModel) run() error {
	term, err := enterTerminal(m.stdout)
	if err != nil {
		tuiDebugf("enter terminal failed: %v", err)
		return runShellREPL(cli.Command{
			Name:  "shell",
			Root:  m.info.Root,
			Query: m.initialQuery,
		}, m.stdout)
	}
	defer term.Restore()

	reader := bufio.NewReader(os.Stdin)
	for {
		if err := m.draw(); err != nil {
			return err
		}

		key, err := readTUIKey(reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		stop, err := m.handleKey(key)
		if err != nil {
			return err
		}
		if stop {
			return nil
		}
	}
}

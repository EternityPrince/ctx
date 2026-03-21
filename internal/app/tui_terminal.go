package app

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type terminalState struct {
	stdout *os.File
	saved  string
	active bool
}

type tuiKey struct {
	Name string
	Rune rune
}

func shouldUseTUI(stdout io.Writer) bool {
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	_, ok := stdout.(*os.File)
	return ok
}

func enterTerminal(stdout *os.File) (*terminalState, error) {
	state, err := currentTTYState()
	if err != nil {
		return nil, err
	}
	if err := runStty("raw", "-echo"); err != nil {
		return nil, err
	}
	if _, err := fmt.Fprint(stdout, "\x1b[?1049h\x1b[2J\x1b[H\x1b[?25l"); err != nil {
		_ = restoreTTY(state)
		return nil, err
	}
	return &terminalState{
		stdout: stdout,
		saved:  state,
		active: true,
	}, nil
}

func (t *terminalState) Restore() {
	if t == nil {
		return
	}
	if t.active && t.stdout != nil {
		_, _ = fmt.Fprint(t.stdout, "\x1b[0m\x1b[2J\x1b[H\x1b[?1049l\x1b[?25h")
	}
	if t.saved != "" {
		_ = restoreTTY(t.saved)
	}
	t.active = false
}

func currentTTYState() (string, error) {
	cmd := exec.Command("stty", "-g")
	cmd.Stdin = os.Stdin
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("capture tty state: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func restoreTTY(state string) error {
	if strings.TrimSpace(state) == "" {
		return nil
	}
	return runStty(state)
}

func runStty(args ...string) error {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run stty %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func terminalSize() (int, int) {
	cmd := exec.Command("stty", "size")
	cmd.Stdin = os.Stdin
	output, err := cmd.Output()
	if err == nil {
		fields := strings.Fields(string(output))
		if len(fields) == 2 {
			rows, rowErr := strconv.Atoi(fields[0])
			cols, colErr := strconv.Atoi(fields[1])
			if rowErr == nil && colErr == nil && rows > 0 && cols > 0 {
				return cols, rows
			}
		}
	}

	cols := 120
	rows := 36
	if value := strings.TrimSpace(os.Getenv("COLUMNS")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			cols = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("LINES")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			rows = parsed
		}
	}
	return cols, rows
}

func readTUIKey(reader *bufio.Reader) (tuiKey, error) {
	b, err := reader.ReadByte()
	if err != nil {
		return tuiKey{}, err
	}

	switch b {
	case 3:
		return tuiKey{Name: "quit"}, nil
	case 9:
		return tuiKey{Name: "tab"}, nil
	case 13, 10:
		return tuiKey{Name: "enter"}, nil
	case 27:
		next, err := reader.ReadByte()
		if err != nil {
			return tuiKey{Name: "escape"}, nil
		}
		if next != '[' {
			return tuiKey{Name: "escape"}, nil
		}
		third, err := reader.ReadByte()
		if err != nil {
			return tuiKey{Name: "escape"}, nil
		}
		switch third {
		case 'A':
			return tuiKey{Name: "up"}, nil
		case 'B':
			return tuiKey{Name: "down"}, nil
		case 'C':
			return tuiKey{Name: "right"}, nil
		case 'D':
			return tuiKey{Name: "left"}, nil
		default:
			return tuiKey{Name: "escape"}, nil
		}
	case 127, 8:
		return tuiKey{Name: "backspace"}, nil
	default:
		return tuiKey{Name: "rune", Rune: rune(b)}, nil
	}
}

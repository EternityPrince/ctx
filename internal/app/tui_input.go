package app

import (
	"fmt"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/clipboard"
)

func (m *tuiModel) handleKey(key tuiKey) (bool, error) {
	if m.inputMode != "" {
		return m.handleInputKey(key)
	}

	switch key.Name {
	case "quit":
		return true, nil
	case "up":
		m.moveItem(-1)
	case "down":
		m.moveItem(1)
	case "left":
		m.moveSection(-1)
	case "right", "tab":
		m.moveSection(1)
	case "enter":
		return false, m.openSelected(true)
	case "backspace":
		return false, nil
	case "rune":
		return m.handleRune(key.Rune)
	}
	return false, nil
}

func (m *tuiModel) handleInputKey(key tuiKey) (bool, error) {
	switch key.Name {
	case "escape":
		m.inputMode = ""
		m.inputValue = ""
		m.message = "Search cancelled"
		return false, nil
	case "enter":
		query := strings.TrimSpace(m.inputValue)
		m.inputMode = ""
		m.inputValue = ""
		if query == "" {
			m.message = "Type a symbol name to search"
			return false, nil
		}
		return false, m.search(query, true)
	case "backspace":
		if len(m.inputValue) > 0 {
			m.inputValue = m.inputValue[:len(m.inputValue)-1]
		}
		return false, nil
	case "rune":
		if key.Rune >= 32 && key.Rune <= 126 {
			m.inputValue += string(key.Rune)
		}
		return false, nil
	default:
		return false, nil
	}
}

func (m *tuiModel) handleRune(r rune) (bool, error) {
	switch r {
	case 'q':
		return true, nil
	case 'k':
		m.moveItem(-1)
	case 'j':
		m.moveItem(1)
	case 'h':
		m.moveSection(-1)
	case 'l':
		m.moveSection(1)
	case '/':
		m.inputMode = "search"
		m.inputValue = ""
		m.message = "Search for symbols or text"
	case 'g':
		return false, m.loadLanding()
	case 'b':
		return false, m.back()
	case 'n':
		return false, m.forward()
	case 'v', ' ':
		m.showSource = !m.showSource
		if m.showSource {
			m.message = "Expanded source view"
		} else {
			m.message = "Compact preview view"
		}
	case 'y':
		return false, m.copySelection()
	case 'o':
		return false, m.openSelectionFile()
	case 'f':
		return false, m.openCurrentFile()
	case 'r':
		return false, m.reloadSummary()
	default:
		if r >= '1' && r <= '9' {
			return false, m.openIndex(int(r - '1'))
		}
	}
	return false, nil
}

func (m *tuiModel) copySelection() error {
	text := ""
	item, ok := m.selectedItem()
	if ok && item.CopyText != "" {
		text = item.CopyText
	}
	if text == "" && m.mode == tuiModeSymbol {
		text = fmt.Sprintf("%s\n%s:%d", displaySignature(m.current.Symbol), m.current.Symbol.FilePath, m.current.Symbol.Line)
	}
	if text == "" && m.mode == tuiModeFile {
		text = m.currentFile
	}
	if text == "" {
		m.message = "Nothing to copy"
		return nil
	}
	if err := clipboard.Copy(text); err != nil {
		m.message = fmt.Sprintf("Copy failed: %v", err)
		return nil
	}
	m.message = "Copied current selection to clipboard"
	return nil
}

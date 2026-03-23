package app

import (
	"os"
	"strings"
)

type palette struct {
	enabled bool
}

func newPalette() palette {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return palette{}
	}
	return palette{enabled: true}
}

func (p palette) title(value string) string   { return p.wrap("1;36", value) }
func (p palette) section(value string) string { return p.wrap("1;34", value) }
func (p palette) label(value string) string   { return p.wrap("0;37", value) }
func (p palette) accent(value string) string  { return p.wrap("1;33", value) }
func (p palette) muted(value string) string   { return p.wrap("0;90", value) }

func (p palette) kind(kind string) string {
	switch kind {
	case "func":
		return p.wrap("1;36", strings.ToUpper(kind))
	case "method":
		return p.wrap("1;34", strings.ToUpper(kind))
	case "struct":
		return p.wrap("1;35", strings.ToUpper(kind))
	case "interface":
		return p.wrap("1;32", strings.ToUpper(kind))
	case "type", "alias", "class":
		return p.wrap("1;33", strings.ToUpper(kind))
	case "test", "benchmark", "fuzz":
		return p.wrap("1;32", strings.ToUpper(kind))
	default:
		if kind == "" {
			return p.wrap("1;37", "SYMBOL")
		}
		return p.wrap("1;37", strings.ToUpper(kind))
	}
}

func (p palette) kindBadge(kind string) string {
	return "[" + p.kind(kind) + "]"
}

func (p palette) rule(title string) string {
	base := "========================================================================"
	if title == "" {
		return p.wrap("0;90", base)
	}
	prefix := "== " + title + " "
	if len(prefix) >= len(base) {
		return p.wrap("0;90", prefix)
	}
	return p.wrap("0;90", prefix+strings.Repeat("=", len(base)-len(prefix)))
}

func (p palette) badge(value string) string {
	switch value {
	case "high":
		return p.wrap("1;31", strings.ToUpper(value))
	case "medium":
		return p.wrap("1;33", strings.ToUpper(value))
	case "low":
		return p.wrap("1;32", strings.ToUpper(value))
	default:
		return p.wrap("1;35", strings.ToUpper(value))
	}
}

func (p palette) wrap(code, value string) string {
	if !p.enabled || value == "" {
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

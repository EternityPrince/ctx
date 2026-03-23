package app

import (
	"strings"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

func styleHumanSignature(p palette, value string) string {
	return p.wrap("1;37", value)
}

func shortenValues(modulePath string, values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, shortenQName(modulePath, value))
	}
	return result
}

func impactLabel(callers, refs, tests, reverseDeps int) string {
	score := callers + refs + tests + reverseDeps
	switch {
	case score >= 12:
		return "high"
	case score >= 5:
		return "medium"
	default:
		return "low"
	}
}

func reportImportance(score int) string {
	switch {
	case score >= 18:
		return "high"
	case score >= 7:
		return "medium"
	default:
		return "low"
	}
}

func limitStrings(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

func displaySignature(symbol storage.SymbolMatch) string {
	switch symbol.Kind {
	case "func":
		if strings.HasPrefix(symbol.Signature, "func(") {
			return "func " + symbol.Name + symbol.Signature[len("func"):]
		}
	case "method":
		if strings.HasPrefix(symbol.Signature, "func(") {
			if symbol.Receiver != "" {
				return "func (" + symbol.Receiver + ") " + symbol.Name + symbol.Signature[len("func"):]
			}
			return "func " + symbol.Name + symbol.Signature[len("func"):]
		}
	case "struct", "interface", "type", "alias", "class":
		if symbol.Signature != "" && symbol.Signature != symbol.Name {
			if strings.HasPrefix(symbol.Signature, "type ") {
				return symbol.Signature
			}
			return "type " + symbol.Name + " " + symbol.Signature
		}
		if symbol.Name != "" {
			return "type " + symbol.Name
		}
	}
	if symbol.Signature != "" {
		return symbol.Signature
	}
	return symbol.Name
}

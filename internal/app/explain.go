package app

import (
	"fmt"
	"io"
	"strings"
)

type explainFact struct {
	Key   string
	Value string
}

type explainItem struct {
	Label   string
	Details []string
}

type explainGroup struct {
	Title string
	Items []explainItem
}

type explainSection struct {
	Title  string
	Facts  []explainFact
	Notes  []string
	Groups []explainGroup
}

func renderHumanExplainSection(stdout io.Writer, p palette, section explainSection) error {
	if explainSectionEmpty(section) {
		return nil
	}

	title := strings.TrimSpace(section.Title)
	if title == "" {
		title = "Explain"
	}
	if _, err := fmt.Fprintf(stdout, "%s\n", p.section(title)); err != nil {
		return err
	}
	for _, fact := range section.Facts {
		key := strings.TrimSpace(fact.Key)
		value := strings.TrimSpace(fact.Value)
		if key == "" || value == "" {
			continue
		}
		if _, err := fmt.Fprintf(stdout, "  %s %s\n", p.label(key+":"), value); err != nil {
			return err
		}
	}
	for _, note := range section.Notes {
		note = strings.TrimSpace(note)
		if note == "" {
			continue
		}
		if _, err := fmt.Fprintf(stdout, "  %s %s\n", p.label("why:"), note); err != nil {
			return err
		}
	}
	for _, group := range section.Groups {
		items := compactExplainItems(group.Items)
		if len(items) == 0 {
			continue
		}
		if _, err := fmt.Fprintf(stdout, "  %s (%d)\n", p.label(group.Title), len(items)); err != nil {
			return err
		}
		for _, item := range items {
			if _, err := fmt.Fprintf(stdout, "    - %s\n", item.Label); err != nil {
				return err
			}
			for _, detail := range item.Details {
				detail = strings.TrimSpace(detail)
				if detail == "" {
					continue
				}
				if _, err := fmt.Fprintf(stdout, "      %s %s\n", p.label("why:"), detail); err != nil {
					return err
				}
			}
		}
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

func renderAIExplainSection(stdout io.Writer, prefix string, section explainSection) error {
	if explainSectionEmpty(section) {
		return nil
	}

	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "explain"
	}
	if title := strings.TrimSpace(section.Title); title != "" {
		if _, err := fmt.Fprintf(stdout, "%s_title=%q\n", prefix, title); err != nil {
			return err
		}
	}
	for _, fact := range section.Facts {
		key := explainKeyName(fact.Key)
		value := strings.TrimSpace(fact.Value)
		if key == "" || value == "" {
			continue
		}
		if _, err := fmt.Fprintf(stdout, "%s_%s=%q\n", prefix, key, value); err != nil {
			return err
		}
	}
	for _, note := range section.Notes {
		note = strings.TrimSpace(note)
		if note == "" {
			continue
		}
		if _, err := fmt.Fprintf(stdout, "%s_note=%q\n", prefix, note); err != nil {
			return err
		}
	}
	for _, group := range section.Groups {
		items := compactExplainItems(group.Items)
		key := explainKeyName(group.Title)
		if key == "" {
			continue
		}
		if _, err := fmt.Fprintf(stdout, "%s_%s=%d\n", prefix, key, len(items)); err != nil {
			return err
		}
		for _, item := range items {
			payload := item.Label
			if len(item.Details) > 0 {
				payload += " | " + strings.Join(item.Details, " | ")
			}
			if _, err := fmt.Fprintf(stdout, "%s_%s_item=%q\n", prefix, key, payload); err != nil {
				return err
			}
		}
	}
	return nil
}

func compactExplainItems(items []explainItem) []explainItem {
	compact := make([]explainItem, 0, len(items))
	for _, item := range items {
		label := strings.TrimSpace(item.Label)
		if label == "" {
			continue
		}
		details := make([]string, 0, len(item.Details))
		for _, detail := range item.Details {
			detail = strings.TrimSpace(detail)
			if detail != "" {
				details = append(details, detail)
			}
		}
		compact = append(compact, explainItem{Label: label, Details: details})
	}
	return compact
}

func explainSectionEmpty(section explainSection) bool {
	return len(section.Facts) == 0 && len(section.Notes) == 0 && len(section.Groups) == 0
}

func explainKeyName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}

	var b strings.Builder
	underscore := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			underscore = false
		default:
			if !underscore {
				b.WriteByte('_')
				underscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

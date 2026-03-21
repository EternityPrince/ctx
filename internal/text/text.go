package text

import "strings"

func NormalizeNewlines(content string) string {
	return strings.ReplaceAll(content, "\r\n", "\n")
}

func CountLines(content string) (int, int) {
	if content == "" {
		return 0, 0
	}

	lines := strings.Split(content, "\n")
	total := len(lines)
	if strings.HasSuffix(content, "\n") {
		total--
		lines = lines[:len(lines)-1]
	}

	nonEmpty := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmpty++
		}
	}

	return total, nonEmpty
}

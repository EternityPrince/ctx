package filter

import (
	"bytes"
	"strings"
	"unicode/utf8"
)

var ignoredDirectories = map[string]struct{}{
	".git":         {},
	".hg":          {},
	".svn":         {},
	".idea":        {},
	".vscode":      {},
	"node_modules": {},
	"vendor":       {},
}

func SkipDirectory(name string, includeHidden bool) (bool, string) {
	if !includeHidden && strings.HasPrefix(name, ".") {
		return true, "hidden directory"
	}
	if _, ok := ignoredDirectories[name]; ok {
		return true, "ignored directory"
	}
	return false, ""
}

func SkipFile(name string, includeHidden bool) (bool, string) {
	if !includeHidden && strings.HasPrefix(name, ".") {
		return true, "hidden file"
	}
	return false, ""
}

func IsLikelyBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return true
	}

	sample := data
	if len(sample) > 8192 {
		sample = sample[:8192]
	}
	if !utf8.Valid(sample) {
		return true
	}

	var suspicious int
	for _, b := range sample {
		if b < 0x09 {
			suspicious++
			continue
		}
		if b > 0x0D && b < 0x20 {
			suspicious++
		}
	}

	return float64(suspicious)/float64(len(sample)) > 0.08
}

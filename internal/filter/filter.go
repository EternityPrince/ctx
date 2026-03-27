package filter

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

var ignoredDirectoryNames = map[string]struct{}{
	".git":         {},
	".hg":          {},
	".svn":         {},
	".idea":        {},
	".vscode":      {},
	"node_modules": {},
	"vendor":       {},
}

var ignoredDirectoryPaths = []string{
	".Trash",
	"Library",
	"go/pkg/mod",
}

func SkipDirectory(path, name string, includeHidden bool) (bool, string) {
	if matchesIgnoredDirectoryPath(path) {
		return true, "ignored directory path"
	}
	if reason := rustSkipDirectoryReason(path, name); reason != "" {
		return true, reason
	}
	if !includeHidden && strings.HasPrefix(name, ".") {
		return true, "hidden directory"
	}
	if _, ok := ignoredDirectoryNames[name]; ok {
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

func HandleWalkError(path string, walkErr error) error {
	if walkErr == nil {
		return nil
	}
	if matchesIgnoredDirectoryPath(path) {
		return filepath.SkipDir
	}
	if errors.Is(walkErr, fs.ErrPermission) || errors.Is(walkErr, os.ErrPermission) {
		return filepath.SkipDir
	}
	return walkErr
}

func matchesIgnoredDirectoryPath(path string) bool {
	if path == "" {
		return false
	}

	cleanPath := filepath.Clean(path)
	for _, ignored := range IgnoredDirectoryPaths() {
		if ignored == "" {
			continue
		}
		if cleanPath == ignored {
			return true
		}
		if strings.HasPrefix(cleanPath, ignored+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func IgnoredDirectoryPaths() []string {
	paths := make([]string, 0, len(ignoredDirectoryPaths))
	homeDir, err := os.UserHomeDir()
	for _, ignored := range ignoredDirectoryPaths {
		value := ignored
		if homeDir != "" && err == nil && !filepath.IsAbs(ignored) {
			value = filepath.Join(homeDir, ignored)
		}
		paths = append(paths, filepath.Clean(value))
	}
	return paths
}

func rustSkipDirectoryReason(path, name string) string {
	name = strings.TrimSpace(name)
	switch name {
	case "target":
		return "rust build directory"
	case ".rust-analyzer", ".rust-analyzer-cache":
		return "rust analyzer cache"
	}

	cleanPath := filepath.ToSlash(filepath.Clean(path))
	switch {
	case strings.Contains(cleanPath, "/.cargo/registry") || strings.HasSuffix(cleanPath, "/.cargo/registry"):
		return "rust cargo registry cache"
	case strings.Contains(cleanPath, "/.cargo/git") || strings.HasSuffix(cleanPath, "/.cargo/git"):
		return "rust cargo git cache"
	default:
		return ""
	}
}

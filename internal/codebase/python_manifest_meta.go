package codebase

import (
	"path/filepath"
	"regexp"
	"strings"
)

var setupPyStringPattern = regexp.MustCompile(`["']([^"']+)["']`)
var pythonPathPattern = regexp.MustCompile(`(?i)(?:path\s*=\s*|@\s*|file:)\s*(?:["']([^"']+)["']|([^"',\]\s}]+))`)

func ParsePythonProjectManifestMeta(name string, data []byte) ManifestMeta {
	switch strings.TrimSpace(name) {
	case "pyproject.toml":
		return ParsePyProjectManifestMeta(data)
	case "setup.cfg":
		return ParseSetupCfgManifestMeta(data)
	case "setup.py":
		return ParseSetupPyManifestMeta(data)
	case "requirements.txt":
		return ParseRequirementsManifestMeta(data)
	case "Pipfile":
		return ParsePipfileManifestMeta(data)
	case "poetry.lock":
		return ManifestMeta{Kind: "poetry.lock"}
	default:
		return ManifestMeta{Kind: strings.TrimSpace(name)}
	}
}

func ParseSetupCfgManifestMeta(data []byte) ManifestMeta {
	meta := ManifestMeta{Kind: "setup.cfg"}
	section := ""
	lines := strings.Split(string(data), "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			continue
		}
		key, value, ok := splitManifestAssignmentAllowEmpty(line)
		if !ok {
			continue
		}
		if value == "" && i+1 < len(lines) {
			block := collectIndentedManifestBlock(lines, &i)
			if block != "" {
				value = block
			}
		}
		switch section {
		case "metadata":
			if key == "name" {
				meta.Name = normalizeManifestPythonName(trimManifestQuoted(value))
			}
		case "options":
			switch key {
			case "python_requires":
				meta.RequiresPython = trimManifestQuoted(value)
			case "package_dir":
				meta.PackageRoots = append(meta.PackageRoots, extractSetupCfgPackageRoots(value)...)
			case "install_requires":
				addPythonDependencies(&meta, parseIndentedPythonDeps(value))
			}
		case "options.packages.find":
			if key == "where" {
				meta.PackageRoots = append(meta.PackageRoots, parseIndentedPythonDeps(value)...)
			}
		}
	}
	return meta
}

func ParseSetupPyManifestMeta(data []byte) ManifestMeta {
	source := string(data)
	meta := ManifestMeta{Kind: "setup.py"}
	if match := regexp.MustCompile(`(?m)\bname\s*=\s*["']([^"']+)["']`).FindStringSubmatch(source); len(match) == 2 {
		meta.Name = normalizeManifestPythonName(match[1])
	}
	if match := regexp.MustCompile(`(?m)\bpython_requires\s*=\s*["']([^"']+)["']`).FindStringSubmatch(source); len(match) == 2 {
		meta.RequiresPython = strings.TrimSpace(match[1])
	}
	if match := regexp.MustCompile(`(?s)find_(?:namespace_)?packages\s*\(\s*where\s*=\s*["']([^"']+)["']`).FindStringSubmatch(source); len(match) == 2 {
		meta.PackageRoots = append(meta.PackageRoots, filepath.ToSlash(strings.TrimSpace(match[1])))
	}
	if match := regexp.MustCompile(`(?s)package_dir\s*=\s*\{[^}]*["']["']\s*:\s*["']([^"']+)["']`).FindStringSubmatch(source); len(match) == 2 {
		meta.PackageRoots = append(meta.PackageRoots, filepath.ToSlash(strings.TrimSpace(match[1])))
	}
	if match := regexp.MustCompile(`(?s)install_requires\s*=\s*\[(.*?)\]`).FindStringSubmatch(source); len(match) == 2 {
		addPythonDependencies(&meta, extractPythonStringLiterals(match[1]))
	}
	return meta
}

func ParseRequirementsManifestMeta(data []byte) ManifestMeta {
	meta := ManifestMeta{Kind: "requirements.txt"}
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-r ") || strings.HasPrefix(line, "--requirement ") {
			continue
		}
		if dep, local := parsePythonDependencyEntry(line); dep != "" {
			if local {
				meta.LocalDeps = append(meta.LocalDeps, dep)
			} else {
				meta.ExternalDeps = append(meta.ExternalDeps, dep)
			}
		}
	}
	return meta
}

func ParsePipfileManifestMeta(data []byte) ManifestMeta {
	meta := ManifestMeta{Kind: "Pipfile"}
	section := ""
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			continue
		}
		if section != "packages" && section != "dev-packages" {
			continue
		}
		key, value, ok := splitManifestAssignment(line)
		if !ok {
			continue
		}
		addPythonDependencyKeyValue(&meta, key, value)
	}
	return meta
}

func addPythonDependencies(meta *ManifestMeta, values []string) {
	for _, value := range values {
		if dep, local := parsePythonDependencyEntry(value); dep != "" {
			if local {
				meta.LocalDeps = append(meta.LocalDeps, dep)
			} else {
				meta.ExternalDeps = append(meta.ExternalDeps, dep)
			}
		}
	}
}

func addPythonDependencyKeyValue(meta *ManifestMeta, key, value string) {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" {
		return
	}
	if localPath := extractPythonPathReference(value); localPath != "" {
		meta.LocalDeps = append(meta.LocalDeps, key+"=>"+localPath)
		return
	}
	if strings.EqualFold(trimManifestQuoted(value), "true") {
		return
	}
	meta.ExternalDeps = append(meta.ExternalDeps, key)
}

func parsePythonDependencyEntry(value string) (string, bool) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "-e ")
	value = strings.TrimPrefix(value, "--editable ")
	value = trimManifestQuoted(value)
	if value == "" {
		return "", false
	}
	if path := extractPythonPathReference(value); path != "" {
		label := pythonDependencyLabel(value)
		if label == "" {
			label = path
		}
		return label + "=>" + path, true
	}

	label := pythonDependencyLabel(value)
	if label == "" {
		return "", false
	}
	return label, false
}

func pythonDependencyLabel(value string) string {
	value = strings.TrimSpace(value)
	switch {
	case strings.Contains(value, "@"):
		parts := strings.SplitN(value, "@", 2)
		return normalizeManifestPythonName(trimManifestQuoted(parts[0]))
	case strings.HasPrefix(value, ".") || strings.HasPrefix(value, "/"):
		return ""
	default:
		value = strings.TrimLeft(value, "<>!=~ ")
		for idx, r := range value {
			if r == ' ' || r == '=' || r == '<' || r == '>' || r == '!' || r == '~' || r == '[' {
				value = value[:idx]
				break
			}
		}
		return normalizeManifestPythonName(value)
	}
}

func extractPythonPathReference(value string) string {
	for _, match := range pythonPathPattern.FindAllStringSubmatch(value, -1) {
		if len(match) < 3 {
			continue
		}
		pathValue := strings.TrimSpace(match[1])
		if pathValue == "" {
			pathValue = strings.TrimSpace(match[2])
		}
		pathValue = filepath.ToSlash(pathValue)
		if isLikelyLocalPythonPath(pathValue) {
			return pathValue
		}
	}
	value = strings.TrimSpace(value)
	if isLikelyLocalPythonPath(value) {
		return filepath.ToSlash(value)
	}
	return ""
}

func isLikelyLocalPythonPath(value string) bool {
	value = strings.TrimSpace(strings.Trim(value, `"'`))
	switch {
	case value == "":
		return false
	case strings.HasPrefix(value, "./"), strings.HasPrefix(value, "../"), strings.HasPrefix(value, "/"):
		return true
	case strings.HasPrefix(value, "file://"):
		return true
	default:
		return false
	}
}

func collectIndentedManifestBlock(lines []string, idx *int) string {
	values := make([]string, 0, 4)
	for *idx+1 < len(lines) {
		next := lines[*idx+1]
		if strings.TrimSpace(next) == "" {
			*idx = *idx + 1
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(next), "[") {
			break
		}
		if !strings.HasPrefix(next, " ") && !strings.HasPrefix(next, "\t") {
			break
		}
		*idx = *idx + 1
		values = append(values, strings.TrimSpace(next))
	}
	return strings.Join(values, "\n")
}

func parseIndentedPythonDeps(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parts := strings.Split(value, "\n")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = trimManifestQuoted(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		result = append(result, filepath.ToSlash(part))
	}
	return stableStrings(result)
}

func extractInlineRootMap(value string) []string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "{") || !strings.Contains(value, "}") {
		return nil
	}
	matches := regexp.MustCompile(`["'][^"']*["']\s*[:=]\s*["']([^"']+)["']`).FindAllStringSubmatch(value, -1)
	roots := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) == 2 {
			roots = append(roots, filepath.ToSlash(strings.TrimSpace(match[1])))
		}
	}
	return stableStrings(roots)
}

func extractSetupCfgPackageRoots(value string) []string {
	roots := extractInlineRootMap(value)
	if len(roots) > 0 {
		return roots
	}
	return parseIndentedPythonDeps(value)
}

func extractPythonPackageRoots(value string) []string {
	roots := make([]string, 0, 4)
	for _, match := range regexp.MustCompile(`from\s*=\s*["']([^"']+)["']`).FindAllStringSubmatch(value, -1) {
		if len(match) == 2 {
			roots = append(roots, filepath.ToSlash(strings.TrimSpace(match[1])))
		}
	}
	return stableStrings(roots)
}

func extractHatchPackageRoots(value string) []string {
	entries := parseManifestStringArray(value)
	roots := make([]string, 0, len(entries))
	for _, entry := range entries {
		entry = filepath.ToSlash(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			roots = append(roots, filepath.ToSlash(filepath.Dir(entry)))
			continue
		}
		roots = append(roots, ".")
	}
	return stableStrings(roots)
}

func extractPythonStringLiterals(value string) []string {
	matches := setupPyStringPattern.FindAllStringSubmatch(value, -1)
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) == 2 {
			result = append(result, match[1])
		}
	}
	return result
}

func splitManifestAssignmentAllowEmpty(line string) (string, string, bool) {
	idx := strings.Index(line, "=")
	if idx <= 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:idx])
	value := strings.TrimSpace(line[idx+1:])
	if comment := strings.Index(value, "#"); comment >= 0 {
		value = strings.TrimSpace(value[:comment])
	}
	if key == "" {
		return "", "", false
	}
	return key, value, true
}

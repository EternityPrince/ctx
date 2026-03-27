package codebase

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
)

type ManifestMeta struct {
	Kind             string   `json:"kind,omitempty"`
	Name             string   `json:"name,omitempty"`
	Module           string   `json:"module,omitempty"`
	GoVersion        string   `json:"go_version,omitempty"`
	RequiresPython   string   `json:"requires_python,omitempty"`
	Edition          string   `json:"edition,omitempty"`
	PackageRoots     []string `json:"package_roots,omitempty"`
	LocalDeps        []string `json:"local_deps,omitempty"`
	ExternalDeps     []string `json:"external_deps,omitempty"`
	Features         []string `json:"features,omitempty"`
	WorkspaceMembers []string `json:"workspace_members,omitempty"`
	WorkspaceExclude []string `json:"workspace_exclude,omitempty"`
}

func EncodeManifestMeta(meta ManifestMeta) string {
	meta.Kind = strings.TrimSpace(meta.Kind)
	meta.Name = strings.TrimSpace(meta.Name)
	meta.Module = strings.TrimSpace(meta.Module)
	meta.GoVersion = strings.TrimSpace(meta.GoVersion)
	meta.RequiresPython = strings.TrimSpace(meta.RequiresPython)
	meta.Edition = strings.TrimSpace(meta.Edition)
	meta.PackageRoots = stableStrings(meta.PackageRoots)
	meta.LocalDeps = stableStrings(meta.LocalDeps)
	meta.ExternalDeps = stableStrings(meta.ExternalDeps)
	meta.Features = stableStrings(meta.Features)
	meta.WorkspaceMembers = stableStrings(meta.WorkspaceMembers)
	meta.WorkspaceExclude = stableStrings(meta.WorkspaceExclude)
	if emptyManifestMeta(meta) {
		return ""
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return ""
	}
	return string(data)
}

func DecodeManifestMeta(value string) ManifestMeta {
	value = strings.TrimSpace(value)
	if value == "" {
		return ManifestMeta{}
	}
	var meta ManifestMeta
	if err := json.Unmarshal([]byte(value), &meta); err != nil {
		return ManifestMeta{}
	}
	meta.PackageRoots = stableStrings(meta.PackageRoots)
	meta.LocalDeps = stableStrings(meta.LocalDeps)
	meta.ExternalDeps = stableStrings(meta.ExternalDeps)
	meta.Features = stableStrings(meta.Features)
	meta.WorkspaceMembers = stableStrings(meta.WorkspaceMembers)
	meta.WorkspaceExclude = stableStrings(meta.WorkspaceExclude)
	return meta
}

func ParseGoModManifestMeta(data []byte) ManifestMeta {
	file, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return ManifestMeta{Kind: "go.mod"}
	}

	meta := ManifestMeta{Kind: "go.mod"}
	if file.Module != nil {
		meta.Module = strings.TrimSpace(file.Module.Mod.Path)
	}
	if file.Go != nil {
		meta.GoVersion = strings.TrimSpace(file.Go.Version)
	}

	localReplaces := make(map[string]struct{})
	externalDeps := make(map[string]struct{})
	for _, req := range file.Require {
		path := strings.TrimSpace(req.Mod.Path)
		if path != "" {
			externalDeps[path] = struct{}{}
		}
	}
	for _, repl := range file.Replace {
		oldPath := strings.TrimSpace(repl.Old.Path)
		newPath := strings.TrimSpace(repl.New.Path)
		if oldPath == "" || newPath == "" {
			continue
		}
		if strings.HasPrefix(newPath, ".") || strings.HasPrefix(newPath, "/") {
			localReplaces[oldPath+"=>"+filepath.ToSlash(newPath)] = struct{}{}
		}
	}
	meta.LocalDeps = setKeys(localReplaces)
	meta.ExternalDeps = setKeys(externalDeps)
	return meta
}

func ParsePyProjectManifestMeta(data []byte) ManifestMeta {
	meta := ManifestMeta{Kind: "pyproject.toml"}
	section := ""
	lines := strings.Split(string(data), "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			continue
		}
		key, value, ok := splitManifestAssignment(line)
		if !ok {
			continue
		}
		if strings.HasPrefix(value, "[") && !strings.Contains(value, "]") {
			for i+1 < len(lines) {
				i++
				value += "\n" + strings.TrimSpace(lines[i])
				if strings.Contains(lines[i], "]") {
					break
				}
			}
		}
		switch section {
		case "project":
			switch key {
			case "name":
				meta.Name = normalizeManifestPythonName(trimManifestQuoted(value))
			case "requires-python":
				meta.RequiresPython = trimManifestQuoted(value)
			case "dependencies", "optional-dependencies":
				addPythonDependencies(&meta, parseManifestStringArray(value))
			}
		case "tool.poetry":
			if key == "name" && meta.Name == "" {
				meta.Name = normalizeManifestPythonName(trimManifestQuoted(value))
			}
			if key == "packages" {
				meta.PackageRoots = append(meta.PackageRoots, extractPythonPackageRoots(value)...)
			}
		case "tool.poetry.dependencies", "tool.poetry.group.dev.dependencies":
			addPythonDependencyKeyValue(&meta, key, value)
		case "tool.setuptools":
			if key == "package-dir" {
				meta.PackageRoots = append(meta.PackageRoots, extractInlineRootMap(value)...)
			}
		case "tool.setuptools.packages.find":
			if key == "where" {
				meta.PackageRoots = append(meta.PackageRoots, parseManifestStringArray(value)...)
			}
		case "tool.hatch.build.targets.wheel":
			if key == "packages" {
				meta.PackageRoots = append(meta.PackageRoots, extractHatchPackageRoots(value)...)
			}
		}
	}
	return meta
}

func ParseCargoManifestMeta(data []byte) ManifestMeta {
	meta := ManifestMeta{Kind: "cargo"}
	section := ""
	lines := strings.Split(string(data), "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			switch section {
			case "package":
				if meta.Kind == "cargo" {
					meta.Kind = "cargo-crate"
				}
			case "workspace":
				if meta.Kind == "cargo" {
					meta.Kind = "cargo-workspace"
				} else if meta.Kind == "cargo-crate" {
					meta.Kind = "cargo-hybrid"
				}
			}
			continue
		}

		key, value, ok := splitManifestAssignment(line)
		if !ok {
			continue
		}
		if strings.HasPrefix(value, "[") && !strings.Contains(value, "]") {
			for i+1 < len(lines) {
				i++
				value += "\n" + strings.TrimSpace(lines[i])
				if strings.Contains(lines[i], "]") {
					break
				}
			}
		}

		switch {
		case section == "package":
			switch key {
			case "name":
				meta.Name = trimManifestQuoted(value)
			case "edition":
				meta.Edition = trimManifestQuoted(value)
			}
		case section == "workspace":
			switch key {
			case "members":
				meta.WorkspaceMembers = parseManifestStringArray(value)
			case "exclude":
				meta.WorkspaceExclude = parseManifestStringArray(value)
			}
		case section == "workspace.package":
			if key == "edition" && meta.Edition == "" {
				meta.Edition = trimManifestQuoted(value)
			}
		case section == "features":
			meta.Features = append(meta.Features, key)
		case cargoDependencySection(section):
			depName := strings.TrimSpace(key)
			if depName == "" {
				continue
			}
			switch {
			case strings.Contains(value, "path"):
				meta.LocalDeps = append(meta.LocalDeps, depName)
			case strings.Contains(value, "workspace") && strings.Contains(value, "true"):
				meta.LocalDeps = append(meta.LocalDeps, depName)
			default:
				meta.ExternalDeps = append(meta.ExternalDeps, depName)
			}
		}
	}
	return meta
}

func ManifestDeltaReason(delta ManifestDelta) string {
	parts := make([]string, 0, len(delta.Details)+1)
	if delta.RelPath != "" {
		parts = append(parts, delta.RelPath)
	}
	parts = append(parts, delta.Details...)
	return strings.Join(parts, ": ")
}

func stableStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = filepath.ToSlash(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func setKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func diffManifestList(previous, current []string) (added []string, removed []string) {
	prevSet := make(map[string]struct{}, len(previous))
	curSet := make(map[string]struct{}, len(current))
	for _, value := range previous {
		prevSet[value] = struct{}{}
	}
	for _, value := range current {
		curSet[value] = struct{}{}
	}
	for _, value := range current {
		if _, ok := prevSet[value]; !ok {
			added = append(added, value)
		}
	}
	for _, value := range previous {
		if _, ok := curSet[value]; !ok {
			removed = append(removed, value)
		}
	}
	return added, removed
}

func emptyManifestMeta(meta ManifestMeta) bool {
	return meta.Kind == "" &&
		meta.Name == "" &&
		meta.Module == "" &&
		meta.GoVersion == "" &&
		meta.RequiresPython == "" &&
		meta.Edition == "" &&
		len(meta.PackageRoots) == 0 &&
		len(meta.LocalDeps) == 0 &&
		len(meta.ExternalDeps) == 0 &&
		len(meta.Features) == 0 &&
		len(meta.WorkspaceMembers) == 0 &&
		len(meta.WorkspaceExclude) == 0
}

func splitManifestAssignment(line string) (string, string, bool) {
	idx := strings.Index(line, "=")
	if idx <= 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:idx])
	value := strings.TrimSpace(line[idx+1:])
	if comment := strings.Index(value, "#"); comment >= 0 {
		value = strings.TrimSpace(value[:comment])
	}
	if key == "" || value == "" {
		return "", "", false
	}
	return key, value, true
}

func trimManifestQuoted(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"'`)
	return strings.TrimSpace(value)
}

func normalizeManifestPythonName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	return value
}

func parseManifestStringArray(value string) []string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "[") || !strings.Contains(value, "]") {
		return nil
	}
	value = strings.TrimPrefix(value, "[")
	if idx := strings.LastIndex(value, "]"); idx >= 0 {
		value = value[:idx]
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = trimManifestQuoted(part)
		if part == "" {
			continue
		}
		result = append(result, filepath.ToSlash(part))
	}
	return stableStrings(result)
}

func cargoDependencySection(section string) bool {
	if section == "dependencies" || section == "dev-dependencies" || section == "build-dependencies" || section == "workspace.dependencies" {
		return true
	}
	return strings.HasSuffix(section, ".dependencies") ||
		strings.HasSuffix(section, ".dev-dependencies") ||
		strings.HasSuffix(section, ".build-dependencies")
}

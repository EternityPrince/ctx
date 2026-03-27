package project

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/filter"
)

type rustManifest struct {
	Dir                     string
	HasPackage              bool
	PackageName             string
	Edition                 string
	HasWorkspace            bool
	WorkspaceMembers        []string
	WorkspaceExclude        []string
	WorkspacePackageEdition string
}

type RustPackage struct {
	Name    string
	Root    string
	Edition string
}

func findRustProjectRoot(start string) (string, string, string, error) {
	boundary, hasBoundary := findRepositoryRoot(start)
	current := start
	var manifests []rustManifest

	for {
		manifestPath := filepath.Join(current, "Cargo.toml")
		if info, err := os.Stat(manifestPath); err == nil && !info.IsDir() {
			manifest, parseErr := parseCargoManifest(manifestPath)
			if parseErr != nil {
				return "", "", "", parseErr
			}
			manifests = append(manifests, manifest)
		}

		if hasBoundary && current == boundary {
			break
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	if len(manifests) == 0 {
		if hasBoundary {
			return "", "", "", fmt.Errorf("Cargo.toml not found between %s and repository root %s", start, boundary)
		}
		return "", "", "", errors.New("Cargo.toml not found from the provided path")
	}

	sort.Slice(manifests, func(i, j int) bool {
		return pathDepth(manifests[i].Dir) < pathDepth(manifests[j].Dir)
	})

	for _, manifest := range manifests {
		if manifest.HasWorkspace {
			name := manifest.PackageName
			if name == "" {
				name = filepath.Base(manifest.Dir)
			}
			version := manifest.Edition
			if version == "" {
				version = manifest.WorkspacePackageEdition
			}
			return manifest.Dir, name, version, nil
		}
	}

	nearest := manifests[len(manifests)-1]
	name := nearest.PackageName
	if name == "" {
		name = filepath.Base(nearest.Dir)
	}
	return nearest.Dir, name, nearest.Edition, nil
}

func DiscoverRustPackages(root string) ([]RustPackage, error) {
	manifests, err := collectRustManifests(root)
	if err != nil {
		return nil, err
	}

	type workspace struct {
		root    string
		members []string
		exclude []string
		edition string
	}

	workspaces := make([]workspace, 0)
	for _, manifest := range manifests {
		if !manifest.HasWorkspace {
			continue
		}
		memberSet := make(map[string]struct{})
		for _, pattern := range manifest.WorkspaceMembers {
			expanded, expandErr := expandCargoMemberPattern(manifest.Dir, pattern)
			if expandErr != nil {
				return nil, expandErr
			}
			for _, dir := range expanded {
				memberSet[filepath.Clean(dir)] = struct{}{}
			}
		}
		members := make([]string, 0, len(memberSet)+1)
		for dir := range memberSet {
			members = append(members, dir)
		}
		if manifest.HasPackage {
			members = append(members, filepath.Clean(manifest.Dir))
		}

		excludeSet := make(map[string]struct{})
		for _, pattern := range manifest.WorkspaceExclude {
			expanded, expandErr := expandCargoMemberPattern(manifest.Dir, pattern)
			if expandErr != nil {
				return nil, expandErr
			}
			for _, dir := range expanded {
				excludeSet[filepath.Clean(dir)] = struct{}{}
			}
		}
		exclude := make([]string, 0, len(excludeSet))
		for dir := range excludeSet {
			exclude = append(exclude, dir)
		}

		workspaces = append(workspaces, workspace{
			root:    filepath.Clean(manifest.Dir),
			members: members,
			exclude: exclude,
			edition: manifest.WorkspacePackageEdition,
		})
	}

	sort.Slice(workspaces, func(i, j int) bool {
		return pathDepth(workspaces[i].root) > pathDepth(workspaces[j].root)
	})

	byRoot := make(map[string]RustPackage)
	for _, manifest := range manifests {
		if !manifest.HasPackage {
			continue
		}

		pkgRoot := filepath.Clean(manifest.Dir)
		edition := manifest.Edition
		for _, ws := range workspaces {
			if pkgRoot != ws.root && !pathHasPrefix(pkgRoot, ws.root) {
				continue
			}
			if len(ws.members) > 0 && !matchesWorkspaceMember(pkgRoot, ws.members, ws.exclude) {
				continue
			}
			if edition == "" {
				edition = ws.edition
			}
			break
		}

		byRoot[pkgRoot] = RustPackage{
			Name:    manifest.PackageName,
			Root:    pkgRoot,
			Edition: edition,
		}
	}

	packages := make([]RustPackage, 0, len(byRoot))
	for _, pkg := range byRoot {
		packages = append(packages, pkg)
	}
	sort.Slice(packages, func(i, j int) bool {
		return packages[i].Root < packages[j].Root
	})
	return packages, nil
}

func collectRustManifests(root string) ([]rustManifest, error) {
	var manifests []rustManifest
	walker, err := filter.NewWalker(root, false, "index")
	if err != nil {
		return nil, err
	}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return filter.HandleWalkError(path, walkErr)
		}
		relPath := ""
		if path != root {
			value, err := filepath.Rel(root, path)
			if err != nil {
				return fmt.Errorf("make relative path: %w", err)
			}
			relPath = filepath.ToSlash(value)
		}
		if entry.IsDir() {
			if path != root {
				if skip, _, err := walker.ShouldSkipDirectory(path, relPath, entry.Name()); err != nil {
					return err
				} else if skip {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if skip, _, err := walker.ShouldSkipFile(path, relPath, entry.Name()); err != nil {
			return err
		} else if skip {
			return nil
		}
		if entry.Name() != "Cargo.toml" {
			return nil
		}
		manifest, parseErr := parseCargoManifest(path)
		if parseErr != nil {
			return parseErr
		}
		manifests = append(manifests, manifest)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(manifests, func(i, j int) bool {
		return manifests[i].Dir < manifests[j].Dir
	})
	return manifests, nil
}

func parseCargoManifest(path string) (rustManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return rustManifest{}, fmt.Errorf("read Cargo.toml: %w", err)
	}

	manifest := rustManifest{Dir: filepath.Dir(path)}
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
				manifest.HasPackage = true
			case "workspace":
				manifest.HasWorkspace = true
			}
			continue
		}

		key, value, ok := splitAssignment(line)
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
		case "package":
			switch key {
			case "name":
				manifest.PackageName = trimQuoted(value)
			case "edition":
				manifest.Edition = trimQuoted(value)
			}
		case "workspace":
			switch key {
			case "members":
				manifest.WorkspaceMembers = parseTOMLStringArray(value)
			case "exclude":
				manifest.WorkspaceExclude = parseTOMLStringArray(value)
			}
		case "workspace.package":
			if key == "edition" {
				manifest.WorkspacePackageEdition = trimQuoted(value)
			}
		}
	}

	return manifest, nil
}

func parseTOMLStringArray(value string) []string {
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
		part = trimQuoted(part)
		if part == "" {
			continue
		}
		result = append(result, filepath.ToSlash(part))
	}
	return result
}

func expandCargoMemberPattern(root, pattern string) ([]string, error) {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	if pattern == "" {
		return nil, nil
	}
	if !strings.Contains(pattern, "*") {
		return []string{filepath.Join(root, filepath.FromSlash(pattern))}, nil
	}

	segments := strings.Split(pattern, "/")
	dirs := []string{filepath.Clean(root)}
	for _, segment := range segments {
		if segment == "" {
			continue
		}
		nextDirs := make([]string, 0)
		for _, dir := range dirs {
			if !strings.Contains(segment, "*") {
				nextDirs = append(nextDirs, filepath.Join(dir, segment))
				continue
			}

			entries, err := os.ReadDir(dir)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, fmt.Errorf("read cargo workspace dir %s: %w", dir, err)
			}
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				matched, err := path.Match(segment, entry.Name())
				if err != nil {
					return nil, fmt.Errorf("match cargo workspace pattern %q: %w", segment, err)
				}
				if matched {
					nextDirs = append(nextDirs, filepath.Join(dir, entry.Name()))
				}
			}
		}
		dirs = nextDirs
	}
	return dirs, nil
}

func matchesWorkspaceMember(dir string, members, exclude []string) bool {
	cleanDir := filepath.Clean(dir)
	for _, excluded := range exclude {
		if cleanDir == excluded || pathHasPrefix(cleanDir, excluded) {
			return false
		}
	}
	if len(members) == 0 {
		return true
	}
	for _, member := range members {
		if cleanDir == member || pathHasPrefix(cleanDir, member) {
			return true
		}
	}
	return false
}

func pathHasPrefix(pathValue, prefix string) bool {
	cleanPath := filepath.Clean(pathValue)
	cleanPrefix := filepath.Clean(prefix)
	if cleanPath == cleanPrefix {
		return true
	}
	return strings.HasPrefix(cleanPath, cleanPrefix+string(filepath.Separator))
}

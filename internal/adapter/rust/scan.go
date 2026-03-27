package rust

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/filter"
	"github.com/vladimirkasterin/ctx/internal/project"
)

type rustPackageIndex struct {
	Name    string
	Root    string
	RelRoot string
	Edition string
}

func (a *Adapter) Scan(root string) ([]codebase.ScanFile, error) {
	packages, err := loadRustPackageIndex(root)
	if err != nil {
		return nil, err
	}
	walker, err := filter.NewWalker(root, false, "index")
	if err != nil {
		return nil, err
	}

	files := make([]codebase.ScanFile, 0, 128)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
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

		name := entry.Name()
		if skip, _, err := walker.ShouldSkipFile(path, relPath, name); err != nil {
			return err
		} else if skip {
			return nil
		}
		isRust := codebase.IsRustFile(name)
		isModule := codebase.IsRustProjectFile(name)
		if !isRust && !isModule {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read file %s: %w", path, err)
		}

		packageImportPath := ""
		identity := ""
		semanticMeta := ""
		if isRust {
			packageImportPath = rustPackageImportPathForFile(packages, relPath)
		}
		if isModule {
			identity = rustManifestIdentity(packages, relPath)
			if name == "Cargo.toml" {
				semanticMeta = codebase.EncodeManifestMeta(codebase.ParseCargoManifestMeta(data))
			}
		}
		sum := sha256.Sum256(data)
		files = append(files, codebase.ScanFile{
			AbsPath:           path,
			RelPath:           relPath,
			PackageImportPath: packageImportPath,
			Identity:          identity,
			SemanticMeta:      semanticMeta,
			Hash:              hex.EncodeToString(sum[:]),
			SizeBytes:         int64(len(data)),
			IsRust:            isRust,
			IsTest:            isRust && codebase.IsRustTestFile(relPath),
			IsModule:          isModule,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].RelPath < files[j].RelPath
	})
	return files, nil
}

func loadRustPackageIndex(root string) ([]rustPackageIndex, error) {
	packages, err := project.DiscoverRustPackages(root)
	if err != nil {
		return nil, fmt.Errorf("discover rust packages: %w", err)
	}

	index := make([]rustPackageIndex, 0, len(packages))
	for _, pkg := range packages {
		relRoot, err := filepath.Rel(root, pkg.Root)
		if err != nil {
			return nil, fmt.Errorf("make relative rust package root: %w", err)
		}
		relRoot = filepath.ToSlash(relRoot)
		index = append(index, rustPackageIndex{
			Name:    pkg.Name,
			Root:    pkg.Root,
			RelRoot: relRoot,
			Edition: pkg.Edition,
		})
	}

	sort.Slice(index, func(i, j int) bool {
		left := normalizedRustRelRoot(index[i].RelRoot)
		right := normalizedRustRelRoot(index[j].RelRoot)
		if len(left) == len(right) {
			return left < right
		}
		return len(left) > len(right)
	})
	return index, nil
}

func rustPackageImportPathForFile(packages []rustPackageIndex, relPath string) string {
	if !codebase.IsRustFile(relPath) {
		return ""
	}

	for _, pkg := range packages {
		relRoot := normalizedRustRelRoot(pkg.RelRoot)
		if relRoot != "" && relPath != relRoot && !strings.HasPrefix(relPath, relRoot+"/") {
			continue
		}
		return deriveRustPackageImportPath(pkg.Name, relRoot, relPath)
	}
	return deriveRustPackageImportPath(filepath.Base(filepath.Dir(relPath)), "", relPath)
}

func rustManifestIdentity(packages []rustPackageIndex, relPath string) string {
	normalized := filepath.ToSlash(strings.TrimSpace(relPath))
	if normalized == "" || filepath.Base(normalized) != "Cargo.toml" {
		return ""
	}
	manifestDir := filepath.ToSlash(filepath.Dir(normalized))
	if manifestDir == "." {
		manifestDir = ""
	}

	matchCount := 0
	matchName := ""
	for _, pkg := range packages {
		relRoot := normalizedRustRelRoot(pkg.RelRoot)
		if relRoot != manifestDir {
			continue
		}
		matchCount++
		matchName = strings.TrimSpace(pkg.Name)
	}
	if matchCount != 1 {
		return "rust:workspace:" + manifestDir
	}
	return "rust:crate:" + matchName
}

func deriveRustPackageImportPath(crateName, crateRootRel, relPath string) string {
	crateName = strings.TrimSpace(crateName)
	if crateName == "" {
		crateName = "crate"
	}

	relPath = filepath.ToSlash(strings.TrimSpace(relPath))
	crateRootRel = normalizedRustRelRoot(crateRootRel)
	within := relPath
	if crateRootRel != "" {
		within = strings.TrimPrefix(relPath, crateRootRel+"/")
	}

	segments := rustModuleSegments(within)
	if len(segments) == 0 {
		return crateName
	}
	return crateName + "::" + strings.Join(segments, "::")
}

func rustModuleSegments(within string) []string {
	within = filepath.ToSlash(strings.TrimSpace(within))
	switch {
	case within == "build.rs":
		return []string{"build_script"}
	case strings.HasPrefix(within, "src/"):
		return rustSegmentsFromFile(strings.TrimPrefix(within, "src/"), true)
	case strings.HasPrefix(within, "tests/"):
		return append([]string{"tests"}, rustSegmentsFromFile(strings.TrimPrefix(within, "tests/"), false)...)
	case strings.HasPrefix(within, "examples/"):
		return append([]string{"examples"}, rustSegmentsFromFile(strings.TrimPrefix(within, "examples/"), false)...)
	case strings.HasPrefix(within, "benches/"):
		return append([]string{"benches"}, rustSegmentsFromFile(strings.TrimPrefix(within, "benches/"), false)...)
	default:
		return rustSegmentsFromFile(within, false)
	}
}

func rustSegmentsFromFile(path string, srcLayout bool) []string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	path = strings.TrimPrefix(path, "./")
	path = strings.TrimSuffix(path, ".rs")
	if path == "" || path == "." {
		return nil
	}

	parts := strings.Split(path, "/")
	result := make([]string, 0, len(parts))
	for idx, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." {
			continue
		}
		if srcLayout && idx == 0 && (part == "lib" || part == "main") && len(parts) == 1 {
			continue
		}
		if part == "mod" && idx == len(parts)-1 {
			continue
		}
		result = append(result, part)
	}
	return result
}

func normalizedRustRelRoot(value string) string {
	value = filepath.ToSlash(strings.TrimSpace(value))
	if value == "." {
		return ""
	}
	return value
}

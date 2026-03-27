package filter

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/config"
)

type Walker struct {
	includeHidden bool
	matcher       *IgnoreMatcher
}

func NewWalker(root string, includeHidden bool, profile string) (*Walker, error) {
	cfg, err := config.LoadProject(root)
	if err != nil {
		return nil, err
	}
	effective := config.EffectiveProfile(cfg, profile)
	if effective.IncludeHidden != nil {
		includeHidden = includeHidden || *effective.IncludeHidden
	}

	matcher, err := NewIgnoreMatcher(root, profile)
	if err != nil {
		return nil, err
	}
	return &Walker{
		includeHidden: includeHidden,
		matcher:       matcher,
	}, nil
}

func (w *Walker) ShouldSkipDirectory(absPath, relPath, name string) (bool, string, error) {
	if skip, reason := SkipDirectory(absPath, name, w.includeHidden); skip {
		return true, reason, nil
	}
	if w.matcher == nil {
		return false, "", nil
	}
	skip, source, err := w.matcher.Match(relPath, true)
	if err != nil {
		return false, "", err
	}
	if skip {
		return true, "ignored by " + source, nil
	}
	return false, "", nil
}

func (w *Walker) ShouldSkipFile(absPath, relPath, name string) (bool, string, error) {
	if skip, reason := SkipFile(name, w.includeHidden); skip {
		return true, reason, nil
	}
	if w.matcher == nil {
		return false, "", nil
	}
	skip, source, err := w.matcher.Match(relPath, false)
	if err != nil {
		return false, "", err
	}
	if skip {
		return true, "ignored by " + source, nil
	}
	return false, "", nil
}

type IgnoreMatcher struct {
	root       string
	loaded     map[string]bool
	rulesByDir map[string][]ignoreRule
	extraRules []ignoreRule
}

type ignoreRule struct {
	baseDir   string
	source    string
	negated   bool
	dirOnly   bool
	matchBase bool
	regex     *regexp.Regexp
}

func NewIgnoreMatcher(root, profile string) (*IgnoreMatcher, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve ignore root: %w", err)
	}
	cfg, err := config.LoadProject(absRoot)
	if err != nil {
		return nil, err
	}

	extraRules, err := buildConfigIgnoreRules(config.EffectiveProfile(cfg, profile), profile)
	if err != nil {
		return nil, err
	}
	return &IgnoreMatcher{
		root:       absRoot,
		loaded:     make(map[string]bool),
		rulesByDir: make(map[string][]ignoreRule),
		extraRules: extraRules,
	}, nil
}

func (m *IgnoreMatcher) Match(relPath string, isDir bool) (bool, string, error) {
	if m == nil {
		return false, "", nil
	}

	relPath = normalizeIgnoreRelPath(relPath)
	if relPath == "" || relPath == "." {
		return false, "", nil
	}

	var ignored bool
	var source string
	for _, dir := range applicableIgnoreDirs(relPath, isDir) {
		rules, err := m.loadDirRules(dir)
		if err != nil {
			return false, "", err
		}
		for _, rule := range rules {
			if !rule.matches(relPath, isDir) {
				continue
			}
			ignored = !rule.negated
			source = rule.source
		}
	}
	for _, rule := range m.extraRules {
		if !rule.matches(relPath, isDir) {
			continue
		}
		ignored = !rule.negated
		source = rule.source
	}

	return ignored, source, nil
}

func (m *IgnoreMatcher) loadDirRules(dir string) ([]ignoreRule, error) {
	dir = normalizeIgnoreRelPath(dir)
	if m.loaded[dir] {
		return m.rulesByDir[dir], nil
	}
	m.loaded[dir] = true

	basePath := m.root
	if dir != "" {
		basePath = filepath.Join(basePath, filepath.FromSlash(dir))
	}

	for _, fileName := range []string{".gitignore", ".ctxignore"} {
		rules, err := parseIgnoreFile(filepath.Join(basePath, fileName), dir, fileName)
		if err != nil {
			return nil, err
		}
		m.rulesByDir[dir] = append(m.rulesByDir[dir], rules...)
	}
	return m.rulesByDir[dir], nil
}

func parseIgnoreFile(path, baseDir, source string) ([]ignoreRule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	rules := make([]ignoreRule, 0, len(lines))
	for index, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, `\#`) || strings.HasPrefix(line, `\!`) {
			line = line[1:]
		}

		negated := strings.HasPrefix(line, "!")
		if negated {
			line = strings.TrimSpace(strings.TrimPrefix(line, "!"))
		}
		if line == "" {
			continue
		}

		dirOnly := strings.HasSuffix(line, "/")
		line = strings.TrimSuffix(line, "/")
		anchored := strings.HasPrefix(line, "/")
		line = strings.TrimPrefix(line, "/")
		if line == "" {
			continue
		}

		regex, err := ignorePatternRegex(line)
		if err != nil {
			return nil, fmt.Errorf("parse %s:%d: %w", path, index+1, err)
		}

		rules = append(rules, ignoreRule{
			baseDir:   normalizeIgnoreRelPath(baseDir),
			source:    source,
			negated:   negated,
			dirOnly:   dirOnly,
			matchBase: !anchored && !strings.Contains(line, "/"),
			regex:     regex,
		})
	}
	return rules, nil
}

func buildConfigIgnoreRules(profile config.Profile, profileName string) ([]ignoreRule, error) {
	source := ".ctxconfig"
	if value := strings.TrimSpace(profileName); value != "" && value != "global" {
		source += " [" + value + "]"
	}

	rules := make([]ignoreRule, 0, len(profile.ExcludePaths)+len(profile.IncludePaths))
	for _, pattern := range profile.ExcludePaths {
		rule, err := ignoreRuleForPattern(pattern, false, source)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	for _, pattern := range profile.IncludePaths {
		rule, err := ignoreRuleForPattern(pattern, true, source)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func ignoreRuleForPattern(pattern string, negated bool, source string) (ignoreRule, error) {
	pattern = normalizeIgnoreRelPath(pattern)
	if pattern == "" {
		return ignoreRule{}, fmt.Errorf("empty ignore pattern")
	}
	dirOnly := strings.HasSuffix(pattern, "/")
	pattern = strings.TrimSuffix(pattern, "/")
	anchored := strings.HasPrefix(pattern, "/")
	pattern = strings.TrimPrefix(pattern, "/")
	regex, err := ignorePatternRegex(pattern)
	if err != nil {
		return ignoreRule{}, err
	}
	return ignoreRule{
		baseDir:   "",
		source:    source,
		negated:   negated,
		dirOnly:   dirOnly,
		matchBase: !anchored && !strings.Contains(pattern, "/"),
		regex:     regex,
	}, nil
}

func ignorePatternRegex(pattern string) (*regexp.Regexp, error) {
	var builder strings.Builder
	builder.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				builder.WriteString(".*")
				i++
				continue
			}
			builder.WriteString(`[^/]*`)
		case '?':
			builder.WriteString(`[^/]`)
		default:
			builder.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
	}
	builder.WriteString("$")
	return regexp.Compile(builder.String())
}

func (r ignoreRule) matches(relPath string, isDir bool) bool {
	if r.dirOnly && !isDir {
		return false
	}

	candidate, ok := ignoreRelativeToBase(relPath, r.baseDir)
	if !ok || candidate == "" || candidate == "." {
		return false
	}

	if r.matchBase {
		return r.regex.MatchString(pathBase(candidate))
	}
	return r.regex.MatchString(candidate)
}

func ignoreRelativeToBase(relPath, baseDir string) (string, bool) {
	relPath = normalizeIgnoreRelPath(relPath)
	baseDir = normalizeIgnoreRelPath(baseDir)
	if baseDir == "" {
		return relPath, true
	}
	if relPath == baseDir {
		return ".", true
	}
	if !strings.HasPrefix(relPath, baseDir+"/") {
		return "", false
	}
	return strings.TrimPrefix(relPath, baseDir+"/"), true
}

func applicableIgnoreDirs(relPath string, isDir bool) []string {
	relPath = normalizeIgnoreRelPath(relPath)
	parent := pathDir(relPath)
	if parent == "." {
		parent = ""
	}
	if !isDir && relPath != "" {
		parent = pathDir(relPath)
		if parent == "." {
			parent = ""
		}
	}

	if isDir {
		parent = pathDir(relPath)
		if parent == "." {
			parent = ""
		}
	}

	dirs := []string{""}
	if parent == "" {
		return dirs
	}

	parts := strings.Split(parent, "/")
	current := ""
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if current == "" {
			current = part
		} else {
			current += "/" + part
		}
		dirs = append(dirs, current)
	}
	return dirs
}

func normalizeIgnoreRelPath(value string) string {
	value = filepath.ToSlash(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "./")
	if value == "." {
		return ""
	}
	return value
}

func pathBase(value string) string {
	value = normalizeIgnoreRelPath(value)
	if idx := strings.LastIndex(value, "/"); idx >= 0 {
		return value[idx+1:]
	}
	return value
}

func pathDir(value string) string {
	value = normalizeIgnoreRelPath(value)
	if value == "" {
		return ""
	}
	if idx := strings.LastIndex(value, "/"); idx >= 0 {
		return value[:idx]
	}
	return ""
}

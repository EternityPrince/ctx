package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type ProjectConfig struct {
	Path     string
	Global   Profile
	Profiles map[string]Profile
}

type Profile struct {
	IncludeHidden    *bool
	MaxFileSize      *int64
	KeepEmpty        *bool
	IncludeGenerated *bool
	IncludeMinified  *bool
	IncludeArtifacts *bool
	SummaryOnly      *bool
	NoTree           *bool
	NoContents       *bool
	Extensions       []string
	IncludePaths     []string
	ExcludePaths     []string
}

func LoadProject(root string) (ProjectConfig, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return ProjectConfig{}, fmt.Errorf("resolve config root: %w", err)
	}

	path := filepath.Join(absRoot, ".ctxconfig")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ProjectConfig{
				Path:     path,
				Profiles: make(map[string]Profile),
			}, nil
		}
		return ProjectConfig{}, fmt.Errorf("read .ctxconfig: %w", err)
	}

	cfg := ProjectConfig{
		Path:     path,
		Profiles: make(map[string]Profile),
	}
	current := ""
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	for index, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = normalizeProfileName(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")))
			if current != "" && current != "global" {
				profile := cfg.Profiles[current]
				cfg.Profiles[current] = profile
			}
			continue
		}

		eq := strings.Index(line, "=")
		if eq <= 0 {
			return ProjectConfig{}, fmt.Errorf("parse .ctxconfig:%d: expected key = value", index+1)
		}
		key := normalizeConfigKey(strings.TrimSpace(line[:eq]))
		value := strings.TrimSpace(line[eq+1:])
		if err := setConfigValue(&cfg, current, key, value); err != nil {
			return ProjectConfig{}, fmt.Errorf("parse .ctxconfig:%d: %w", index+1, err)
		}
	}

	cfg.Global.Extensions = normalizeExtensions(strings.Join(cfg.Global.Extensions, ","))
	cfg.Global.IncludePaths = normalizePathList(cfg.Global.IncludePaths)
	cfg.Global.ExcludePaths = normalizePathList(cfg.Global.ExcludePaths)
	for name, profile := range cfg.Profiles {
		profile.Extensions = normalizeExtensions(strings.Join(profile.Extensions, ","))
		profile.IncludePaths = normalizePathList(profile.IncludePaths)
		profile.ExcludePaths = normalizePathList(profile.ExcludePaths)
		cfg.Profiles[name] = profile
	}

	return cfg, nil
}

func EffectiveProfile(cfg ProjectConfig, name string) Profile {
	profile := cfg.Global
	if name == "" || name == "global" {
		return profile
	}
	applyProfile(&profile, cfg.Profiles[normalizeProfileName(name)])
	return profile
}

func ApplyProfile(options Options, cfg ProjectConfig, name string) Options {
	profile := EffectiveProfile(cfg, name)
	if profile.IncludeHidden != nil {
		options.IncludeHidden = *profile.IncludeHidden || options.IncludeHidden
	}
	if profile.MaxFileSize != nil && options.MaxFileSize < 0 {
		options.MaxFileSize = *profile.MaxFileSize
	}
	if profile.KeepEmpty != nil {
		options.KeepEmpty = options.KeepEmpty || *profile.KeepEmpty
	}
	if profile.IncludeGenerated != nil {
		options.IncludeGenerated = options.IncludeGenerated || *profile.IncludeGenerated
	}
	if profile.IncludeMinified != nil {
		options.IncludeMinified = options.IncludeMinified || *profile.IncludeMinified
	}
	if profile.IncludeArtifacts != nil {
		options.IncludeArtifacts = options.IncludeArtifacts || *profile.IncludeArtifacts
	}
	if profile.SummaryOnly != nil {
		options.SummaryOnly = options.SummaryOnly || *profile.SummaryOnly
	}
	if profile.NoTree != nil {
		options.NoTree = options.NoTree || *profile.NoTree
	}
	if profile.NoContents != nil {
		options.NoContents = options.NoContents || *profile.NoContents
	}
	if len(options.Extensions) == 0 && len(profile.Extensions) > 0 {
		options.Extensions = append([]string(nil), profile.Extensions...)
	}
	if options.MaxFileSize < 0 {
		options.MaxFileSize = 2 * 1024 * 1024
	}
	return options
}

func HasConfigFile(cfg ProjectConfig) bool {
	if strings.TrimSpace(cfg.Path) == "" {
		return false
	}
	_, err := os.Stat(cfg.Path)
	return err == nil
}

func setConfigValue(cfg *ProjectConfig, profileName, key, rawValue string) error {
	profile := cfg.Global
	if name := normalizeProfileName(profileName); name != "" && name != "global" {
		profile = cfg.Profiles[name]
		defer func() {
			cfg.Profiles[name] = profile
		}()
	} else {
		defer func() {
			cfg.Global = profile
		}()
	}

	switch key {
	case "include_hidden":
		value, err := parseBoolValue(rawValue)
		if err != nil {
			return err
		}
		profile.IncludeHidden = &value
	case "max_file_size":
		value, err := parseInt64Value(rawValue)
		if err != nil {
			return err
		}
		profile.MaxFileSize = &value
	case "keep_empty":
		value, err := parseBoolValue(rawValue)
		if err != nil {
			return err
		}
		profile.KeepEmpty = &value
	case "include_generated":
		value, err := parseBoolValue(rawValue)
		if err != nil {
			return err
		}
		profile.IncludeGenerated = &value
	case "include_minified":
		value, err := parseBoolValue(rawValue)
		if err != nil {
			return err
		}
		profile.IncludeMinified = &value
	case "include_artifacts":
		value, err := parseBoolValue(rawValue)
		if err != nil {
			return err
		}
		profile.IncludeArtifacts = &value
	case "summary_only":
		value, err := parseBoolValue(rawValue)
		if err != nil {
			return err
		}
		profile.SummaryOnly = &value
	case "no_tree":
		value, err := parseBoolValue(rawValue)
		if err != nil {
			return err
		}
		profile.NoTree = &value
	case "no_contents":
		value, err := parseBoolValue(rawValue)
		if err != nil {
			return err
		}
		profile.NoContents = &value
	case "extensions":
		profile.Extensions = parseListValue(rawValue)
	case "include_paths":
		profile.IncludePaths = parseListValue(rawValue)
	case "exclude_paths":
		profile.ExcludePaths = parseListValue(rawValue)
	default:
		return fmt.Errorf("unsupported key %q", key)
	}
	return nil
}

func applyProfile(dst *Profile, src Profile) {
	if src.IncludeHidden != nil {
		dst.IncludeHidden = src.IncludeHidden
	}
	if src.MaxFileSize != nil {
		dst.MaxFileSize = src.MaxFileSize
	}
	if src.KeepEmpty != nil {
		dst.KeepEmpty = src.KeepEmpty
	}
	if src.IncludeGenerated != nil {
		dst.IncludeGenerated = src.IncludeGenerated
	}
	if src.IncludeMinified != nil {
		dst.IncludeMinified = src.IncludeMinified
	}
	if src.IncludeArtifacts != nil {
		dst.IncludeArtifacts = src.IncludeArtifacts
	}
	if src.SummaryOnly != nil {
		dst.SummaryOnly = src.SummaryOnly
	}
	if src.NoTree != nil {
		dst.NoTree = src.NoTree
	}
	if src.NoContents != nil {
		dst.NoContents = src.NoContents
	}
	if len(src.Extensions) > 0 {
		dst.Extensions = append([]string(nil), src.Extensions...)
	}
	if len(src.IncludePaths) > 0 {
		dst.IncludePaths = append([]string(nil), src.IncludePaths...)
	}
	if len(src.ExcludePaths) > 0 {
		dst.ExcludePaths = append([]string(nil), src.ExcludePaths...)
	}
}

func normalizeProfileName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	switch value {
	case "", "global", "default":
		return "global"
	default:
		return value
	}
}

func normalizeConfigKey(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "-", "_")
	return value
}

func parseBoolValue(value string) (bool, error) {
	result, err := strconv.ParseBool(trimQuoted(value))
	if err != nil {
		return false, fmt.Errorf("invalid bool %q", value)
	}
	return result, nil
}

func parseInt64Value(value string) (int64, error) {
	result, err := strconv.ParseInt(trimQuoted(value), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid integer %q", value)
	}
	return result, nil
}

func parseListValue(value string) []string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = trimQuoted(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		result = append(result, part)
	}
	return result
}

func normalizePathList(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
		value = strings.TrimPrefix(value, "./")
		if value == "" {
			continue
		}
		result = append(result, value)
	}
	return result
}

func trimQuoted(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"`)
	value = strings.Trim(value, `'`)
	return value
}

func normalizeExtensions(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = trimQuoted(strings.TrimSpace(strings.ToLower(part)))
		if part == "" {
			continue
		}
		if part != "[no extension]" && !strings.HasPrefix(part, ".") {
			part = "." + part
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		result = append(result, part)
	}
	return result
}

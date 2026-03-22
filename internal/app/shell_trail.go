package app

import (
	"fmt"
	"strings"
)

func (s *shellSession) rememberTrail(title string) {
	label := s.trailLabel(title)
	if label == "" {
		return
	}
	if s.trailIndex >= 0 && s.trailIndex < len(s.trail) && s.trail[s.trailIndex].Label == label {
		return
	}
	if s.trailIndex+1 < len(s.trail) {
		s.trail = append([]shellTrailEntry{}, s.trail[:s.trailIndex+1]...)
	}
	s.trail = append(s.trail, shellTrailEntry{Label: label})
	s.trailIndex = len(s.trail) - 1
}

func (s *shellSession) trailLabel(title string) string {
	title = strings.TrimSpace(title)
	switch s.currentMode {
	case "landing":
		return "home"
	case "tree":
		return "tree"
	case "file":
		base := shortenQName(s.info.ModulePath, s.currentFile)
		if title == "File Report" {
			return "report:file:" + base
		}
		if title == "Location" {
			return "loc:" + base
		}
		if base == "" {
			return strings.ToLower(title)
		}
		return "file:" + base
	case "report":
		return "report:project"
	case "status":
		return "status"
	}

	focus := shortenQName(s.info.ModulePath, s.currentQName)
	if focus == "" && s.currentFile != "" {
		focus = shortenQName(s.info.ModulePath, s.currentFile)
	}
	switch title {
	case "Entity Report":
		return "report:" + focus
	case "Impact":
		return "impact:" + focus
	case "Source", "Body Preview":
		return "source:" + focus
	case "Related Tests":
		return "tests:" + focus
	case "Direct Callers":
		return "callers:" + focus
	case "Direct Callees":
		return "callees:" + focus
	case "References In":
		return "refs.in:" + focus
	case "References Out":
		return "refs.out:" + focus
	case "Related Symbols":
		return "related:" + focus
	case "Next Steps":
		return "menu:" + focus
	case "Matches":
		return "matches"
	case "Error":
		return "error"
	case "Help":
		return "help"
	}
	if focus == "" {
		return strings.ToLower(title)
	}
	return strings.ToLower(title) + ":" + focus
}

func (s *shellSession) renderTrail(limit int) string {
	if len(s.trail) == 0 {
		return s.palette.muted("home")
	}
	start := max(0, len(s.trail)-limit)
	parts := make([]string, 0, len(s.trail)-start)
	if start > 0 {
		parts = append(parts, s.palette.muted("..."))
	}
	for idx := start; idx < len(s.trail); idx++ {
		label := s.trail[idx].Label
		text := label
		if idx == s.trailIndex {
			text = s.palette.accent(label)
		} else {
			text = s.palette.muted(label)
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, " "+s.palette.muted("<->")+" ")
}

func (s *shellSession) resetToHome() {
	s.currentKey = ""
	s.currentQName = ""
	s.currentFile = ""
	s.currentMode = "landing"
}

func (s *shellSession) describeFileTarget(path string) string {
	if path == "" {
		return ""
	}
	return fmt.Sprintf("file:%s", shortenQName(s.info.ModulePath, path))
}

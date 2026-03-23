package app

import "github.com/vladimirkasterin/ctx/internal/storage"

func (s *shellSession) ensureRiskContext() error {
	if s.riskHotScores != nil && s.riskRecentFiles != nil {
		return nil
	}

	report, err := s.store.LoadReportView(64)
	if err != nil {
		return err
	}
	hotScores := make(map[string]int)
	for _, item := range rankShellHotFiles(report, "") {
		hotScores[item.Path] = item.Score
	}

	recentFiles, err := loadRecentChangedFileSet(s.store)
	if err != nil {
		return err
	}

	s.riskHotScores = hotScores
	s.riskRecentFiles = recentFiles
	return nil
}

func (s *shellSession) fileRiskSignals(filePath string, fallbackHotScore int) (int, bool, error) {
	if err := s.ensureRiskContext(); err != nil {
		return fallbackHotScore, false, err
	}
	hotScore := fallbackHotScore
	if value, ok := s.riskHotScores[filePath]; ok {
		hotScore = value
	}
	_, recentChanged := s.riskRecentFiles[filePath]
	return hotScore, recentChanged, nil
}

func (s *shellSession) symbolJourneyRiskSummary(view storage.SymbolView) (string, error) {
	summary, err := s.store.LoadFileSummary(view.Symbol.FilePath)
	if err != nil {
		return "", err
	}
	hotScore, recentChanged, err := s.fileRiskSignals(view.Symbol.FilePath, 0)
	if err != nil {
		return "", err
	}
	return symbolViewRiskWithFileSummary(view, summary, hotScore, recentChanged), nil
}

func loadRecentChangedFileSet(store *storage.Store) (map[string]struct{}, error) {
	current, ok, err := store.CurrentSnapshot()
	if err != nil {
		return nil, err
	}
	if !ok || !current.ParentID.Valid {
		return map[string]struct{}{}, nil
	}

	diff, err := store.Diff(current.ParentID.Int64, current.ID)
	if err != nil {
		return nil, err
	}

	changed := make(map[string]struct{}, len(diff.AddedFiles)+len(diff.ChangedFiles))
	for _, filePath := range diff.AddedFiles {
		changed[filePath] = struct{}{}
	}
	for _, filePath := range diff.ChangedFiles {
		changed[filePath] = struct{}{}
	}
	return changed, nil
}

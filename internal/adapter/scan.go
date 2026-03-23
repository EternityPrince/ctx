package adapter

import "github.com/vladimirkasterin/ctx/internal/codebase"

func (a *Adapter) Scan(root string) ([]codebase.ScanFile, error) {
	parts := make([][]codebase.ScanFile, 0, 2)

	if hasGoProject(root) {
		files, err := a.goAdapter.Scan(root)
		if err != nil {
			return nil, err
		}
		parts = append(parts, files)
	}

	files, err := a.pythonAdapter.Scan(root)
	if err != nil {
		return nil, err
	}
	parts = append(parts, files)

	return mergeScannedFiles(parts...), nil
}

package codebase

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

func ScanFingerprint(scanned []ScanFile) string {
	if len(scanned) == 0 {
		return "empty"
	}

	files := append([]ScanFile(nil), scanned...)
	sort.Slice(files, func(i, j int) bool {
		return files[i].RelPath < files[j].RelPath
	})

	hash := sha256.New()
	for _, file := range files {
		_, _ = fmt.Fprintf(
			hash,
			"%s\t%s\t%s\t%s\t%d\t%t\t%t\t%t\t%t\n",
			file.RelPath,
			file.PackageImportPath,
			file.Identity,
			file.Hash,
			file.SizeBytes,
			file.IsGo,
			file.IsRust,
			file.IsTest,
			file.IsModule,
		)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

package python

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

//go:embed runtime/*.py
var runtimeFiles embed.FS

var (
	runtimeDirOnce sync.Once
	runtimeDirPath string
	runtimeDirErr  error
)

func runAnalyzer(input analyzerInput) (analyzerOutput, error) {
	runtimeDir, err := materializeRuntimeDir()
	if err != nil {
		return analyzerOutput{}, err
	}

	payload, err := json.Marshal(input)
	if err != nil {
		return analyzerOutput{}, fmt.Errorf("marshal python analyzer input: %w", err)
	}

	cmd := exec.Command("python3", filepath.Join(runtimeDir, "entry.py"))
	cmd.Stdin = bytes.NewReader(payload)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return analyzerOutput{}, fmt.Errorf("run python analyzer: %s", commandErrorMessage(err, stdout.String(), stderr.String()))
	}

	var output analyzerOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		return analyzerOutput{}, fmt.Errorf("decode python analyzer output: %w", err)
	}
	return output, nil
}

func materializeRuntimeDir() (string, error) {
	runtimeDirOnce.Do(func() {
		runtimeDirPath, runtimeDirErr = writeRuntimeDir()
	})
	return runtimeDirPath, runtimeDirErr
}

func writeRuntimeDir() (string, error) {
	dir, err := os.MkdirTemp("", "ctx-python-runtime-*")
	if err != nil {
		return "", fmt.Errorf("create python runtime dir: %w", err)
	}

	entries, err := fs.ReadDir(runtimeFiles, "runtime")
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("list embedded python runtime: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		data, err := runtimeFiles.ReadFile(path.Join("runtime", entry.Name()))
		if err != nil {
			_ = os.RemoveAll(dir)
			return "", fmt.Errorf("read embedded python runtime file %s: %w", entry.Name(), err)
		}

		targetPath := filepath.Join(dir, entry.Name())
		if err := os.WriteFile(targetPath, data, 0o644); err != nil {
			_ = os.RemoveAll(dir)
			return "", fmt.Errorf("write python runtime file %s: %w", entry.Name(), err)
		}
	}

	return dir, nil
}

func commandErrorMessage(err error, stdout, stderr string) string {
	for _, candidate := range []string{stderr, stdout, err.Error()} {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			return candidate
		}
	}
	return "unknown error"
}

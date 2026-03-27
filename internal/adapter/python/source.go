package python

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

const locateSymbolScript = `
import ast
import json
import sys
import tokenize

payload = json.load(sys.stdin)
path = payload["path"]
name = payload["name"]
kind = payload["kind"]
receiver = payload.get("receiver") or ""
line = int(payload["line"])

result = {"start": 0, "end": 0}

try:
    with tokenize.open(path) as handle:
        source = handle.read()
    tree = ast.parse(source, filename=path, type_comments=True)
except Exception:
    json.dump(result, sys.stdout)
    raise SystemExit(0)

for node in tree.body:
    if kind == "func" and isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)) and node.name == name and node.lineno == line:
        result = {"start": node.lineno, "end": getattr(node, "end_lineno", node.lineno)}
        break
    if isinstance(node, ast.ClassDef):
        if kind == "class" and node.name == name and node.lineno == line:
            result = {"start": node.lineno, "end": getattr(node, "end_lineno", node.lineno)}
            break
        if kind == "method" and node.name == receiver:
            for child in node.body:
                if isinstance(child, (ast.FunctionDef, ast.AsyncFunctionDef)) and child.name == name and child.lineno == line:
                    result = {"start": child.lineno, "end": getattr(child, "end_lineno", child.lineno)}
                    break
            if result["start"]:
                break

json.dump(result, sys.stdout)
`

type locateSymbolInput struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Receiver string `json:"receiver"`
	Line     int    `json:"line"`
}

type locateSymbolOutput struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

func LocateSymbolBlock(path, name, kind, receiver string, line int) (int, int, error) {
	payload, err := json.Marshal(locateSymbolInput{
		Path:     path,
		Name:     name,
		Kind:     kind,
		Receiver: receiver,
		Line:     line,
	})
	if err != nil {
		return 0, 0, fmt.Errorf("marshal python source lookup: %w", err)
	}

	cmd := exec.Command("python3", "-c", locateSymbolScript)
	cmd.Stdin = bytes.NewReader(payload)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return 0, 0, fmt.Errorf("run python source lookup: %s", message)
	}

	var output locateSymbolOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		return 0, 0, fmt.Errorf("decode python source lookup: %w", err)
	}
	if output.Start <= 0 || output.End < output.Start {
		return 0, 0, fmt.Errorf("symbol block not found")
	}
	return output.Start, output.End, nil
}

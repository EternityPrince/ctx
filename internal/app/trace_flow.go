package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/sourcerange"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

type traceFlowView struct {
	InputLanes   []string
	Ingress      []string
	FlowPath     []string
	OutputLanes  []string
	StateTouches []string
	Precision    string
}

func buildTraceFlow(projectRoot, modulePath string, symbol storage.SymbolMatch, callers, callees []storage.RelatedSymbolView, refsOut []storage.RefView, flows []storage.FlowEdgeView) traceFlowView {
	inputNames := extractFlowInputs(symbol.Signature, symbol.Receiver)
	if len(flows) > 0 {
		view := traceFlowView{
			InputLanes:   buildFlowInputLanesFromEdges(modulePath, symbol, flows),
			Ingress:      buildFlowIngress(modulePath, callers),
			FlowPath:     buildFlowPathFromEdges(modulePath, flows),
			OutputLanes:  buildFlowOutputsFromEdges(modulePath, flows),
			StateTouches: buildFlowStateTouches(modulePath, refsOut, callees),
			Precision:    "analyzer-backed data journey from indexed flow edges, with call/ref context layered on top",
		}
		if len(view.InputLanes) == 0 {
			view.InputLanes = buildFlowInputLanes(modulePath, symbol, inputNames)
		}
		if len(view.FlowPath) == 0 || len(view.OutputLanes) == 0 {
			startLine, lineByNumber, err := loadSymbolBlockLines(projectRoot, symbol)
			if err == nil {
				callees = sortRelatedByUseSite(callees)
				if len(view.FlowPath) == 0 {
					seenFlow := make(map[string]struct{})
					for _, callee := range callees {
						snippet := strings.TrimSpace(lineByNumber[callee.UseLine])
						step := buildFlowPathStep(modulePath, callee, snippet, inputNames)
						if step == "" {
							continue
						}
						key := fmt.Sprintf("%d|%s", callee.UseLine, step)
						if _, ok := seenFlow[key]; ok {
							continue
						}
						seenFlow[key] = struct{}{}
						view.FlowPath = append(view.FlowPath, step)
					}
				}
				if len(view.OutputLanes) == 0 {
					view.OutputLanes = buildFlowOutputs(modulePath, symbol, inputNames, startLine, lineByNumber, callees)
				}
			}
		}
		if len(view.InputLanes) > 0 || len(view.FlowPath) > 0 || len(view.OutputLanes) > 0 {
			return view
		}
	}

	startLine, lineByNumber, err := loadSymbolBlockLines(projectRoot, symbol)
	if err != nil {
		return traceFlowView{
			Precision: "flow is derived from indexed call/ref edges plus local source lookup when the symbol body can be found",
		}
	}

	view := traceFlowView{
		InputLanes: buildFlowInputLanes(modulePath, symbol, inputNames),
		Ingress:    buildFlowIngress(modulePath, callers),
		Precision:  "best-effort data journey from indexed call/ref edges, use-site lines, and the current symbol body",
	}

	callees = sortRelatedByUseSite(callees)
	seenFlow := make(map[string]struct{})
	for _, callee := range callees {
		snippet := strings.TrimSpace(lineByNumber[callee.UseLine])
		step := buildFlowPathStep(modulePath, callee, snippet, inputNames)
		if step == "" {
			continue
		}
		key := fmt.Sprintf("%d|%s", callee.UseLine, step)
		if _, ok := seenFlow[key]; ok {
			continue
		}
		seenFlow[key] = struct{}{}
		view.FlowPath = append(view.FlowPath, step)
	}

	view.OutputLanes = buildFlowOutputs(modulePath, symbol, inputNames, startLine, lineByNumber, callees)
	view.StateTouches = buildFlowStateTouches(modulePath, refsOut, callees)
	return view
}

func traceFlowEmpty(view traceFlowView) bool {
	return len(view.InputLanes) == 0 &&
		len(view.Ingress) == 0 &&
		len(view.FlowPath) == 0 &&
		len(view.OutputLanes) == 0 &&
		len(view.StateTouches) == 0
}

func renderHumanTraceFlow(stdout io.Writer, p palette, view traceFlowView, limit int) error {
	if traceFlowEmpty(view) {
		return nil
	}
	if limit <= 0 {
		limit = 6
	}
	if _, err := fmt.Fprintf(stdout, "%s\n", p.section("Data Flow")); err != nil {
		return err
	}
	if strings.TrimSpace(view.Precision) != "" {
		if _, err := fmt.Fprintf(stdout, "  %s %s\n", p.label("Precision:"), view.Precision); err != nil {
			return err
		}
	}
	if err := renderHumanTraceFlowGroup(stdout, p, "Input Lanes", view.InputLanes, limit); err != nil {
		return err
	}
	if err := renderHumanTraceFlowGroup(stdout, p, "Ingress Signals", view.Ingress, limit); err != nil {
		return err
	}
	if err := renderHumanTraceFlowGroup(stdout, p, "Flow Path", view.FlowPath, limit); err != nil {
		return err
	}
	if err := renderHumanTraceFlowGroup(stdout, p, "Output Lanes", view.OutputLanes, limit); err != nil {
		return err
	}
	if err := renderHumanTraceFlowGroup(stdout, p, "State Touches", view.StateTouches, limit); err != nil {
		return err
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

func renderAITraceFlow(stdout io.Writer, view traceFlowView, limit int) error {
	if traceFlowEmpty(view) {
		return nil
	}
	if _, err := fmt.Fprintf(stdout, "flow_precision=%q\n", view.Precision); err != nil {
		return err
	}
	if err := renderAITraceFlowGroup(stdout, "flow_inputs", view.InputLanes, limit); err != nil {
		return err
	}
	if err := renderAITraceFlowGroup(stdout, "flow_ingress", view.Ingress, limit); err != nil {
		return err
	}
	if err := renderAITraceFlowGroup(stdout, "flow_path", view.FlowPath, limit); err != nil {
		return err
	}
	if err := renderAITraceFlowGroup(stdout, "flow_outputs", view.OutputLanes, limit); err != nil {
		return err
	}
	return renderAITraceFlowGroup(stdout, "flow_state", view.StateTouches, limit)
}

func renderHumanTraceFlowGroup(stdout io.Writer, p palette, title string, items []string, limit int) error {
	if len(items) == 0 {
		return nil
	}
	if limit <= 0 || limit > len(items) {
		limit = len(items)
	}
	if _, err := fmt.Fprintf(stdout, "  %s (%d)\n", p.label(title), len(items)); err != nil {
		return err
	}
	for _, item := range items[:limit] {
		if _, err := fmt.Fprintf(stdout, "    - %s\n", item); err != nil {
			return err
		}
	}
	if len(items) > limit {
		if _, err := fmt.Fprintf(stdout, "    %s +%d more\n", p.label("more:"), len(items)-limit); err != nil {
			return err
		}
	}
	return nil
}

func renderAITraceFlowGroup(stdout io.Writer, key string, items []string, limit int) error {
	if _, err := fmt.Fprintf(stdout, "%s=%d\n", key, len(items)); err != nil {
		return err
	}
	if limit <= 0 || limit > len(items) {
		limit = len(items)
	}
	for _, item := range items[:limit] {
		if _, err := fmt.Fprintf(stdout, "%s_item=%q\n", key, item); err != nil {
			return err
		}
	}
	return nil
}

func buildFlowInputLanes(modulePath string, symbol storage.SymbolMatch, inputNames []string) []string {
	items := make([]string, 0, len(inputNames)+1)
	if symbol.Receiver != "" {
		items = append(items, fmt.Sprintf("receiver %s anchors method state.", symbol.Receiver))
	}
	for _, input := range inputNames {
		items = append(items, fmt.Sprintf("input %s enters %s.", input, shortenQName(modulePath, symbol.QName)))
	}
	return items
}

func buildFlowInputLanesFromEdges(modulePath string, symbol storage.SymbolMatch, flows []storage.FlowEdgeView) []string {
	items := make([]string, 0, 4)
	seen := make(map[string]struct{})
	for _, flow := range flows {
		switch flow.SourceKind {
		case "receiver":
			label := flow.SourceLabel
			if label == "" {
				label = symbol.Receiver
			}
			if label == "" {
				label = "receiver"
			}
			item := fmt.Sprintf("receiver %s carries method state through %s.", label, shortenQName(modulePath, symbol.QName))
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			items = append(items, item)
		case "param":
			if flow.SourceLabel == "" {
				continue
			}
			item := fmt.Sprintf("input %s is consumed inside %s.", flow.SourceLabel, shortenQName(modulePath, symbol.QName))
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			items = append(items, item)
		}
	}
	return items
}

func buildFlowIngress(modulePath string, callers []storage.RelatedSymbolView) []string {
	if len(callers) == 0 {
		return nil
	}
	callers = sortRelatedByUseSite(callers)
	items := make([]string, 0, min(4, len(callers)))
	for _, caller := range callers[:min(4, len(callers))] {
		snippet := caller.Why
		if snippet == "" {
			snippet = fmt.Sprintf("@ %s:%d", caller.UseFilePath, caller.UseLine)
		}
		items = append(items, fmt.Sprintf("%s feeds this symbol via %s.", shortenQName(modulePath, caller.Symbol.QName), snippet))
	}
	return items
}

func buildFlowPathStep(modulePath string, callee storage.RelatedSymbolView, snippet string, inputNames []string) string {
	calleeName := shortenQName(modulePath, callee.Symbol.QName)
	lineLabel := fmt.Sprintf("%s:%d", callee.UseFilePath, callee.UseLine)
	usedInputs := flowInputsUsedInLine(snippet, inputNames)
	switch {
	case len(usedInputs) > 0 && strings.Contains(snippet, "return "):
		return fmt.Sprintf("%s returns through %s @ %s.", strings.Join(usedInputs, ", "), calleeName, lineLabel)
	case len(usedInputs) > 0:
		return fmt.Sprintf("%s flows into %s @ %s.", strings.Join(usedInputs, ", "), calleeName, lineLabel)
	case strings.Contains(snippet, "return "):
		return fmt.Sprintf("%s produces an immediate return path @ %s.", calleeName, lineLabel)
	case strings.Contains(snippet, "=") || strings.Contains(snippet, ":="):
		return fmt.Sprintf("%s writes into a local handoff step @ %s.", calleeName, lineLabel)
	default:
		return fmt.Sprintf("%s participates in the local flow @ %s.", calleeName, lineLabel)
	}
}

func buildFlowOutputs(modulePath string, symbol storage.SymbolMatch, inputNames []string, startLine int, lineByNumber map[int]string, callees []storage.RelatedSymbolView) []string {
	if len(lineByNumber) == 0 {
		return nil
	}
	calleeNames := make([]string, 0, len(callees))
	for _, callee := range callees {
		calleeNames = append(calleeNames, callee.Symbol.Name)
	}
	lines := make([]int, 0, len(lineByNumber))
	for line := range lineByNumber {
		lines = append(lines, line)
	}
	sort.Ints(lines)

	items := make([]string, 0, 4)
	for _, line := range lines {
		snippet := strings.TrimSpace(lineByNumber[line])
		if !strings.Contains(snippet, "return") {
			continue
		}
		sources := flowInputsUsedInLine(snippet, inputNames)
		calls := flowInputsUsedInLine(snippet, calleeNames)
		switch {
		case len(sources) > 0 && len(calls) > 0:
			items = append(items, fmt.Sprintf("return exits with %s after %s @ %s:%d.", strings.Join(sources, ", "), strings.Join(calls, ", "), symbol.FilePath, line))
		case len(calls) > 0:
			items = append(items, fmt.Sprintf("return forwards the result of %s @ %s:%d.", strings.Join(calls, ", "), symbol.FilePath, line))
		case len(sources) > 0:
			items = append(items, fmt.Sprintf("return carries %s out of the symbol @ %s:%d.", strings.Join(sources, ", "), symbol.FilePath, line))
		default:
			items = append(items, fmt.Sprintf("return exits through %q @ %s:%d.", oneLine(snippet), symbol.FilePath, line))
		}
	}
	if len(items) == 0 && len(callees) > 0 {
		first := sortRelatedByUseSite(callees)[0]
		items = append(items, fmt.Sprintf("%s is the first downstream handoff inside %s.", shortenQName(modulePath, first.Symbol.QName), shortenQName(modulePath, symbol.QName)))
	}
	return items
}

func buildFlowStateTouches(modulePath string, refsOut []storage.RefView, callees []storage.RelatedSymbolView) []string {
	if len(refsOut) == 0 {
		return nil
	}
	callLines := make(map[int]struct{}, len(callees))
	for _, callee := range callees {
		callLines[callee.UseLine] = struct{}{}
	}
	sort.Slice(refsOut, func(i, j int) bool {
		if refsOut[i].UseLine != refsOut[j].UseLine {
			return refsOut[i].UseLine < refsOut[j].UseLine
		}
		return refsOut[i].Symbol.QName < refsOut[j].Symbol.QName
	})
	items := make([]string, 0, 4)
	seen := make(map[string]struct{})
	for _, ref := range refsOut {
		if _, ok := callLines[ref.UseLine]; ok {
			continue
		}
		label := fmt.Sprintf("%s [%s] @ %s:%d", shortenQName(modulePath, ref.Symbol.QName), ref.Kind, ref.UseFilePath, ref.UseLine)
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		items = append(items, label)
		if len(items) >= 4 {
			break
		}
	}
	return items
}

func buildFlowPathFromEdges(modulePath string, flows []storage.FlowEdgeView) []string {
	items := make([]string, 0, len(flows))
	seen := make(map[string]struct{})
	for _, flow := range flows {
		if flow.Kind != "param_to_call" && flow.Kind != "receiver_to_call" {
			continue
		}
		target := flowTargetLabel(modulePath, flow)
		if target == "" {
			continue
		}
		lineLabel := fmt.Sprintf("%s:%d", flow.UseFilePath, flow.UseLine)
		var item string
		switch flow.SourceKind {
		case "receiver":
			label := flow.SourceLabel
			if label == "" {
				label = "receiver"
			}
			item = fmt.Sprintf("receiver %s flows into %s @ %s.", label, target, lineLabel)
		case "param":
			if flow.SourceLabel == "" {
				continue
			}
			item = fmt.Sprintf("%s flows into %s @ %s.", flow.SourceLabel, target, lineLabel)
		default:
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		items = append(items, item)
	}
	return items
}

func buildFlowOutputsFromEdges(modulePath string, flows []storage.FlowEdgeView) []string {
	items := make([]string, 0, len(flows))
	seen := make(map[string]struct{})
	for _, flow := range flows {
		lineLabel := fmt.Sprintf("%s:%d", flow.UseFilePath, flow.UseLine)
		var item string
		switch flow.Kind {
		case "call_to_return":
			source := flowSourceLabel(modulePath, flow)
			if source == "" {
				continue
			}
			item = fmt.Sprintf("return forwards the result of %s @ %s.", source, lineLabel)
		case "param_to_return":
			if flow.SourceLabel == "" {
				continue
			}
			item = fmt.Sprintf("return carries %s out of the symbol @ %s.", flow.SourceLabel, lineLabel)
		case "receiver_to_return":
			label := flow.SourceLabel
			if label == "" {
				label = "receiver"
			}
			item = fmt.Sprintf("return carries receiver %s out of the symbol @ %s.", label, lineLabel)
		default:
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		items = append(items, item)
	}
	return items
}

func flowTargetLabel(modulePath string, flow storage.FlowEdgeView) string {
	if flow.TargetQName != "" {
		return shortenQName(modulePath, flow.TargetQName)
	}
	return strings.TrimSpace(flow.TargetLabel)
}

func flowSourceLabel(modulePath string, flow storage.FlowEdgeView) string {
	if flow.SourceQName != "" {
		return shortenQName(modulePath, flow.SourceQName)
	}
	return strings.TrimSpace(flow.SourceLabel)
}

func loadSymbolBlockLines(projectRoot string, symbol storage.SymbolMatch) (int, map[int]string, error) {
	path := filepath.Join(projectRoot, filepath.FromSlash(symbol.FilePath))
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, nil, err
	}
	start, end := sourcerange.Locate(path, data, sourcerange.Symbol{
		Name:     symbol.Name,
		Kind:     symbol.Kind,
		Receiver: symbol.Receiver,
		Line:     symbol.Line,
	})
	if start <= 0 || end < start {
		return 0, nil, nil
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	lineByNumber := make(map[int]string, end-start+1)
	for line := start; line <= end && line <= len(lines); line++ {
		lineByNumber[line] = lines[line-1]
	}
	return start, lineByNumber, nil
}

func extractFlowInputs(signature, receiver string) []string {
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return nil
	}
	start := strings.Index(signature, "(")
	if start < 0 {
		return nil
	}
	depth := 0
	end := -1
	for idx := start; idx < len(signature); idx++ {
		switch signature[idx] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				end = idx
				break
			}
		}
		if end >= 0 {
			break
		}
	}
	if end <= start {
		return nil
	}
	params := splitTopLevel(signature[start+1:end], ',')
	items := make([]string, 0, len(params))
	for _, param := range params {
		name := flowParamName(param)
		if name == "" {
			continue
		}
		items = append(items, name)
	}
	return items
}

func splitTopLevel(value string, sep rune) []string {
	depth := 0
	start := 0
	items := make([]string, 0, 4)
	for idx, r := range value {
		switch r {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		default:
			if r == sep && depth == 0 {
				items = append(items, strings.TrimSpace(value[start:idx]))
				start = idx + 1
			}
		}
	}
	items = append(items, strings.TrimSpace(value[start:]))
	return items
}

func flowParamName(param string) string {
	param = strings.TrimSpace(param)
	param = strings.TrimPrefix(param, "mut ")
	param = strings.TrimPrefix(param, "&")
	param = strings.TrimSpace(param)
	switch param {
	case "", "self", "&self", "cls":
		return ""
	}
	if idx := strings.Index(param, ":"); idx >= 0 {
		param = param[:idx]
	}
	fields := strings.Fields(param)
	if len(fields) == 0 {
		return ""
	}
	name := strings.Trim(fields[0], "*&")
	name = strings.TrimPrefix(name, "...")
	if name == "" || strings.EqualFold(name, "self") {
		return ""
	}
	return name
}

func flowInputsUsedInLine(line string, names []string) []string {
	lower := strings.ToLower(line)
	items := make([]string, 0, len(names))
	for _, name := range names {
		token := strings.TrimSpace(name)
		if token == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(token)) {
			items = append(items, token)
		}
	}
	return items
}

func sortRelatedByUseSite(values []storage.RelatedSymbolView) []storage.RelatedSymbolView {
	result := append([]storage.RelatedSymbolView(nil), values...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].UseFilePath != result[j].UseFilePath {
			return result[i].UseFilePath < result[j].UseFilePath
		}
		if result[i].UseLine != result[j].UseLine {
			return result[i].UseLine < result[j].UseLine
		}
		return result[i].Symbol.QName < result[j].Symbol.QName
	})
	return result
}

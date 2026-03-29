package app

import (
	"fmt"
	"io"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/cli"
)

func Run(command cli.Command, stdout io.Writer) error {
	switch command.Name {
	case "report":
		return runProjectReport(command, stdout)
	case "legacy-report":
		return runLegacyReport(command, stdout)
	case "index":
		return runIndexLike(command, stdout, true)
	case "update":
		return runIndexLike(command, stdout, false)
	case "shell":
		return runShell(command, stdout)
	case "watch":
		return runWatch(command, stdout)
	case "status":
		return runStatus(command, stdout)
	case "doctor":
		return runDoctor(command, stdout)
	case "projects":
		return runProjects(command, stdout)
	case "symbol":
		return runSymbol(command, stdout)
	case "impact":
		return runImpact(command, stdout)
	case "trace":
		return runTrace(command, stdout)
	case "handoff":
		return runHandoff(command, stdout)
	case "review":
		return runReview(command, stdout)
	case "history":
		return runHistory(command, stdout)
	case "cochange":
		return runCoChange(command, stdout)
	case "diff":
		return runDiff(command, stdout)
	case "snapshots":
		return runSnapshots(command, stdout)
	case "snapshot":
		return runSnapshot(command, stdout)
	default:
		return fmt.Errorf("unsupported command %q", command.Name)
	}
}

func shortenQName(modulePath, qname string) string {
	if modulePath == "" {
		return qname
	}
	if trimmed, ok := strings.CutPrefix(qname, modulePath+"/"); ok {
		return trimmed
	}
	if trimmed, ok := strings.CutPrefix(qname, modulePath+"."); ok {
		return trimmed
	}
	return qname
}

func oneLine(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.Join(strings.Fields(value), " ")
}

const timeFormat = "2006-01-02 15:04:05"

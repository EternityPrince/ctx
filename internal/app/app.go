package app

import (
	"fmt"
	"io"
	"os"

	"github.com/vladimirkasterin/ctx/internal/clipboard"
	"github.com/vladimirkasterin/ctx/internal/collector"
	"github.com/vladimirkasterin/ctx/internal/config"
	"github.com/vladimirkasterin/ctx/internal/render"
	"github.com/vladimirkasterin/ctx/internal/tree"
)

func Run(options config.Options, stdout io.Writer) error {
	snapshot, err := collector.Collect(options)
	if err != nil {
		return fmt.Errorf("collect project context: %w", err)
	}

	projectTree := tree.Build(snapshot.Root, snapshot.Directories, snapshot.Files)
	output := render.Report(snapshot, projectTree, options)

	if options.CopyToClipboard {
		if err := clipboard.Copy(output); err != nil {
			return fmt.Errorf("copy report to clipboard: %w", err)
		}
		_, err := fmt.Fprintln(stdout, "Report copied to clipboard")
		return err
	}

	if options.OutputPath == "" {
		_, err := io.WriteString(stdout, output)
		return err
	}

	if err := os.WriteFile(options.OutputPath, []byte(output), 0o644); err != nil {
		return fmt.Errorf("write output file: %w", err)
	}

	_, err = fmt.Fprintf(stdout, "Report written to %s\n", options.OutputPath)
	return err
}

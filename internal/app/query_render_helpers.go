package app

import (
	"fmt"
	"io"
)

func renderHumanEmpty(stdout io.Writer, p palette) error {
	_, err := fmt.Fprintf(stdout, "  %s\n\n", p.muted("none"))
	return err
}

func renderMoreLine(stdout io.Writer, total, limit int) error {
	if total > limit {
		if _, err := fmt.Fprintf(stdout, "  ... and %d more\n", total-limit); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

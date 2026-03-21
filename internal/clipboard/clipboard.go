package clipboard

import (
	"fmt"
	"io"
	"os/exec"
)

func Copy(text string) error {
	cmd := exec.Command("pbcopy")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("open pbcopy stdin: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start pbcopy: %w", err)
	}

	if _, err := io.WriteString(stdin, text); err != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		return fmt.Errorf("write to pbcopy: %w", err)
	}

	if err := stdin.Close(); err != nil {
		_ = cmd.Wait()
		return fmt.Errorf("close pbcopy stdin: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("wait for pbcopy: %w", err)
	}

	return nil
}

package tui

import (
	"os/exec"
	"strings"
)

// copyToClipboard copies text to the system clipboard using pbcopy (macOS).
func copyToClipboard(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

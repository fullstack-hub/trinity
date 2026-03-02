package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
)

func renderMarkdown(content string, width int) string {
	if content == "" {
		return ""
	}
	if width < 20 {
		width = 20
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStylePath("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return content
	}
	out, err := r.Render(content)
	if err != nil {
		return content
	}
	return strings.TrimRight(out, "\n ")
}

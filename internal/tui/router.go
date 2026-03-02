package tui

import (
	"strings"
)

// RouteResult holds a routing result for display purposes.
type RouteResult struct {
	Agent string `json:"agent"` // "claude", "gemini", "copilot"
	Model string `json:"model"` // specific model ID
}

// DisplayName returns a human-readable label for the route.
func (r *RouteResult) DisplayName() string {
	switch {
	case r.Agent == "claude":
		return "Claude " + friendlyModel(r.Model)
	case r.Agent == "gemini":
		return "Gemini " + friendlyModel(r.Model)
	default:
		return friendlyModel(r.Model)
	}
}

func friendlyModel(model string) string {
	replacer := strings.NewReplacer(
		"claude-opus-4-6", "Opus 4.6",
		"claude-sonnet-4-6", "Sonnet 4.6",
		"claude-haiku-4-5", "Haiku 4.5",
		"gemini-3.1-pro", "3.1 Pro",
		"gemini-3-flash", "3 Flash",
		"gpt-5.3-codex", "GPT-5.3 Codex",
		"gpt-5.2-codex", "GPT-5.2 Codex",
		"gpt-5-mini", "GPT-5 Mini",
		"gpt-4.1", "GPT-4.1",
	)
	return replacer.Replace(model)
}

package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ModelOption represents a selectable model for a tab.
type ModelOption struct {
	ID     string
	Name   string
	Server string // Direct mode: determines which server to use. "claude", "gemini", "copilot"
}

// agentModels defines available models per tab.
var agentModels = map[string][]ModelOption{
	"agent": {
		{ID: "claude-sonnet-4-6", Name: "Sonnet 4.6", Server: "claude"},
		{ID: "claude-opus-4-6", Name: "Opus 4.6", Server: "claude"},
	},
	"direct": {
		{ID: "claude-opus-4-6", Name: "Opus 4.6", Server: "claude"},
		{ID: "claude-sonnet-4-6", Name: "Sonnet 4.6", Server: "claude"},
		{ID: "claude-haiku-4-5", Name: "Haiku 4.5", Server: "claude"},
		{ID: "gemini-3.1-pro", Name: "3.1 Pro", Server: "gemini"},
		{ID: "gemini-3-flash", Name: "3 Flash", Server: "gemini"},
		{ID: "gpt-5.3-codex", Name: "Codex 5.3", Server: "copilot"},
		{ID: "claude-sonnet-4-6", Name: "Sonnet 4.6 (Copilot)", Server: "copilot"},
		{ID: "gpt-5-mini", Name: "GPT-5 Mini", Server: "copilot"},
		{ID: "gpt-4.1", Name: "GPT-4.1", Server: "copilot"},
	},
}

// ThinkLevel represents a thinking/reasoning budget option.
type ThinkLevel struct {
	Name   string
	Budget int // 0 = off
}

// modelThinking maps model ID → available thinking levels.
var modelThinking = map[string][]ThinkLevel{
	"claude-opus-4-6":   {{"off", 0}, {"low", 2048}, {"high", 10240}, {"max", 32768}},
	"claude-sonnet-4-6": {{"off", 0}, {"low", 2048}, {"high", 10240}, {"max", 32768}},
	"gemini-3.1-pro":    {{"off", 0}, {"low", 2048}, {"high", 8192}, {"max", 24576}},
	"gemini-3-flash":    {{"off", 0}, {"low", 1024}, {"high", 4096}, {"max", 8192}},
}

type TabBar struct {
	Tabs             []string
	Keys             []string       // internal keys: "agent", "direct"
	ActiveTab        int
	ActiveModels     map[string]int // per-tab selected model index
	ThinkingPerModel map[string]int // per-model thinking level index
}

// currentModelID returns the model ID for the currently active tab+model.
func (t TabBar) currentModelID() string {
	key := t.Keys[t.ActiveTab]
	models := agentModels[key]
	if len(models) == 0 {
		return ""
	}
	idx := t.ActiveModels[key]
	if idx < 0 || idx >= len(models) {
		return models[0].ID
	}
	return models[idx].ID
}

// SelectedModelID returns the model ID for the current tab's selected model.
func (t TabBar) SelectedModelID() string {
	return t.currentModelID()
}

// SelectedModelName returns the display name for the current tab's selected model.
func (t TabBar) SelectedModelName() string {
	key := t.Keys[t.ActiveTab]
	models := agentModels[key]
	if len(models) == 0 {
		return ""
	}
	idx := t.ActiveModels[key]
	if idx < 0 || idx >= len(models) {
		return models[0].Name
	}
	return models[idx].Name
}

// SelectedServer returns the server key for the current tab's selected model.
func (t TabBar) SelectedServer() string {
	key := t.Keys[t.ActiveTab]
	models := agentModels[key]
	if len(models) == 0 {
		return "claude"
	}
	idx := t.ActiveModels[key]
	if idx < 0 || idx >= len(models) {
		return models[0].Server
	}
	return models[idx].Server
}

// CycleModel advances to the next model for the current tab.
func (t *TabBar) CycleModel() {
	key := t.Keys[t.ActiveTab]
	models := agentModels[key]
	if len(models) == 0 {
		return
	}
	idx := t.ActiveModels[key]
	t.ActiveModels[key] = (idx + 1) % len(models)
}

// HasModels returns whether the current tab has selectable models.
func (t TabBar) HasModels() bool {
	key := t.Keys[t.ActiveTab]
	return len(agentModels[key]) > 0
}

// thinkLevels returns the thinking levels for the current model, or nil.
func (t TabBar) thinkLevels() []ThinkLevel {
	return modelThinking[t.currentModelID()]
}

// CycleThinking advances to the next thinking level for the current model.
func (t *TabBar) CycleThinking() {
	levels := t.thinkLevels()
	if len(levels) == 0 {
		return
	}
	mid := t.currentModelID()
	idx := t.ThinkingPerModel[mid]
	t.ThinkingPerModel[mid] = (idx + 1) % len(levels)
}

// ThinkingBudget returns the current thinking budget in tokens.
func (t TabBar) ThinkingBudget() int {
	levels := t.thinkLevels()
	if len(levels) == 0 {
		return 0
	}
	mid := t.currentModelID()
	idx := t.ThinkingPerModel[mid]
	if idx < 0 || idx >= len(levels) {
		return 0
	}
	return levels[idx].Budget
}

// ThinkingName returns the current thinking level name for display.
func (t TabBar) ThinkingName() string {
	levels := t.thinkLevels()
	if len(levels) == 0 {
		return "n/a"
	}
	mid := t.currentModelID()
	idx := t.ThinkingPerModel[mid]
	if idx < 0 || idx >= len(levels) {
		return levels[0].Name
	}
	return levels[idx].Name
}

// HasThinking returns whether the current model supports thinking.
func (t TabBar) HasThinking() bool {
	return len(t.thinkLevels()) > 0
}

// BottomBar renders the unified bottom bar: tabs + models + thinking + shortcuts.
func (t TabBar) BottomBar(width int) string {
	bg := lipgloss.Color("236")
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("242")).Background(bg)
	key := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Bold(true).Background(bg)
	pad := lipgloss.NewStyle().Background(bg)

	right := key.Render("ctrl+t") + dim.Render(" variants  ") +
		key.Render("tab") + dim.Render(" agents  ") +
		key.Render("ctrl+p") + dim.Render(" commands")

	rightW := lipgloss.Width(right)
	gap := width - rightW
	if gap < 0 {
		gap = 0
	}

	return pad.Render(strings.Repeat(" ", gap)) + right
}

// providerName maps server key to display provider name.
func providerName(server string) string {
	switch server {
	case "claude":
		return "Anthropic"
	case "gemini":
		return "Google"
	case "copilot":
		return "OpenAI"
	default:
		return server
	}
}

// fullModelName prepends the brand prefix to the model display name.
func fullModelName(server, name string) string {
	switch server {
	case "claude":
		if !strings.HasPrefix(name, "Claude") {
			return "Claude " + name
		}
	case "gemini":
		if !strings.HasPrefix(name, "Gemini") {
			return "Gemini " + name
		}
	}
	return name
}

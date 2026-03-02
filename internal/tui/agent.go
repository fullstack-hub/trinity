package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/fullstack-hub/trinity/internal/client"
)

const agentSystemPrompt = `You are Trinity Agent, an intelligent AI orchestrator.
You analyze requests and either answer directly or delegate to specialized sub-agents in parallel.

## Sub-Agent Roles

You can delegate tasks to these specialized roles:

| Role | Server | Model | Use For |
|------|--------|-------|---------|
| explorer | gemini | gemini-3-flash | Fast codebase/web exploration, file search, quick lookup |
| researcher | gemini | gemini-3.1-pro | Long documents, papers, data analysis, deep research |
| analyst | claude | claude-opus-4-6 | Architecture analysis, security review, debugging, complex reasoning |
| coder | copilot | gpt-5.3-codex | Code generation, implementation, refactoring, boilerplate |
| reviewer | copilot | claude-sonnet-4-6 | Code review, bug detection, quality checks |
| summarizer | copilot | gpt-5-mini | Summarization, translation, simple aggregation (FREE) |

## Behavior Rules

**Simple requests** (greetings, quick questions, translations):
→ Answer directly. No orchestration needed.

**Coding requests** (bug fix, function writing, code review):
→ Answer directly with code.

**Planning requests** ("계획 세워줘", "어떻게 해야 할까"):
→ Ask clarifying questions first, then provide a structured plan.

**Complex multi-step requests** (project analysis, large refactoring, multi-file changes):
→ Return a JSON orchestration plan using sub-agent roles:

{"tasks":[{"id":"explore-codebase","agent":"gemini","model":"gemini-3-flash","description":"Explore project structure","prompt":"...","depends_on":[]},{"id":"analyze-arch","agent":"claude","model":"claude-opus-4-6","description":"Analyze architecture","prompt":"...","depends_on":["explore-codebase"]},{"id":"gen-code","agent":"copilot","model":"gpt-5.3-codex","description":"Generate implementation","prompt":"...","depends_on":["analyze-arch"]}],"synthesis":{"agent":"claude","model":"claude-opus-4-6","prompt":"Combine all findings into a comprehensive response"}}

Rules for orchestration plans:
- Use 2-5 tasks maximum
- Minimize dependencies — prefer parallel execution
- Each task prompt must be self-contained
- Use the cheapest appropriate role for each task
- Only use orchestration for genuinely complex multi-step tasks
- Most requests should be answered DIRECTLY without orchestration

## LSP Integration

You have access to LSP (Language Server Protocol) for the current workspace.
LSP diagnostics, symbols, and type information are automatically included in the context when relevant.
Use this information to provide more accurate code analysis and suggestions.`

// Mode override prompts (appended to system prompt for /plan, /deep, /orch)
const planModeOverride = `

IMPORTANT: For this request, use interview-planning mode.
Ask clarifying questions first to fully understand the requirements.
Then provide a detailed, structured plan with numbered steps.
Do NOT generate code yet. Focus on planning.`

const deepModeOverride = `

IMPORTANT: For this request, work autonomously.
Provide a complete, thorough answer without asking any questions.
If coding is involved, generate full implementation code.`

const orchModeOverride = `

IMPORTANT: For this request, ALWAYS return a JSON orchestration plan.
Delegate to multiple sub-agents for parallel execution.
Even if the request seems simple, break it into sub-tasks for parallel processing.`

// ── Pre-classifier (GPT-5 Mini, FREE) ──

const classifyPrompt = `You are a request classifier. Return ONLY a raw JSON object. No markdown, no explanation.

{"level":"simple|coding|complex","server":"copilot|claude|gemini","model":"model-id"}

RULES (apply top to bottom, first match wins):

1. Greetings, chitchat, "안녕", "hi", "hello", simple questions, translation, summary, trivial tasks
   → {"level":"simple","server":"copilot","model":"gpt-5-mini"}

2. Quick search, short explanation, simple lookup, "뭐야", "알려줘"
   → {"level":"simple","server":"gemini","model":"gemini-3-flash"}

3. Code generation, boilerplate, scaffolding, implementation, "만들어줘", "작성해줘"
   → {"level":"coding","server":"copilot","model":"gpt-5.3-codex"}

4. Bug fix, code review, refactoring, general coding, "고쳐줘", "리뷰해줘"
   → {"level":"coding","server":"claude","model":"claude-sonnet-4-6"}

5. Architecture analysis, security review, complex debugging, planning
   → {"level":"complex","server":"claude","model":"claude-sonnet-4-6"}

6. Long documents, research, data analysis, papers
   → {"level":"complex","server":"gemini","model":"gemini-3.1-pro"}

DEFAULT: If unsure → {"level":"simple","server":"copilot","model":"gpt-5-mini"}

Request: `

// classifyRoute is the result of the pre-classifier.
type classifyRoute struct {
	Level  string `json:"level"`  // "simple", "coding", "complex"
	Server string `json:"server"` // "claude", "gemini", "copilot"
	Model  string `json:"model"`
}

var defaultClassifyRoute = classifyRoute{Level: "simple", Server: "copilot", Model: "gpt-5-mini"}

// classifyResultMsg is a bubbletea message carrying the classification result.
type classifyResultMsg struct {
	route   classifyRoute
	message string // original user message
}

// classifyMessage sends the user message to GPT-5 Mini for classification.
func classifyMessage(copilotClient *client.Client, text string) func() classifyResultMsg {
	return func() classifyResultMsg {
		if copilotClient == nil {
			return classifyResultMsg{route: defaultClassifyRoute, message: text}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		ch, err := copilotClient.Chat(ctx, classifyPrompt+text, "gpt-5-mini", 0)
		if err != nil {
			return classifyResultMsg{route: defaultClassifyRoute, message: text}
		}

		var full strings.Builder
		for event := range ch {
			if event.Type == client.EventContent {
				full.WriteString(event.Delta)
			}
		}

		// Reset copilot session after classification to avoid session conflict
		copilotClient.Reset(context.Background())

		route := parseClassifyJSON(full.String())
		return classifyResultMsg{route: route, message: text}
	}
}

func parseClassifyJSON(raw string) classifyRoute {
	raw = strings.TrimSpace(raw)
	if idx := strings.Index(raw, "{"); idx >= 0 {
		if end := strings.LastIndex(raw, "}"); end > idx {
			raw = raw[idx : end+1]
		}
	}
	var route classifyRoute
	if err := json.Unmarshal([]byte(raw), &route); err != nil {
		return defaultClassifyRoute
	}
	// Validate
	switch route.Server {
	case "claude", "gemini", "copilot":
	default:
		return defaultClassifyRoute
	}
	if route.Model == "" {
		return defaultClassifyRoute
	}
	return route
}

// needsAgentPrompt returns whether this route level needs the agent system prompt.
// Simple requests go directly to the target model without system prompt overhead.
func needsAgentPrompt(level string) bool {
	return level == "complex"
}

// isOrchPlan checks if a response contains a JSON orchestration plan.
func isOrchPlan(content string) (*TaskPlan, bool) {
	trimmed := strings.TrimSpace(content)
	// Strip markdown code fences if present
	if idx := strings.Index(trimmed, "```"); idx >= 0 {
		start := strings.Index(trimmed[idx:], "\n")
		if start >= 0 {
			inner := trimmed[idx+start:]
			if end := strings.Index(inner, "```"); end >= 0 {
				trimmed = strings.TrimSpace(inner[:end])
			}
		}
	}
	if idx := strings.Index(trimmed, "{"); idx >= 0 {
		if end := strings.LastIndex(trimmed, "}"); end > idx {
			var plan TaskPlan
			if json.Unmarshal([]byte(trimmed[idx:end+1]), &plan) == nil {
				if len(plan.Tasks) > 0 {
					return &plan, true
				}
			}
		}
	}
	return nil, false
}

// formatTaskPlan returns a human-readable summary of the orchestration plan.
func formatTaskPlan(plan *TaskPlan) string {
	var sb strings.Builder
	sb.WriteString("[orchestration plan]\n")
	for _, t := range plan.Tasks {
		deps := ""
		if len(t.DependsOn) > 0 {
			deps = fmt.Sprintf(" (after: %s)", strings.Join(t.DependsOn, ", "))
		}
		sb.WriteString(fmt.Sprintf("  ○ %s — %s [%s]%s\n", t.ID, t.Description, t.Model, deps))
	}
	if plan.Synthesis.Model != "" {
		sb.WriteString(fmt.Sprintf("  ◎ synthesis — %s\n", plan.Synthesis.Model))
	}
	return sb.String()
}

// choicePattern matches numbered choices like "1)", "1.", "1:", "**1.**", "**1)**"
var choicePattern = regexp.MustCompile(`(?m)^\s*(?:\*{0,2})(\d)[.):](?:\*{0,2})\s+(.+)`)

// detectChoices extracts numbered choices (1-4) from the last assistant message.
// Returns up to 4 choices, or nil if no valid choice pattern found.
func detectChoices(content string) []string {
	// Only look at the last ~1000 chars to find choices near the end
	if len(content) > 1000 {
		content = content[len(content)-1000:]
	}
	matches := choicePattern.FindAllStringSubmatch(content, -1)
	if len(matches) < 2 {
		return nil // need at least 2 choices
	}

	choices := make([]string, 0, 4)
	for _, m := range matches {
		num := m[1]
		text := strings.TrimSpace(m[2])
		// Strip trailing markdown bold
		text = strings.TrimRight(text, "*")
		text = strings.TrimSpace(text)
		// Truncate long choice text
		if r := []rune(text); len(r) > 60 {
			text = string(r[:57]) + "..."
		}
		expected := fmt.Sprintf("%d", len(choices)+1)
		if num == expected {
			choices = append(choices, text)
		}
		if len(choices) >= 4 {
			break
		}
	}
	if len(choices) < 2 {
		return nil
	}
	return choices
}

package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/fullstack-hub/trinity/internal/lsp"
	"github.com/fullstack-hub/trinity/internal/version"
)

// MCPTool represents an MCP tool shown in the sidebar.
type MCPTool struct {
	Name   string
	Status string // "Connected", "Available", "Error"
}

type headerPos struct {
	section string
	y       int
}

type Sidebar struct {
	activeKey    string
	msgCounts    map[string]int
	streaming    bool
	width        int
	height       int
	healthCache  map[string]bool
	modelNames   map[string]string
	quota        *QuotaTracker
	orchPlan     *TaskPlan
	orchPhase    orchPhase
	sessionID    string
	lspStatus    []lsp.ServerInfo
	spinnerFrame int

	// MCP tools
	mcpTools []MCPTool

	// Collapsible sections
	collapsed map[string]bool
	headerYs  []headerPos // populated during render for click detection

	// Fixed bottom info
	workDir string
	gitInfo gitStatus
}

func NewSidebar() *Sidebar {
	return &Sidebar{
		msgCounts:   make(map[string]int),
		healthCache: make(map[string]bool),
		modelNames:  make(map[string]string),
		collapsed:   make(map[string]bool),
		mcpTools: []MCPTool{
			{Name: "context7", Status: "Connected"},
			{Name: "websearch", Status: "Connected"},
			{Name: "grep_app", Status: "Connected"},
		},
	}
}

// HitSection returns the section name if contentY matches a header line.
func (s *Sidebar) HitSection(contentY int) string {
	for _, h := range s.headerYs {
		if contentY == h.y {
			return h.section
		}
	}
	return ""
}

// ToggleSection toggles the collapsed state of a section.
func (s *Sidebar) ToggleSection(section string) {
	s.collapsed[section] = !s.collapsed[section]
}

func (s *Sidebar) collapseIcon(section string) string {
	if s.collapsed[section] {
		return "▶"
	}
	return "▼"
}

// contextWindow returns max tokens for the active agent.
func contextWindow(agent string) int {
	switch agent {
	case "gemini":
		return 1_000_000
	case "claude":
		return 200_000
	case "copilot":
		return 200_000
	default:
		return 200_000
	}
}

// progressBar renders a colored progress bar with percentage.
func progressBar(pct, barWidth int) string {
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	yellow := lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	red := lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("242"))

	if barWidth < 5 {
		barWidth = 5
	}
	filled := pct * barWidth / 100
	if filled > barWidth {
		filled = barWidth
	}
	if filled < 0 {
		filled = 0
	}

	barColor := green
	if pct > 70 {
		barColor = yellow
	}
	if pct > 90 {
		barColor = red
	}
	return barColor.Render(strings.Repeat("█", filled)) +
		dim.Render(strings.Repeat("░", barWidth-filled)) +
		" " + dim.Render(fmt.Sprintf("%d%%", pct))
}

// RenderScrollable returns sidebar content for the scrollable viewport.
func (s *Sidebar) RenderScrollable() string {
	w := s.width
	if w < 2 {
		return ""
	}
	contentW := w - 3
	if contentW < 10 {
		contentW = 10
	}

	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	red := lipgloss.NewStyle().Foreground(lipgloss.Color("203"))

	barWidth := contentW - 8
	if barWidth < 8 {
		barWidth = 8
	}

	var lines []string
	s.headerYs = nil
	lines = append(lines, "") // top padding

	// ── Servers ──
	s.headerYs = append(s.headerYs, headerPos{"servers", len(lines)})
	lines = append(lines, header.Render(s.collapseIcon("servers")+" Servers"))
	if !s.collapsed["servers"] {
		lines = append(lines, "")
		for _, name := range []string{"claude", "gemini", "copilot"} {
			if s.healthCache[name] {
				lines = append(lines, "  "+green.Render("●")+" "+label.Render(fmt.Sprintf("%-8s", name))+green.Render("Connected"))
			} else {
				lines = append(lines, "  "+red.Render("○")+" "+label.Render(fmt.Sprintf("%-8s", name))+dim.Render("Stopped"))
			}
		}
	}
	lines = append(lines, "")

	// ── MCP ──
	if len(s.mcpTools) > 0 {
		s.headerYs = append(s.headerYs, headerPos{"mcp", len(lines)})
		lines = append(lines, header.Render(s.collapseIcon("mcp")+" MCP"))
		if !s.collapsed["mcp"] {
			lines = append(lines, "")
			for _, tool := range s.mcpTools {
				switch tool.Status {
				case "Connected":
					lines = append(lines, "  "+green.Render("●")+" "+label.Render(tool.Name)+" "+green.Render(tool.Status))
				case "Error":
					lines = append(lines, "  "+red.Render("✗")+" "+red.Render(tool.Name)+" "+red.Render(tool.Status))
				default:
					lines = append(lines, "  "+dim.Render("○")+" "+dim.Render(tool.Name)+" "+dim.Render(tool.Status))
				}
			}
		}
		lines = append(lines, "")
	}

	// ── LSP ──
	if len(s.lspStatus) > 0 {
		spinnerChars := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		spinner := lipgloss.NewStyle().Foreground(lipgloss.Color("220"))

		s.headerYs = append(s.headerYs, headerPos{"lsp", len(lines)})
		lines = append(lines, header.Render(s.collapseIcon("lsp")+" LSP"))
		if !s.collapsed["lsp"] {
			lines = append(lines, "")
			for _, srv := range s.lspStatus {
				switch srv.State {
				case lsp.StateRunning:
					lines = append(lines, "  "+green.Render("●")+" "+label.Render(srv.Name))
				case lsp.StateStarting:
					frame := spinnerChars[s.spinnerFrame%len(spinnerChars)]
					lines = append(lines, "  "+spinner.Render(frame)+" "+dim.Render(srv.Name))
				case lsp.StateError:
					lines = append(lines, "  "+red.Render("✗")+" "+red.Render(srv.Name))
				default:
					lines = append(lines, "  "+dim.Render("○")+" "+dim.Render(srv.Name))
				}
			}
		}
		lines = append(lines, "")
	}

	// ── Quota ──
	s.headerYs = append(s.headerYs, headerPos{"quota", len(lines)})
	lines = append(lines, header.Render(s.collapseIcon("quota")+" Quota"))
	if !s.collapsed["quota"] {
		lines = append(lines, "")
		if s.quota != nil {
			// Claude 5h
			c5 := s.quota.Claude5h()
			lines = append(lines, "  "+dim.Render("Claude 5h: ")+label.Render(c5.Label)+" "+dim.Render(c5.Sub))
			if c5.Pct >= 0 {
				lines = append(lines, "  "+progressBar(c5.Pct, barWidth))
			}
			// Claude weekly
			cw := s.quota.ClaudeWeekly()
			lines = append(lines, "  "+dim.Render("Claude wk: ")+label.Render(cw.Label)+" "+dim.Render(cw.Sub))
			if cw.Pct >= 0 {
				lines = append(lines, "  "+progressBar(cw.Pct, barWidth))
			}
			lines = append(lines, "")

			// Copilot
			cpq := s.quota.Copilot()
			lines = append(lines, "  "+dim.Render("Copilot:   ")+label.Render(cpq.Label)+" "+dim.Render(cpq.Sub))
			if cpq.Pct >= 0 {
				lines = append(lines, "  "+progressBar(cpq.Pct, barWidth))
			}
			lines = append(lines, "")

			// Gemini
			gBuckets := s.quota.GeminiBuckets()
			shown := 0
			for _, b := range gBuckets {
				if b.ModelID != "gemini-3.1-pro-preview" && b.ModelID != "gemini-3-flash-preview" {
					continue
				}
				usedPct := int(100 - b.RemainingPct)
				if usedPct < 0 {
					usedPct = 0
				}
				if usedPct > 100 {
					usedPct = 100
				}
				name := strings.Replace(b.ModelID, "gemini-", "", 1)
				name = strings.Replace(name, "-preview", "", 1)
				name = strings.Replace(name, "-", " ", -1)
				name = "Gemini " + name
				resetLabel := ""
				if b.ResetTime != "" {
					if r := formatResetTime(b.ResetTime); r != "" {
						resetLabel = " (" + r + ")"
					}
				}
				lines = append(lines, "  "+dim.Render(name+": ")+label.Render(fmt.Sprintf("%.1f%%", b.RemainingPct))+dim.Render(resetLabel))
				lines = append(lines, "  "+progressBar(usedPct, barWidth))
				shown++
			}
			if shown == 0 {
				gq := s.quota.Gemini()
				lines = append(lines, "  "+dim.Render("Gemini:    ")+label.Render(gq.Label))
			}
		} else {
			lines = append(lines, "  "+dim.Render("Not available"))
		}
	}
	lines = append(lines, "")

	// ── Context ──
	s.headerYs = append(s.headerYs, headerPos{"context", len(lines)})
	lines = append(lines, header.Render(s.collapseIcon("context")+" Context"))
	if !s.collapsed["context"] {
		lines = append(lines, "")

		contextAgent := s.activeKey
		if contextAgent == "agent" {
			contextAgent = "claude"
		}

		maxTokens := contextWindow(contextAgent)
		totalMsgs := 0
		for _, c := range s.msgCounts {
			totalMsgs += c
		}
		estTokens := totalMsgs * 500
		if estTokens > maxTokens {
			estTokens = maxTokens
		}

		pct := 0
		if maxTokens > 0 {
			pct = estTokens * 100 / maxTokens
		}

		tokenStr := formatTokens(estTokens)
		maxStr := formatTokens(maxTokens)
		lines = append(lines, "  "+dim.Render("Tokens:  ")+label.Render(fmt.Sprintf("%s / %s", tokenStr, maxStr)))
		lines = append(lines, "  "+progressBar(pct, barWidth))
	}
	lines = append(lines, "")

	// ── Session ──
	s.headerYs = append(s.headerYs, headerPos{"session", len(lines)})
	lines = append(lines, header.Render(s.collapseIcon("session")+" Session"))
	if !s.collapsed["session"] {
		lines = append(lines, "")

		if s.sessionID != "" {
			lines = append(lines, "  "+dim.Render("ID:      ")+label.Render(s.sessionID))
		}

		agentDisplay := s.activeKey
		if agentDisplay == "" {
			agentDisplay = "—"
		} else {
			agentDisplay = strings.ToUpper(agentDisplay[:1]) + agentDisplay[1:]
		}
		lines = append(lines, "  "+dim.Render("Agent:   ")+label.Render(agentDisplay))

		modelDisplay := "—"
		if m, ok := s.modelNames[s.activeKey]; ok && m != "" {
			modelDisplay = m
		}
		lines = append(lines, "  "+dim.Render("Model:   ")+label.Render(modelDisplay))

		statusText := "Ready"
		if s.streaming {
			statusText = "Streaming"
		}
		if s.orchPhase == orchRunning {
			statusText = "Orchestrating"
		} else if s.orchPhase == orchSynthesizing {
			statusText = "Synthesizing"
		}
		lines = append(lines, "  "+dim.Render("Status:  ")+label.Render(statusText))

		count := s.msgCounts[s.activeKey]
		lines = append(lines, "  "+dim.Render("Messages:")+label.Render(fmt.Sprintf(" %d", count)))
	}

	// ── Orchestration status (if running) ──
	if s.orchPlan != nil && s.orchPhase != orchIdle {
		orchDone := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
		orchRun := lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
		orchWait := lipgloss.NewStyle().Foreground(lipgloss.Color("242"))

		lines = append(lines, "")
		lines = append(lines, "  "+header.Render("Orchestration"))
		for _, t := range s.orchPlan.Tasks {
			var icon, style string
			switch t.State {
			case TaskDone:
				icon = orchDone.Render("✓")
				style = orchDone.Render(fmt.Sprintf("%-12s %s", t.ID, friendlyModel(t.Model)))
			case TaskRunning:
				icon = orchRun.Render("●")
				style = orchRun.Render(fmt.Sprintf("%-12s %s...", t.ID, friendlyModel(t.Model)))
			case TaskError:
				icon = red.Render("✗")
				style = red.Render(fmt.Sprintf("%-12s error", t.ID))
			default:
				icon = orchWait.Render("○")
				style = orchWait.Render(fmt.Sprintf("%-12s waiting...", t.ID))
			}
			lines = append(lines, "    "+icon+" "+style)
		}
		lines = append(lines, "")
		lines = append(lines, "  "+dim.Render("ctrl+d detail view"))
	}

	lines = append(lines, "")

	// Build sidebar with left border
	border := dim.Render("┃")
	var rendered []string
	for _, line := range lines {
		rendered = append(rendered, border+" "+truncate(line, contentW))
	}

	return strings.Join(rendered, "\n")
}

// RenderFixed returns the fixed bottom strip with p10k-style git info.
func (s *Sidebar) RenderFixed() string {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	yellow := lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	red := lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	cyan := lipgloss.NewStyle().Foreground(lipgloss.Color("117"))
	bold := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Bold(true)
	border := dim.Render("┃")
	contentW := s.width - 3
	if contentW < 10 {
		contentW = 10
	}

	sep := border + dim.Render(strings.Repeat("─", contentW+1))

	// Line 1: cwd
	cwd := s.workDir
	maxCwd := contentW - 2
	if maxCwd < 10 {
		maxCwd = 10
	}
	if len(cwd) > maxCwd {
		cwd = "…" + cwd[len(cwd)-maxCwd+1:]
	}
	cwdLine := border + " " + truncate(dim.Render(cwd), contentW)

	// Line 2: p10k-style git status
	gi := s.gitInfo
	gitLine := border
	if gi.Branch != "" {
		var parts []string
		parts = append(parts, green.Render("")+" "+green.Render(gi.Branch))
		if gi.Ahead > 0 {
			parts = append(parts, cyan.Render(fmt.Sprintf("⇡%d", gi.Ahead)))
		}
		if gi.Behind > 0 {
			parts = append(parts, cyan.Render(fmt.Sprintf("⇣%d", gi.Behind)))
		}
		if gi.Staged > 0 {
			parts = append(parts, green.Render(fmt.Sprintf("+%d", gi.Staged)))
		}
		if gi.Unstaged > 0 {
			parts = append(parts, red.Render(fmt.Sprintf("!%d", gi.Unstaged)))
		}
		if gi.Untracked > 0 {
			parts = append(parts, yellow.Render(fmt.Sprintf("?%d", gi.Untracked)))
		}
		gitLine = border + " " + truncate(strings.Join(parts, " "), contentW)
	}

	// Line 3: empty
	emptyLine := border

	// Line 4: ● Trinity 0.0.1
	verLine := border + " " + green.Render("●") + " " + bold.Render("Trinity") + " " + dim.Render(version.String())

	return sep + "\n" + cwdLine + "\n" + gitLine + "\n" + emptyLine + "\n" + verLine
}

func truncate(s string, maxWidth int) string {
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	runes := []rune(s)
	if len(runes) > maxWidth {
		return string(runes[:maxWidth])
	}
	return s
}

func formatTokens(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

package tui

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/fullstack-hub/trinity/internal/client"
	"github.com/fullstack-hub/trinity/internal/config"
	"github.com/fullstack-hub/trinity/internal/lsp"
	"github.com/fullstack-hub/trinity/internal/session"
)

// ── Chat message ──

type chatRole int

const (
	roleUser chatRole = iota
	roleAssistant
	roleSystem
)

type chatMessage struct {
	role     chatRole
	agent    string // "claude", "gemini", "copilot"
	model    string // dynamic model name from server
	content  string
	thinking string // thinking/reasoning content (streamed separately)
	ts       time.Time
}

// ── Bubbletea messages ──

type streamStartMsg struct {
	ch     <-chan client.SSEEvent
	key    string
	model  string
	cancel context.CancelFunc
}
type streamEventMsg client.SSEEvent
type streamErrMsg struct{ err error }
type healthTickMsg time.Time
type healthResultMsg map[string]bool
type spinnerTickMsg time.Time

var spinnerFrames = []string{".  ", ".. ", "...", " ..", "  .", "   "}

var banner = makeBanner()

func makeBanner() string {
	raw := []string{
		" ████████╗██████╗ ██╗███╗   ██╗██╗████████╗██╗   ██╗",
		" ╚══██╔══╝██╔══██╗██║████╗  ██║██║╚══██╔══╝╚██╗ ██╔╝",
		"    ██║   ██████╔╝██║██╔██╗ ██║██║   ██║    ╚████╔╝ ",
		"    ██║   ██╔══██╗██║██║╚██╗██║██║   ██║     ╚██╔╝  ",
		"    ██║   ██║  ██║██║██║ ╚████║██║   ██║      ██║   ",
		"    ╚═╝   ╚═╝  ╚═╝╚═╝╚═╝  ╚═══╝╚═╝   ╚═╝      ╚═╝   ",
	}

	// 3-color gradient: pink(205) → purple(99) → cyan(14)
	colors := []color.Color{
		lipgloss.Color("205"), lipgloss.Color("205"), lipgloss.Color("204"), lipgloss.Color("204"), lipgloss.Color("170"), lipgloss.Color("170"),
		lipgloss.Color("135"), lipgloss.Color("135"), lipgloss.Color("99"), lipgloss.Color("99"), lipgloss.Color("63"), lipgloss.Color("63"),
		lipgloss.Color("33"), lipgloss.Color("33"), lipgloss.Color("39"), lipgloss.Color("38"), lipgloss.Color("14"), lipgloss.Color("14"),
	}

	// Trinity symbol (6 lines, each colored differently)
	pink := lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	purple := lipgloss.NewStyle().Foreground(lipgloss.Color("99"))
	cyan := lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	dimSym := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	symbol := []string{
		"          ",
		"    " + pink.Render("◆") + "     ",
		"   " + dimSym.Render("╱") + " " + dimSym.Render("╲") + "    ",
		"  " + purple.Render("◆") + dimSym.Render("───") + cyan.Render("◆") + "   ",
		"   " + dimSym.Render("╲") + " " + dimSym.Render("╱") + "    ",
		"    " + dimSym.Render("◆") + "     ",
	}

	// Render gradient text + symbol side by side
	var lines []string
	for i, line := range raw {
		grad := renderGradientLine(line, colors)
		sym := ""
		if i < len(symbol) {
			sym = symbol[i]
		}
		lines = append(lines, grad+"  "+sym)
	}

	// Subtitle + powered by
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	powered := lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	lines = append(lines, dim.Render("    Unified AI CLI — Claude · Gemini · Copilot"))
	lines = append(lines, powered.Render("              Powered by fullstackhub"))

	return strings.Join(lines, "\n")
}

func renderGradientLine(line string, colors []color.Color) string {
	runes := []rune(line)
	n := len(runes)
	if n == 0 || len(colors) == 0 {
		return line
	}
	var sb strings.Builder
	for i, r := range runes {
		idx := i * (len(colors) - 1) / max(n-1, 1)
		if idx >= len(colors) {
			idx = len(colors) - 1
		}
		sb.WriteString(lipgloss.NewStyle().Foreground(colors[idx]).Render(string(r)))
	}
	return sb.String()
}

type slashCmd struct {
	name string
	desc string
}

var slashCommands = []slashCmd{
	{"/exit", "Exit Trinity"},
	{"/new", "New session"},
	{"/reset", "Reset & new session"},
	{"/sessions", "Browse sessions"},
	{"/status", "Show server status"},
	{"/clear", "Clear display"},
	{"/plan", "Force planning mode"},
	{"/deep", "Force autonomous mode"},
	{"/orch", "Force orchestration"},
	{"/diagnostics", "Show LSP diagnostics"},
	{"/symbols", "Show file symbols (path)"},
	{"/hover", "LSP hover (path line col)"},
	{"/definition", "Go to definition (path line col)"},
	{"/references", "Find references (path line col)"},
}

// ── App ──

type App struct {
	cfg              *config.Config
	tabs             TabBar
	clients          map[string]*client.Client
	input            textarea.Model
	messages         []chatMessage // unified chat history
	streaming        bool
	classifying      bool // pre-classification in progress
	cancel           context.CancelFunc
	streamCh         <-chan client.SSEEvent
	streamKey        string
	width            int
	height           int
	sidebar          *Sidebar
	msgCounts        map[string]int
	modelNames       map[string]string // dynamic model names from servers
	spinnerFrame     int
	quota            *QuotaTracker
	chatVP           viewport.Model
	sideVP           viewport.Model
	history          []string // input history (oldest first)
	historyIdx       int      // -1 = new input, 0..len-1 = browsing history
	historySaved     string   // saved current input while browsing
	nextModeOverride string   // "plan", "deep", "orch" — one-shot override

	// Orchestration state
	orchPlan    *TaskPlan
	orchCh      chan orchEventMsg
	orchPhase   orchPhase
	orchOrigMsg string // original user message for synthesis
	orchCancels []context.CancelFunc

	// Copy feedback
	copiedTimer int // countdown frames for "[copied]" flash

	// Slash command selection
	slashIdx int // selected slash command index (-1 = none)

	// Workspace info
	gitInfo gitStatus

	// Session persistence
	store     *session.Store
	session   *session.Session
	workspace string

	// Session picker modal
	modal sessionModal

	// Choice bar (1~4 numbered choices from assistant)
	choices []string // detected choices from last assistant message

	// LSP
	lspManager *lsp.Manager

	// Subagent detail view
	subagentView    bool   // true = showing subagent detail instead of main chat
	subagentTaskID  string // which task to display
	subagentVP      viewport.Model
}

func NewApp(cfg *config.Config, store *session.Store, sess *session.Session, workspace string) *App {
	ta := textarea.New()
	ta.Placeholder = ""
	ta.Prompt = "┃ "
	inputBg := lipgloss.Color("236")
	styles := textarea.DefaultDarkStyles()
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Background(inputBg)
	styles.Focused.Base = lipgloss.NewStyle().Background(inputBg)
	styles.Focused.CursorLine = lipgloss.NewStyle().Background(inputBg)
	styles.Focused.Text = lipgloss.NewStyle().Background(inputBg)
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color("242")).Background(inputBg)
	styles.Focused.EndOfBuffer = lipgloss.NewStyle().Foreground(inputBg).Background(inputBg)
	styles.Blurred.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("242")).Background(inputBg)
	styles.Blurred.Base = lipgloss.NewStyle().Background(inputBg)
	ta.SetStyles(styles)
	ta.Focus()
	ta.SetHeight(1)
	ta.SetWidth(80)
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	// Enter → send message (handled in Update switch), Alt+Enter → newline
	ta.KeyMap.InsertNewline.SetKeys("shift+enter")
	ta.SetVirtualCursor(false) // real cursor for accurate CJK wide-char positioning

	tabNames := []string{"Agent", "Direct"}
	tabKeys := []string{"agent", "direct"}

	// Build clients for real servers
	serverKeys := []string{"claude", "gemini", "copilot"}
	clients := make(map[string]*client.Client)
	for _, key := range serverKeys {
		if srv, ok := cfg.Servers[key]; ok {
			clients[key] = client.New(srv.URL)
		}
	}

	defaultTab := 0
	for i, key := range tabKeys {
		if key == cfg.DefaultAgent {
			defaultTab = i
		}
	}

	chatVP := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	chatVP.MouseWheelEnabled = true
	sideVP := viewport.New(viewport.WithWidth(28), viewport.WithHeight(20))
	sideVP.MouseWheelEnabled = true
	subVP := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	subVP.MouseWheelEnabled = true

	// Create or restore session
	if sess == nil {
		sess = store.Create(workspace)
	}

	a := &App{
		cfg:        cfg,
		tabs:       TabBar{Tabs: tabNames, Keys: tabKeys, ActiveTab: defaultTab, ActiveModels: make(map[string]int), ThinkingPerModel: make(map[string]int)},
		clients:    clients,
		input:      ta,
		sidebar:    NewSidebar(),
		msgCounts:  make(map[string]int),
		modelNames: make(map[string]string),
		quota:      NewQuotaTracker(clients["claude"], clients["copilot"], clients["gemini"]),
		chatVP:     chatVP,
		sideVP:     sideVP,
		subagentVP: subVP,
		historyIdx: -1,
		slashIdx:   -1,
		store:      store,
		session:    sess,
		workspace:  workspace,
		gitInfo:    getGitStatus(workspace),
		lspManager: lsp.NewManager(workspace),
	}

	// Start LSP servers in background
	a.lspManager.StartAll()

	// Restore messages from continued session
	if len(sess.Messages) > 0 {
		for _, m := range sess.Messages {
			a.messages = append(a.messages, sessionMsgToChatMsg(m))
		}
		// Rebuild message counts
		for _, m := range a.messages {
			if m.role == roleUser {
				a.msgCounts[m.agent]++
			}
		}
		a.messages = append(a.messages, chatMessage{
			role: roleSystem, content: fmt.Sprintf("[session %s continued — %d messages restored]", sess.ID, len(sess.Messages)), ts: time.Now(),
		})
	} else {
		a.messages = append(a.messages, chatMessage{
			role: roleSystem, content: fmt.Sprintf("[session %s]", sess.ID), ts: time.Now(),
		})
	}

	return a
}

func (a *App) activeKey() string {
	return a.tabs.Keys[a.tabs.ActiveTab]
}

type quotaFetchedMsg struct{}

func (a *App) Init() tea.Cmd {
	return tea.Batch(a.checkHealth(), a.healthTick(), a.fetchQuotas(), a.spinnerTick())
}

func (a *App) fetchQuotas() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var wg sync.WaitGroup
		wg.Add(3)
		go func() { defer wg.Done(); a.quota.FetchClaudeUsage(ctx) }()
		go func() { defer wg.Done(); a.quota.FetchCopilotQuota(ctx) }()
		go func() { defer wg.Done(); a.quota.FetchGeminiStats(ctx) }()
		wg.Wait()
		return quotaFetchedMsg{}
	}
}

func (a *App) healthTick() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return healthTickMsg(t)
	})
}

func (a *App) checkHealth() tea.Cmd {
	return func() tea.Msg {
		result := make(map[string]bool)
		for name, c := range a.clients {
			ok, err := c.Health(context.Background())
			result[name] = err == nil && ok
		}
		return healthResultMsg(result)
	}
}

func (a *App) spinnerTick() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		return spinnerTickMsg(t)
	})
}

func (a *App) matchingCommands() []slashCmd {
	text := strings.TrimSpace(a.input.Value())
	if !strings.HasPrefix(text, "/") {
		return nil
	}
	var matches []slashCmd
	for _, cmd := range slashCommands {
		if strings.HasPrefix(cmd.name, text) {
			matches = append(matches, cmd)
		}
	}
	return matches
}

func (a *App) lastMsg() *chatMessage {
	if len(a.messages) == 0 {
		return nil
	}
	return &a.messages[len(a.messages)-1]
}

// ── Session persistence helpers ──

func sessionMsgToChatMsg(m session.Message) chatMessage {
	var role chatRole
	switch m.Role {
	case "user":
		role = roleUser
	case "assistant":
		role = roleAssistant
	default:
		role = roleSystem
	}
	return chatMessage{
		role:     role,
		agent:    m.Server,
		model:    m.Model,
		content:  m.Content,
		thinking: m.Thinking,
		ts:       m.Timestamp,
	}
}

func chatMsgToSessionMsg(m chatMessage) session.Message {
	var role string
	switch m.role {
	case roleUser:
		role = "user"
	case roleAssistant:
		role = "assistant"
	default:
		role = "system"
	}
	return session.Message{
		Role:      role,
		Content:   m.content,
		Thinking:  m.thinking,
		Model:     m.model,
		Server:    m.agent,
		Timestamp: m.ts,
	}
}

func (a *App) saveSession() {
	if a.store == nil || a.session == nil {
		return
	}
	a.session.Messages = make([]session.Message, len(a.messages))
	for i, m := range a.messages {
		a.session.Messages[i] = chatMsgToSessionMsg(m)
	}
	a.store.Save(a.session)
}

// ── Update ──

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height

		sidebarW := a.sidebarWidth()
		mainW := a.width - sidebarW
		if mainW < 30 {
			mainW = 30
			sidebarW = a.width - mainW
		}
		a.input.SetWidth(mainW - 4)

		contentLines := strings.Count(a.input.Value(), "\n") + 1
		taHeight := max(1, min(contentLines, 10))
		a.input.SetHeight(taHeight)
		indicatorH := 1
		bottomBarH := 1
		inputH := taHeight + 2
		choiceBarH := 0
		if len(a.choices) > 0 && !a.streaming && !a.classifying && a.orchPhase == orchIdle {
			choiceBarH = 1
		}
		cmdHintH := 0
		if matches := a.matchingCommands(); len(matches) > 0 {
			cmdHintH = len(matches)
		}
		chatH := a.height - inputH - bottomBarH - indicatorH - choiceBarH - cmdHintH
		if chatH < 5 {
			chatH = 5
		}
		sideFixedH := 5 // sep + cwd + git + empty + version
		sideVPH := a.height - sideFixedH
		if sideVPH < 5 {
			sideVPH = 5
		}
		a.chatVP.SetWidth(mainW)
		a.chatVP.SetHeight(chatH)
		a.sideVP.SetWidth(sidebarW)
		a.sideVP.SetHeight(sideVPH)
		a.subagentVP.SetWidth(mainW)
		a.subagentVP.SetHeight(chatH)
		return a, nil

	case tea.MouseClickMsg:
		m := tea.Mouse(msg)
		sidebarW := a.sidebarWidth()
		mainW := a.width - sidebarW
		if mainW < 30 {
			mainW = 30
		}
		// Modal: only clicking "esc" button closes modal
		if a.modal.visible {
			if a.modal.HitEsc(m.X, m.Y) {
				a.modal.Close()
			}
			return a, nil
		}
		// Sidebar section click → toggle collapse
		if m.X >= mainW {
			contentY := m.Y + a.sideVP.YOffset()
			if section := a.sidebar.HitSection(contentY); section != "" {
				a.sidebar.ToggleSection(section)
			}
		}
		return a, nil

	case tea.MouseReleaseMsg:
		m := tea.Mouse(msg)
		sidebarW := a.sidebarWidth()
		mainW := a.width - sidebarW
		if mainW < 30 {
			mainW = 30
		}
		if a.modal.visible {
			if a.modal.HitEsc(m.X, m.Y) {
				a.modal.Close()
			}
			return a, nil
		}
		// Sidebar section click → toggle collapse
		if m.X >= mainW {
			contentY := m.Y + a.sideVP.YOffset()
			if section := a.sidebar.HitSection(contentY); section != "" {
				a.sidebar.ToggleSection(section)
			}
		}
		return a, nil

	case tea.MouseWheelMsg:
		m := tea.Mouse(msg)
		sidebarW := a.sidebarWidth()
		mainW := a.width - sidebarW
		if mainW < 30 {
			mainW = 30
		}
		if a.modal.visible {
			return a, nil
		}
		if m.X < mainW {
			if a.subagentView {
				var cmd tea.Cmd
				a.subagentVP, cmd = a.subagentVP.Update(msg)
				return a, cmd
			}
			var cmd tea.Cmd
			a.chatVP, cmd = a.chatVP.Update(msg)
			return a, cmd
		}
		var cmd tea.Cmd
		a.sideVP, cmd = a.sideVP.Update(msg)
		return a, cmd

	case tea.KeyPressMsg:
		// Modal intercepts all keys when visible
		if a.modal.visible {
			return a.handleModalKey(msg)
		}

		// Subagent detail view intercepts keys
		if a.subagentView {
			return a.handleSubagentKey(msg)
		}

		switch msg.String() {
		case "pgup":
			a.chatVP.ScrollUp(10)
			return a, nil
		case "pgdown":
			a.chatVP.ScrollDown(10)
			return a, nil
		case "ctrl+c":
			// Save session before exit
			a.saveSession()
			// Stop LSP in background to avoid blocking exit
			if a.lspManager != nil {
				go a.lspManager.StopAll()
			}
			// Cancel all orchestration tasks
			for _, cancel := range a.orchCancels {
				cancel()
			}
			a.orchCancels = nil
			if a.cancel != nil {
				a.cancel()
			}
			return a, tea.Quit
		case "tab":
			if !a.streaming && !a.classifying && a.orchPhase == orchIdle {
				a.tabs.ActiveTab = (a.tabs.ActiveTab + 1) % len(a.tabs.Tabs)
			}
			return a, nil
		case "shift+tab":
			if !a.streaming && !a.classifying && a.orchPhase == orchIdle {
				a.tabs.CycleModel()
			}
			return a, nil
		case "ctrl+t":
			if !a.streaming && !a.classifying && a.orchPhase == orchIdle {
				a.tabs.CycleThinking()
			}
			return a, nil
		case "ctrl+p":
			if !a.streaming && !a.classifying && a.orchPhase == orchIdle {
				a.input.SetValue("/")
				a.input.CursorEnd()
			}
			return a, nil
		case "ctrl+y":
			// Copy last assistant message to clipboard
			for i := len(a.messages) - 1; i >= 0; i-- {
				if a.messages[i].role == roleAssistant && a.messages[i].content != "" {
					copyToClipboard(a.messages[i].content)
					a.copiedTimer = 15
					return a, a.spinnerTick()
				}
			}
			return a, nil
		case "esc":
			// Dismiss slash command menu if open
			if matches := a.matchingCommands(); len(matches) > 0 {
				a.input.Reset()
				a.slashIdx = -1
				return a, nil
			}
			return a, nil
		case "ctrl+d":
			// Enter subagent detail view during orchestration
			if a.orchPlan != nil && len(a.orchPlan.Tasks) > 0 {
				a.subagentView = true
				// Default to first running or first task
				a.subagentTaskID = a.orchPlan.Tasks[0].ID
				for _, t := range a.orchPlan.Tasks {
					if t.State == TaskRunning {
						a.subagentTaskID = t.ID
						break
					}
				}
				return a, nil
			}
			return a, nil
		case "up":
			// Slash command navigation takes priority
			if matches := a.matchingCommands(); len(matches) > 0 {
				if a.slashIdx <= 0 {
					a.slashIdx = len(matches) - 1
				} else {
					a.slashIdx--
				}
				return a, nil
			}
			// Input history
			if len(a.history) == 0 {
				return a, nil
			}
			if a.historyIdx == -1 {
				a.historySaved = a.input.Value()
				a.historyIdx = 0
			} else if a.historyIdx < len(a.history)-1 {
				a.historyIdx++
			}
			a.input.Reset()
			a.input.SetValue(a.history[len(a.history)-1-a.historyIdx])
			a.input.CursorEnd()
			return a, nil
		case "down":
			// Slash command navigation takes priority
			if matches := a.matchingCommands(); len(matches) > 0 {
				a.slashIdx++
				if a.slashIdx >= len(matches) {
					a.slashIdx = 0
				}
				return a, nil
			}
			// Input history
			if a.historyIdx == -1 {
				return a, nil
			}
			a.historyIdx--
			if a.historyIdx < 0 {
				a.historyIdx = -1
				a.input.Reset()
				a.input.SetValue(a.historySaved)
				a.input.CursorEnd()
			} else {
				a.input.Reset()
				a.input.SetValue(a.history[len(a.history)-1-a.historyIdx])
				a.input.CursorEnd()
			}
			return a, nil
		case "1", "2", "3", "4":
			// Quick choice selection: only when input is empty and choices are available
			if len(a.choices) > 0 && strings.TrimSpace(a.input.Value()) == "" {
				idx := int(msg.String()[0]-'0') - 1
				if idx >= 0 && idx < len(a.choices) {
					a.input.SetValue(msg.String())
					// Fall through to enter handling
				}
			} else {
				// Normal text input
				var cmd tea.Cmd
				a.input, cmd = a.input.Update(msg)
				return a, cmd
			}
			fallthrough
		case "enter":
			text := strings.TrimSpace(a.input.Value())
			if text == "" {
				return a, nil
			}

			// Save to input history
			a.history = append(a.history, text)
			a.historyIdx = -1
			a.historySaved = ""

			// Auto-complete slash commands: if partial match, fill input and stop (don't execute)
			if strings.HasPrefix(text, "/") {
				matches := a.matchingCommands()
				completed := ""
				if a.slashIdx >= 0 && a.slashIdx < len(matches) {
					completed = matches[a.slashIdx].name
				} else if len(matches) == 1 {
					completed = matches[0].name
				}
				a.slashIdx = -1
				if completed != "" && completed != text {
					// Only autocomplete — don't execute yet
					a.input.SetValue(completed)
					a.input.SetCursorColumn(len(completed))
					return a, nil
				}
				if completed != "" {
					text = completed
				}
			}

			if text == "/exit" || text == "/quit" {
				a.saveSession()
				// Stop LSP in background to avoid blocking exit
				if a.lspManager != nil {
					go a.lspManager.StopAll()
				}
				for _, cancel := range a.orchCancels {
					cancel()
				}
				if a.cancel != nil {
					a.cancel()
				}
				a.input.Reset()
				return a, tea.Quit
			}

			if a.streaming || a.classifying || a.orchPhase != orchIdle {
				return a, nil
			}

			if strings.HasPrefix(text, "/") {
				a.input.Reset()
				return a, a.handleSlashCommand(text)
			}

			key := a.activeKey()
			a.choices = nil // clear previous choices
			a.messages = append(a.messages, chatMessage{
				role: roleUser, agent: key, content: text, ts: time.Now(),
			})
			a.input.Reset()
			a.spinnerFrame = 0
			a.saveSession() // persist user message

			if key == "agent" {
				a.orchOrigMsg = text // save for synthesis
				ctx := a.buildContext(10)

				// Mode override → skip classification, send directly to Agent model
				if a.nextModeOverride != "" {
					sysPrompt := agentSystemPrompt
					switch a.nextModeOverride {
					case "plan":
						sysPrompt += planModeOverride
					case "deep":
						sysPrompt += deepModeOverride
					case "orch":
						sysPrompt += orchModeOverride
					}
					a.nextModeOverride = ""
					lspCtx := a.buildLSPContext(text)
					fullPrompt := sysPrompt + "\n\n" + ctx + lspCtx + text
					model := a.tabs.SelectedModelID()
					a.streaming = true
					a.msgCounts[key]++
					budget := a.tabs.ThinkingBudget()
					return a, tea.Batch(a.startChat("claude", fullPrompt, model, budget), a.spinnerTick())
				}

				// Agent mode: ALWAYS classify first with GPT-5 Mini (FREE)
				a.classifying = true
				a.msgCounts[key]++
				copilotClient := a.clients["copilot"]
				return a, tea.Batch(
					func() tea.Msg { return classifyMessage(copilotClient, text)() },
					a.spinnerTick(),
				)
			}

			// Direct mode: send directly to selected model's server
			ctx := a.buildContext(10)
			model := a.tabs.SelectedModelID()
			server := a.tabs.SelectedServer()
			a.streaming = true
			a.msgCounts[key]++
			budget := a.tabs.ThinkingBudget()
			return a, tea.Batch(a.startChat(server, ctx+text, model, budget), a.spinnerTick())
		}

	case classifyResultMsg:
		a.classifying = false
		route := msg.route
		text := msg.message
		ctx := a.buildContext(10)

		// Show routing info
		a.messages = append(a.messages, chatMessage{
			role: roleSystem, content: fmt.Sprintf("[agent → %s %s]", route.Server, friendlyModel(route.Model)), ts: time.Now(),
		})

		// Complex → send with Agent system prompt (may return orchestration plan)
		if needsAgentPrompt(route.Level) {
			lspCtx := a.buildLSPContext(text)
			fullPrompt := agentSystemPrompt + "\n\n" + ctx + lspCtx + text
			a.streaming = true
			budget := a.tabs.ThinkingBudget()
			return a, tea.Batch(a.startChat(route.Server, fullPrompt, route.Model, budget), a.spinnerTick())
		}

		// Simple/Coding → send directly to routed model (no system prompt overhead)
		lspCtx := a.buildLSPContext(text)
		a.streaming = true
		budget := a.tabs.ThinkingBudget()
		return a, tea.Batch(a.startChat(route.Server, ctx+lspCtx+text, route.Model, budget), a.spinnerTick())

	case streamStartMsg:
		a.streamCh = msg.ch
		a.streamKey = msg.key
		a.cancel = msg.cancel
		// Add empty assistant message to fill via streaming
		a.messages = append(a.messages, chatMessage{
			role: roleAssistant, agent: msg.key, model: msg.model, ts: time.Now(),
		})
		return a, a.readNextEvent()

	case streamEventMsg:
		event := client.SSEEvent(msg)
		m := a.lastMsg()
		if m == nil {
			return a, nil
		}
		switch event.Type {
		case client.EventModel:
			a.modelNames[a.streamKey] = event.Model
			m.model = event.Model
		case client.EventThinking:
			m.thinking += event.Delta
		case client.EventContent:
			m.content += event.Delta
		case client.EventToolCall:
			m.content += fmt.Sprintf("\n[tool: %s]\n", event.Name)
		case client.EventDone:
			// Agent tab: check for orchestration plan
			if a.activeKey() == "agent" {
				if plan, ok := isOrchPlan(m.content); ok {
					// Replace the response with a plan summary
					m.role = roleSystem
					m.content = formatTaskPlan(plan)
					a.orchPlan = plan
					a.orchPhase = orchRunning
					a.streaming = false
					a.streamCh = nil
					a.saveSession() // persist orchestration start
					return a, a.startReadyOrchTasks()
				}
			}
			// Synthesis complete → return to idle (keep orchPlan for detail view)
			if a.orchPhase == orchSynthesizing {
				a.orchPhase = orchIdle
			}
			a.streaming = false
			a.streamCh = nil
			a.choices = detectChoices(m.content)
			a.saveSession() // persist assistant response
			return a, a.fetchQuotas()
		case client.EventError:
			m.content += fmt.Sprintf("\n[error: %s]", event.Message)
			a.streaming = false
			a.streamCh = nil
			a.saveSession()
			return a, a.fetchQuotas()
		}
		return a, a.readNextEvent()

	case orchEventMsg:
		return a, a.handleOrchEvent(msg)

	case streamErrMsg:
		a.messages = append(a.messages, chatMessage{
			role: roleSystem, content: fmt.Sprintf("[connection error: %s]", msg.err), ts: time.Now(),
		})
		a.streaming = false
		a.streamCh = nil
		a.saveSession()
		return a, nil

	case spinnerTickMsg:
		if a.copiedTimer > 0 {
			a.copiedTimer--
		}
		lspStarting := a.hasLSPStarting()
		if !a.streaming && !a.classifying && a.orchPhase == orchIdle && a.copiedTimer <= 0 && !lspStarting {
			return a, nil
		}
		a.spinnerFrame = (a.spinnerFrame + 1) % len(spinnerFrames)
		return a, a.spinnerTick()

	case healthTickMsg:
		return a, a.checkHealth()

	case healthResultMsg:
		a.sidebar.healthCache = map[string]bool(msg)
		return a, a.healthTick()

	case quotaFetchedMsg:
		return a, nil
	}

	var cmd tea.Cmd
	oldVal := a.input.Value()
	a.input, cmd = a.input.Update(msg)
	if a.input.Value() != oldVal {
		a.slashIdx = -1 // reset slash selection only on actual input change
	}
	return a, cmd
}

// ── Slash commands ──

func (a *App) handleSlashCommand(text string) tea.Cmd {
	switch {
	case text == "/reset" || text == "/new":
		a.saveSession()
		a.session = a.store.Create(a.workspace)
		a.messages = nil
		a.orchPlan = nil
		a.orchPhase = orchIdle
		a.orchCh = nil
		a.orchCancels = nil
		a.orchOrigMsg = ""
		a.nextModeOverride = ""
		a.messages = append(a.messages, chatMessage{
			role: roleSystem, content: fmt.Sprintf("[new session %s]", a.session.ID), ts: time.Now(),
		})
		return a.resetServers()

	case text == "/sessions":
		sessions, _ := a.store.List("")
		a.modal.Open(sessions, a.session.ID, a.width, a.height)
		return nil

	case text == "/clear":
		a.messages = nil
		return nil

	case text == "/status":
		var sb strings.Builder
		sb.WriteString("[server status]\n")
		for _, name := range []string{"claude", "gemini", "copilot"} {
			if a.sidebar.healthCache[name] {
				sb.WriteString(fmt.Sprintf("  ● %s  healthy\n", name))
			} else {
				sb.WriteString(fmt.Sprintf("  ○ %s  not responding\n", name))
			}
		}
		a.messages = append(a.messages, chatMessage{
			role: roleSystem, content: sb.String(), ts: time.Now(),
		})
		return nil

	case text == "/plan":
		a.nextModeOverride = "plan"
		a.messages = append(a.messages, chatMessage{
			role: roleSystem, content: "[mode: planning — next message will use interview-planning mode]", ts: time.Now(),
		})
		return nil

	case text == "/deep":
		a.nextModeOverride = "deep"
		a.messages = append(a.messages, chatMessage{
			role: roleSystem, content: "[mode: deep — next message will use autonomous execution mode]", ts: time.Now(),
		})
		return nil

	case text == "/orch":
		a.nextModeOverride = "orch"
		a.messages = append(a.messages, chatMessage{
			role: roleSystem, content: "[mode: orchestration — next message will force parallel delegation]", ts: time.Now(),
		})
		return nil

	case text == "/diagnostics":
		return a.lspDiagnostics()
	case strings.HasPrefix(text, "/symbols"):
		return a.lspSymbols(text)
	case strings.HasPrefix(text, "/hover"):
		return a.lspHover(text)
	case strings.HasPrefix(text, "/definition"):
		return a.lspDefinition(text)
	case strings.HasPrefix(text, "/references"):
		return a.lspReferences(text)

	default:
		a.messages = append(a.messages, chatMessage{
			role: roleSystem, content: fmt.Sprintf("[unknown command: %s]", text), ts: time.Now(),
		})
		return nil
	}
}

func (a *App) handleSessionSwitch(id string) tea.Cmd {
	if id == "" {
		a.messages = append(a.messages, chatMessage{
			role: roleSystem, content: "[usage: /session <id>]", ts: time.Now(),
		})
		return nil
	}

	// Save current session
	a.saveSession()

	// Load target session
	sess, err := a.store.Load(id)
	if err != nil {
		a.messages = append(a.messages, chatMessage{
			role: roleSystem, content: fmt.Sprintf("[session not found: %s]", id), ts: time.Now(),
		})
		return nil
	}

	// Switch to the loaded session
	a.session = sess
	a.messages = nil
	a.orchPlan = nil
	a.orchPhase = orchIdle
	a.orchCh = nil
	a.orchCancels = nil
	a.orchOrigMsg = ""
	a.nextModeOverride = ""

	// Restore messages
	for _, m := range sess.Messages {
		a.messages = append(a.messages, sessionMsgToChatMsg(m))
	}
	// Rebuild message counts
	a.msgCounts = make(map[string]int)
	for _, m := range a.messages {
		if m.role == roleUser {
			a.msgCounts[m.agent]++
		}
	}
	a.messages = append(a.messages, chatMessage{
		role: roleSystem, content: fmt.Sprintf("[switched to session %s — %d messages]", sess.ID, len(sess.Messages)), ts: time.Now(),
	})

	return a.resetServers()
}

func (a *App) handleModalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	keyStr := msg.String()

	// Rename mode
	if a.modal.mode == modalRename {
		switch keyStr {
		case "esc":
			a.modal.mode = modalBrowse
			return a, nil
		case "enter":
			sel := a.modal.Selected()
			if sel != nil && a.modal.renameInput != "" {
				a.store.Rename(sel.ID, a.modal.renameInput)
				// Refresh list
				sessions, _ := a.store.List("")
				a.modal.sessions = sessions
				a.modal.applyFilter()
				a.modal.mode = modalBrowse
				// Update current session title if renamed
				if sel.ID == a.session.ID {
					a.session.Title = a.modal.renameInput
				}
			}
			return a, nil
		case "backspace":
			r := []rune(a.modal.renameInput)
			if len(r) > 0 {
				a.modal.renameInput = string(r[:len(r)-1])
			}
			return a, nil
		default:
			r := []rune(keyStr)
			if len(r) == 1 && r[0] >= 32 {
				a.modal.renameInput += keyStr
			}
			return a, nil
		}
	}

	// Browse mode
	switch keyStr {
	case "esc":
		a.modal.Close()
		return a, nil
	case "enter":
		sel := a.modal.Selected()
		if sel != nil {
			a.modal.Close()
			return a, a.handleSessionSwitch(sel.ID)
		}
		return a, nil
	case "up", "k":
		if a.modal.cursor > 0 {
			a.modal.cursor--
		}
		return a, nil
	case "down", "j":
		if a.modal.cursor < len(a.modal.filtered)-1 {
			a.modal.cursor++
		}
		return a, nil
	case "ctrl+d":
		sel := a.modal.Selected()
		if sel != nil && sel.ID != a.session.ID {
			a.store.Delete(sel.ID)
			sessions, _ := a.store.List("")
			a.modal.sessions = sessions
			a.modal.applyFilter()
			if a.modal.cursor >= len(a.modal.filtered) {
				a.modal.cursor = len(a.modal.filtered) - 1
			}
			if a.modal.cursor < 0 {
				a.modal.cursor = 0
			}
		}
		return a, nil
	case "ctrl+r":
		sel := a.modal.Selected()
		if sel != nil {
			a.modal.mode = modalRename
			a.modal.renameInput = sel.Title
		}
		return a, nil
	case "backspace":
		r := []rune(a.modal.search)
		if len(r) > 0 {
			a.modal.search = string(r[:len(r)-1])
			a.modal.applyFilter()
		}
		return a, nil
	default:
		r := []rune(keyStr)
		if len(r) == 1 && r[0] >= 32 {
			a.modal.search += keyStr
			a.modal.applyFilter()
		}
		return a, nil
	}
}

func (a *App) resetServers() tea.Cmd {
	return func() tea.Msg {
		for _, c := range a.clients {
			c.Reset(context.Background())
		}
		return nil
	}
}

// ── Chat ──

func (a *App) startChat(server, text, model string, budget int) tea.Cmd {
	if server == "copilot" {
		a.quota.IncrCopilot(modelCost(model))
	}
	return func() tea.Msg {
		c, ok := a.clients[server]
		if !ok {
			return streamErrMsg{fmt.Errorf("no server configured for %s", server)}
		}
		ctx, cancel := context.WithCancel(context.Background())
		ch, err := c.Chat(ctx, text, model, budget)
		if err != nil {
			cancel()
			return streamErrMsg{err}
		}
		return streamStartMsg{ch: ch, key: server, model: model, cancel: cancel}
	}
}

// modelCost returns the premium request cost multiplier for a copilot model.
func modelCost(model string) int {
	switch model {
	case "gpt-5-mini", "gpt-4.1":
		return 0 // free
	case "claude-haiku-4-5":
		return 1
	case "claude-opus-4-6":
		return 3
	default:
		return 1
	}
}

// ── Orchestration ──

func (a *App) startReadyOrchTasks() tea.Cmd {
	if a.orchPlan == nil {
		return nil
	}
	if a.orchCh == nil {
		a.orchCh = make(chan orchEventMsg, 64)
	}
	ready := ReadyTasks(a.orchPlan)
	var cmds []tea.Cmd
	for _, t := range ready {
		t.State = TaskRunning
		a.messages = append(a.messages, chatMessage{
			role: roleSystem, content: fmt.Sprintf("[▶ %s] %s — %s", t.ID, t.Description, friendlyModel(t.Model)), ts: time.Now(),
		})
		cmds = append(cmds, a.startOrchTask(t))
	}
	cmds = append(cmds, a.readNextOrchEvent())
	return tea.Batch(cmds...)
}

func (a *App) startOrchTask(t *SubTask) tea.Cmd {
	orchCh := a.orchCh
	return func() tea.Msg {
		c, ok := a.clients[t.Agent]
		if !ok {
			orchCh <- orchEventMsg{taskID: t.ID, event: client.SSEEvent{Type: client.EventError, Message: "no server for " + t.Agent}}
			return nil
		}
		ctx, cancel := context.WithCancel(context.Background())
		a.orchCancels = append(a.orchCancels, cancel)
		ch, err := c.Chat(ctx, t.Prompt, t.Model, 0)
		if err != nil {
			orchCh <- orchEventMsg{taskID: t.ID, event: client.SSEEvent{Type: client.EventError, Message: err.Error()}}
			return nil
		}
		for event := range ch {
			orchCh <- orchEventMsg{taskID: t.ID, event: event}
		}
		return nil
	}
}

func (a *App) readNextOrchEvent() tea.Cmd {
	ch := a.orchCh
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return nil
		}
		return event
	}
}

func (a *App) handleOrchEvent(msg orchEventMsg) tea.Cmd {
	if a.orchPlan == nil {
		return nil
	}
	t := findTask(a.orchPlan, msg.taskID)
	if t == nil {
		return a.readNextOrchEvent()
	}

	switch msg.event.Type {
	case client.EventModel:
		t.ActualModel = msg.event.Model
	case client.EventThinking:
		t.Thinking += msg.event.Delta
	case client.EventContent:
		t.Result += msg.event.Delta
	case client.EventDone:
		t.State = TaskDone
		a.messages = append(a.messages, chatMessage{
			role: roleSystem, content: fmt.Sprintf("[✓ %s] %s completed", t.ID, t.Description), ts: time.Now(),
		})
		// Check if all done → synthesis
		if AllDone(a.orchPlan) {
			return a.startSynthesis()
		}
		// Start newly ready tasks
		return a.startReadyOrchTasks()
	case client.EventError:
		t.State = TaskError
		t.Result = "[error: " + msg.event.Message + "]"
		a.messages = append(a.messages, chatMessage{
			role: roleSystem, content: fmt.Sprintf("[✗ %s] %s error: %s", t.ID, t.Description, msg.event.Message), ts: time.Now(),
		})
		if AllDone(a.orchPlan) {
			return a.startSynthesis()
		}
		return a.startReadyOrchTasks()
	}

	return a.readNextOrchEvent()
}

// ── Subagent detail view ──

func (a *App) handleSubagentKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		a.subagentView = false
		return a, nil
	case "left", "h":
		a.cycleSubagentTask(-1)
		return a, nil
	case "right", "l":
		a.cycleSubagentTask(1)
		return a, nil
	case "pgup":
		a.subagentVP.ScrollUp(10)
		return a, nil
	case "pgdown":
		a.subagentVP.ScrollDown(10)
		return a, nil
	case "ctrl+c":
		a.saveSession()
		if a.lspManager != nil {
			go a.lspManager.StopAll()
		}
		for _, cancel := range a.orchCancels {
			cancel()
		}
		if a.cancel != nil {
			a.cancel()
		}
		return a, tea.Quit
	}
	return a, nil
}

func (a *App) cycleSubagentTask(dir int) {
	if a.orchPlan == nil || len(a.orchPlan.Tasks) == 0 {
		return
	}
	idx := 0
	for i, t := range a.orchPlan.Tasks {
		if t.ID == a.subagentTaskID {
			idx = i
			break
		}
	}
	idx += dir
	if idx < 0 {
		idx = len(a.orchPlan.Tasks) - 1
	}
	if idx >= len(a.orchPlan.Tasks) {
		idx = 0
	}
	a.subagentTaskID = a.orchPlan.Tasks[idx].ID
}

func (a *App) renderSubagentDetail(width int) string {
	if a.orchPlan == nil {
		return ""
	}
	t := findTask(a.orchPlan, a.subagentTaskID)
	if t == nil {
		return ""
	}

	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	thinkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Italic(true)

	var sb strings.Builder

	// Tab bar for tasks
	sb.WriteString("  ")
	for _, task := range a.orchPlan.Tasks {
		var icon string
		switch task.State {
		case TaskDone:
			icon = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("✓")
		case TaskRunning:
			frame := spinnerFrames[a.spinnerFrame%len(spinnerFrames)]
			icon = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render("●" + frame[:1])
		case TaskError:
			icon = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render("✗")
		default:
			icon = dim.Render("○")
		}

		name := task.ID
		if task.ID == a.subagentTaskID {
			sb.WriteString(" " + icon + " " + headerStyle.Render(name) + " ")
		} else {
			sb.WriteString(" " + icon + " " + dim.Render(name) + " ")
		}
	}
	sb.WriteString("\n")
	sb.WriteString(dim.Render(strings.Repeat("─", width-2)) + "\n\n")

	// Task info
	sb.WriteString("  " + labelStyle.Render("Description: ") + t.Description + "\n")
	sb.WriteString("  " + labelStyle.Render("Agent:       ") + t.Agent + "\n")
	sb.WriteString("  " + labelStyle.Render("Model:       ") + friendlyModel(t.Model) + "\n")
	if t.ActualModel != "" && t.ActualModel != t.Model {
		sb.WriteString("  " + labelStyle.Render("Actual:      ") + t.ActualModel + "\n")
	}

	stateStr := "Pending"
	switch t.State {
	case TaskRunning:
		stateStr = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render("Running...")
	case TaskDone:
		stateStr = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("Done")
	case TaskError:
		stateStr = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render("Error")
	}
	sb.WriteString("  " + labelStyle.Render("Status:      ") + stateStr + "\n")

	if len(t.DependsOn) > 0 {
		sb.WriteString("  " + labelStyle.Render("Depends on:  ") + strings.Join(t.DependsOn, ", ") + "\n")
	}

	sb.WriteString("\n")

	// Thinking section
	if t.Thinking != "" {
		sb.WriteString("  " + thinkStyle.Render("─── thinking ───") + "\n")
		for _, line := range strings.Split(t.Thinking, "\n") {
			sb.WriteString("  " + thinkStyle.Render(line) + "\n")
		}
		sb.WriteString("\n")
	}

	// Result section
	if t.Result != "" {
		sb.WriteString("  " + labelStyle.Render("─── output ───") + "\n\n")
		rendered := renderMarkdown(t.Result, width-4)
		for _, line := range strings.Split(rendered, "\n") {
			sb.WriteString("  " + line + "\n")
		}
	} else if t.State == TaskRunning {
		frame := spinnerFrames[a.spinnerFrame%len(spinnerFrames)]
		sb.WriteString("  " + dim.Render("Waiting for output"+frame) + "\n")
	} else if t.State == TaskPending {
		sb.WriteString("  " + dim.Render("Waiting for dependencies...") + "\n")
	}

	return sb.String()
}

func (a *App) startSynthesis() tea.Cmd {
	a.orchPhase = orchSynthesizing
	a.messages = append(a.messages, chatMessage{
		role: roleSystem, content: "[synthesizing results...]", ts: time.Now(),
	})

	synthPrompt := BuildSynthesisPrompt(a.orchPlan, a.orchOrigMsg)
	server := a.orchPlan.Synthesis.Agent
	model := a.orchPlan.Synthesis.Model
	if server == "" {
		server = "claude"
	}
	if model == "" {
		model = "claude-opus-4-6"
	}

	a.streaming = true
	return tea.Batch(a.startSynthesisChat(server, synthPrompt, model), a.spinnerTick())
}

func (a *App) startSynthesisChat(server, text, model string) tea.Cmd {
	return func() tea.Msg {
		c, ok := a.clients[server]
		if !ok {
			return streamErrMsg{fmt.Errorf("no server for synthesis: %s", server)}
		}
		ctx, cancel := context.WithCancel(context.Background())
		ch, err := c.Chat(ctx, text, model, 0)
		if err != nil {
			cancel()
			return streamErrMsg{err}
		}
		// Synthesis uses the normal streaming path
		return streamStartMsg{ch: ch, key: server, model: model, cancel: cancel}
	}
}

func (a *App) readNextEvent() tea.Cmd {
	ch := a.streamCh
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return streamEventMsg(client.SSEEvent{Type: client.EventDone})
		}
		return streamEventMsg(event)
	}
}

// buildContext returns recent conversation history formatted for inclusion in prompts.
// It includes the last maxTurns user/assistant exchanges (skipping system messages).
func (a *App) buildContext(maxTurns int) string {
	var turns []chatMessage
	for _, m := range a.messages {
		if m.role == roleUser || m.role == roleAssistant {
			turns = append(turns, m)
		}
	}
	// Keep last N*2 messages (N turns = N user + N assistant)
	limit := maxTurns * 2
	if len(turns) > limit {
		turns = turns[len(turns)-limit:]
	}
	// Exclude the very last message (current user input, already sent separately)
	if len(turns) > 0 && turns[len(turns)-1].role == roleUser {
		turns = turns[:len(turns)-1]
	}
	if len(turns) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<conversation_history>\n")
	for _, m := range turns {
		switch m.role {
		case roleUser:
			sb.WriteString("User: " + m.content + "\n")
		case roleAssistant:
			// Truncate long responses to save tokens
			content := m.content
			if len(content) > 2000 {
				content = content[:2000] + "...(truncated)"
			}
			sb.WriteString("Assistant: " + content + "\n")
		}
	}
	sb.WriteString("</conversation_history>\n\n")

	// Inject LSP diagnostics if available
	if a.lspManager != nil {
		if diags := a.lspManager.DiagnosticSummary(); diags != "" {
			sb.WriteString("<lsp_diagnostics>\n")
			// Limit diagnostics to avoid bloating context
			if len(diags) > 2000 {
				diags = diags[:2000] + "\n...(truncated)"
			}
			sb.WriteString(diags)
			sb.WriteString("</lsp_diagnostics>\n\n")
		}
	}

	return sb.String()
}

// buildLSPContext extracts file paths mentioned in the user message,
// opens them in LSP, and returns symbol/type context for the agent.
func (a *App) buildLSPContext(userMsg string) string {
	if a.lspManager == nil {
		return ""
	}

	// Detect file paths in the message
	paths := detectFilePaths(userMsg, a.workspace)
	if len(paths) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("<lsp_context>\n")
	for _, p := range paths {
		// Open file in LSP
		a.lspManager.OpenFileForLSP(p)

		// Get symbols
		symbols, err := a.lspManager.SymbolsOf(p)
		if err != nil || symbols == "(no symbols found)" {
			continue
		}
		rel := p
		if r, err := filepath.Rel(a.workspace, p); err == nil {
			rel = r
		}
		sb.WriteString(fmt.Sprintf("## %s symbols:\n%s\n", rel, symbols))

		// Limit total size
		if sb.Len() > 3000 {
			sb.WriteString("...(truncated)\n")
			break
		}
	}
	sb.WriteString("</lsp_context>\n\n")
	if sb.Len() < 30 { // only tags, no content
		return ""
	}
	return sb.String()
}

// detectFilePaths finds file paths mentioned in text that exist in the workspace.
func detectFilePaths(text, workspace string) []string {
	words := strings.Fields(text)
	var paths []string
	seen := make(map[string]bool)

	for _, w := range words {
		// Strip common surrounding characters
		w = strings.Trim(w, "`\"',;:()[]{}?!")
		// Remove line:col suffix (e.g. file.go:10:5)
		if idx := strings.Index(w, ":"); idx > 0 {
			w = w[:idx]
		}

		if w == "" || !strings.Contains(w, ".") {
			continue
		}

		// Try as relative path
		candidate := w
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(workspace, candidate)
		}
		if seen[candidate] {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			paths = append(paths, candidate)
			seen[candidate] = true
		}
		if len(paths) >= 3 {
			break
		}
	}
	return paths
}

type gitStatus struct {
	Branch    string
	Ahead     int
	Behind    int
	Staged    int
	Unstaged  int
	Untracked int
}

func getGitStatus(workspace string) gitStatus {
	var gs gitStatus

	// Branch + ahead/behind
	cmd := exec.Command("git", "-C", workspace, "status", "--porcelain=v2", "--branch")
	out, err := cmd.Output()
	if err != nil {
		return gs
	}

	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "# branch.head ") {
			gs.Branch = strings.TrimPrefix(line, "# branch.head ")
		} else if strings.HasPrefix(line, "# branch.ab ") {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				fmt.Sscanf(parts[2], "+%d", &gs.Ahead)
				fmt.Sscanf(parts[3], "-%d", &gs.Behind)
			}
		} else if len(line) > 0 && line[0] == '1' || len(line) > 0 && line[0] == '2' {
			// Changed entry: "1 XY ..." or "2 XY ..."
			if len(line) >= 4 {
				xy := line[2:4]
				if xy[0] != '.' {
					gs.Staged++
				}
				if xy[1] != '.' {
					gs.Unstaged++
				}
			}
		} else if strings.HasPrefix(line, "? ") {
			gs.Untracked++
		}
	}
	return gs
}

func shortenPath(path string) string {
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

func (a *App) hasLSPStarting() bool {
	if a.lspManager == nil {
		return false
	}
	for _, s := range a.lspManager.Status() {
		if s.State == lsp.StateStarting {
			return true
		}
	}
	return false
}

func (a *App) sidebarWidth() int {
	sw := a.width / 4
	if sw < 28 {
		sw = 28
	}
	if sw > a.width {
		sw = a.width
	}
	return sw
}

// modelLabel returns the display label for an agent message.
func (a *App) modelLabel(m *chatMessage) string {
	name := strings.ToUpper(m.agent[:1]) + m.agent[1:]
	model := m.model
	if model == "" {
		model = a.modelNames[m.agent]
	}
	if model != "" {
		return "[" + name + "] " + model
	}
	return "[" + name + "]"
}

// accentColor returns the mode-specific accent color: cyan for Agent, pink for Direct.
func (a *App) accentColor() color.Color {
	if a.activeKey() == "agent" {
		return lipgloss.Color("14") // cyan
	}
	return lipgloss.Color("205") // pink
}

// agentIndicator renders the status line below the input: TabName  Model  Provider · Variant
func (a *App) agentIndicator() string {
	key := a.activeKey()
	inputBg := lipgloss.Color("236")
	accent := a.accentColor()

	// All styles include inputBg background for consistent box appearance
	accentStyle := lipgloss.NewStyle().Foreground(accent).Background(inputBg)
	white := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(inputBg)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("242")).Background(inputBg)
	yellow := lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Background(inputBg)
	spaceBg := lipgloss.NewStyle().Background(inputBg)

	// Tab name
	var tabDisplay string
	if key == "agent" {
		tabDisplay = accentStyle.Bold(true).Render("Agent")
	} else {
		tabDisplay = accentStyle.Bold(true).Render("Direct")
	}

	// Model name (with brand prefix)
	server := a.tabs.SelectedServer()
	modelDisplay := white.Render(fullModelName(server, a.tabs.SelectedModelName()))

	// Provider
	providerDisplay := dim.Render(providerName(server))

	// Variant (thinking level)
	var variantPart string
	if a.tabs.HasThinking() {
		thinkName := a.tabs.ThinkingName()
		if a.tabs.ThinkingBudget() > 0 {
			variantPart = dim.Render(" · ") + yellow.Render(thinkName)
		} else {
			variantPart = dim.Render(" · ") + dim.Render(thinkName)
		}
	}

	return spaceBg.Render(" ") + tabDisplay + spaceBg.Render("  ") + modelDisplay + spaceBg.Render("  ") + providerDisplay + variantPart
}

// renderMessages renders the unified chat history.
func (a *App) renderMessages(width int) string {
	if len(a.messages) == 0 {
		return ""
	}

	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	userLabel := lipgloss.NewStyle().Foreground(lipgloss.Color("99")).Bold(true)
	agentLabel := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	systemStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("242")).Italic(true)
	userAccent := lipgloss.NewStyle().Foreground(lipgloss.Color("99"))
	agentAccent := lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	timeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	var sections []string

	for i := range a.messages {
		m := &a.messages[i]
		ts := timeStyle.Render(m.ts.Format("15:04"))

		switch m.role {
		case roleUser:
			label := userLabel.Render("[You]")
			header := userAccent.Render("┃") + " " + label
			headerW := lipgloss.Width(header)
			tsW := lipgloss.Width(ts)
			gap := width - headerW - tsW - 1
			if gap < 1 {
				gap = 1
			}
			line1 := header + strings.Repeat(" ", gap) + ts

			contentLines := strings.Split(m.content, "\n")
			var body []string
			for _, cl := range contentLines {
				body = append(body, userAccent.Render("┃")+"  "+cl)
			}
			sections = append(sections, line1+"\n"+strings.Join(body, "\n"))

		case roleAssistant:
			label := agentLabel.Render(a.modelLabel(m))
			header := agentAccent.Render("┃") + " " + label
			headerW := lipgloss.Width(header)
			tsW := lipgloss.Width(ts)
			gap := width - headerW - tsW - 1
			if gap < 1 {
				gap = 1
			}
			line1 := header + strings.Repeat(" ", gap) + ts

			var body []string

			if m.thinking != "" {
				thinkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Italic(true)
				thinkLines := strings.Split(m.thinking, "\n")
				body = append(body, agentAccent.Render("┃")+"  "+thinkStyle.Render("thinking..."))
				for _, tl := range thinkLines {
					body = append(body, agentAccent.Render("┃")+"  "+thinkStyle.Render("  "+tl))
				}
				body = append(body, agentAccent.Render("┃"))
			}

			rendered := renderMarkdown(m.content, width-4)
			contentLines := strings.Split(rendered, "\n")
			for _, cl := range contentLines {
				body = append(body, agentAccent.Render("┃")+"  "+cl)
			}
			sections = append(sections, line1+"\n"+strings.Join(body, "\n"))

		case roleSystem:
			sections = append(sections, dim.Render("  "+ts+"  ")+systemStyle.Render(m.content))
		}
	}

	return strings.Join(sections, "\n\n")
}

func (a *App) View() tea.View {
	if a.width == 0 || a.height == 0 {
		return tea.View{}
	}

	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("242"))

	indicatorH := 1
	bottomBarH := 1
	contentLines := strings.Count(a.input.Value(), "\n") + 1
	taHeight := max(1, min(contentLines, 10))
	a.input.SetHeight(taHeight)
	inputH := taHeight + 3 // textarea + statusLine + accentLine + separators
	choiceBarH := 0
	if len(a.choices) > 0 && !a.streaming && !a.classifying && a.orchPhase == orchIdle {
		choiceBarH = 1
	}
	cmdHintH := 0
	if matches := a.matchingCommands(); len(matches) > 0 {
		cmdHintH = len(matches)
	}
	chatH := a.height - inputH - bottomBarH - indicatorH - choiceBarH - cmdHintH
	if chatH < 5 {
		chatH = 5
	}
	a.chatVP.SetHeight(chatH)

	sidebarW := a.sidebarWidth()
	mainW := a.width - sidebarW
	if mainW < 30 {
		mainW = 30
		sidebarW = a.width - mainW
	}

	key := a.activeKey()

	var status string
	if a.classifying {
		frame := spinnerFrames[a.spinnerFrame%len(spinnerFrames)]
		status = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render("● classifying" + frame)
	} else if a.orchPhase == orchRunning {
		frame := spinnerFrames[a.spinnerFrame%len(spinnerFrames)]
		status = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render("● orchestrating" + frame)
	} else if a.orchPhase == orchSynthesizing {
		frame := spinnerFrames[a.spinnerFrame%len(spinnerFrames)]
		status = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render("● synthesizing" + frame)
	} else if a.streaming {
		frame := spinnerFrames[a.spinnerFrame%len(spinnerFrames)]
		status = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render("● streaming" + frame)
	} else {
		status = dim.Render("○ ready")
	}

	// Subagent detail view replaces the main chat panel
	if a.subagentView {
		subContent := a.renderSubagentDetail(mainW - 2)
		a.subagentVP.SetContent(subContent)
		if a.orchPhase == orchRunning {
			a.subagentVP.GotoBottom()
		}
	}

	chatOutput := a.renderMessages(mainW - 2)
	chatContent := banner + "\n\n" + status + "\n\n" + chatOutput

	wasAtBottom := a.chatVP.AtBottom()
	a.chatVP.SetContent(chatContent)
	if a.streaming || a.orchPhase != orchIdle || wasAtBottom {
		a.chatVP.GotoBottom()
	}

	a.sidebar.activeKey = key
	a.sidebar.streaming = a.streaming
	a.sidebar.msgCounts = a.msgCounts
	a.sidebar.width = sidebarW
	a.sidebar.height = chatH
	a.sidebar.modelNames = a.modelNames
	a.sidebar.quota = a.quota
	a.sidebar.orchPlan = a.orchPlan
	a.sidebar.orchPhase = a.orchPhase
	a.sidebar.workDir = shortenPath(a.workspace)
	a.sidebar.gitInfo = a.gitInfo
	if a.lspManager != nil {
		a.sidebar.lspStatus = a.lspManager.Status()
		a.sidebar.spinnerFrame = a.spinnerFrame
	}
	if a.session != nil {
		a.sidebar.sessionID = a.session.ID
	}

	// Sidebar: full height (scrollable + fixed bottom)
	sideFixedH := 5 // sep + cwd + git + empty + version
	sideVPH := a.height - sideFixedH
	if sideVPH < 5 {
		sideVPH = 5
	}
	a.sideVP.SetHeight(sideVPH)
	a.sidebar.height = a.height

	sideContent := a.sidebar.RenderScrollable()
	a.sideVP.SetContent(sideContent)
	sideFixed := a.sidebar.RenderFixed()
	sidePanel := a.sideVP.View() + "\n" + sideFixed

	// Chat panel
	var chatPanel string
	if a.subagentView {
		chatPanel = a.subagentVP.View()
	} else {
		chatPanel = a.chatVP.View()
		if a.modal.visible {
			a.modal.width = mainW
			a.modal.height = chatH
			chatPanel = a.modal.OverlayOnPanel(chatPanel, mainW, chatH)
		}
	}

	// Left bottom area (input box + accent + shortcuts)
	accent := a.accentColor()
	accentLine := lipgloss.NewStyle().Foreground(accent).Render(strings.Repeat("\u2500", mainW))

	cmdHint := ""
	if matches := a.matchingCommands(); len(matches) > 0 {
		// Placeholder lines for layout height; real rendering overlaid full-width after JoinHorizontal
		placeholders := make([]string, len(matches))
		for i := range placeholders {
			placeholders[i] = strings.Repeat(" ", mainW)
		}
		cmdHint = strings.Join(placeholders, "\n") + "\n"
	}

	// Build input box: textarea + status line, all with background
	inputBg := lipgloss.Color("236")
	bgStyle := lipgloss.NewStyle().Background(inputBg)
	barStyle := lipgloss.NewStyle().Foreground(accent).Background(inputBg)

	// Update textarea prompt color to match current mode
	curStyles := a.input.Styles()
	curStyles.Focused.Prompt = lipgloss.NewStyle().Foreground(accent).Background(inputBg)
	a.input.SetStyles(curStyles)
	inputView := a.input.View()
	indicator := a.agentIndicator()
	if a.subagentView {
		subLabel := lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
		indicator = subLabel.Render("\u25a0 Subagent Detail") + "  " + dim.Render(a.subagentTaskID) +
			"  " + dim.Render("\u2190 \u2192 switch  esc back")
	}
	if a.copiedTimer > 0 {
		copiedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
		indicator += "  " + copiedStyle.Render("[copied]")
	}

	// Pad each textarea line to full width with background
	padLine := func(line string, w int) string {
		lw := lipgloss.Width(line)
		if lw < w {
			return line + bgStyle.Render(strings.Repeat(" ", w-lw))
		}
		return line
	}

	inputLines := strings.Split(inputView, "\n")
	// Ensure minimum 2 visible lines for consistent box height
	for len(inputLines) < 2 {
		inputLines = append(inputLines, barStyle.Render("\u2503")+bgStyle.Render(" "))
	}
	var boxLines []string
	for _, line := range inputLines {
		boxLines = append(boxLines, padLine(line, mainW))
	}
	inputBox := strings.Join(boxLines, "\n")

	// Status line OUTSIDE the box (fixed position)
	statusLine := barStyle.Render("\u2503") + bgStyle.Render(indicator)
	statusLine = padLine(statusLine, mainW)

	choiceBar := ""
	if len(a.choices) > 0 && !a.streaming && !a.classifying && a.orchPhase == orchIdle {
		choiceKey := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
		choiceText := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
		var parts []string
		for i, c := range a.choices {
			parts = append(parts, choiceKey.Render(fmt.Sprintf(" %d ", i+1))+choiceText.Render(" "+c))
		}
		choiceBar = strings.Join(parts, "  ") + "\n"
	}

	bottomBar := a.tabs.BottomBar(mainW)

	leftPanel := chatPanel + "\n" +
		choiceBar + cmdHint + inputBox + "\n" +
		statusLine + "\n" +
		accentLine + "\n" +
		bottomBar

	view := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, sidePanel)

	// Overlay full-width slash command menu on top of combined view
	if matches := a.matchingCommands(); len(matches) > 0 {
		viewLines := strings.Split(view, "\n")
		selBg := lipgloss.Color("236")
		for i, m := range matches {
			pos := chatH + choiceBarH + i
			if pos >= len(viewLines) {
				break
			}
			var line string
			if i == a.slashIdx {
				cmdPart := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true).Background(selBg).
					Render(fmt.Sprintf("  %-14s", m.name))
				descPart := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(selBg).
					Render(" " + m.desc)
				line = cmdPart + descPart
				w := lipgloss.Width(line)
				if w < a.width {
					line += lipgloss.NewStyle().Background(selBg).Render(strings.Repeat(" ", a.width-w))
				}
			} else {
				cmdPart := lipgloss.NewStyle().Foreground(lipgloss.Color("99")).Bold(true).
					Render(fmt.Sprintf("  %-14s", m.name))
				descPart := lipgloss.NewStyle().Foreground(lipgloss.Color("242")).
					Render(" " + m.desc)
				line = cmdPart + descPart
				w := lipgloss.Width(line)
				if w < a.width {
					line += strings.Repeat(" ", a.width-w)
				}
			}
			viewLines[pos] = line
		}
		view = strings.Join(viewLines, "\n")
	}

	var v tea.View
	v.SetContent(view)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.KeyboardEnhancements.ReportEventTypes = true

	// Set real terminal cursor position for accurate CJK wide-char rendering
	if a.input.Focused() {
		cur := a.input.Cursor()
		if cur != nil {
			// Y offset: chatPanel(chatH) + choiceBarH + cmdHintH
			cur.Y += chatH + choiceBarH + cmdHintH
			v.Cursor = cur
		}
	}

	return v
}

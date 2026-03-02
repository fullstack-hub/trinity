package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fullstack-hub/trinity/internal/client"
	"github.com/fullstack-hub/trinity/internal/config"
)

type streamEventMsg client.SSEEvent
type streamErrMsg struct{ err error }

type App struct {
	cfg       *config.Config
	tabs      TabBar
	clients   map[string]*client.Client
	input     textarea.Model
	outputs   map[string]string
	streaming bool
	cancel    context.CancelFunc
}

func NewApp(cfg *config.Config) *App {
	ta := textarea.New()
	ta.Placeholder = "메시지를 입력하세요..."
	ta.Focus()
	ta.SetHeight(3)
	ta.SetWidth(58)

	tabNames := []string{"Claude", "Gemini", "Copilot"}
	tabKeys := []string{"claude", "gemini", "copilot"}

	clients := make(map[string]*client.Client)
	for _, key := range tabKeys {
		if srv, ok := cfg.Servers[key]; ok {
			clients[key] = client.New(srv.URL)
		}
	}

	outputs := make(map[string]string)
	for _, key := range tabKeys {
		outputs[key] = ""
	}

	defaultTab := 0
	for i, key := range tabKeys {
		if key == cfg.DefaultAgent {
			defaultTab = i
		}
	}

	return &App{
		cfg:     cfg,
		tabs:    TabBar{Tabs: tabNames, ActiveTab: defaultTab},
		clients: clients,
		input:   ta,
		outputs: outputs,
	}
}

func (a *App) activeKey() string {
	keys := []string{"claude", "gemini", "copilot"}
	return keys[a.tabs.ActiveTab]
}

func (a *App) Init() tea.Cmd {
	return textarea.Blink
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			if a.cancel != nil {
				a.cancel()
			}
			return a, tea.Quit
		case "tab":
			if !a.streaming {
				a.tabs.ActiveTab = (a.tabs.ActiveTab + 1) % len(a.tabs.Tabs)
			}
			return a, nil
		case "enter":
			if a.streaming {
				return a, nil
			}
			text := strings.TrimSpace(a.input.Value())
			if text == "" {
				return a, nil
			}
			if strings.HasPrefix(text, "/reset") {
				a.input.Reset()
				return a, a.handleReset()
			}
			a.input.Reset()
			a.streaming = true
			return a, a.handleChat(text)
		}

	case streamEventMsg:
		event := client.SSEEvent(msg)
		key := a.activeKey()
		switch event.Type {
		case client.EventContent:
			a.outputs[key] += event.Delta
		case client.EventToolCall:
			a.outputs[key] += fmt.Sprintf("\n[tool: %s]\n", event.Name)
		case client.EventDone:
			a.outputs[key] += "\n"
			a.streaming = false
		case client.EventError:
			a.outputs[key] += fmt.Sprintf("\n[error: %s]\n", event.Message)
			a.streaming = false
		}
		return a, nil

	case streamErrMsg:
		a.outputs[a.activeKey()] += fmt.Sprintf("\n[connection error: %s]\n", msg.err)
		a.streaming = false
		return a, nil
	}

	var cmd tea.Cmd
	a.input, cmd = a.input.Update(msg)
	return a, cmd
}

func (a *App) handleChat(text string) tea.Cmd {
	return func() tea.Msg {
		key := a.activeKey()
		c, ok := a.clients[key]
		if !ok {
			return streamErrMsg{fmt.Errorf("no server configured for %s", key)}
		}
		ctx, cancel := context.WithCancel(context.Background())
		a.cancel = cancel
		ch, err := c.Chat(ctx, text)
		if err != nil {
			return streamErrMsg{err}
		}
		for event := range ch {
			return streamEventMsg(event)
		}
		return streamEventMsg(client.SSEEvent{Type: client.EventDone})
	}
}

func (a *App) handleReset() tea.Cmd {
	return func() tea.Msg {
		key := a.activeKey()
		c, ok := a.clients[key]
		if !ok {
			return nil
		}
		c.Reset(context.Background())
		a.outputs[key] = "[session reset]\n"
		return nil
	}
}

func (a *App) View() string {
	key := a.activeKey()
	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("242"))

	var status string
	if a.streaming {
		status = statusStyle.Render("● streaming...")
	} else {
		status = statusStyle.Render("○ ready")
	}

	return fmt.Sprintf(
		"%s\n%s\n\n%s\n\n%s\n\n%s",
		a.tabs.View(),
		status,
		a.outputs[key],
		a.input.View(),
		statusStyle.Render("Tab: switch | Enter: send | /reset: clear | Ctrl+C: quit"),
	)
}

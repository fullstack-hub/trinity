package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func initialModel() tea.Model {
	return &app{activeTab: 0}
}

type app struct {
	activeTab int
}

func (a *app) Init() tea.Cmd { return nil }

func (a *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return a, tea.Quit
		case "tab":
			a.activeTab = (a.activeTab + 1) % 3
		}
	}
	return a, nil
}

func (a *app) View() string {
	tabs := []string{"Claude", "Gemini", "Copilot"}
	return fmt.Sprintf("[ %s ] | [ %s ] | [ %s ]\n\nActive: %s\n\nPress Tab to switch, q to quit",
		tabs[0], tabs[1], tabs[2], tabs[a.activeTab])
}

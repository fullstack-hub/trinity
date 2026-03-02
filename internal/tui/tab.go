package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	activeTabStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		Border(lipgloss.RoundedBorder()).
		Padding(0, 2)

	inactiveTabStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Border(lipgloss.HiddenBorder()).
		Padding(0, 2)
)

type TabBar struct {
	Tabs      []string
	ActiveTab int
}

func (t TabBar) View() string {
	var rendered []string
	for i, tab := range t.Tabs {
		if i == t.ActiveTab {
			rendered = append(rendered, activeTabStyle.Render(tab))
		} else {
			rendered = append(rendered, inactiveTabStyle.Render(tab))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...) + "\n" + strings.Repeat("─", 60)
}

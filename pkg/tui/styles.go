package tui

import "github.com/charmbracelet/lipgloss"

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			MarginBottom(1)

	userStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).
			Bold(true)

	agentStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("78")).
			Italic(true)

	systemStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	promptStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("208")).
			Bold(true)

	sepStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("238"))

	toolErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	toolDotStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).
			Bold(true)

	toolNameStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).
			Bold(true)

	toolFileStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	toolInfoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	mascotStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("208"))

	versionStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))
)

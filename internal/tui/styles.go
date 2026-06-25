package tui

import "github.com/charmbracelet/lipgloss"

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	mutedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

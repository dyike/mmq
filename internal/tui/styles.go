package tui

import "github.com/charmbracelet/lipgloss"

// Colors
var (
	PrimaryColor   = lipgloss.Color("39")  // Blue
	SecondaryColor = lipgloss.Color("212") // Pink
	AccentColor    = lipgloss.Color("76")  // Green
	ErrorColor     = lipgloss.Color("196") // Red
	WarningColor   = lipgloss.Color("214") // Orange
	MutedColor     = lipgloss.Color("240") // Gray
	TextColor      = lipgloss.Color("252") // Light gray
	BgColor        = lipgloss.Color("235") // Dark gray
)

// Styles
var (
	// Base styles
	BaseStyle = lipgloss.NewStyle().
			Foreground(TextColor)

	// Sidebar styles
	SidebarStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(MutedColor).
			Padding(0, 1)

	SidebarTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(PrimaryColor).
				Padding(0, 1).
				MarginBottom(1)

	SessionItemStyle = lipgloss.NewStyle().
				Padding(0, 1)

	SessionItemSelectedStyle = lipgloss.NewStyle().
					Background(PrimaryColor).
					Foreground(lipgloss.Color("0")).
					Padding(0, 1)

	SessionItemActiveStyle = lipgloss.NewStyle().
				Foreground(AccentColor).
				Bold(true).
				Padding(0, 1)

	// Chat view styles
	ChatViewStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(MutedColor).
			Padding(0, 1)

	UserMessageStyle = lipgloss.NewStyle().
				Foreground(PrimaryColor).
				Bold(true)

	AssistantMessageStyle = lipgloss.NewStyle().
				Foreground(AccentColor)

	SystemMessageStyle = lipgloss.NewStyle().
				Foreground(WarningColor).
				Italic(true)

	ToolMessageStyle = lipgloss.NewStyle().
				Foreground(SecondaryColor)

	// Input styles
	InputStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(PrimaryColor).
			Padding(0, 1)

	InputFocusedStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(AccentColor).
				Padding(0, 1)

	// Status bar styles
	StatusBarStyle = lipgloss.NewStyle().
			Background(BgColor).
			Foreground(TextColor).
			Padding(0, 1)

	StatusAgentStyle = lipgloss.NewStyle().
				Background(PrimaryColor).
				Foreground(lipgloss.Color("0")).
				Padding(0, 1).
				MarginRight(1)

	StatusRunningStyle = lipgloss.NewStyle().
				Background(AccentColor).
				Foreground(lipgloss.Color("0")).
				Padding(0, 1)

	StatusErrorStyle = lipgloss.NewStyle().
				Background(ErrorColor).
				Foreground(lipgloss.Color("0")).
				Padding(0, 1)

	// Help styles
	HelpStyle = lipgloss.NewStyle().
			Foreground(MutedColor)

	HelpKeyStyle = lipgloss.NewStyle().
			Foreground(PrimaryColor).
			Bold(true)

	// Error styles
	ErrorStyle = lipgloss.NewStyle().
			Foreground(ErrorColor).
			Bold(true)

	// Permission dialog styles
	PermissionDialogStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.DoubleBorder()).
				BorderForeground(WarningColor).
				Padding(1, 2).
				Align(lipgloss.Center)

	PermissionTitleStyle = lipgloss.NewStyle().
				Foreground(WarningColor).
				Bold(true).
				MarginBottom(1)

	PermissionToolStyle = lipgloss.NewStyle().
				Foreground(SecondaryColor).
				Bold(true)

	ButtonStyle = lipgloss.NewStyle().
			Padding(0, 2).
			MarginRight(1)

	ButtonFocusedStyle = lipgloss.NewStyle().
				Background(PrimaryColor).
				Foreground(lipgloss.Color("0")).
				Padding(0, 2).
				MarginRight(1)

	ButtonDangerStyle = lipgloss.NewStyle().
				Background(ErrorColor).
				Foreground(lipgloss.Color("0")).
				Padding(0, 2).
				MarginRight(1)
)

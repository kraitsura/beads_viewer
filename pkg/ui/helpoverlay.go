package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// HelpOverlayModel shows keyboard shortcuts help
type HelpOverlayModel struct {
	visible bool
	width   int
	height  int
	theme   Theme
}

// NewHelpOverlayModel creates a new help overlay
func NewHelpOverlayModel(theme Theme) HelpOverlayModel {
	return HelpOverlayModel{
		theme: theme,
	}
}

// Show makes the help overlay visible
func (m *HelpOverlayModel) Show() {
	m.visible = true
}

// Hide makes the help overlay invisible
func (m *HelpOverlayModel) Hide() {
	m.visible = false
}

// Toggle toggles visibility
func (m *HelpOverlayModel) Toggle() {
	m.visible = !m.visible
}

// IsVisible returns true if overlay is showing
func (m HelpOverlayModel) IsVisible() bool {
	return m.visible
}

// SetSize sets dimensions
func (m *HelpOverlayModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Update handles input
func (m HelpOverlayModel) Update(msg tea.Msg) (HelpOverlayModel, tea.Cmd) {
	if !m.visible {
		return m, nil
	}

	switch msg.(type) {
	case tea.KeyMsg:
		// Any key closes help
		m.visible = false
	}

	return m, nil
}

// View renders the help overlay
func (m HelpOverlayModel) View() string {
	if !m.visible {
		return ""
	}

	var b strings.Builder

	titleStyle := m.theme.Renderer.NewStyle().
		Bold(true).
		Foreground(m.theme.Primary).
		MarginBottom(1)
	b.WriteString(titleStyle.Render("Review Dashboard Help"))
	b.WriteString("\n\n")

	sectionStyle := m.theme.Renderer.NewStyle().Bold(true).Foreground(m.theme.Secondary)
	keyStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Primary).Width(12)
	descStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext)

	// Navigation section
	b.WriteString(sectionStyle.Render("NAVIGATION") + "\n")
	shortcuts := []struct{ key, desc string }{
		{"j/↓", "Move down"},
		{"k/↑", "Move up"},
		{"g", "Go to top"},
		{"G", "Go to bottom"},
		{"]", "Next unreviewed"},
		{"[", "Previous unreviewed"},
		{"Tab", "Switch panel focus"},
	}
	for _, s := range shortcuts {
		b.WriteString("  " + keyStyle.Render(s.key) + descStyle.Render(s.desc) + "\n")
	}
	b.WriteString("\n")

	// Actions section
	b.WriteString(sectionStyle.Render("ACTIONS") + "\n")
	actions := []struct{ key, desc string }{
		{"a", "Approve"},
		{"r", "Request revision"},
		{"d", "Defer"},
		{"s", "Skip"},
		{"n", "Add note"},
	}
	for _, a := range actions {
		b.WriteString("  " + keyStyle.Render(a.key) + descStyle.Render(a.desc) + "\n")
	}
	b.WriteString("\n")

	// View section
	b.WriteString(sectionStyle.Render("VIEW") + "\n")
	views := []struct{ key, desc string }{
		{"f", "Cycle filter"},
		{"?", "Toggle this help"},
		{"q/Esc", "Quit"},
	}
	for _, v := range views {
		b.WriteString("  " + keyStyle.Render(v.key) + descStyle.Render(v.desc) + "\n")
	}

	b.WriteString("\n")
	hintStyle := m.theme.Renderer.NewStyle().Faint(true).Italic(true)
	b.WriteString(hintStyle.Render("[Press any key to close]"))

	// Wrap in box
	boxStyle := m.theme.Renderer.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.Border).
		Padding(1, 2)

	return boxStyle.Render(b.String())
}

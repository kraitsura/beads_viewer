package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// NoteInputModel provides a modal for entering review notes
type NoteInputModel struct {
	textarea textarea.Model
	title    string
	action   string // "revision", "defer", "note"
	issueID  string
	width    int
	height   int
	theme    Theme

	// Result
	submitted bool
	cancelled bool
	notes     string
}

// NewNoteInputModel creates a new note input modal
func NewNoteInputModel(title, action, issueID string, theme Theme) NoteInputModel {
	ta := textarea.New()
	ta.Placeholder = "Enter your notes here..."
	ta.Focus()
	ta.CharLimit = 1000
	ta.SetWidth(50)
	ta.SetHeight(5)

	return NoteInputModel{
		textarea: ta,
		title:    title,
		action:   action,
		issueID:  issueID,
		theme:    theme,
	}
}

// Init implements tea.Model
func (m NoteInputModel) Init() tea.Cmd {
	return textarea.Blink
}

// Update implements tea.Model
func (m NoteInputModel) Update(msg tea.Msg) (NoteInputModel, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.cancelled = true
			return m, nil
		case "ctrl+enter", "ctrl+s", "ctrl+j":
			// ctrl+j is alternate for terminals that don't support ctrl+enter
			m.submitted = true
			m.notes = m.textarea.Value()
			return m, nil
		}
	}

	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

// View implements tea.Model
func (m NoteInputModel) View() string {
	var b strings.Builder

	// Modal box
	width := 60
	if m.width > 0 && m.width < 70 {
		width = m.width - 10
	}

	// Title
	titleStyle := m.theme.Renderer.NewStyle().
		Bold(true).
		Foreground(m.theme.Primary).
		Width(width).
		Align(lipgloss.Center)

	actionTitle := "Add Note"
	switch m.action {
	case "revision":
		actionTitle = "Request Revision"
	case "defer":
		actionTitle = "Defer Review"
	}
	b.WriteString(titleStyle.Render(actionTitle + " for " + m.issueID))
	b.WriteString("\n\n")

	// Prompt
	promptStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext)
	b.WriteString(promptStyle.Render("Enter your notes:"))
	b.WriteString("\n\n")

	// Textarea
	b.WriteString(m.textarea.View())
	b.WriteString("\n\n")

	// Hints
	hintStyle := m.theme.Renderer.NewStyle().Faint(true)
	b.WriteString(hintStyle.Render("[Ctrl+Enter/Ctrl+J] Submit  [Esc] Cancel"))

	// Wrap in box
	boxStyle := m.theme.Renderer.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.Border).
		Padding(1, 2).
		Width(width)

	return boxStyle.Render(b.String())
}

// SetSize sets the modal dimensions
func (m *NoteInputModel) SetSize(width, height int) {
	m.width = width
	m.height = height

	// Adjust textarea width
	taWidth := width - 20
	if taWidth < 30 {
		taWidth = 30
	}
	if taWidth > 60 {
		taWidth = 60
	}
	m.textarea.SetWidth(taWidth)
}

// IsSubmitted returns true if the user submitted the note
func (m NoteInputModel) IsSubmitted() bool {
	return m.submitted
}

// IsCancelled returns true if the user cancelled
func (m NoteInputModel) IsCancelled() bool {
	return m.cancelled
}

// Notes returns the entered note text
func (m NoteInputModel) Notes() string {
	return m.notes
}

// Action returns the action type
func (m NoteInputModel) Action() string {
	return m.action
}

// IssueID returns the issue being noted
func (m NoteInputModel) IssueID() string {
	return m.issueID
}

// Reset prepares the modal for reuse
func (m *NoteInputModel) Reset() {
	m.submitted = false
	m.cancelled = false
	m.notes = ""
	m.textarea.Reset()
}

package ui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type IssueDelegate struct {
	ShowExtraCols bool // Show Age and Comments columns if true
}

func (d IssueDelegate) Height() int {
	return 1
}

func (d IssueDelegate) Spacing() int {
	return 0
}

func (d IssueDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	return nil
}

func (d IssueDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(IssueItem)
	if !ok {
		return
	}

	// Styles
	var baseStyle lipgloss.Style
	if index == m.Index() {
		baseStyle = SelectedItemStyle
	} else {
		baseStyle = ItemStyle
	}

	// Columns
	id := ColIDStyle.Render(i.Issue.ID)
	
	iconStr, iconColor := GetTypeIcon(string(i.Issue.IssueType))
	typeIcon := ColTypeStyle.Foreground(iconColor).Render(iconStr)
	
	prio := ColPrioStyle.Render(GetPriorityIcon(i.Issue.Priority))
	
	statusColor := GetStatusColor(string(i.Issue.Status))
	status := ColStatusStyle.Foreground(statusColor).Render(strings.ToUpper(string(i.Issue.Status)))

	// Optional Columns
	age := ""
	comments := ""
	extraWidth := 0

	if d.ShowExtraCols {
		ageStr := FormatTimeRel(i.Issue.CreatedAt)
		age = ColAgeStyle.Render(ageStr)
		
		commentCount := len(i.Issue.Comments)
		if commentCount > 0 {
			comments = ColCommentsStyle.Render(fmt.Sprintf("💬%d", commentCount))
		} else {
			comments = ColCommentsStyle.Render("")
		}
		extraWidth = 12 + 2 // 8(Age) + 4(Comments) + gaps
	}

	assignee := ""
	assigneeWidth := 0
	if i.Issue.Assignee != "" {
		assignee = ColAssigneeStyle.Render("@" + i.Issue.Assignee)
		assigneeWidth = 12 
	}

	// Calculate Title Width
	// Fixed widths: ID(8) + Type(2) + Prio(3) + Status(12) + Assignee(12 or 0) + Extra + Spacing
	fixedWidth := 8 + 2 + 3 + 12 + assigneeWidth + extraWidth + 8 // +8 for gaps
	availableWidth := m.Width() - fixedWidth - 4 // -4 for left/right padding/borders
	
	if availableWidth < 10 {
		availableWidth = 10
	}

	titleStyle := ColTitleStyle.Copy().Width(availableWidth).MaxWidth(availableWidth)
	if index == m.Index() {
		titleStyle = titleStyle.Foreground(ColorPrimary).Bold(true)
	}
	
	titleStr := i.Issue.Title
	title := titleStyle.Render(titleStr)

	// Compose Row
	var row string
	if d.ShowExtraCols {
		row = lipgloss.JoinHorizontal(lipgloss.Left,
			id, typeIcon, prio, status, title, comments, age, assignee,
		)
	} else {
		row = lipgloss.JoinHorizontal(lipgloss.Left,
			id, typeIcon, prio, status, title, assignee,
		)
	}

	fmt.Fprint(w, baseStyle.Render(row))
}

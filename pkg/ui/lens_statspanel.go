package ui

import (
	"fmt"
	"strings"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"

	"github.com/charmbracelet/lipgloss"
)

// StatusCounts holds counts for each issue status.
type StatusCounts struct {
	Open       int
	InProgress int
	Blocked    int
	Closed     int
	Total      int
}

// NewStatusCounts creates StatusCounts from a status count map.
func NewStatusCounts(counts map[model.Status]int) StatusCounts {
	sc := StatusCounts{
		Open:       counts[model.StatusOpen],
		InProgress: counts[model.StatusInProgress],
		Blocked:    counts[model.StatusBlocked],
		Closed:     counts[model.StatusClosed],
	}
	sc.Total = sc.Open + sc.InProgress + sc.Blocked + sc.Closed
	return sc
}

// RenderStatsHeaderBox renders a consistent header box for stats panels.
// typeLabel should be "EPIC:", "LABEL:", "BEAD:", etc.
// color should be a lipgloss.TerminalColor (e.g., theme.Epic, theme.Secondary).
func RenderStatsHeaderBox(title, typeLabel string, width int, theme Theme, color lipgloss.TerminalColor) []string {
	headerStyle := theme.Renderer.NewStyle().Bold(true).Foreground(color)

	boxWidth := width - StatsPanelPadding
	if boxWidth < MinBoxWidth {
		boxWidth = MinBoxWidth
	}

	// Calculate max title length accounting for type label
	maxTitleLen := boxWidth - len(typeLabel) - 4
	if maxTitleLen < 5 {
		maxTitleLen = 5
	}
	if len(title) > maxTitleLen {
		title = title[:maxTitleLen-1] + "…"
	}

	topBorder := "╔" + strings.Repeat("═", boxWidth-2) + "╗"
	bottomBorder := "╚" + strings.Repeat("═", boxWidth-2) + "╝"

	// Format title line to fit box width
	contentWidth := boxWidth - 4 // Account for "║ " and " ║"
	titleContent := fmt.Sprintf("%s %s", typeLabel, title)
	if len(titleContent) > contentWidth {
		titleContent = titleContent[:contentWidth]
	}
	titleLine := fmt.Sprintf("║ %-*s║", boxWidth-3, titleContent)

	return []string{
		headerStyle.Render(topBorder),
		headerStyle.Render(titleLine),
		headerStyle.Render(bottomBorder),
	}
}

// RenderStatusBars renders a consistent status breakdown section with mini bars.
func RenderStatusBars(counts StatusCounts, theme Theme) []string {
	t := theme
	total := counts.Total
	if total == 0 {
		total = 1
	}

	openBar := RenderMiniBar(float64(counts.Open)/float64(total), 10, t)
	inProgBar := RenderMiniBar(float64(counts.InProgress)/float64(total), 10, t)
	blockedBar := RenderMiniBar(float64(counts.Blocked)/float64(total), 10, t)
	closedBar := RenderMiniBar(float64(counts.Closed)/float64(total), 10, t)

	openStyle := t.Renderer.NewStyle().Foreground(t.Open)
	inProgStyle := t.Renderer.NewStyle().Foreground(t.InProgress)
	blockedStyle := t.Renderer.NewStyle().Foreground(t.Blocked)
	closedStyle := t.Renderer.NewStyle().Foreground(t.Closed)

	return []string{
		fmt.Sprintf("   %s %-12s %2d %s", openStyle.Render("●"), "Open:", counts.Open, openBar),
		fmt.Sprintf("   %s %-12s %2d %s", inProgStyle.Render("●"), "In Progress:", counts.InProgress, inProgBar),
		fmt.Sprintf("   %s %-12s %2d %s", blockedStyle.Render("●"), "Blocked:", counts.Blocked, blockedBar),
		fmt.Sprintf("   %s %-12s %2d %s", closedStyle.Render("●"), "Closed:", counts.Closed, closedBar),
	}
}

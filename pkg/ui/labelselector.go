package ui

import (
	"sort"
	"strconv"
	"strings"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

// LabelItem represents a selectable label or epic in the selector
type LabelItem struct {
	Type        string  // "label" or "epic"
	Value       string  // label name or epic ID
	Title       string  // display text (same as Value for labels, title for epics)
	IssueCount  int     // total issues with this label
	ClosedCount int     // closed issues
	Progress    float64 // completion percentage
	IsPinned    bool    // is this item pinned
}

// LabelSelectorModel represents the label selector overlay
type LabelSelectorModel struct {
	// Data
	allItems      []LabelItem // All available items (labels + epics)
	filteredItems []LabelItem // Filtered by search
	pinnedItems   []LabelItem // Pinned labels (persisted)
	recentItems   []LabelItem // Recently selected labels

	// UI State
	searchInput   textinput.Model
	selectedIndex int
	currentSection int // 0=pinned, 1=recent, 2=epics, 3=labels (or search results)

	// Dimensions
	width  int
	height int
	theme  Theme

	// Selection result
	confirmed    bool
	selectedItem *LabelItem
}

// NewLabelSelectorModel creates a new label selector
func NewLabelSelectorModel(issues []model.Issue, theme Theme) LabelSelectorModel {
	// Create search input
	ti := textinput.New()
	ti.Placeholder = "Search labels and epics..."
	ti.Focus()
	ti.CharLimit = 64
	ti.Width = 40

	// Extract unique labels with counts
	labelCounts := make(map[string]struct {
		total  int
		closed int
	})
	var epics []LabelItem

	for _, issue := range issues {
		// Collect epics
		if issue.IssueType == model.TypeEpic && issue.Status != model.StatusClosed {
			// Count children for epic progress
			childTotal, childClosed := countEpicChildren(issue.ID, issues)
			progress := 0.0
			if childTotal > 0 {
				progress = float64(childClosed) / float64(childTotal)
			}
			epics = append(epics, LabelItem{
				Type:        "epic",
				Value:       issue.ID,
				Title:       issue.Title,
				IssueCount:  childTotal,
				ClosedCount: childClosed,
				Progress:    progress,
			})
		}

		// Collect labels
		for _, label := range issue.Labels {
			counts := labelCounts[label]
			counts.total++
			if issue.Status == model.StatusClosed {
				counts.closed++
			}
			labelCounts[label] = counts
		}
	}

	// Convert labels to items
	var labels []LabelItem
	for name, counts := range labelCounts {
		progress := 0.0
		if counts.total > 0 {
			progress = float64(counts.closed) / float64(counts.total)
		}
		labels = append(labels, LabelItem{
			Type:        "label",
			Value:       name,
			Title:       name,
			IssueCount:  counts.total,
			ClosedCount: counts.closed,
			Progress:    progress,
		})
	}

	// Sort labels alphabetically
	sort.Slice(labels, func(i, j int) bool {
		return labels[i].Value < labels[j].Value
	})

	// Sort epics by progress (incomplete first)
	sort.Slice(epics, func(i, j int) bool {
		if epics[i].Progress == epics[j].Progress {
			return epics[i].Title < epics[j].Title
		}
		return epics[i].Progress < epics[j].Progress
	})

	// Combine all items: epics first, then labels
	allItems := append(epics, labels...)

	return LabelSelectorModel{
		allItems:      allItems,
		filteredItems: allItems,
		searchInput:   ti,
		selectedIndex: 0,
		theme:         theme,
		width:         60,
		height:        20,
	}
}

// countEpicChildren counts total and closed children for an epic
func countEpicChildren(epicID string, issues []model.Issue) (total, closed int) {
	for _, issue := range issues {
		for _, dep := range issue.Dependencies {
			if dep.DependsOnID == epicID && dep.Type == model.DepParentChild {
				total++
				if issue.Status == model.StatusClosed {
					closed++
				}
				break
			}
		}
	}
	return
}

// SetSize updates the selector dimensions
func (m *LabelSelectorModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	// Update text input width
	inputWidth := width - 20
	if inputWidth < 20 {
		inputWidth = 20
	}
	if inputWidth > 50 {
		inputWidth = 50
	}
	m.searchInput.Width = inputWidth
}

// Update handles input and returns whether the model changed
func (m *LabelSelectorModel) Update(key string) (handled bool) {
	switch key {
	case "up", "k":
		m.moveUp()
		return true
	case "down", "j":
		m.moveDown()
		return true
	case "enter":
		if len(m.filteredItems) > 0 && m.selectedIndex < len(m.filteredItems) {
			item := m.filteredItems[m.selectedIndex]
			m.selectedItem = &item
			m.confirmed = true
		}
		return true
	case "esc":
		m.confirmed = false
		m.selectedItem = nil
		return true
	case "backspace":
		if len(m.searchInput.Value()) > 0 {
			m.searchInput.SetValue(m.searchInput.Value()[:len(m.searchInput.Value())-1])
			m.filterItems()
		}
		return true
	default:
		// Handle text input for single characters
		if len(key) == 1 {
			m.searchInput.SetValue(m.searchInput.Value() + key)
			m.filterItems()
			return true
		}
	}
	return false
}

// HandleTextInput processes a text input message
func (m *LabelSelectorModel) HandleTextInput(value string) {
	m.searchInput.SetValue(value)
	m.filterItems()
}

func (m *LabelSelectorModel) moveUp() {
	if m.selectedIndex > 0 {
		m.selectedIndex--
	}
}

func (m *LabelSelectorModel) moveDown() {
	if m.selectedIndex < len(m.filteredItems)-1 {
		m.selectedIndex++
	}
}

func (m *LabelSelectorModel) filterItems() {
	query := strings.TrimSpace(m.searchInput.Value())
	if query == "" {
		m.filteredItems = m.allItems
		m.selectedIndex = 0
		return
	}

	// Build searchable strings
	searchStrings := make([]string, len(m.allItems))
	for i, item := range m.allItems {
		searchStrings[i] = item.Title + " " + item.Value
	}

	// Fuzzy search
	matches := fuzzy.Find(query, searchStrings)

	m.filteredItems = make([]LabelItem, 0, len(matches))
	for _, match := range matches {
		m.filteredItems = append(m.filteredItems, m.allItems[match.Index])
	}

	// Sort filtered items to match display order: epics first, then labels
	// This ensures selectedIndex matches the visual ordering in View()
	sort.SliceStable(m.filteredItems, func(i, j int) bool {
		if m.filteredItems[i].Type == "epic" && m.filteredItems[j].Type != "epic" {
			return true
		}
		if m.filteredItems[i].Type != "epic" && m.filteredItems[j].Type == "epic" {
			return false
		}
		return false // preserve fuzzy match order within each type
	})

	// Reset selection to top
	m.selectedIndex = 0
}

// IsConfirmed returns true if user confirmed a selection
func (m *LabelSelectorModel) IsConfirmed() bool {
	return m.confirmed
}

// IsCancelled returns true if user cancelled the selector
func (m *LabelSelectorModel) IsCancelled() bool {
	return m.selectedItem == nil && !m.confirmed
}

// SelectedItem returns the selected label item, or nil if none
func (m *LabelSelectorModel) SelectedItem() *LabelItem {
	return m.selectedItem
}

// Reset clears the selection state for reuse
func (m *LabelSelectorModel) Reset() {
	m.confirmed = false
	m.selectedItem = nil
	m.searchInput.SetValue("")
	m.filteredItems = m.allItems
	m.selectedIndex = 0
}

// View renders the label selector overlay
func (m *LabelSelectorModel) View() string {
	t := m.theme

	// Calculate box dimensions
	boxWidth := 55
	if m.width < 65 {
		boxWidth = m.width - 10
	}
	if boxWidth < 35 {
		boxWidth = 35
	}

	contentWidth := boxWidth - 4 // Account for padding

	var lines []string

	// Title
	titleStyle := t.Renderer.NewStyle().
		Foreground(t.Primary).
		Bold(true)
	lines = append(lines, titleStyle.Render("Select Label or Epic"))
	lines = append(lines, "")

	// Search input
	inputStyle := t.Renderer.NewStyle().
		Foreground(t.Base.GetForeground()).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Secondary).
		Padding(0, 1).
		Width(contentWidth - 2)

	searchValue := m.searchInput.Value()
	if searchValue == "" {
		searchValue = t.Renderer.NewStyle().Foreground(t.Subtext).Render(m.searchInput.Placeholder)
	}
	lines = append(lines, inputStyle.Render(searchValue))
	lines = append(lines, "")

	// Calculate max visible items
	maxVisible := m.height - 12
	if maxVisible < 5 {
		maxVisible = 5
	}
	if maxVisible > 15 {
		maxVisible = 15
	}

	// Render items by section
	if len(m.filteredItems) == 0 {
		emptyStyle := t.Renderer.NewStyle().Foreground(t.Subtext).Italic(true)
		lines = append(lines, emptyStyle.Render("  No matching labels or epics"))
	} else {
		// Group items by type for display
		var epics, labels []LabelItem
		for _, item := range m.filteredItems {
			if item.Type == "epic" {
				epics = append(epics, item)
			} else {
				labels = append(labels, item)
			}
		}

		// Track global index for selection
		globalIdx := 0

		// Show epics section if any
		if len(epics) > 0 {
			sectionStyle := t.Renderer.NewStyle().
				Foreground(t.Secondary).
				Bold(true)
			lines = append(lines, sectionStyle.Render("EPICS"))

			for _, item := range epics {
				if globalIdx >= maxVisible {
					break
				}
				line := m.renderItem(item, globalIdx == m.selectedIndex, contentWidth)
				lines = append(lines, line)
				globalIdx++
			}
			lines = append(lines, "")
		}

		// Show labels section if any
		if len(labels) > 0 && globalIdx < maxVisible {
			sectionStyle := t.Renderer.NewStyle().
				Foreground(t.Secondary).
				Bold(true)
			lines = append(lines, sectionStyle.Render("LABELS"))

			for _, item := range labels {
				if globalIdx >= maxVisible {
					break
				}
				line := m.renderItem(item, globalIdx == m.selectedIndex, contentWidth)
				lines = append(lines, line)
				globalIdx++
			}
		}

		// Show "more" indicator if truncated
		if len(m.filteredItems) > maxVisible {
			moreStyle := t.Renderer.NewStyle().Foreground(t.Subtext).Italic(true)
			lines = append(lines, moreStyle.Render(
				strings.Repeat(" ", 2)+"... and "+
					strconv.Itoa(len(m.filteredItems)-maxVisible)+" more"))
		}
	}

	// Footer with keybindings
	lines = append(lines, "")
	footerStyle := t.Renderer.NewStyle().
		Foreground(t.Subtext).
		Italic(true)
	lines = append(lines, footerStyle.Render("j/k: navigate • enter: select • esc: cancel"))

	content := strings.Join(lines, "\n")

	// Box style
	boxStyle := t.Renderer.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Primary).
		Padding(1, 2).
		Width(boxWidth)

	box := boxStyle.Render(content)

	// Center in viewport
	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		box,
	)
}

func (m *LabelSelectorModel) renderItem(item LabelItem, isSelected bool, maxWidth int) string {
	t := m.theme

	// Prefix based on selection and type
	prefix := "  "
	if isSelected {
		prefix = "▸ "
	}

	// Icon for type
	icon := ""
	if item.Type == "epic" {
		icon = "📋 "
	}

	// Name/title
	nameStyle := t.Renderer.NewStyle()
	if isSelected {
		nameStyle = nameStyle.Foreground(t.Primary).Bold(true)
	} else {
		nameStyle = nameStyle.Foreground(t.Base.GetForeground())
	}

	// Truncate title if needed
	title := item.Title
	maxTitleLen := maxWidth - 25 // Leave room for progress bar
	if len(title) > maxTitleLen {
		title = title[:maxTitleLen-1] + "…"
	}

	// Progress bar
	progressBar := m.renderProgressBar(item.Progress, item.ClosedCount, item.IssueCount)

	// Build the line
	name := prefix + icon + title

	// Pad to align progress bars
	padding := maxWidth - len(name) - len(progressBar) - 2
	if padding < 1 {
		padding = 1
	}

	return nameStyle.Render(name) + strings.Repeat(" ", padding) + progressBar
}

func (m *LabelSelectorModel) renderProgressBar(progress float64, closed, total int) string {
	t := m.theme

	if total == 0 {
		return t.Renderer.NewStyle().Foreground(t.Subtext).Render("(0)")
	}

	// Progress bar: 8 characters wide
	barWidth := 8
	filled := int(progress * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}

	var barColor lipgloss.AdaptiveColor
	if progress >= 1.0 {
		barColor = t.Closed // Completed
	} else if progress >= 0.5 {
		barColor = t.InProgress
	} else {
		barColor = t.Open
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	barStyled := t.Renderer.NewStyle().Foreground(barColor).Render("[" + bar + "]")

	// Count
	countStr := t.Renderer.NewStyle().Foreground(t.Subtext).Render(
		" " + strconv.Itoa(closed) + "/" + strconv.Itoa(total))

	// Checkmark if complete
	if progress >= 1.0 {
		countStr += " ✓"
	}

	return barStyled + countStr
}

// SearchValue returns the current search input value
func (m *LabelSelectorModel) SearchValue() string {
	return m.searchInput.Value()
}

// ItemCount returns the number of filtered items
func (m *LabelSelectorModel) ItemCount() int {
	return len(m.filteredItems)
}

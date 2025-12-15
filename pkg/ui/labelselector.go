package ui

import (
	"fmt"
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
	Type         string  // "label" or "epic"
	Value        string  // label name or epic ID
	Title        string  // display text (same as Value for labels, title for epics)
	IssueCount   int     // total issues with this label
	ClosedCount  int     // closed issues
	Progress     float64 // completion percentage
	IsPinned     bool    // is this item pinned
	OverlapCount int     // issues overlapping with scope label (when scope is set)
}

// LabelSelectorModel represents the label selector overlay
type LabelSelectorModel struct {
	// Data
	allItems      []LabelItem   // All available items (labels + epics)
	filteredItems []LabelItem   // Filtered by search
	pinnedItems   []LabelItem   // Pinned labels (persisted)
	recentItems   []LabelItem   // Recently selected labels
	issues        []model.Issue // Reference to issues for scope filtering

	// UI State
	searchInput    textinput.Model
	selectedIndex  int
	currentSection int // 0=pinned, 1=recent, 2=epics, 3=labels (or search results)

	// Scope state (multi-scope)
	scopeLabels []string // Currently set scope labels (empty = no scope)
	scopeMode   bool     // True when in scope mode

	// Mode state (vim-style)
	insertMode     bool // True when in insert mode (typing into search)
	scopeAddMode   bool // True when insert mode was triggered by 's' (adding to scope)
	reviewMode     bool // True when searching for issue IDs (triggered by 'r')

	// Dimensions
	width  int
	height int
	theme  Theme

	// Selection result
	confirmed    bool
	selectedItem *LabelItem
	scopedLabels []string // When scope is set and item selected, both labels returned
}

// NewLabelSelectorModel creates a new label selector
func NewLabelSelectorModel(issues []model.Issue, theme Theme) LabelSelectorModel {
	// Create search input
	ti := textinput.New()
	ti.Placeholder = "Search labels and epics..."
	ti.Focus()
	ti.CharLimit = 64
	ti.Width = 40

	// Collect unique label names and epics
	labelSet := make(map[string]bool)
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

		// Collect unique label names
		for _, label := range issue.Labels {
			labelSet[label] = true
		}
	}

	// Build label items with direct counts only (no descendants)
	labelCounts := make(map[string]struct{ total, closed int })
	for _, issue := range issues {
		for _, label := range issue.Labels {
			counts := labelCounts[label]
			counts.total++
			if issue.Status == model.StatusClosed {
				counts.closed++
			}
			labelCounts[label] = counts
		}
	}

	var labels []LabelItem
	for name := range labelSet {
		counts := labelCounts[name]
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
		issues:        issues,
		searchInput:   ti,
		selectedIndex: 0,
		theme:         theme,
		width:         60,
		height:        20,
	}
}

// countEpicChildren counts total and closed descendants for an epic (recursive)
func countEpicChildren(epicID string, issues []model.Issue) (total, closed int) {
	children, issueStatus := buildChildrenMap(issues)

	// BFS to count all descendants
	visited := make(map[string]bool)
	queue := []string{epicID}
	visited[epicID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, childID := range children[current] {
			if !visited[childID] {
				visited[childID] = true
				total++
				if issueStatus[childID] == model.StatusClosed {
					closed++
				}
				queue = append(queue, childID)
			}
		}
	}
	return
}

// buildChildrenMap builds parent -> children map and issue status map for efficient traversal
func buildChildrenMap(issues []model.Issue) (children map[string][]string, issueStatus map[string]model.Status) {
	children = make(map[string][]string)
	issueStatus = make(map[string]model.Status)
	for _, issue := range issues {
		issueStatus[issue.ID] = issue.Status
		for _, dep := range issue.Dependencies {
			if dep.Type == model.DepParentChild {
				children[dep.DependsOnID] = append(children[dep.DependsOnID], issue.ID)
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
	// Handle insert mode (all keys go to search except esc/enter)
	if m.insertMode {
		return m.updateInsertMode(key)
	}
	return m.updateNormalMode(key)
}

// updateInsertMode handles keys when in insert/search mode
func (m *LabelSelectorModel) updateInsertMode(key string) bool {
	switch key {
	case "esc":
		// Exit insert mode, return to normal
		m.insertMode = false
		m.scopeAddMode = false
		// For review mode, clear search and reset to all items
		if m.reviewMode {
			m.reviewMode = false
			m.searchInput.SetValue("")
			m.filteredItems = m.allItems
			m.selectedIndex = 0
		}
		// Keep search text and filtered items so user can resume where they left off
		// User can press backspace in normal mode to clear search if desired
		return true
	case "enter":
		if len(m.filteredItems) > 0 && m.selectedIndex < len(m.filteredItems) {
			item := m.filteredItems[m.selectedIndex]

			if m.scopeAddMode && item.Type == "label" {
				// Adding to scope - add label and stay in selector
				m.addToScope(item.Value)
				m.insertMode = false
				m.scopeAddMode = false
				m.searchInput.SetValue("")
				// Don't confirm - stay in selector
				return true
			}

			// Normal selection - confirm and close
			m.selectedItem = &item
			// Build scoped labels: all scope labels + selected label
			if m.scopeMode && len(m.scopeLabels) > 0 && item.Type == "label" {
				m.scopedLabels = make([]string, 0, len(m.scopeLabels)+1)
				m.scopedLabels = append(m.scopedLabels, m.scopeLabels...)
				m.scopedLabels = append(m.scopedLabels, item.Value)
			}
			m.confirmed = true
		}
		return true
	case "backspace":
		if len(m.searchInput.Value()) > 0 {
			m.searchInput.SetValue(m.searchInput.Value()[:len(m.searchInput.Value())-1])
			m.filterItems()
		}
		return true
	case "up":
		m.moveUp()
		return true
	case "down":
		m.moveDown()
		return true
	default:
		// All single characters go to search input (including j, k, s, q)
		if len(key) == 1 {
			m.searchInput.SetValue(m.searchInput.Value() + key)
			m.filterItems()
			return true
		}
	}
	return false
}

// updateNormalMode handles keys when in normal/navigation mode
func (m *LabelSelectorModel) updateNormalMode(key string) bool {
	switch key {
	case "up", "k":
		m.moveUp()
		return true
	case "down", "j":
		m.moveDown()
		return true
	case "i", "/":
		// Enter insert mode (for searching)
		m.insertMode = true
		m.scopeAddMode = false
		return true
	case "s":
		// Enter insert mode for scope adding
		m.insertMode = true
		m.scopeAddMode = true
		return true
	case "r":
		// Enter insert mode for review/ID-based search
		m.insertMode = true
		m.reviewMode = true
		m.searchInput.SetValue("")
		m.filteredItems = nil // Clear filtered items until user types
		m.selectedIndex = 0
		return true
	case "enter":
		if len(m.filteredItems) > 0 && m.selectedIndex < len(m.filteredItems) {
			item := m.filteredItems[m.selectedIndex]
			m.selectedItem = &item
			// Build scoped labels: all scope labels + selected label
			if m.scopeMode && len(m.scopeLabels) > 0 && item.Type == "label" {
				m.scopedLabels = make([]string, 0, len(m.scopeLabels)+1)
				m.scopedLabels = append(m.scopedLabels, m.scopeLabels...)
				m.scopedLabels = append(m.scopedLabels, item.Value)
			}
			m.confirmed = true
		}
		return true
	case "esc":
		// If in scope mode, clear scope first
		if m.scopeMode {
			m.clearScope()
			return true
		}
		m.confirmed = false
		m.selectedItem = nil
		return true
	case "backspace":
		// Clear search in normal mode, or remove last scope if search empty
		if len(m.searchInput.Value()) > 0 {
			m.searchInput.SetValue("")
			m.filterItems()
		} else if len(m.scopeLabels) > 0 {
			// Remove last scope label
			m.scopeLabels = m.scopeLabels[:len(m.scopeLabels)-1]
			if len(m.scopeLabels) == 0 {
				m.scopeMode = false
				m.filteredItems = m.allItems
			} else {
				m.filterByScope()
			}
			m.selectedIndex = 0
		}
		return true
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

	// Review mode: search by issue ID prefix
	if m.reviewMode {
		m.filterByIssueID(query)
		return
	}

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

// filterByIssueID filters by issue ID prefix or title substring match (for review mode)
func (m *LabelSelectorModel) filterByIssueID(query string) {
	if query == "" {
		m.filteredItems = nil
		m.selectedIndex = 0
		return
	}

	queryLower := strings.ToLower(query)
	var idMatches, titleMatches []LabelItem
	seen := make(map[string]bool)

	for _, issue := range m.issues {
		idLower := strings.ToLower(issue.ID)
		titleLower := strings.ToLower(issue.Title)

		// ID prefix match takes priority
		if strings.HasPrefix(idLower, queryLower) {
			idMatches = append(idMatches, LabelItem{
				Type:       "bead",
				Value:      issue.ID,
				Title:      issue.Title,
				IssueCount: 1,
			})
			seen[issue.ID] = true
		} else if strings.Contains(titleLower, queryLower) {
			// Title substring match (only if not already matched by ID)
			titleMatches = append(titleMatches, LabelItem{
				Type:       "bead",
				Value:      issue.ID,
				Title:      issue.Title,
				IssueCount: 1,
			})
			seen[issue.ID] = true
		}
	}

	// Sort ID matches by ID
	sort.Slice(idMatches, func(i, j int) bool {
		return idMatches[i].Value < idMatches[j].Value
	})

	// Sort title matches by title
	sort.Slice(titleMatches, func(i, j int) bool {
		return titleMatches[i].Title < titleMatches[j].Title
	})

	// Combine: ID matches first, then title matches
	m.filteredItems = append(idMatches, titleMatches...)
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

// ScopedLabels returns the scoped labels when scope mode is active
func (m *LabelSelectorModel) ScopedLabels() []string {
	return m.scopedLabels
}

// ScopeLabels returns all current scope labels
func (m *LabelSelectorModel) ScopeLabels() []string {
	return m.scopeLabels
}

// IsScopeMode returns true if scope mode is active
func (m *LabelSelectorModel) IsScopeMode() bool {
	return m.scopeMode
}

// addToScope adds a label to the scope set (no toggle, just add if not present)
func (m *LabelSelectorModel) addToScope(label string) {
	// Check if already in scope
	for _, l := range m.scopeLabels {
		if l == label {
			// Already in scope, just refilter
			m.filterByScope()
			return
		}
	}

	// Add to scope
	m.scopeLabels = append(m.scopeLabels, label)
	m.scopeMode = true
	m.filterByScope()
}

// toggleScope toggles a label in/out of the scope set
func (m *LabelSelectorModel) toggleScope(label string) {
	// Check if already in scope
	idx := -1
	for i, l := range m.scopeLabels {
		if l == label {
			idx = i
			break
		}
	}

	if idx >= 0 {
		// Remove from scope
		m.scopeLabels = append(m.scopeLabels[:idx], m.scopeLabels[idx+1:]...)
		if len(m.scopeLabels) == 0 {
			m.scopeMode = false
		}
	} else {
		// Add to scope
		m.scopeLabels = append(m.scopeLabels, label)
		m.scopeMode = true
	}

	m.searchInput.SetValue("")
	if m.scopeMode {
		m.filterByScope()
	} else {
		m.filteredItems = m.allItems
		m.selectedIndex = 0
	}
}

// clearScope clears all scopes and resets to full list
func (m *LabelSelectorModel) clearScope() {
	m.scopeLabels = nil
	m.scopeMode = false
	m.scopedLabels = nil
	m.searchInput.SetValue("")
	m.filteredItems = m.allItems
	m.selectedIndex = 0
}

// filterByScope filters to show only labels that co-occur with ALL scope labels
func (m *LabelSelectorModel) filterByScope() {
	if len(m.scopeLabels) == 0 {
		m.filteredItems = m.allItems
		return
	}

	// Build set of scope labels for quick lookup
	scopeSet := make(map[string]bool)
	for _, l := range m.scopeLabels {
		scopeSet[l] = true
	}

	// Find all issues that have ALL scope labels
	scopeIssues := make(map[string]bool)
	for _, issue := range m.issues {
		hasAll := true
		for _, scopeLabel := range m.scopeLabels {
			found := false
			for _, label := range issue.Labels {
				if label == scopeLabel {
					found = true
					break
				}
			}
			if !found {
				hasAll = false
				break
			}
		}
		if hasAll {
			scopeIssues[issue.ID] = true
		}
	}

	// Count co-occurring labels (excluding scope labels)
	labelOverlap := make(map[string]int)
	for _, issue := range m.issues {
		if !scopeIssues[issue.ID] {
			continue
		}
		for _, label := range issue.Labels {
			if !scopeSet[label] {
				labelOverlap[label]++
			}
		}
	}

	// Build filtered items with overlap counts
	var filtered []LabelItem
	for _, item := range m.allItems {
		if item.Type == "label" && !scopeSet[item.Value] {
			if overlap, ok := labelOverlap[item.Value]; ok && overlap > 0 {
				itemCopy := item
				itemCopy.OverlapCount = overlap
				filtered = append(filtered, itemCopy)
			}
		}
	}

	// Sort by overlap count (descending)
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].OverlapCount > filtered[j].OverlapCount
	})

	m.filteredItems = filtered
	m.selectedIndex = 0
}

// Reset clears the selection state for reuse
func (m *LabelSelectorModel) Reset() {
	m.confirmed = false
	m.selectedItem = nil
	m.scopedLabels = nil
	m.scopeLabels = nil
	m.scopeMode = false
	m.searchInput.SetValue("")
	m.filteredItems = m.allItems
	m.selectedIndex = 0
	m.insertMode = false
	m.scopeAddMode = false
	m.reviewMode = false
}

// IsReviewMode returns true if in review/ID search mode
func (m *LabelSelectorModel) IsReviewMode() bool {
	return m.reviewMode
}

// IsInsertMode returns true if in insert/search mode
func (m *LabelSelectorModel) IsInsertMode() bool {
	return m.insertMode
}

// IsScopeAddMode returns true if in scope-adding insert mode
func (m *LabelSelectorModel) IsScopeAddMode() bool {
	return m.scopeAddMode
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

	if m.scopeMode {
		lines = append(lines, titleStyle.Render("Select Label (within scope)"))
	} else {
		lines = append(lines, titleStyle.Render("Select Label or Epic"))
	}

	// Scope indicator - sleek inline chips
	if m.scopeMode && len(m.scopeLabels) > 0 {
		scopeIcon := t.Renderer.NewStyle().Foreground(t.Primary).Render("⊕")
		chipStyle := t.Renderer.NewStyle().
			Foreground(t.Primary).
			Bold(true)
		sepStyle := t.Renderer.NewStyle().Foreground(t.Subtext).Render(" ∩ ")

		var chips []string
		for _, label := range m.scopeLabels {
			chips = append(chips, chipStyle.Render(label))
		}
		scopeLine := scopeIcon + " " + strings.Join(chips, sepStyle)
		countStyle := t.Renderer.NewStyle().Foreground(t.Subtext).Italic(true)
		scopeLine += countStyle.Render(fmt.Sprintf("  (%d)", len(m.filteredItems)))
		lines = append(lines, scopeLine)
	}
	lines = append(lines, "")

	// Search input (highlighted when in insert mode)
	inputBorderColor := t.Secondary
	if m.insertMode {
		inputBorderColor = t.Primary
	}
	inputStyle := t.Renderer.NewStyle().
		Foreground(t.Base.GetForeground()).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(inputBorderColor).
		Padding(0, 1).
		Width(contentWidth - 2)

	searchValue := m.searchInput.Value()
	if m.insertMode {
		// Show cursor in insert mode
		cursorStyle := t.Renderer.NewStyle().Background(t.Primary).Foreground(t.Base.GetBackground())
		searchValue = searchValue + cursorStyle.Render(" ")
	} else if searchValue == "" {
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
		var beads, epics, labels []LabelItem
		for _, item := range m.filteredItems {
			switch item.Type {
			case "bead":
				beads = append(beads, item)
			case "epic":
				epics = append(epics, item)
			default:
				labels = append(labels, item)
			}
		}

		// Track global index for selection
		globalIdx := 0

		// Show beads/issues section if any (from review mode)
		if len(beads) > 0 {
			sectionStyle := t.Renderer.NewStyle().
				Foreground(t.Secondary).
				Bold(true)
			lines = append(lines, sectionStyle.Render("ISSUES"))

			for _, item := range beads {
				if globalIdx >= maxVisible {
					break
				}
				line := m.renderItem(item, globalIdx == m.selectedIndex, contentWidth)
				lines = append(lines, line)
				globalIdx++
			}
			lines = append(lines, "")
		}

		// Show epics section if any
		if len(epics) > 0 && globalIdx < maxVisible {
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
			if m.scopeMode {
				lines = append(lines, sectionStyle.Render("MATCHING LABELS (within scope)"))
			} else {
				lines = append(lines, sectionStyle.Render("LABELS"))
			}

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

	// Footer with keybindings (mode-aware)
	lines = append(lines, "")
	footerStyle := t.Renderer.NewStyle().
		Foreground(t.Subtext).
		Italic(true)
	modeStyle := t.Renderer.NewStyle().
		Foreground(t.Primary).
		Bold(true)

	if m.insertMode {
		// Insert mode footer - different hint for scope adding vs searching vs review
		if m.scopeAddMode {
			modeIndicator := modeStyle.Render("SCOPE+")
			lines = append(lines, modeIndicator+" "+footerStyle.Render("type to filter • ↑↓: nav • enter: add scope • esc: cancel"))
		} else if m.reviewMode {
			modeIndicator := modeStyle.Render("REVIEW")
			lines = append(lines, modeIndicator+" "+footerStyle.Render("type ID or title • ↑↓: nav • enter: view bead • esc: cancel"))
		} else {
			modeIndicator := modeStyle.Render("SEARCH")
			lines = append(lines, modeIndicator+" "+footerStyle.Render("type to filter • ↑↓: nav • enter: select • esc: cancel"))
		}
	} else if m.scopeMode {
		// Scope mode footer (normal mode with scopes set)
		modeIndicator := modeStyle.Render("SCOPE")
		lines = append(lines, modeIndicator+" "+footerStyle.Render("j/k: nav • s: +scope • ⌫: -scope • enter: select • esc: clear • q: close"))
	} else {
		// Normal mode footer
		modeIndicator := modeStyle.Render("NORMAL")
		lines = append(lines, modeIndicator+" "+footerStyle.Render("j/k: nav • i: search • r: ID lookup • s: +scope • enter: select • q: close"))
	}

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
	switch item.Type {
	case "epic":
		icon = "📋 "
	case "bead":
		icon = "🔷 "
	}

	// Name/title
	nameStyle := t.Renderer.NewStyle()
	if isSelected {
		nameStyle = nameStyle.Foreground(t.Primary).Bold(true)
	} else {
		nameStyle = nameStyle.Foreground(t.Base.GetForeground())
	}

	// Build display text: for beads show ID + title, for others just title
	var displayText string
	if item.Type == "bead" {
		// Show ID in bold/highlighted followed by title
		idPart := item.Value
		titlePart := item.Title
		maxTitleLen := maxWidth - 30 - len(idPart) // Leave room for ID and padding
		if len(titlePart) > maxTitleLen && maxTitleLen > 5 {
			titlePart = titlePart[:maxTitleLen-1] + "…"
		}
		displayText = idPart + " " + titlePart
	} else {
		// Truncate title if needed
		title := item.Title
		maxTitleLen := maxWidth - 25 // Leave room for progress bar or overlap
		if len(title) > maxTitleLen {
			title = title[:maxTitleLen-1] + "…"
		}
		displayText = title
	}

	// Build the line
	name := prefix + icon + displayText

	// Show overlap count when in scope mode, otherwise progress bar
	var suffix string
	if m.scopeMode && item.OverlapCount > 0 {
		overlapStyle := t.Renderer.NewStyle().Foreground(t.InProgress)
		suffix = overlapStyle.Render("(" + strconv.Itoa(item.OverlapCount) + " overlap)")
	} else {
		suffix = m.renderProgressBar(item.Progress, item.ClosedCount, item.IssueCount)
	}

	// Pad to align
	padding := maxWidth - len(name) - len(suffix) - 2
	if padding < 1 {
		padding = 1
	}

	return nameStyle.Render(name) + strings.Repeat(" ", padding) + suffix
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

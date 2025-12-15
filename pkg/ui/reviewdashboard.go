package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/Dicklesworthstone/beads_viewer/pkg/loader"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

// ReviewFlatNode represents a single node in the flattened tree for display
type ReviewFlatNode struct {
	Issue      *model.Issue
	TreePrefix string // Visual tree prefix (├─, └─, │ )
	Depth      int
	IsLast     bool
	ParentPath []bool // Track which ancestors were last children
}

// ReviewDashboardModel is the main model for the review dashboard
type ReviewDashboardModel struct {
	// Tree data
	tree        *loader.ReviewTree
	flatNodes   []ReviewFlatNode

	// UI state
	cursor      int
	scroll      int
	width       int
	height      int
	theme       Theme

	// Review state
	reviewType  string // "plan", "implementation", "security"
	reviewer    string

	// Filtering
	showFilter  string // "all", "unreviewed", "needs_revision"

	// Focus state for split panel
	detailFocus  bool // true when detail panel has focus
	detailScroll int  // scroll offset for detail panel

	// Note input modal
	noteInput     NoteInputModel
	showNoteInput bool

	// Session tracking
	sessionStarted     time.Time
	itemsReviewed      int
	itemsApproved      int
	itemsNeedsRevision int
	itemsDeferred      int

	// Quit state
	showSummary bool
	quitting    bool

	// Assignee input
	showAssigneeInput bool
	assigneeInput     string

	// Search
	showSearch  bool
	searchQuery string

	// Help
	showHelp bool

	// Label filtering
	showLabelInput bool
	labelInput     string
	activeLabels   []string
}

// NewReviewDashboardModel creates a new review dashboard
func NewReviewDashboardModel(rootID string, issues []model.Issue, reviewer string, reviewType string, theme Theme) (*ReviewDashboardModel, error) {
	tree, err := loader.LoadReviewTree(rootID, issues)
	if err != nil {
		return nil, err
	}

	m := &ReviewDashboardModel{
		tree:           tree,
		reviewer:       reviewer,
		reviewType:     reviewType,
		theme:          theme,
		showFilter:     "all",
		sessionStarted: time.Now(),
	}

	m.rebuildFlatNodes()
	return m, nil
}

// rebuildFlatNodes flattens the tree into a list for display
func (m *ReviewDashboardModel) rebuildFlatNodes() {
	m.flatNodes = make([]ReviewFlatNode, 0)

	// Add root
	m.flatNodes = append(m.flatNodes, ReviewFlatNode{
		Issue:      m.tree.Root,
		TreePrefix: "",
		Depth:      0,
		IsLast:     true,
		ParentPath: []bool{},
	})

	// Build children map for traversal
	childrenMap := make(map[string][]*model.Issue)
	for _, desc := range m.tree.Descendants {
		for _, dep := range desc.Dependencies {
			if dep.Type == model.DepParentChild {
				childrenMap[dep.DependsOnID] = append(childrenMap[dep.DependsOnID], desc)
			}
		}
	}

	// DFS to flatten tree
	var flatten func(issue *model.Issue, depth int, parentPath []bool)
	flatten = func(issue *model.Issue, depth int, parentPath []bool) {
		children := childrenMap[issue.ID]
		for i, child := range children {
			isLast := i == len(children)-1
			newPath := append([]bool{}, parentPath...)
			newPath = append(newPath, isLast)

			// Build tree prefix
			prefix := ""
			for j, wasLast := range parentPath {
				if j == 0 {
					continue // Skip root level
				}
				if wasLast {
					prefix += "   "
				} else {
					prefix += "│  "
				}
			}
			if depth > 0 {
				if isLast {
					prefix += "└─ "
				} else {
					prefix += "├─ "
				}
			}

			// Apply filter
			if m.shouldShow(child) {
				m.flatNodes = append(m.flatNodes, ReviewFlatNode{
					Issue:      child,
					TreePrefix: prefix,
					Depth:      depth,
					IsLast:     isLast,
					ParentPath: newPath,
				})
			}

			flatten(child, depth+1, newPath)
		}
	}

	flatten(m.tree.Root, 1, []bool{true})
}

// shouldShow returns true if the issue passes the current filter
func (m *ReviewDashboardModel) shouldShow(issue *model.Issue) bool {
	// Check status filter
	switch m.showFilter {
	case "unreviewed":
		if issue.ReviewStatus != "" && issue.ReviewStatus != model.ReviewStatusUnreviewed {
			return false
		}
	case "needs_revision":
		if issue.ReviewStatus != model.ReviewStatusNeedsRevision {
			return false
		}
	}

	// Check search filter
	if m.searchQuery != "" {
		query := strings.ToLower(m.searchQuery)
		title := strings.ToLower(issue.Title)
		id := strings.ToLower(issue.ID)
		if !strings.Contains(title, query) && !strings.Contains(id, query) {
			return false
		}
	}

	// Check label filter (must have ALL active labels)
	if len(m.activeLabels) > 0 {
		for _, requiredLabel := range m.activeLabels {
			found := false
			for _, issueLabel := range issue.Labels {
				if strings.EqualFold(issueLabel, requiredLabel) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}

	return true
}

// filterBySearch rebuilds flat nodes with current search filter
func (m *ReviewDashboardModel) filterBySearch() {
	m.rebuildFlatNodes()
	m.cursor = 0
	m.scroll = 0
}

// Init implements tea.Model
func (m *ReviewDashboardModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model
func (m *ReviewDashboardModel) Update(msg tea.Msg) (*ReviewDashboardModel, tea.Cmd) {
	// Handle help overlay
	if m.showHelp {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			m.showHelp = false
			_ = msg
		}
		return m, nil
	}

	// Handle search input when active
	if m.showSearch {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc":
				m.showSearch = false
				m.searchQuery = ""
				m.rebuildFlatNodes()
				return m, nil
			case "enter":
				m.showSearch = false
				return m, nil
			case "backspace":
				if len(m.searchQuery) > 0 {
					m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
					m.filterBySearch()
				}
				return m, nil
			default:
				if len(msg.String()) == 1 && msg.String()[0] >= 32 && msg.String()[0] < 127 {
					m.searchQuery += msg.String()
					m.filterBySearch()
				}
				return m, nil
			}
		}
		return m, nil
	}

	// Handle label input when active
	if m.showLabelInput {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc":
				m.showLabelInput = false
				m.labelInput = ""
				return m, nil
			case "enter":
				// Add label to active labels
				if m.labelInput != "" {
					// Check if already exists
					exists := false
					for _, l := range m.activeLabels {
						if strings.EqualFold(l, m.labelInput) {
							exists = true
							break
						}
					}
					if !exists {
						m.activeLabels = append(m.activeLabels, m.labelInput)
						m.rebuildFlatNodes()
						m.cursor = 0
						m.scroll = 0
					}
				}
				m.showLabelInput = false
				m.labelInput = ""
				return m, nil
			case "backspace":
				if len(m.labelInput) > 0 {
					m.labelInput = m.labelInput[:len(m.labelInput)-1]
				}
				return m, nil
			default:
				if len(msg.String()) == 1 && msg.String()[0] >= 32 && msg.String()[0] < 127 {
					m.labelInput += msg.String()
				}
				return m, nil
			}
		}
		return m, nil
	}

	// Handle assignee input when active
	if m.showAssigneeInput {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc":
				m.showAssigneeInput = false
				m.assigneeInput = ""
				return m, nil
			case "enter":
				// Apply assignee to current issue
				if issue := m.SelectedIssue(); issue != nil {
					issue.Assignee = m.assigneeInput
				}
				m.showAssigneeInput = false
				m.assigneeInput = ""
				return m, nil
			case "backspace":
				if len(m.assigneeInput) > 0 {
					m.assigneeInput = m.assigneeInput[:len(m.assigneeInput)-1]
				}
				return m, nil
			default:
				// Add typed character (only printable)
				if len(msg.String()) == 1 && msg.String()[0] >= 32 && msg.String()[0] < 127 {
					m.assigneeInput += msg.String()
				}
				return m, nil
			}
		}
		return m, nil
	}

	// Handle note input modal when active
	if m.showNoteInput {
		var cmd tea.Cmd
		m.noteInput, cmd = m.noteInput.Update(msg)

		if m.noteInput.IsSubmitted() {
			// Apply note and status to current issue
			if issue := m.SelectedIssue(); issue != nil {
				note := m.noteInput.Notes()
				action := m.noteInput.Action()

				// Append note to existing notes
				if note != "" {
					if issue.Notes != "" {
						issue.Notes = issue.Notes + "\n\n---\n\n" + note
					} else {
						issue.Notes = note
					}
				}

				// Set review status based on action
				wasUnreviewed := issue.ReviewStatus == "" || issue.ReviewStatus == model.ReviewStatusUnreviewed
				switch action {
				case "revision":
					issue.ReviewStatus = model.ReviewStatusNeedsRevision
					issue.ReviewedBy = m.reviewer
					issue.ReviewedAt = time.Now()
					if wasUnreviewed {
						m.itemsReviewed++
						m.itemsNeedsRevision++
					}
				case "defer":
					issue.ReviewStatus = model.ReviewStatusDeferred
					issue.ReviewedBy = m.reviewer
					issue.ReviewedAt = time.Now()
					if wasUnreviewed {
						m.itemsReviewed++
						m.itemsDeferred++
					}
				// "note" action doesn't change status
				}
			}
			m.showNoteInput = false
			m.noteInput.Reset()
			return m, nil
		}

		if m.noteInput.IsCancelled() {
			m.showNoteInput = false
			m.noteInput.Reset()
			return m, nil
		}

		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if m.detailFocus {
				// Scroll detail panel down
				m.detailScroll++
			} else {
				if m.cursor < len(m.flatNodes)-1 {
					m.cursor++
					m.ensureVisible()
					m.detailScroll = 0 // Reset detail scroll on cursor change
				}
			}
		case "k", "up":
			if m.detailFocus {
				// Scroll detail panel up
				if m.detailScroll > 0 {
					m.detailScroll--
				}
			} else {
				if m.cursor > 0 {
					m.cursor--
					m.ensureVisible()
					m.detailScroll = 0 // Reset detail scroll on cursor change
				}
			}
		case "g", "home":
			m.cursor = 0
			m.scroll = 0
		case "G", "end":
			m.cursor = len(m.flatNodes) - 1
			m.ensureVisible()
		case "f":
			m.cycleFilter()
		case "tab":
			m.detailFocus = !m.detailFocus
		case "]":
			// Jump to next unreviewed
			m.jumpToNextUnreviewed()
		case "[":
			// Jump to previous unreviewed
			m.jumpToPrevUnreviewed()
		case "n":
			// Add note without changing status
			if issue := m.SelectedIssue(); issue != nil {
				m.noteInput = NewNoteInputModel(issue.Title, "note", issue.ID, m.theme)
				m.noteInput.SetSize(m.width, m.height)
				m.showNoteInput = true
				return m, m.noteInput.Init()
			}
		case "a":
			// Approve - sets status directly, no note required
			if issue := m.SelectedIssue(); issue != nil {
				// Only count if not already reviewed
				wasUnreviewed := issue.ReviewStatus == "" || issue.ReviewStatus == model.ReviewStatusUnreviewed
				issue.ReviewStatus = model.ReviewStatusApproved
				issue.ReviewedBy = m.reviewer
				issue.ReviewedAt = time.Now()
				if wasUnreviewed {
					m.itemsReviewed++
					m.itemsApproved++
				}
			}
		case "r":
			// Request revision - opens note modal
			if issue := m.SelectedIssue(); issue != nil {
				m.noteInput = NewNoteInputModel(issue.Title, "revision", issue.ID, m.theme)
				m.noteInput.SetSize(m.width, m.height)
				m.showNoteInput = true
				return m, m.noteInput.Init()
			}
		case "d":
			// Defer - opens note modal
			if issue := m.SelectedIssue(); issue != nil {
				m.noteInput = NewNoteInputModel(issue.Title, "defer", issue.ID, m.theme)
				m.noteInput.SetSize(m.width, m.height)
				m.showNoteInput = true
				return m, m.noteInput.Init()
			}
		case "?":
			m.showHelp = true
		case "/":
			m.showSearch = true
			m.searchQuery = ""
		case "l":
			m.showLabelInput = true
			m.labelInput = ""
		case "L":
			// Clear all label filters
			m.activeLabels = nil
			m.rebuildFlatNodes()
			m.cursor = 0
			m.scroll = 0
		case "A":
			// Assign - opens assignee input
			if issue := m.SelectedIssue(); issue != nil {
				m.assigneeInput = issue.Assignee // Pre-fill with current assignee
				m.showAssigneeInput = true
			}
		case "q":
			if m.showSummary {
				// Already showing summary, confirm quit
				m.quitting = true
				return m, tea.Quit
			}
			// Show summary first
			m.showSummary = true
		case "esc":
			if m.showSummary {
				// Cancel quit, go back to dashboard
				m.showSummary = false
			}
		}
	}
	return m, nil
}

// cycleFilter cycles through filter options
func (m *ReviewDashboardModel) cycleFilter() {
	switch m.showFilter {
	case "all":
		m.showFilter = "unreviewed"
	case "unreviewed":
		m.showFilter = "needs_revision"
	case "needs_revision":
		m.showFilter = "all"
	}
	m.rebuildFlatNodes()
	if m.cursor >= len(m.flatNodes) {
		m.cursor = len(m.flatNodes) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// jumpToNextUnreviewed moves cursor to the next unreviewed item
func (m *ReviewDashboardModel) jumpToNextUnreviewed() {
	startIdx := m.cursor + 1
	// Search from current position to end
	for i := startIdx; i < len(m.flatNodes); i++ {
		if m.isUnreviewed(m.flatNodes[i].Issue) {
			m.cursor = i
			m.ensureVisible()
			return
		}
	}
	// Wrap around to beginning
	for i := 0; i < startIdx && i < len(m.flatNodes); i++ {
		if m.isUnreviewed(m.flatNodes[i].Issue) {
			m.cursor = i
			m.ensureVisible()
			return
		}
	}
}

// jumpToPrevUnreviewed moves cursor to the previous unreviewed item
func (m *ReviewDashboardModel) jumpToPrevUnreviewed() {
	startIdx := m.cursor - 1
	// Search from current position to beginning
	for i := startIdx; i >= 0; i-- {
		if m.isUnreviewed(m.flatNodes[i].Issue) {
			m.cursor = i
			m.ensureVisible()
			return
		}
	}
	// Wrap around to end
	for i := len(m.flatNodes) - 1; i > startIdx && i >= 0; i-- {
		if m.isUnreviewed(m.flatNodes[i].Issue) {
			m.cursor = i
			m.ensureVisible()
			return
		}
	}
}

// isUnreviewed returns true if the issue is unreviewed
func (m *ReviewDashboardModel) isUnreviewed(issue *model.Issue) bool {
	return issue.ReviewStatus == "" || issue.ReviewStatus == model.ReviewStatusUnreviewed
}

// ensureVisible adjusts scroll to keep cursor visible
func (m *ReviewDashboardModel) ensureVisible() {
	// Calculate visible height based on layout
	// Split view: height - 6 (header, progress, separator, footer lines)
	// Single view: height - 6
	visibleHeight := m.height - 7
	if m.showSearch {
		visibleHeight-- // Search bar takes a line
	}
	if visibleHeight < 3 {
		visibleHeight = 3
	}

	// Keep cursor within visible area with 1 line margin
	if m.cursor < m.scroll {
		m.scroll = m.cursor
	} else if m.cursor >= m.scroll+visibleHeight {
		m.scroll = m.cursor - visibleHeight + 1
	}

	// Clamp scroll
	if m.scroll < 0 {
		m.scroll = 0
	}
	maxScroll := len(m.flatNodes) - visibleHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.scroll > maxScroll {
		m.scroll = maxScroll
	}
}

// View implements tea.Model
func (m *ReviewDashboardModel) View() string {
	// Show help overlay
	if m.showHelp {
		return m.renderHelp()
	}
	// Show session summary if quitting
	if m.showSummary {
		return m.renderSummary()
	}

	// Get base view first
	var base string
	if m.width >= 100 {
		base = m.renderSplitView()
	} else {
		base = m.renderBaseView()
	}

	// Show modals as centered overlays on top of base
	if m.showNoteInput {
		return m.renderModalOverlay(base, m.noteInput.View())
	}
	if m.showAssigneeInput {
		return m.renderModalOverlay(base, m.renderAssigneeInput())
	}
	if m.showLabelInput {
		return m.renderModalOverlay(base, m.renderLabelInput())
	}

	return base
}

// renderSummary renders the session summary screen
func (m *ReviewDashboardModel) renderSummary() string {
	var b strings.Builder
	duration := time.Since(m.sessionStarted).Round(time.Second)

	// Header
	headerStyle := m.theme.Renderer.NewStyle().Bold(true).Foreground(m.theme.Primary)
	b.WriteString(headerStyle.Render("Review Session Summary") + "\n")
	b.WriteString(strings.Repeat("─", 40) + "\n\n")

	// Session info
	infoStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext)
	b.WriteString(infoStyle.Render(fmt.Sprintf("Root:     %s", m.tree.Root.ID)) + "\n")
	b.WriteString(infoStyle.Render(fmt.Sprintf("Reviewer: %s", m.reviewer)) + "\n")
	b.WriteString(infoStyle.Render(fmt.Sprintf("Duration: %s", duration)) + "\n\n")

	// Stats
	statsHeaderStyle := m.theme.Renderer.NewStyle().Bold(true)
	b.WriteString(statsHeaderStyle.Render("Items Reviewed:") + "\n")

	approvedStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Open)
	revisionStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Blocked)
	deferredStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext)

	b.WriteString(fmt.Sprintf("  Total:          %d\n", m.itemsReviewed))
	b.WriteString(approvedStyle.Render(fmt.Sprintf("  ✓ Approved:     %d", m.itemsApproved)) + "\n")
	b.WriteString(revisionStyle.Render(fmt.Sprintf("  ! Needs Revision: %d", m.itemsNeedsRevision)) + "\n")
	b.WriteString(deferredStyle.Render(fmt.Sprintf("  ? Deferred:     %d", m.itemsDeferred)) + "\n\n")

	// Progress bar
	total := len(m.flatNodes)
	reviewed := 0
	for _, node := range m.flatNodes {
		if node.Issue.ReviewStatus != "" && node.Issue.ReviewStatus != model.ReviewStatusUnreviewed {
			reviewed++
		}
	}
	pct := 0
	if total > 0 {
		pct = (reviewed * 100) / total
	}
	b.WriteString(statsHeaderStyle.Render("Overall Progress:") + "\n")
	b.WriteString(fmt.Sprintf("  %d/%d items reviewed (%d%%)\n\n", reviewed, total, pct))

	// Hints
	hintStyle := m.theme.Renderer.NewStyle().Faint(true)
	b.WriteString(hintStyle.Render("[q] Quit  [Esc] Continue reviewing"))

	return b.String()
}

// renderHelp renders the help overlay
func (m *ReviewDashboardModel) renderHelp() string {
	width := 60
	if m.width < 70 {
		width = m.width - 10
	}

	titleStyle := m.theme.Renderer.NewStyle().Bold(true).Foreground(m.theme.Primary)
	sectionStyle := m.theme.Renderer.NewStyle().Bold(true).Foreground(m.theme.Secondary)
	keyStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Primary)
	descStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Review Dashboard Help") + "\n")
	b.WriteString(strings.Repeat("─", width-4) + "\n\n")

	// Navigation
	b.WriteString(sectionStyle.Render("Navigation") + "\n")
	b.WriteString(keyStyle.Render("  j/k, ↑/↓") + descStyle.Render("   Move cursor / scroll detail") + "\n")
	b.WriteString(keyStyle.Render("  g/G") + descStyle.Render("        Go to first/last item") + "\n")
	b.WriteString(keyStyle.Render("  [/]") + descStyle.Render("        Jump to prev/next unreviewed") + "\n")
	b.WriteString(keyStyle.Render("  Tab") + descStyle.Render("        Switch focus: tree ↔ detail") + "\n")
	b.WriteString(keyStyle.Render("  /") + descStyle.Render("          Search issues") + "\n\n")

	// Review Actions
	b.WriteString(sectionStyle.Render("Review Actions") + "\n")
	b.WriteString(keyStyle.Render("  a") + descStyle.Render("          Approve current item") + "\n")
	b.WriteString(keyStyle.Render("  r") + descStyle.Render("          Request revision (+ note)") + "\n")
	b.WriteString(keyStyle.Render("  d") + descStyle.Render("          Defer review (+ note)") + "\n")
	b.WriteString(keyStyle.Render("  n") + descStyle.Render("          Add note (no status change)") + "\n")
	b.WriteString(keyStyle.Render("  A") + descStyle.Render("          Assign to reviewer") + "\n\n")

	// Filters
	b.WriteString(sectionStyle.Render("Filters") + "\n")
	b.WriteString(keyStyle.Render("  f") + descStyle.Render("          Cycle: all → unreviewed → needs_revision") + "\n")
	b.WriteString(keyStyle.Render("  l") + descStyle.Render("          Add label filter") + "\n")
	b.WriteString(keyStyle.Render("  L") + descStyle.Render("          Clear all label filters") + "\n\n")

	// Other
	b.WriteString(sectionStyle.Render("Other") + "\n")
	b.WriteString(keyStyle.Render("  ?") + descStyle.Render("          Show this help") + "\n")
	b.WriteString(keyStyle.Render("  q") + descStyle.Render("          Show summary / quit") + "\n")
	b.WriteString(keyStyle.Render("  Esc") + descStyle.Render("        Close modal / cancel") + "\n\n")

	hintStyle := m.theme.Renderer.NewStyle().Faint(true)
	b.WriteString(hintStyle.Render("Press any key to close"))

	// Wrap in box
	boxStyle := m.theme.Renderer.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.Primary).
		Padding(1, 2).
		Width(width)

	// Center on screen
	content := boxStyle.Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

// renderModalOverlay renders a modal centered over the base view
func (m *ReviewDashboardModel) renderModalOverlay(base, modal string) string {
	// Use lipgloss.Place to center the modal
	modalWidth := lipgloss.Width(modal)
	modalHeight := lipgloss.Height(modal)

	// Create a semi-transparent background effect by dimming the base
	baseLines := strings.Split(base, "\n")

	// Calculate modal position
	startRow := (m.height - modalHeight) / 2
	startCol := (m.width - modalWidth) / 2
	if startRow < 0 {
		startRow = 0
	}
	if startCol < 0 {
		startCol = 0
	}

	modalLines := strings.Split(modal, "\n")

	// Overlay modal onto base
	for i, modalLine := range modalLines {
		row := startRow + i
		if row >= 0 && row < len(baseLines) {
			// Pad modal line placement
			newLine := strings.Repeat(" ", startCol) + modalLine
			baseLines[row] = newLine
		}
	}

	return strings.Join(baseLines, "\n")
}

// renderAssigneeInput renders the assignee input modal
func (m *ReviewDashboardModel) renderAssigneeInput() string {
	issue := m.SelectedIssue()
	issueID := ""
	if issue != nil {
		issueID = issue.ID
	}

	titleStyle := m.theme.Renderer.NewStyle().Bold(true).Foreground(m.theme.Primary)
	labelStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext)
	inputStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Primary)
	hintStyle := m.theme.Renderer.NewStyle().Faint(true)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Assign "+issueID) + "\n\n")
	b.WriteString(labelStyle.Render("Assignee:") + "\n")
	b.WriteString(inputStyle.Render(m.assigneeInput+"█") + "\n\n")
	b.WriteString(hintStyle.Render("[Enter] Save  [Esc] Cancel"))

	boxStyle := m.theme.Renderer.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.Primary).
		Padding(1, 3).
		Width(40)

	return boxStyle.Render(b.String())
}

// renderLabelInput renders the label input modal
func (m *ReviewDashboardModel) renderLabelInput() string {
	titleStyle := m.theme.Renderer.NewStyle().Bold(true).Foreground(m.theme.Primary)
	labelStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext)
	inputStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Primary)
	hintStyle := m.theme.Renderer.NewStyle().Faint(true)
	tagStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Secondary)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Add Label Filter") + "\n\n")

	// Show current labels
	if len(m.activeLabels) > 0 {
		b.WriteString(labelStyle.Render("Active: "))
		for i, l := range m.activeLabels {
			if i > 0 {
				b.WriteString(" ")
			}
			b.WriteString(tagStyle.Render("[" + l + "]"))
		}
		b.WriteString("\n\n")
	}

	b.WriteString(labelStyle.Render("Label:") + "\n")
	b.WriteString(inputStyle.Render(m.labelInput+"█") + "\n\n")
	b.WriteString(hintStyle.Render("[Enter] Add  [Esc] Cancel  [L] Clear all"))

	boxStyle := m.theme.Renderer.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.Primary).
		Padding(1, 3).
		Width(45)

	return boxStyle.Render(b.String())
}

// renderDetailPanel renders the right-side detail panel for the selected issue
func (m *ReviewDashboardModel) renderDetailPanel() string {
	var b strings.Builder

	issue := m.SelectedIssue()
	if issue == nil {
		return "No issue selected"
	}

	// Header: ID and Title
	headerStyle := m.theme.Renderer.NewStyle().Bold(true).Foreground(m.theme.Primary)
	b.WriteString(headerStyle.Render(issue.ID+": "+issue.Title) + "\n")
	b.WriteString(strings.Repeat("─", 40) + "\n")

	// Status line
	statusStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext)
	statusLine := fmt.Sprintf("Status: %s | Priority: %d | Type: %s",
		issue.Status, issue.Priority, issue.IssueType)
	b.WriteString(statusStyle.Render(statusLine) + "\n")

	// Review status
	reviewStatus := issue.ReviewStatus
	if reviewStatus == "" {
		reviewStatus = "UNREVIEWED"
	} else {
		reviewStatus = strings.ToUpper(reviewStatus)
	}

	reviewLine := fmt.Sprintf("Review: %s", reviewStatus)
	if issue.ReviewedBy != "" {
		reviewLine += fmt.Sprintf(" by %s", issue.ReviewedBy)
	}
	if !issue.ReviewedAt.IsZero() {
		reviewLine += fmt.Sprintf(" @ %s", issue.ReviewedAt.Format("01/02 15:04"))
	}

	var reviewStyle lipgloss.Style
	switch issue.ReviewStatus {
	case model.ReviewStatusApproved:
		reviewStyle = m.theme.Renderer.NewStyle().Foreground(m.theme.Open).Bold(true)
	case model.ReviewStatusNeedsRevision:
		reviewStyle = m.theme.Renderer.NewStyle().Foreground(m.theme.Blocked).Bold(true)
	case model.ReviewStatusDeferred:
		reviewStyle = m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext).Bold(true)
	default:
		reviewStyle = m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext).Bold(true)
	}
	b.WriteString(reviewStyle.Render(reviewLine) + "\n\n")

	// Description section
	if issue.Description != "" {
		sectionStyle := m.theme.Renderer.NewStyle().Bold(true)
		b.WriteString(sectionStyle.Render("## Description") + "\n")
		b.WriteString(issue.Description + "\n\n")
	}

	// Design section
	if issue.Design != "" {
		sectionStyle := m.theme.Renderer.NewStyle().Bold(true)
		b.WriteString(sectionStyle.Render("## Design") + "\n")
		b.WriteString(issue.Design + "\n\n")
	}

	// Acceptance Criteria section
	if issue.AcceptanceCriteria != "" {
		sectionStyle := m.theme.Renderer.NewStyle().Bold(true)
		b.WriteString(sectionStyle.Render("## Acceptance Criteria") + "\n")
		b.WriteString(issue.AcceptanceCriteria + "\n\n")
	}

	// Notes section
	if issue.Notes != "" {
		sectionStyle := m.theme.Renderer.NewStyle().Bold(true)
		b.WriteString(sectionStyle.Render("## Notes") + "\n")
		b.WriteString(issue.Notes + "\n\n")
	}

	return b.String()
}

// renderSplitView renders a split-panel layout with tree on left and detail on right
func (m *ReviewDashboardModel) renderSplitView() string {
	if m.width < 80 {
		return m.renderBaseView()
	}

	// Calculate dimensions
	leftWidth := (m.width * 45) / 100  // 45% for tree
	rightWidth := m.width - leftWidth - 1 // Rest for detail, 1 for divider
	headerLines := 3 // Title + progress + separator
	footerLines := 2 // Separator + keybinds
	searchLines := 0
	if m.showSearch {
		searchLines = 1
	}
	contentHeight := m.height - headerLines - footerLines - searchLines

	if contentHeight < 5 {
		contentHeight = 5
	}

	var output strings.Builder

	// ══════════════════════════════════════════════════════════════════
	// HEADER
	// ══════════════════════════════════════════════════════════════════
	titleStyle := m.theme.Renderer.NewStyle().Bold(true).Foreground(m.theme.Primary)

	// Truncate title if needed
	title := m.tree.Root.Title
	maxTitleLen := m.width - 30
	if len(title) > maxTitleLen {
		title = title[:maxTitleLen-3] + "..."
	}
	output.WriteString(titleStyle.Render("◆ " + title) + "\n")

	// Progress bar and stats
	total := len(m.flatNodes)
	reviewed := 0
	for _, node := range m.flatNodes {
		if node.Issue.ReviewStatus != "" && node.Issue.ReviewStatus != model.ReviewStatusUnreviewed {
			reviewed++
		}
	}
	pct := 0
	if total > 0 {
		pct = (reviewed * 100) / total
	}

	// Visual progress bar
	barWidth := 20
	filled := (pct * barWidth) / 100
	progressBar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	progressStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Open)
	statsStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext)

	output.WriteString(progressStyle.Render(progressBar) + " ")
	output.WriteString(statsStyle.Render(fmt.Sprintf("%d/%d (%d%%)", reviewed, total, pct)))

	// Filter indicator
	if m.showFilter != "all" {
		filterStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Secondary)
		output.WriteString(filterStyle.Render("  ◇ " + m.showFilter))
	}

	// Active labels
	if len(m.activeLabels) > 0 {
		tagStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Secondary)
		output.WriteString("  ")
		for _, l := range m.activeLabels {
			output.WriteString(tagStyle.Render("⬡"+l) + " ")
		}
	}
	output.WriteString("\n")

	// Separator
	sepStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Border)
	output.WriteString(sepStyle.Render(strings.Repeat("─", m.width)) + "\n")

	// ══════════════════════════════════════════════════════════════════
	// SEARCH BAR (if active)
	// ══════════════════════════════════════════════════════════════════
	if m.showSearch {
		searchStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Primary)
		queryStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Secondary)
		output.WriteString(searchStyle.Render(" / ") + queryStyle.Render(m.searchQuery+"█") + "\n")
	}

	// ══════════════════════════════════════════════════════════════════
	// CONTENT PANELS
	// ══════════════════════════════════════════════════════════════════
	leftPanel := m.renderTreePanelFixed(leftWidth-1, contentHeight)
	rightPanel := m.renderDetailPanelFixed(rightWidth-1, contentHeight)

	// Build side-by-side view
	leftLines := strings.Split(leftPanel, "\n")
	rightLines := strings.Split(rightPanel, "\n")

	dividerChar := "│"
	dividerStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Border)
	if m.detailFocus {
		dividerStyle = m.theme.Renderer.NewStyle().Foreground(m.theme.Primary)
	}

	for i := 0; i < contentHeight; i++ {
		leftLine := ""
		if i < len(leftLines) {
			leftLine = leftLines[i]
		}
		rightLine := ""
		if i < len(rightLines) {
			rightLine = rightLines[i]
		}

		// Pad left panel
		leftVisible := lipgloss.Width(leftLine)
		if leftVisible < leftWidth-1 {
			leftLine += strings.Repeat(" ", leftWidth-1-leftVisible)
		}

		output.WriteString(leftLine)
		output.WriteString(dividerStyle.Render(dividerChar))
		output.WriteString(rightLine)
		output.WriteString("\n")
	}

	// ══════════════════════════════════════════════════════════════════
	// FOOTER
	// ══════════════════════════════════════════════════════════════════
	output.WriteString(sepStyle.Render(strings.Repeat("─", m.width)) + "\n")

	// Keybinds - elegant and concise
	keyStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Primary)
	hintStyle := m.theme.Renderer.NewStyle().Faint(true)
	focusStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Secondary)

	focusIndicator := "tree"
	if m.detailFocus {
		focusIndicator = "detail"
	}

	output.WriteString(focusStyle.Render("◆"+focusIndicator) + " ")
	output.WriteString(keyStyle.Render("j/k") + hintStyle.Render(" nav "))
	output.WriteString(keyStyle.Render("[/]") + hintStyle.Render(" jump "))
	output.WriteString(keyStyle.Render("a") + hintStyle.Render("pprove "))
	output.WriteString(keyStyle.Render("r") + hintStyle.Render("evise "))
	output.WriteString(keyStyle.Render("d") + hintStyle.Render("efer "))
	output.WriteString(keyStyle.Render("/") + hintStyle.Render("search "))
	output.WriteString(keyStyle.Render("l") + hintStyle.Render("abel "))
	output.WriteString(keyStyle.Render("?") + hintStyle.Render("help "))
	output.WriteString(keyStyle.Render("q") + hintStyle.Render("uit"))

	return output.String()
}

// renderSplitViewOld is the old implementation for reference
func (m *ReviewDashboardModel) renderSplitViewOld() string {
	// Calculate panel widths
	leftWidth := (m.width - 3) / 2  // -3 for divider and padding
	rightWidth := m.width - leftWidth - 3
	contentHeight := m.height - 6 // Header, progress, separator, footer

	if contentHeight < 5 {
		contentHeight = 5
	}

	// Header
	var header strings.Builder
	headerStyle := m.theme.Renderer.NewStyle().Bold(true).Foreground(m.theme.Primary)
	header.WriteString(headerStyle.Render("Review: "+m.tree.Root.Title) + "\n")

	// Progress indicator
	total := len(m.flatNodes)
	reviewed := 0
	for _, node := range m.flatNodes {
		if node.Issue.ReviewStatus != "" && node.Issue.ReviewStatus != model.ReviewStatusUnreviewed {
			reviewed++
		}
	}
	progressStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext)
	header.WriteString(progressStyle.Render(fmt.Sprintf("[%d/%d reviewed]", reviewed, total)) + "\n")
	header.WriteString(strings.Repeat("─", m.width))

	// Left panel: tree list
	leftPanel := m.renderTreePanelFixed(leftWidth, contentHeight)

	// Right panel: detail view
	rightPanel := m.renderDetailPanelFixed(rightWidth, contentHeight)

	// Style the panels
	leftStyle := m.theme.Renderer.NewStyle().
		Width(leftWidth).
		Height(contentHeight)

	rightStyle := m.theme.Renderer.NewStyle().
		Width(rightWidth).
		Height(contentHeight)

	// Divider
	dividerColor := m.theme.Border
	if m.detailFocus {
		dividerColor = m.theme.Primary
	}
	divider := m.theme.Renderer.NewStyle().
		Foreground(dividerColor).
		Render(strings.Repeat("│\n", contentHeight))

	// Join panels horizontally
	content := lipgloss.JoinHorizontal(
		lipgloss.Top,
		leftStyle.Render(leftPanel),
		divider,
		rightStyle.Render(rightPanel),
	)

	// Footer
	var footer strings.Builder
	footer.WriteString(strings.Repeat("─", m.width) + "\n")
	filterStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext)
	footer.WriteString(filterStyle.Render(fmt.Sprintf("Filter: [%s]", m.showFilter)) + "  ")
	hintStyle := m.theme.Renderer.NewStyle().Faint(true)
	focusHint := "tree"
	if m.detailFocus {
		focusHint = "detail"
	}
	footer.WriteString(hintStyle.Render(fmt.Sprintf("[Tab:%s] [j/k] []/[] [n] [a] [r] [d] [A] [q]", focusHint)))

	return header.String() + "\n" + content + "\n" + footer.String()
}

// renderTreePanelFixed renders tree panel with fixed dimensions
func (m *ReviewDashboardModel) renderTreePanelFixed(width, height int) string {
	var lines []string

	endIdx := m.scroll + height
	if endIdx > len(m.flatNodes) {
		endIdx = len(m.flatNodes)
	}

	for i := m.scroll; i < endIdx; i++ {
		node := m.flatNodes[i]
		var line strings.Builder

		// Cursor indicator
		if i == m.cursor {
			cursorStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Primary)
			line.WriteString(cursorStyle.Render("▸ "))
		} else {
			line.WriteString("  ")
		}

		// Review status indicator
		var statusIndicator string
		var statusStyle lipgloss.Style
		switch node.Issue.ReviewStatus {
		case model.ReviewStatusApproved:
			statusStyle = m.theme.Renderer.NewStyle().Foreground(m.theme.Open)
			statusIndicator = "✓"
		case model.ReviewStatusNeedsRevision:
			statusStyle = m.theme.Renderer.NewStyle().Foreground(m.theme.Blocked)
			statusIndicator = "!"
		case model.ReviewStatusDeferred:
			statusStyle = m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext)
			statusIndicator = "?"
		default:
			statusStyle = m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext).Faint(true)
			statusIndicator = "○"
		}
		line.WriteString(statusStyle.Render(statusIndicator) + " ")

		// Tree prefix (indentation)
		if node.TreePrefix != "" {
			prefixStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Border)
			line.WriteString(prefixStyle.Render(node.TreePrefix))
		}

		// ID (abbreviated)
		idStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Secondary)
		if i == m.cursor {
			idStyle = idStyle.Bold(true)
		}
		line.WriteString(idStyle.Render(node.Issue.ID) + " ")

		// Title - truncate to fit
		titleStyle := m.theme.Renderer.NewStyle()
		if i == m.cursor {
			titleStyle = titleStyle.Foreground(m.theme.Primary)
		} else {
			titleStyle = titleStyle.Foreground(m.theme.Subtext)
		}

		// Calculate remaining width for title
		currentWidth := lipgloss.Width(line.String())
		titleWidth := width - currentWidth - 1
		if titleWidth < 5 {
			titleWidth = 5
		}

		title := node.Issue.Title
		if len(title) > titleWidth {
			title = title[:titleWidth-1] + "…"
		}
		line.WriteString(titleStyle.Render(title))

		lines = append(lines, line.String())
	}

	// Pad to height
	for len(lines) < height {
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}

// renderDetailPanelFixed renders detail panel with fixed dimensions and scroll support
func (m *ReviewDashboardModel) renderDetailPanelFixed(width, height int) string {
	issue := m.SelectedIssue()
	if issue == nil {
		return "No issue selected"
	}

	var lines []string

	// Header
	headerStyle := m.theme.Renderer.NewStyle().Bold(true).Foreground(m.theme.Primary)
	lines = append(lines, headerStyle.Render(issue.ID))
	lines = append(lines, strings.Repeat("─", width-2))

	// Title (may wrap)
	titleLines := wrapTextLines(issue.Title, width-2)
	lines = append(lines, titleLines...)
	lines = append(lines, "")

	// Status line
	statusStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext)
	statusLine := fmt.Sprintf("Status: %s | Type: %s | P%d", issue.Status, issue.IssueType, issue.Priority)
	lines = append(lines, statusStyle.Render(statusLine))

	// Assignee
	if issue.Assignee != "" {
		lines = append(lines, statusStyle.Render("Assignee: "+issue.Assignee))
	}

	// Review status
	reviewStatus := issue.ReviewStatus
	if reviewStatus == "" {
		reviewStatus = "UNREVIEWED"
	}
	var reviewStyle lipgloss.Style
	switch issue.ReviewStatus {
	case model.ReviewStatusApproved:
		reviewStyle = m.theme.Renderer.NewStyle().Foreground(m.theme.Open).Bold(true)
	case model.ReviewStatusNeedsRevision:
		reviewStyle = m.theme.Renderer.NewStyle().Foreground(m.theme.Blocked).Bold(true)
	default:
		reviewStyle = m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext)
	}
	lines = append(lines, reviewStyle.Render("Review: "+strings.ToUpper(reviewStatus)))
	lines = append(lines, "")

	// Description
	if issue.Description != "" {
		sectionStyle := m.theme.Renderer.NewStyle().Bold(true)
		lines = append(lines, sectionStyle.Render("Description:"))
		descLines := wrapTextLines(issue.Description, width-2)
		lines = append(lines, descLines...)
		lines = append(lines, "")
	}

	// Design
	if issue.Design != "" {
		sectionStyle := m.theme.Renderer.NewStyle().Bold(true)
		lines = append(lines, sectionStyle.Render("Design:"))
		designLines := wrapTextLines(issue.Design, width-2)
		lines = append(lines, designLines...)
		lines = append(lines, "")
	}

	// Acceptance Criteria
	if issue.AcceptanceCriteria != "" {
		sectionStyle := m.theme.Renderer.NewStyle().Bold(true)
		lines = append(lines, sectionStyle.Render("Acceptance:"))
		acLines := wrapTextLines(issue.AcceptanceCriteria, width-2)
		lines = append(lines, acLines...)
		lines = append(lines, "")
	}

	// Notes
	if issue.Notes != "" {
		sectionStyle := m.theme.Renderer.NewStyle().Bold(true)
		lines = append(lines, sectionStyle.Render("Notes:"))
		noteLines := wrapTextLines(issue.Notes, width-2)
		lines = append(lines, noteLines...)
	}

	// Apply scroll
	if m.detailScroll >= len(lines) {
		m.detailScroll = len(lines) - 1
	}
	if m.detailScroll < 0 {
		m.detailScroll = 0
	}

	startIdx := m.detailScroll
	endIdx := startIdx + height
	if endIdx > len(lines) {
		endIdx = len(lines)
	}

	visibleLines := lines[startIdx:endIdx]

	// Pad to height
	for len(visibleLines) < height {
		visibleLines = append(visibleLines, "")
	}

	return strings.Join(visibleLines, "\n")
}

// wrapTextLines wraps text to fit within width, returning slice of lines
func wrapTextLines(text string, width int) []string {
	if width <= 0 {
		width = 40
	}
	var lines []string
	// Split by newlines first
	paragraphs := strings.Split(text, "\n")
	for _, para := range paragraphs {
		if para == "" {
			lines = append(lines, "")
			continue
		}
		// Simple word wrap
		words := strings.Fields(para)
		var currentLine string
		for _, word := range words {
			if currentLine == "" {
				currentLine = word
			} else if len(currentLine)+1+len(word) <= width {
				currentLine += " " + word
			} else {
				lines = append(lines, currentLine)
				currentLine = word
			}
		}
		if currentLine != "" {
			lines = append(lines, currentLine)
		}
	}
	return lines
}

// renderTreePanel renders just the tree portion without header/footer (legacy)
func (m *ReviewDashboardModel) renderTreePanel() string {
	var b strings.Builder

	visibleHeight := m.height - 8
	if visibleHeight < 1 {
		visibleHeight = 10
	}

	endIdx := m.scroll + visibleHeight
	if endIdx > len(m.flatNodes) {
		endIdx = len(m.flatNodes)
	}

	for i := m.scroll; i < endIdx; i++ {
		node := m.flatNodes[i]
		var line strings.Builder

		// Cursor indicator
		if i == m.cursor {
			cursorStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Primary)
			line.WriteString(cursorStyle.Render("▸ "))
		} else {
			line.WriteString("  ")
		}

		// Review status indicator
		var statusIndicator string
		var statusStyle lipgloss.Style
		switch node.Issue.ReviewStatus {
		case model.ReviewStatusApproved:
			statusStyle = m.theme.Renderer.NewStyle().Foreground(m.theme.Open)
			statusIndicator = "[✓]"
		case model.ReviewStatusNeedsRevision:
			statusStyle = m.theme.Renderer.NewStyle().Foreground(m.theme.Blocked)
			statusIndicator = "[!]"
		case model.ReviewStatusDeferred:
			statusStyle = m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext)
			statusIndicator = "[?]"
		default:
			statusStyle = m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext).Faint(true)
			statusIndicator = "[ ]"
		}
		line.WriteString(statusStyle.Render(statusIndicator) + " ")

		// Tree prefix
		if node.TreePrefix != "" {
			prefixStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext)
			line.WriteString(prefixStyle.Render(node.TreePrefix))
		}

		// ID and title
		idStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Secondary)
		if i == m.cursor {
			idStyle = idStyle.Bold(true)
		}
		line.WriteString(idStyle.Render(node.Issue.ID))

		b.WriteString(line.String() + "\n")
	}

	return b.String()
}

// truncateOrPad truncates or pads a string to exact width
func truncateOrPad(s string, width int) string {
	// Remove ANSI codes for length calculation
	visibleLen := len(stripAnsi(s))

	if visibleLen > width {
		// Truncate (this is simplified - proper truncation needs ANSI awareness)
		return s[:width]
	}

	return s + strings.Repeat(" ", width-visibleLen)
}

// stripAnsi removes ANSI escape codes for length calculation
func stripAnsi(s string) string {
	// Simple regex-free approach for basic ANSI stripping
	result := make([]byte, 0, len(s))
	inEscape := false
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (s[i] >= 'a' && s[i] <= 'z') || (s[i] >= 'A' && s[i] <= 'Z') {
				inEscape = false
			}
			continue
		}
		result = append(result, s[i])
	}
	return string(result)
}

// renderWithOverlay renders the base view with a modal overlay centered on top
func (m *ReviewDashboardModel) renderWithOverlay(overlay string) string {
	// Get the base view first
	base := m.renderBaseView()
	baseLines := strings.Split(base, "\n")

	// Get overlay dimensions
	overlayLines := strings.Split(overlay, "\n")
	overlayHeight := len(overlayLines)
	overlayWidth := 0
	for _, line := range overlayLines {
		if len(stripAnsi(line)) > overlayWidth {
			overlayWidth = len(stripAnsi(line))
		}
	}

	// Calculate center position
	startRow := (m.height - overlayHeight) / 2
	startCol := (m.width - overlayWidth) / 2
	if startRow < 0 {
		startRow = 0
	}
	if startCol < 0 {
		startCol = 0
	}

	// Overlay the modal onto the base
	for i, overlayLine := range overlayLines {
		baseRowIdx := startRow + i
		if baseRowIdx < len(baseLines) {
			baseLine := baseLines[baseRowIdx]
			// Simple overlay: replace the center portion
			if startCol > 0 && startCol < len(baseLine) {
				prefix := ""
				if startCol <= len(baseLine) {
					prefix = baseLine[:startCol]
				}
				baseLines[baseRowIdx] = prefix + overlayLine
			} else {
				baseLines[baseRowIdx] = strings.Repeat(" ", startCol) + overlayLine
			}
		}
	}

	return strings.Join(baseLines, "\n")
}

// renderBaseView renders the main dashboard view without overlay handling
func (m *ReviewDashboardModel) renderBaseView() string {
	var b strings.Builder

	// Header with title
	headerStyle := m.theme.Renderer.NewStyle().Bold(true).Foreground(m.theme.Primary)
	b.WriteString(headerStyle.Render("Review: "+m.tree.Root.Title) + "\n")

	// Progress indicator
	total := len(m.flatNodes)
	reviewed := 0
	for _, node := range m.flatNodes {
		if node.Issue.ReviewStatus != "" && node.Issue.ReviewStatus != model.ReviewStatusUnreviewed {
			reviewed++
		}
	}
	progressStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext)
	b.WriteString(progressStyle.Render(fmt.Sprintf("[%d/%d reviewed]", reviewed, total)) + "\n\n")

	// Tree
	visibleHeight := m.height - 6
	if visibleHeight < 1 {
		visibleHeight = 10
	}

	endIdx := m.scroll + visibleHeight
	if endIdx > len(m.flatNodes) {
		endIdx = len(m.flatNodes)
	}

	for i := m.scroll; i < endIdx; i++ {
		node := m.flatNodes[i]
		var line strings.Builder

		// Cursor indicator
		if i == m.cursor {
			cursorStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Primary)
			line.WriteString(cursorStyle.Render("▸ "))
		} else {
			line.WriteString("  ")
		}

		// Review status indicator with color
		var statusIndicator string
		var statusStyle lipgloss.Style
		switch node.Issue.ReviewStatus {
		case model.ReviewStatusApproved:
			statusStyle = m.theme.Renderer.NewStyle().Foreground(m.theme.Open)
			statusIndicator = "[✓]"
		case model.ReviewStatusNeedsRevision:
			statusStyle = m.theme.Renderer.NewStyle().Foreground(m.theme.Blocked)
			statusIndicator = "[!]"
		case model.ReviewStatusDeferred:
			statusStyle = m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext)
			statusIndicator = "[?]"
		default:
			statusStyle = m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext).Faint(true)
			statusIndicator = "[ ]"
		}
		line.WriteString(statusStyle.Render(statusIndicator) + " ")

		// Tree prefix in subtext color
		if node.TreePrefix != "" {
			prefixStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext)
			line.WriteString(prefixStyle.Render(node.TreePrefix))
		}

		// ID and title
		idStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Secondary)
		if i == m.cursor {
			idStyle = idStyle.Bold(true)
		}
		line.WriteString(idStyle.Render(node.Issue.ID) + " ")

		titleStyle := m.theme.Renderer.NewStyle()
		if i == m.cursor {
			titleStyle = titleStyle.Foreground(m.theme.Primary)
		}
		line.WriteString(titleStyle.Render(node.Issue.Title))

		b.WriteString(line.String() + "\n")
	}

	// Blockers section
	if len(m.tree.Blockers) > 0 {
		b.WriteString("\n")
		blockerHeaderStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Blocked).Bold(true)
		b.WriteString(blockerHeaderStyle.Render("BLOCKERS (external)") + "\n")
		for _, blocker := range m.tree.Blockers {
			blockerStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Blocked)
			b.WriteString(blockerStyle.Render("  └─ "+blocker.ID+" "+blocker.Title) + "\n")
		}
	}

	// Footer with filter and hints
	b.WriteString("\n")
	filterStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Subtext)
	b.WriteString(filterStyle.Render(fmt.Sprintf("Filter: [%s]", m.showFilter)) + "  ")
	hintStyle := m.theme.Renderer.NewStyle().Faint(true)
	b.WriteString(hintStyle.Render("[j/k] navigate  []/[] jump  [n]ote  [a]pprove  [r]evise  [d]efer  [A]ssign  [?/q]"))

	return b.String()
}

// SetSize sets the terminal dimensions
func (m *ReviewDashboardModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SelectedIssue returns the currently selected issue
func (m *ReviewDashboardModel) SelectedIssue() *model.Issue {
	if m.cursor >= 0 && m.cursor < len(m.flatNodes) {
		return m.flatNodes[m.cursor].Issue
	}
	return nil
}

// Tree returns the underlying review tree
func (m *ReviewDashboardModel) Tree() *loader.ReviewTree {
	return m.tree
}

// SessionStats returns the current session statistics
func (m *ReviewDashboardModel) SessionStats() (started time.Time, reviewed, approved, needsRevision, deferred int) {
	return m.sessionStarted, m.itemsReviewed, m.itemsApproved, m.itemsNeedsRevision, m.itemsDeferred
}

// ReviewProgram wraps ReviewDashboardModel to implement tea.Model for standalone use
type ReviewProgram struct {
	dashboard *ReviewDashboardModel
}

// NewReviewProgram creates a new review program wrapper
func NewReviewProgram(dashboard *ReviewDashboardModel) *ReviewProgram {
	return &ReviewProgram{dashboard: dashboard}
}

// Init implements tea.Model
func (p *ReviewProgram) Init() tea.Cmd {
	return p.dashboard.Init()
}

// Update implements tea.Model
func (p *ReviewProgram) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.dashboard.SetSize(msg.Width, msg.Height)
		return p, nil
	}

	var cmd tea.Cmd
	p.dashboard, cmd = p.dashboard.Update(msg)
	return p, cmd
}

// View implements tea.Model
func (p *ReviewProgram) View() string {
	return p.dashboard.View()
}

package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/ansi"
	reflowtrunc "github.com/muesli/reflow/truncate"
	"github.com/Dicklesworthstone/beads_viewer/pkg/loader"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/Dicklesworthstone/beads_viewer/pkg/review"
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
	saveOnQuit  bool

	// Copy feedback for prompt
	promptCopied   bool
	promptCopiedAt time.Time

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

	// Review persistence
	collector     *review.ReviewActionCollector
	workspaceRoot string
}

// NewReviewDashboardModel creates a new review dashboard
func NewReviewDashboardModel(rootID string, issues []model.Issue, reviewer string, reviewType string, theme Theme, workspaceRoot string) (*ReviewDashboardModel, error) {
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
		collector:      review.NewReviewActionCollector(reviewer, reviewType),
		workspaceRoot:  workspaceRoot,
	}

	m.rebuildFlatNodes()
	m.loadReviewStateFromComments()
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
	// Handle summary screen
	if m.showSummary {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "q":
				// Save and quit
				m.saveOnQuit = true
				m.quitting = true
				return m, tea.Quit
			case "Q":
				// Discard and quit (don't save)
				m.quitting = true
				return m, tea.Quit
			case "esc":
				m.showSummary = false
			case "p":
				// Copy simple summary to clipboard
				prompt := m.generateSimplePrompt()
				if err := clipboard.WriteAll(prompt); err == nil {
					m.promptCopied = true
					m.promptCopiedAt = time.Now()
				}
			case "P":
				// Copy full review prompt with instructions
				prompt := m.generateFullPrompt()
				if err := clipboard.WriteAll(prompt); err == nil {
					m.promptCopied = true
					m.promptCopiedAt = time.Now()
				}
			}
		}
		return m, nil
	}

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
				if IsPrintableKey(msg.String()) {
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
				} else if len(m.activeLabels) > 0 {
					// Remove last label when input is empty
					m.activeLabels = m.activeLabels[:len(m.activeLabels)-1]
					m.rebuildFlatNodes()
					m.cursor = 0
					m.scroll = 0
				}
				return m, nil
			default:
				if IsPrintableKey(msg.String()) {
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
				if IsPrintableKey(msg.String()) {
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
					// Record for persistence
					m.collector.Record(issue.ID, model.ReviewStatusNeedsRevision, note)
				case "defer":
					issue.ReviewStatus = model.ReviewStatusDeferred
					issue.ReviewedBy = m.reviewer
					issue.ReviewedAt = time.Now()
					if wasUnreviewed {
						m.itemsReviewed++
						m.itemsDeferred++
					}
					// Record for persistence
					m.collector.Record(issue.ID, model.ReviewStatusDeferred, note)
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
				// Record for persistence
				m.collector.Record(issue.ID, model.ReviewStatusApproved, "")
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
		case "s":
			m.showLabelInput = true
			m.labelInput = ""
		case "S":
			// Clear all scope filters
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
		case "q", "esc":
			// Only show summary if there are pending review actions
			if m.collector.Count() > 0 {
				m.showSummary = true
			} else {
				// No changes - quit directly
				m.quitting = true
				return m, tea.Quit
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
	if m.width >= BreakpointMedium {
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
	t := m.theme
	var b strings.Builder
	duration := time.Since(m.sessionStarted).Round(time.Second)

	// Header
	headerStyle := t.Renderer.NewStyle().Bold(true).Foreground(t.Primary)
	b.WriteString(headerStyle.Render("Review Session Summary") + "\n")
	b.WriteString(strings.Repeat("─", 40) + "\n\n")

	// Session info
	infoStyle := t.Renderer.NewStyle().Foreground(t.Subtext)
	b.WriteString(infoStyle.Render(fmt.Sprintf("Root:     %s", m.tree.Root.ID)) + "\n")
	b.WriteString(infoStyle.Render(fmt.Sprintf("Reviewer: %s", m.reviewer)) + "\n")
	b.WriteString(infoStyle.Render(fmt.Sprintf("Duration: %s", duration)) + "\n\n")

	// Stats
	statsHeaderStyle := t.Renderer.NewStyle().Bold(true)
	b.WriteString(statsHeaderStyle.Render("Items Reviewed:") + "\n")

	approvedStyle := t.Renderer.NewStyle().Foreground(t.Open)
	revisionStyle := t.Renderer.NewStyle().Foreground(t.Blocked)
	deferredStyle := t.Renderer.NewStyle().Foreground(t.Subtext)

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

	// Copy feedback
	if m.promptCopied && time.Since(m.promptCopiedAt) < 2*time.Second {
		copiedStyle := t.Renderer.NewStyle().Foreground(t.Open).Bold(true)
		b.WriteString(copiedStyle.Render("✓ Copied to clipboard!") + "\n\n")
	}

	// Hints
	hintStyle := t.Renderer.NewStyle().Faint(true)
	keyStyle := t.Renderer.NewStyle().Foreground(t.Primary)
	b.WriteString(keyStyle.Render("q") + hintStyle.Render(" save & quit  "))
	b.WriteString(keyStyle.Render("Q") + hintStyle.Render(" discard & quit\n"))
	b.WriteString(keyStyle.Render("p") + hintStyle.Render(" copy ID list  "))
	b.WriteString(keyStyle.Render("P") + hintStyle.Render(" copy AI prompt\n"))
	b.WriteString(keyStyle.Render("Esc") + hintStyle.Render(" continue reviewing"))

	// Wrap in centered box (same style as LabelTreeReviewModel)
	boxStyle := t.Renderer.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Primary).
		Padding(1, 2).
		Width(55)

	content := boxStyle.Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
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
	b.WriteString(keyStyle.Render("  s") + descStyle.Render("          Add scope filter") + "\n")
	b.WriteString(keyStyle.Render("  S") + descStyle.Render("          Clear all scope filters") + "\n\n")

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

// renderModalOverlay renders a modal centered over the base view, preserving the background
func (m *ReviewDashboardModel) renderModalOverlay(base, modal string) string {
	modalWidth := lipgloss.Width(modal)
	modalHeight := lipgloss.Height(modal)

	baseLines := strings.Split(base, "\n")
	modalLines := strings.Split(modal, "\n")

	// Calculate centered position
	startRow := (m.height - modalHeight) / 2
	startCol := (m.width - modalWidth) / 2
	if startRow < 0 {
		startRow = 0
	}
	if startCol < 0 {
		startCol = 0
	}

	// Overlay modal onto base, preserving left and right portions
	for i, modalLine := range modalLines {
		row := startRow + i
		if row >= 0 && row < len(baseLines) {
			baseLine := baseLines[row]
			baseLineWidth := ansi.PrintableRuneWidth(baseLine)
			modalLineWidth := ansi.PrintableRuneWidth(modalLine)

			var newLine strings.Builder

			// Left portion: truncate base to startCol
			if startCol > 0 {
				if baseLineWidth >= startCol {
					newLine.WriteString(reflowtrunc.String(baseLine, uint(startCol)))
				} else {
					// Base line is shorter than startCol, pad with spaces
					newLine.WriteString(baseLine)
					newLine.WriteString(strings.Repeat(" ", startCol-baseLineWidth))
				}
			}

			// Modal content
			newLine.WriteString(modalLine)

			// Right portion: skip past modal and get remaining base content
			rightStart := startCol + modalLineWidth
			if rightStart < baseLineWidth {
				// Get the portion of baseLine after rightStart
				// reflowtrunc.String gives us up to N chars, so we truncate to rightStart then take the rest
				skipped := reflowtrunc.String(baseLine, uint(rightStart))
				skippedWidth := ansi.PrintableRuneWidth(skipped)
				if skippedWidth < len(baseLine) {
					// Find where in the original string the truncation ended
					remainder := cutAfterWidth(baseLine, rightStart)
					newLine.WriteString(remainder)
				}
			}

			baseLines[row] = newLine.String()
		}
	}

	return strings.Join(baseLines, "\n")
}

// cutAfterWidth returns the portion of s after the first width visible characters
func cutAfterWidth(s string, width int) string {
	if width <= 0 {
		return s
	}

	visible := 0
	inEscape := false
	bytePos := 0

	for i, r := range s {
		if visible >= width && !inEscape {
			bytePos = i
			break
		}

		if r == '\x1b' {
			inEscape = true
			continue
		}

		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}

		visible++
		bytePos = i + len(string(r))
	}

	if bytePos >= len(s) {
		return ""
	}

	return s[bytePos:]
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
	b.WriteString(titleStyle.Render("Add Scope Filter") + "\n\n")

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
	b.WriteString(hintStyle.Render("[Enter] Add  [Esc] Cancel  [Backspace] Remove last  [S] Clear all"))

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
	b.WriteString(reviewStyle.Render(reviewLine) + "\n")

	// Labels section
	if len(issue.Labels) > 0 {
		tagStyle := m.theme.Renderer.NewStyle().Foreground(m.theme.Secondary)
		b.WriteString("Labels: ")
		for i, label := range issue.Labels {
			if i > 0 {
				b.WriteString(" ")
			}
			b.WriteString(tagStyle.Render("[" + label + "]"))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")

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
	if m.width < BreakpointNarrow {
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

// ShouldSave returns true if the user requested to save on quit
func (m *ReviewDashboardModel) ShouldSave() bool {
	return m.saveOnQuit
}

// SaveReviews persists all collected review actions to beads
func (m *ReviewDashboardModel) SaveReviews() *review.ReviewSaveResult {
	if m.collector.Count() == 0 {
		return &review.ReviewSaveResult{Saved: 0, Failed: 0, Errors: nil}
	}

	saver := review.NewReviewSaver(m.workspaceRoot)
	defer saver.Close()

	actions := m.collector.Actions()
	saved, errors := saver.Save(actions)

	return &review.ReviewSaveResult{
		Saved:  saved,
		Failed: len(actions) - saved,
		Errors: errors,
	}
}

// loadReviewStateFromComments parses existing comments to load review state
func (m *ReviewDashboardModel) loadReviewStateFromComments() {
	// Load state for root issue
	if m.tree.Root != nil {
		m.loadIssueReviewState(m.tree.Root)
	}

	// Load state for all descendants
	for _, issue := range m.tree.Descendants {
		m.loadIssueReviewState(issue)
	}
}

// loadIssueReviewState loads review state for a single issue from its comments
func (m *ReviewDashboardModel) loadIssueReviewState(issue *model.Issue) {
	if issue == nil || len(issue.Comments) == 0 {
		return
	}

	// Collect comment texts
	commentTexts := make([]string, len(issue.Comments))
	for i, c := range issue.Comments {
		commentTexts[i] = c.Text
	}

	// Parse to find latest review
	status, reviewer, reviewedAt, found := review.GetLatestReviewFromComments(commentTexts)
	if found {
		issue.ReviewStatus = status
		issue.ReviewedBy = reviewer
		issue.ReviewedAt = reviewedAt
	}
}

// PendingSaveCount returns the number of reviews pending save
func (m *ReviewDashboardModel) PendingSaveCount() int {
	return m.collector.Count()
}

// WorkspaceRoot returns the workspace root path
func (m *ReviewDashboardModel) WorkspaceRoot() string {
	return m.workspaceRoot
}

// HasActiveModal returns true if any modal/dialog is currently shown
func (m *ReviewDashboardModel) HasActiveModal() bool {
	return m.showHelp || m.showAssigneeInput || m.showLabelInput
}

// generateSimplePrompt creates a simple summary of reviewed beads and their status
func (m *ReviewDashboardModel) generateSimplePrompt() string {
	actions := m.collector.Actions()
	if len(actions) == 0 {
		return "No reviews recorded in this session."
	}

	var b strings.Builder
	b.WriteString("# Review Session Summary\n\n")
	b.WriteString(fmt.Sprintf("Reviewed %d issues:\n\n", len(actions)))

	for _, action := range actions {
		statusEmoji := "✓"
		switch action.Status {
		case model.ReviewStatusApproved:
			statusEmoji = "✓"
		case model.ReviewStatusNeedsRevision:
			statusEmoji = "!"
		case model.ReviewStatusDeferred:
			statusEmoji = "?"
		}
		b.WriteString(fmt.Sprintf("- %s %s → %s\n", statusEmoji, action.IssueID, action.Status))
	}

	return b.String()
}

// generateFullPrompt creates a detailed prompt for agents to act on reviews
func (m *ReviewDashboardModel) generateFullPrompt() string {
	actions := m.collector.Actions()
	if len(actions) == 0 {
		return "No reviews recorded in this session."
	}

	var b strings.Builder

	// Header with context
	b.WriteString("# Review Session Summary\n\n")
	b.WriteString("You are reviewing a beads issue tracking session. ")
	b.WriteString("Go over the review feedback and suggest changes.\n\n")

	// Count by status
	approved, revision, deferred := 0, 0, 0
	for _, a := range actions {
		switch a.Status {
		case model.ReviewStatusApproved:
			approved++
		case model.ReviewStatusNeedsRevision:
			revision++
		case model.ReviewStatusDeferred:
			deferred++
		}
	}

	// Session stats
	b.WriteString("## Session Stats\n")
	b.WriteString(fmt.Sprintf("- Approved: %d issues\n", approved))
	b.WriteString(fmt.Sprintf("- Needs Revision: %d issues\n", revision))
	b.WriteString(fmt.Sprintf("- Deferred: %d issues\n\n", deferred))

	// Approved issues (brief list)
	if approved > 0 {
		b.WriteString("## Approved Issues\n")
		for _, a := range actions {
			if a.Status == model.ReviewStatusApproved {
				issue := m.findIssueByID(a.IssueID)
				title := a.IssueID
				if issue != nil {
					title = issue.Title
				}
				b.WriteString(fmt.Sprintf("- `%s`: %s\n", a.IssueID, title))
			}
		}
		b.WriteString("\n")
	}

	// Issues needing revision (detailed with notes)
	if revision > 0 {
		b.WriteString("## Issues Needing Revision\n")
		for _, a := range actions {
			if a.Status == model.ReviewStatusNeedsRevision {
				issue := m.findIssueByID(a.IssueID)
				title := a.IssueID
				if issue != nil {
					title = issue.Title
				}
				b.WriteString(fmt.Sprintf("### `%s`: %s\n", a.IssueID, title))
				if a.Notes != "" {
					b.WriteString(fmt.Sprintf("**Review Notes:** %s\n", a.Notes))
				}
				b.WriteString("**Action Required:** Review feedback and suggest implementation changes.\n\n")
			}
		}
	}

	// Deferred issues (with reason if provided)
	if deferred > 0 {
		b.WriteString("## Deferred Issues\n")
		for _, a := range actions {
			if a.Status == model.ReviewStatusDeferred {
				issue := m.findIssueByID(a.IssueID)
				title := a.IssueID
				if issue != nil {
					title = issue.Title
				}
				b.WriteString(fmt.Sprintf("### `%s`: %s\n", a.IssueID, title))
				if a.Notes != "" {
					b.WriteString(fmt.Sprintf("**Reason:** %s\n\n", a.Notes))
				} else {
					b.WriteString("\n")
				}
			}
		}
	}

	// Instructions footer
	b.WriteString("---\n\n")
	b.WriteString("For each issue with review feedback:\n")
	b.WriteString("1. Analyze the review notes\n")
	b.WriteString("2. Suggest concrete changes based on feedback\n")
	b.WriteString("3. Explain current bead state and dependencies\n")

	return b.String()
}

// findIssueByID finds an issue in the tree by ID
func (m *ReviewDashboardModel) findIssueByID(id string) *model.Issue {
	if m.tree.Root.ID == id {
		return m.tree.Root
	}
	for _, desc := range m.tree.Descendants {
		if desc.ID == id {
			return desc
		}
	}
	return nil
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

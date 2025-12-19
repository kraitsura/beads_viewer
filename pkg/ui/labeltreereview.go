package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/Dicklesworthstone/beads_viewer/pkg/review"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/ansi"
	reflowtrunc "github.com/muesli/reflow/truncate"
)

// cutAfterWidth returns the portion of s after skipping startWidth visual cells
func cutAfterWidth(s string, startWidth int) string {
	if startWidth <= 0 {
		return s
	}
	w := 0
	for i, r := range s {
		if w >= startWidth {
			return s[i:]
		}
		w += ansi.PrintableRuneWidth(string(r))
	}
	return ""
}

// LabelReviewFlatNode represents a single node in the unified flat list
type LabelReviewFlatNode struct {
	Issue       *model.Issue
	TreePrefix  string // Visual tree prefix (├─, └─, │ )
	Depth       int
	IsLast      bool
	StreamIndex int  // Which stream this belongs to
	IsHeader    bool // True if this is a stream header (root of tree)
}

// LabelTreeReviewModel provides a unified review for all issues with a label
// Shows all streams in one scrollable list with stream headers
type LabelTreeReviewModel struct {
	// Data
	labelName string
	issues    []model.Issue
	issueMap  map[string]*model.Issue
	flatNodes []LabelReviewFlatNode // Unified flat list of all streams

	// Navigation state
	cursor int
	scroll int

	// Split panel state
	detailFocus  bool
	detailScroll int

	// Review state
	reviewer       string
	sessionStarted time.Time
	itemsReviewed  int
	itemsApproved  int
	itemsRevision  int
	itemsDeferred  int

	// Filtering
	showFilter   string   // "all", "unreviewed", "needs_revision"
	activeLabels []string // Additional label filters
	searchQuery  string

	// Input modes
	showLabelInput bool
	labelInput     string
	showSearch     bool

	// Note input modal
	noteInput     NoteInputModel
	showNoteInput bool
	showSummary   bool
	quitting      bool
	saveOnQuit    bool

	// Dimensions
	width  int
	height int
	theme  Theme

	// Review persistence
	collector     *review.ReviewActionCollector
	workspaceRoot string
}

// NewLabelTreeReviewModel creates a unified review for a label
func NewLabelTreeReviewModel(labelName string, issues []model.Issue, reviewer string, reviewType string, theme Theme, workspaceRoot string) *LabelTreeReviewModel {
	issueMap := make(map[string]*model.Issue, len(issues))
	for i := range issues {
		issueMap[issues[i].ID] = &issues[i]
	}

	m := &LabelTreeReviewModel{
		labelName:      labelName,
		issues:         issues,
		issueMap:       issueMap,
		theme:          theme,
		width:          80,
		height:         24,
		sessionStarted: time.Now(),
		reviewer:       reviewer,
		showFilter:     "all",
		collector:      review.NewReviewActionCollector(reviewer, reviewType),
		workspaceRoot:  workspaceRoot,
	}

	m.rebuildFlatNodes()
	return m
}

// rebuildFlatNodes builds the unified flat list of all streams
func (m *LabelTreeReviewModel) rebuildFlatNodes() {
	m.flatNodes = nil

	// Find all issues with this label
	labeledIssues := make(map[string]model.Issue)
	for _, issue := range m.issues {
		for _, label := range issue.Labels {
			if label == m.labelName {
				labeledIssues[issue.ID] = issue
				break
			}
		}
	}

	// Build parent->children map
	children := make(map[string][]string)
	for _, issue := range m.issues {
		for _, dep := range issue.Dependencies {
			if dep.Type == model.DepParentChild {
				children[dep.DependsOnID] = append(children[dep.DependsOnID], issue.ID)
			}
		}
	}

	// Find root labeled issues (those with no labeled parent)
	var rootIDs []string
	for id, issue := range labeledIssues {
		isRoot := true
		for _, dep := range issue.Dependencies {
			if dep.Type == model.DepParentChild {
				if _, parentLabeled := labeledIssues[dep.DependsOnID]; parentLabeled {
					isRoot = false
					break
				}
			}
		}
		if isRoot {
			rootIDs = append(rootIDs, id)
		}
	}
	sort.Strings(rootIDs)

	// Build flat nodes for each stream
	issueMap := make(map[string]model.Issue)
	for _, issue := range m.issues {
		issueMap[issue.ID] = issue
	}

	for streamIdx, rootID := range rootIDs {
		rootIssue := labeledIssues[rootID]

		// Add root as stream header
		if m.shouldShow(&rootIssue) {
			m.flatNodes = append(m.flatNodes, LabelReviewFlatNode{
				Issue:       m.issueMap[rootID],
				TreePrefix:  "",
				Depth:       0,
				StreamIndex: streamIdx,
				IsHeader:    true,
			})
		}

		// Add children recursively
		m.addChildrenFlat(rootID, children, issueMap, streamIdx, 1, []bool{})
	}

	// Ensure cursor is valid
	if m.cursor >= len(m.flatNodes) {
		m.cursor = len(m.flatNodes) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// addChildrenFlat recursively adds children to flat nodes
func (m *LabelTreeReviewModel) addChildrenFlat(parentID string, children map[string][]string, issueMap map[string]model.Issue, streamIdx, depth int, parentPath []bool) {
	childIDs := children[parentID]
	sort.Strings(childIDs)

	for i, childID := range childIDs {
		childIssue, ok := issueMap[childID]
		if !ok {
			continue
		}

		if !m.shouldShow(&childIssue) {
			continue
		}

		isLast := i == len(childIDs)-1

		// Build tree prefix
		prefix := ""
		for j, wasLast := range parentPath {
			if j == 0 {
				continue
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

		m.flatNodes = append(m.flatNodes, LabelReviewFlatNode{
			Issue:       m.issueMap[childID],
			TreePrefix:  prefix,
			Depth:       depth,
			IsLast:      isLast,
			StreamIndex: streamIdx,
			IsHeader:    false,
		})

		// Recurse with updated path
		newPath := append([]bool{}, parentPath...)
		newPath = append(newPath, isLast)
		m.addChildrenFlat(childID, children, issueMap, streamIdx, depth+1, newPath)
	}
}

// shouldShow returns true if issue passes current filters
func (m *LabelTreeReviewModel) shouldShow(issue *model.Issue) bool {
	if issue == nil {
		return false
	}

	// Status filter
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

	// Search filter
	if m.searchQuery != "" {
		query := strings.ToLower(m.searchQuery)
		title := strings.ToLower(issue.Title)
		id := strings.ToLower(issue.ID)
		if !strings.Contains(title, query) && !strings.Contains(id, query) {
			return false
		}
	}

	// Label filter (must have ALL active labels)
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

// SetSize updates dimensions
func (m *LabelTreeReviewModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Update handles keyboard input
func (m *LabelTreeReviewModel) Update(key string) bool {
	// Note input handled externally
	if m.showNoteInput {
		return true
	}

	// Handle summary screen
	if m.showSummary {
		switch key {
		case "q":
			// Save and quit
			m.saveOnQuit = true
			m.quitting = true
			return false
		case "Q":
			// Discard and quit (don't save)
			m.quitting = true
			return false
		case "esc":
			m.showSummary = false
			return true
		case "p":
			// Copy simple summary to clipboard
			prompt := m.generateSimplePrompt()
			clipboard.WriteAll(prompt)
			return true
		case "P":
			// Copy full review prompt with instructions
			prompt := m.generateFullPrompt()
			clipboard.WriteAll(prompt)
			return true
		}
		return true
	}

	// Handle search input
	if m.showSearch {
		switch key {
		case "esc":
			m.showSearch = false
			m.searchQuery = ""
			m.rebuildFlatNodes()
			return true
		case "enter":
			m.showSearch = false
			return true
		case "backspace":
			if len(m.searchQuery) > 0 {
				m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
				m.rebuildFlatNodes()
			}
			return true
		default:
			if len(key) == 1 && key[0] >= 32 && key[0] < 127 {
				m.searchQuery += key
				m.rebuildFlatNodes()
			}
			return true
		}
	}

	// Handle label input
	if m.showLabelInput {
		switch key {
		case "esc":
			m.showLabelInput = false
			m.labelInput = ""
			return true
		case "enter":
			if m.labelInput != "" {
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
				}
			}
			m.showLabelInput = false
			m.labelInput = ""
			return true
		case "backspace":
			if len(m.labelInput) > 0 {
				m.labelInput = m.labelInput[:len(m.labelInput)-1]
			}
			return true
		default:
			if len(key) == 1 && key[0] >= 32 && key[0] < 127 {
				m.labelInput += key
			}
			return true
		}
	}

	// Handle detail panel keys
	if m.detailFocus {
		switch key {
		case "tab", "shift+tab":
			m.detailFocus = false
			return true
		case "j", "down":
			m.detailScroll++
			return true
		case "k", "up":
			if m.detailScroll > 0 {
				m.detailScroll--
			}
			return true
		case "g":
			m.detailScroll = 0
			return true
		case "esc", "q":
			return false
		}
		return true
	}

	// Main panel keys
	switch key {
	case "tab":
		m.detailFocus = true
		m.detailScroll = 0
		return true
	case "j", "down":
		if m.cursor < len(m.flatNodes)-1 {
			m.cursor++
			m.ensureVisible()
			m.detailScroll = 0
		}
		return true
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
			m.ensureVisible()
			m.detailScroll = 0
		}
		return true
	case "g", "home":
		m.cursor = 0
		m.scroll = 0
		return true
	case "G", "end":
		m.cursor = len(m.flatNodes) - 1
		m.ensureVisible()
		return true
	case "]":
		m.jumpToNextUnreviewed()
		return true
	case "[":
		m.jumpToPrevUnreviewed()
		return true
	case "f":
		m.cycleFilter()
		return true
	case "/":
		m.showSearch = true
		m.searchQuery = ""
		return true
	case "l":
		m.showLabelInput = true
		m.labelInput = ""
		return true
	case "L":
		m.activeLabels = nil
		m.rebuildFlatNodes()
		return true
	case "a":
		m.markApproved()
		return true
	case "r":
		m.openNoteInput("revision")
		return true
	case "d":
		m.openNoteInput("defer")
		return true
	case "n":
		m.openNoteInput("note")
		return true
	case "esc", "q":
		// Only show summary if changes were made
		if m.itemsReviewed > 0 {
			m.showSummary = true
		} else {
			// No changes - quit directly
			m.quitting = true
			return false
		}
		return true
	}
	return false
}

// cycleFilter cycles through filter options
func (m *LabelTreeReviewModel) cycleFilter() {
	switch m.showFilter {
	case "all":
		m.showFilter = "unreviewed"
	case "unreviewed":
		m.showFilter = "needs_revision"
	case "needs_revision":
		m.showFilter = "all"
	}
	m.rebuildFlatNodes()
}

// jumpToNextUnreviewed moves cursor to next unreviewed item
func (m *LabelTreeReviewModel) jumpToNextUnreviewed() {
	startIdx := m.cursor + 1
	for i := startIdx; i < len(m.flatNodes); i++ {
		if m.isUnreviewed(m.flatNodes[i].Issue) {
			m.cursor = i
			m.ensureVisible()
			return
		}
	}
	for i := 0; i < startIdx && i < len(m.flatNodes); i++ {
		if m.isUnreviewed(m.flatNodes[i].Issue) {
			m.cursor = i
			m.ensureVisible()
			return
		}
	}
}

// jumpToPrevUnreviewed moves cursor to previous unreviewed item
func (m *LabelTreeReviewModel) jumpToPrevUnreviewed() {
	startIdx := m.cursor - 1
	for i := startIdx; i >= 0; i-- {
		if m.isUnreviewed(m.flatNodes[i].Issue) {
			m.cursor = i
			m.ensureVisible()
			return
		}
	}
	for i := len(m.flatNodes) - 1; i > startIdx && i >= 0; i-- {
		if m.isUnreviewed(m.flatNodes[i].Issue) {
			m.cursor = i
			m.ensureVisible()
			return
		}
	}
}

// isUnreviewed returns true if issue is unreviewed
func (m *LabelTreeReviewModel) isUnreviewed(issue *model.Issue) bool {
	if issue == nil {
		return false
	}
	return issue.ReviewStatus == "" || issue.ReviewStatus == model.ReviewStatusUnreviewed
}

// ensureVisible adjusts scroll to keep cursor visible
func (m *LabelTreeReviewModel) ensureVisible() {
	visibleHeight := m.height - 7
	if m.showSearch {
		visibleHeight--
	}
	if visibleHeight < 3 {
		visibleHeight = 3
	}

	if m.cursor < m.scroll {
		m.scroll = m.cursor
	} else if m.cursor >= m.scroll+visibleHeight {
		m.scroll = m.cursor - visibleHeight + 1
	}

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

// openNoteInput opens note input modal
func (m *LabelTreeReviewModel) openNoteInput(action string) {
	issue := m.SelectedIssue()
	if issue == nil {
		return
	}
	m.noteInput = NewNoteInputModel(issue.Title, action, issue.ID, m.theme)
	m.noteInput.SetSize(m.width, m.height)
	m.showNoteInput = true
}

// markApproved marks current item as approved
func (m *LabelTreeReviewModel) markApproved() {
	issue := m.SelectedIssue()
	if issue == nil {
		return
	}

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

// ShowNoteInput returns true if note input is showing
func (m *LabelTreeReviewModel) ShowNoteInput() bool {
	return m.showNoteInput
}

// NoteInput returns pointer to note input model
func (m *LabelTreeReviewModel) NoteInput() *NoteInputModel {
	return &m.noteInput
}

// HandleNoteInputResult processes note input result
func (m *LabelTreeReviewModel) HandleNoteInputResult() {
	if m.noteInput.IsSubmitted() {
		if issue := m.SelectedIssue(); issue != nil {
			note := m.noteInput.Notes()
			action := m.noteInput.Action()

			if note != "" {
				if issue.Notes != "" {
					issue.Notes = issue.Notes + "\n\n---\n\n" + note
				} else {
					issue.Notes = note
				}
			}

			wasUnreviewed := issue.ReviewStatus == "" || issue.ReviewStatus == model.ReviewStatusUnreviewed
			switch action {
			case "revision":
				issue.ReviewStatus = model.ReviewStatusNeedsRevision
				issue.ReviewedBy = m.reviewer
				issue.ReviewedAt = time.Now()
				if wasUnreviewed {
					m.itemsReviewed++
					m.itemsRevision++
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
			}
		}
		m.showNoteInput = false
		m.noteInput.Reset()
	}

	if m.noteInput.IsCancelled() {
		m.showNoteInput = false
		m.noteInput.Reset()
	}
}

// SelectedIssue returns currently selected issue
func (m *LabelTreeReviewModel) SelectedIssue() *model.Issue {
	if m.cursor >= 0 && m.cursor < len(m.flatNodes) {
		return m.flatNodes[m.cursor].Issue
	}
	return nil
}

// GetSelectedIssueID returns currently selected issue ID
func (m *LabelTreeReviewModel) GetSelectedIssueID() string {
	if issue := m.SelectedIssue(); issue != nil {
		return issue.ID
	}
	return ""
}

// TreeCount returns number of streams
func (m *LabelTreeReviewModel) TreeCount() int {
	// Count unique stream indices
	seen := make(map[int]bool)
	for _, node := range m.flatNodes {
		seen[node.StreamIndex] = true
	}
	return len(seen)
}

// CurrentTree returns current stream index
func (m *LabelTreeReviewModel) CurrentTree() int {
	if m.cursor >= 0 && m.cursor < len(m.flatNodes) {
		return m.flatNodes[m.cursor].StreamIndex
	}
	return 0
}

// IsQuitting returns true if user confirmed quit
func (m *LabelTreeReviewModel) IsQuitting() bool {
	return m.quitting
}

// ShouldSave returns true if the user requested to save on quit
func (m *LabelTreeReviewModel) ShouldSave() bool {
	return m.saveOnQuit
}

// View renders the review dashboard
func (m *LabelTreeReviewModel) View() string {
	if m.showNoteInput {
		base := m.renderSplitView()
		return m.renderModalOverlay(base, m.noteInput.View())
	}

	if m.showSummary {
		return m.renderSummary()
	}

	if m.showLabelInput {
		base := m.renderSplitView()
		return m.renderModalOverlay(base, m.renderLabelInput())
	}

	if m.width < 80 {
		return m.renderCompactView()
	}

	return m.renderSplitView()
}

// renderSplitView renders split-panel layout matching ReviewDashboard
func (m *LabelTreeReviewModel) renderSplitView() string {
	t := m.theme

	leftWidth := (m.width * 45) / 100
	rightWidth := m.width - leftWidth - 1
	headerLines := 3
	footerLines := 2
	searchLines := 0
	if m.showSearch {
		searchLines = 1
	}
	contentHeight := m.height - headerLines - footerLines - searchLines

	if contentHeight < 5 {
		contentHeight = 5
	}

	var output strings.Builder

	// Header
	titleStyle := t.Renderer.NewStyle().Bold(true).Foreground(t.Primary)
	title := m.labelName
	maxTitleLen := m.width - 30
	if len(title) > maxTitleLen {
		title = title[:maxTitleLen-3] + "..."
	}
	output.WriteString(titleStyle.Render("◆ " + title) + "\n")

	// Progress bar and stats
	total := len(m.flatNodes)
	reviewed := 0
	for _, node := range m.flatNodes {
		if node.Issue != nil && node.Issue.ReviewStatus != "" && node.Issue.ReviewStatus != model.ReviewStatusUnreviewed {
			reviewed++
		}
	}
	pct := 0
	if total > 0 {
		pct = (reviewed * 100) / total
	}

	barWidth := 20
	filled := (pct * barWidth) / 100
	progressBar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	progressStyle := t.Renderer.NewStyle().Foreground(t.Open)
	statsStyle := t.Renderer.NewStyle().Foreground(t.Subtext)

	output.WriteString(progressStyle.Render(progressBar) + " ")
	output.WriteString(statsStyle.Render(fmt.Sprintf("%d/%d (%d%%)", reviewed, total, pct)))

	// Filter indicator
	if m.showFilter != "all" {
		filterStyle := t.Renderer.NewStyle().Foreground(t.Secondary)
		output.WriteString(filterStyle.Render("  ◇ " + m.showFilter))
	}

	// Active labels
	if len(m.activeLabels) > 0 {
		tagStyle := t.Renderer.NewStyle().Foreground(t.Secondary)
		output.WriteString("  ")
		for _, l := range m.activeLabels {
			output.WriteString(tagStyle.Render("⬡"+l) + " ")
		}
	}
	output.WriteString("\n")

	// Separator
	sepStyle := t.Renderer.NewStyle().Foreground(t.Border)
	output.WriteString(sepStyle.Render(strings.Repeat("─", m.width)) + "\n")

	// Search bar
	if m.showSearch {
		searchStyle := t.Renderer.NewStyle().Foreground(t.Primary)
		queryStyle := t.Renderer.NewStyle().Foreground(t.Secondary)
		output.WriteString(searchStyle.Render(" / ") + queryStyle.Render(m.searchQuery+"█") + "\n")
	}

	// Content panels
	leftPanel := m.renderTreePanel(leftWidth-1, contentHeight)
	rightPanel := m.renderDetailPanel(rightWidth-1, contentHeight)

	leftLines := strings.Split(leftPanel, "\n")
	rightLines := strings.Split(rightPanel, "\n")

	dividerChar := "│"
	dividerStyle := t.Renderer.NewStyle().Foreground(t.Border)
	if m.detailFocus {
		dividerStyle = t.Renderer.NewStyle().Foreground(t.Primary)
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

		leftVisible := lipgloss.Width(leftLine)
		if leftVisible < leftWidth-1 {
			leftLine += strings.Repeat(" ", leftWidth-1-leftVisible)
		}

		output.WriteString(leftLine)
		output.WriteString(dividerStyle.Render(dividerChar))
		output.WriteString(rightLine)
		output.WriteString("\n")
	}

	// Footer
	output.WriteString(sepStyle.Render(strings.Repeat("─", m.width)) + "\n")

	keyStyle := t.Renderer.NewStyle().Foreground(t.Primary)
	hintStyle := t.Renderer.NewStyle().Faint(true)
	focusStyle := t.Renderer.NewStyle().Foreground(t.Secondary)

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
	output.WriteString(keyStyle.Render("f") + hintStyle.Render("ilter "))
	output.WriteString(keyStyle.Render("l") + hintStyle.Render("abel "))
	output.WriteString(keyStyle.Render("/") + hintStyle.Render("search "))
	output.WriteString(keyStyle.Render("q") + hintStyle.Render("uit"))

	return output.String()
}

// renderTreePanel renders tree panel matching ReviewDashboard style
func (m *LabelTreeReviewModel) renderTreePanel(width, height int) string {
	t := m.theme
	var lines []string

	endIdx := m.scroll + height
	if endIdx > len(m.flatNodes) {
		endIdx = len(m.flatNodes)
	}

	lastStreamIdx := -1
	for i := m.scroll; i < endIdx; i++ {
		node := m.flatNodes[i]
		var line strings.Builder

		// Stream separator for new streams (except first)
		if node.IsHeader && node.StreamIndex != lastStreamIdx && lastStreamIdx != -1 {
			sepStyle := t.Renderer.NewStyle().Foreground(t.Border).Faint(true)
			lines = append(lines, sepStyle.Render(strings.Repeat("╌", width-2)))
		}
		lastStreamIdx = node.StreamIndex

		// Cursor indicator
		if i == m.cursor {
			cursorStyle := t.Renderer.NewStyle().Foreground(t.Primary)
			line.WriteString(cursorStyle.Render("▸ "))
		} else {
			line.WriteString("  ")
		}

		// Review status indicator (same as ReviewDashboard)
		var statusIndicator string
		var statusStyle lipgloss.Style
		if node.Issue != nil {
			switch node.Issue.ReviewStatus {
			case model.ReviewStatusApproved:
				statusStyle = t.Renderer.NewStyle().Foreground(t.Open)
				statusIndicator = "✓"
			case model.ReviewStatusNeedsRevision:
				statusStyle = t.Renderer.NewStyle().Foreground(t.Blocked)
				statusIndicator = "!"
			case model.ReviewStatusDeferred:
				statusStyle = t.Renderer.NewStyle().Foreground(t.Subtext)
				statusIndicator = "?"
			default:
				statusStyle = t.Renderer.NewStyle().Foreground(t.Subtext).Faint(true)
				statusIndicator = "○"
			}
		} else {
			statusStyle = t.Renderer.NewStyle().Foreground(t.Subtext).Faint(true)
			statusIndicator = "○"
		}
		line.WriteString(statusStyle.Render(statusIndicator) + " ")

		// Tree prefix
		if node.TreePrefix != "" {
			prefixStyle := t.Renderer.NewStyle().Foreground(t.Border)
			line.WriteString(prefixStyle.Render(node.TreePrefix))
		}

		// ID
		idStyle := t.Renderer.NewStyle().Foreground(t.Secondary)
		if i == m.cursor {
			idStyle = idStyle.Bold(true)
		}
		if node.Issue != nil {
			line.WriteString(idStyle.Render(node.Issue.ID) + " ")
		}

		// Title
		titleStyle := t.Renderer.NewStyle()
		if i == m.cursor {
			titleStyle = titleStyle.Foreground(t.Primary)
		} else if node.IsHeader {
			titleStyle = titleStyle.Foreground(t.Primary).Bold(true)
		} else {
			titleStyle = titleStyle.Foreground(t.Subtext)
		}

		currentWidth := lipgloss.Width(line.String())
		titleWidth := width - currentWidth - 1
		if titleWidth < 5 {
			titleWidth = 5
		}

		title := ""
		if node.Issue != nil {
			title = node.Issue.Title
		}
		if len(title) > titleWidth {
			title = title[:titleWidth-1] + "…"
		}
		line.WriteString(titleStyle.Render(title))

		lines = append(lines, line.String())
	}

	for len(lines) < height {
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}

// renderDetailPanel renders detail panel matching ReviewDashboard style
func (m *LabelTreeReviewModel) renderDetailPanel(width, height int) string {
	t := m.theme
	issue := m.SelectedIssue()
	if issue == nil {
		return "No issue selected"
	}

	var lines []string

	// Header
	headerStyle := t.Renderer.NewStyle().Bold(true).Foreground(t.Primary)
	lines = append(lines, headerStyle.Render(issue.ID))
	lines = append(lines, strings.Repeat("─", width-2))

	// Title (may wrap)
	titleLines := wrapTextLines(issue.Title, width-2)
	lines = append(lines, titleLines...)
	lines = append(lines, "")

	// Status line
	statusStyle := t.Renderer.NewStyle().Foreground(t.Subtext)
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
		reviewStyle = t.Renderer.NewStyle().Foreground(t.Open).Bold(true)
	case model.ReviewStatusNeedsRevision:
		reviewStyle = t.Renderer.NewStyle().Foreground(t.Blocked).Bold(true)
	default:
		reviewStyle = t.Renderer.NewStyle().Foreground(t.Subtext)
	}
	lines = append(lines, reviewStyle.Render("Review: "+strings.ToUpper(reviewStatus)))
	lines = append(lines, "")

	// Description
	if issue.Description != "" {
		sectionStyle := t.Renderer.NewStyle().Bold(true)
		lines = append(lines, sectionStyle.Render("Description:"))
		descLines := wrapTextLines(issue.Description, width-2)
		lines = append(lines, descLines...)
		lines = append(lines, "")
	}

	// Design
	if issue.Design != "" {
		sectionStyle := t.Renderer.NewStyle().Bold(true)
		lines = append(lines, sectionStyle.Render("Design:"))
		designLines := wrapTextLines(issue.Design, width-2)
		lines = append(lines, designLines...)
		lines = append(lines, "")
	}

	// Acceptance Criteria
	if issue.AcceptanceCriteria != "" {
		sectionStyle := t.Renderer.NewStyle().Bold(true)
		lines = append(lines, sectionStyle.Render("Acceptance:"))
		acLines := wrapTextLines(issue.AcceptanceCriteria, width-2)
		lines = append(lines, acLines...)
		lines = append(lines, "")
	}

	// Notes
	if issue.Notes != "" {
		sectionStyle := t.Renderer.NewStyle().Bold(true)
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

	for len(visibleLines) < height {
		visibleLines = append(visibleLines, "")
	}

	return strings.Join(visibleLines, "\n")
}

// renderLabelInput renders label input modal
func (m *LabelTreeReviewModel) renderLabelInput() string {
	t := m.theme

	titleStyle := t.Renderer.NewStyle().Bold(true).Foreground(t.Primary)
	labelStyle := t.Renderer.NewStyle().Foreground(t.Subtext)
	inputStyle := t.Renderer.NewStyle().Foreground(t.Primary)
	hintStyle := t.Renderer.NewStyle().Faint(true)
	tagStyle := t.Renderer.NewStyle().Foreground(t.Secondary)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Add Label Filter") + "\n\n")

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

	boxStyle := t.Renderer.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Primary).
		Padding(1, 3).
		Width(45)

	return boxStyle.Render(b.String())
}

// renderSummary renders session summary matching ReviewDashboard style
func (m *LabelTreeReviewModel) renderSummary() string {
	t := m.theme
	var b strings.Builder
	duration := time.Since(m.sessionStarted).Round(time.Second)

	// Header
	headerStyle := t.Renderer.NewStyle().Bold(true).Foreground(t.Primary)
	b.WriteString(headerStyle.Render("Review Session Summary") + "\n")
	b.WriteString(strings.Repeat("─", 40) + "\n\n")

	// Session info
	infoStyle := t.Renderer.NewStyle().Foreground(t.Subtext)
	b.WriteString(infoStyle.Render(fmt.Sprintf("Label:    %s", m.labelName)) + "\n")
	b.WriteString(infoStyle.Render(fmt.Sprintf("Streams:  %d", m.TreeCount())) + "\n")
	b.WriteString(infoStyle.Render(fmt.Sprintf("Duration: %s", duration)) + "\n\n")

	// Stats
	statsHeaderStyle := t.Renderer.NewStyle().Bold(true)
	b.WriteString(statsHeaderStyle.Render("Items Reviewed:") + "\n")

	approvedStyle := t.Renderer.NewStyle().Foreground(t.Open)
	revisionStyle := t.Renderer.NewStyle().Foreground(t.Blocked)
	deferredStyle := t.Renderer.NewStyle().Foreground(t.Subtext)

	b.WriteString(fmt.Sprintf("  Total:          %d\n", m.itemsReviewed))
	b.WriteString(approvedStyle.Render(fmt.Sprintf("  ✓ Approved:     %d", m.itemsApproved)) + "\n")
	b.WriteString(revisionStyle.Render(fmt.Sprintf("  ! Needs Revision: %d", m.itemsRevision)) + "\n")
	b.WriteString(deferredStyle.Render(fmt.Sprintf("  ? Deferred:     %d", m.itemsDeferred)) + "\n\n")

	// Progress bar
	total := len(m.flatNodes)
	reviewed := 0
	for _, node := range m.flatNodes {
		if node.Issue != nil && node.Issue.ReviewStatus != "" && node.Issue.ReviewStatus != model.ReviewStatusUnreviewed {
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
	hintStyle := t.Renderer.NewStyle().Faint(true)
	keyStyle := t.Renderer.NewStyle().Foreground(t.Primary)
	b.WriteString(keyStyle.Render("q") + hintStyle.Render(" save & quit  "))
	b.WriteString(keyStyle.Render("Q") + hintStyle.Render(" discard & quit\n"))
	b.WriteString(keyStyle.Render("p") + hintStyle.Render(" copy ID list  "))
	b.WriteString(keyStyle.Render("P") + hintStyle.Render(" fix issues\n"))
	b.WriteString(keyStyle.Render("Esc") + hintStyle.Render(" continue reviewing"))

	boxStyle := t.Renderer.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Primary).
		Padding(1, 2).
		Width(55)

	content := boxStyle.Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

// renderModalOverlay renders a modal centered over the base view, preserving the background
func (m *LabelTreeReviewModel) renderModalOverlay(base, modal string) string {
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
				remainder := cutAfterWidth(baseLine, rightStart)
				newLine.WriteString(remainder)
			}

			baseLines[row] = newLine.String()
		}
	}

	return strings.Join(baseLines, "\n")
}

// renderCompactView renders compact single-panel view
func (m *LabelTreeReviewModel) renderCompactView() string {
	t := m.theme
	var lines []string

	headerStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)
	lines = append(lines, headerStyle.Render(fmt.Sprintf("LENS: %s", m.labelName)))
	lines = append(lines, "")

	maxVisible := m.height - 6
	if maxVisible < 5 {
		maxVisible = 5
	}

	endIdx := m.scroll + maxVisible
	if endIdx > len(m.flatNodes) {
		endIdx = len(m.flatNodes)
	}

	for i := m.scroll; i < endIdx; i++ {
		node := m.flatNodes[i]
		var line strings.Builder

		if i == m.cursor {
			line.WriteString("▸ ")
		} else {
			line.WriteString("  ")
		}

		if node.Issue != nil {
			var statusIndicator string
			switch node.Issue.ReviewStatus {
			case model.ReviewStatusApproved:
				statusIndicator = "✓"
			case model.ReviewStatusNeedsRevision:
				statusIndicator = "!"
			case model.ReviewStatusDeferred:
				statusIndicator = "?"
			default:
				statusIndicator = "○"
			}
			line.WriteString(statusIndicator + " ")
			line.WriteString(node.TreePrefix)
			line.WriteString(node.Issue.ID + " ")

			title := node.Issue.Title
			maxLen := m.width - lipgloss.Width(line.String()) - 2
			if maxLen > 5 && len(title) > maxLen {
				title = title[:maxLen-1] + "…"
			}
			line.WriteString(title)
		}

		lines = append(lines, line.String())
	}

	lines = append(lines, "")
	footerStyle := t.Renderer.NewStyle().Foreground(t.Subtext)
	keyStyle := t.Renderer.NewStyle().Foreground(t.Primary)
	lines = append(lines, keyStyle.Render("j/k")+" "+footerStyle.Render("nav")+"  "+
		keyStyle.Render("a/r/d")+" "+footerStyle.Render("review")+"  "+
		keyStyle.Render("q")+" "+footerStyle.Render("quit"))

	return strings.Join(lines, "\n")
}

// SaveReviews persists all collected review actions to beads
func (m *LabelTreeReviewModel) SaveReviews() *review.ReviewSaveResult {
	if m.collector.Count() == 0 {
		return &review.ReviewSaveResult{Saved: 0, Failed: 0, Errors: nil}
	}

	saver, _ := review.NewReviewSaver(m.workspaceRoot)
	defer saver.Close()

	actions := m.collector.Actions()
	saved, errors := saver.Save(actions)

	return &review.ReviewSaveResult{
		Saved:  saved,
		Failed: len(actions) - saved,
		Errors: errors,
	}
}

// PendingSaveCount returns the number of reviews pending save
func (m *LabelTreeReviewModel) PendingSaveCount() int {
	return m.collector.Count()
}

// WorkspaceRoot returns the workspace root path
func (m *LabelTreeReviewModel) WorkspaceRoot() string {
	return m.workspaceRoot
}

// generateSimplePrompt creates a simple summary of reviewed beads and their status
func (m *LabelTreeReviewModel) generateSimplePrompt() string {
	actions := m.collector.Actions()
	if len(actions) == 0 {
		return "No reviews recorded in this session."
	}

	var b strings.Builder
	b.WriteString("# Review Session Summary\n\n")
	b.WriteString(fmt.Sprintf("Reviewed %d issues (label: %s):\n\n", len(actions), m.labelName))

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
func (m *LabelTreeReviewModel) generateFullPrompt() string {
	actions := m.collector.Actions()
	if len(actions) == 0 {
		return "No reviews recorded in this session."
	}

	var b strings.Builder
	b.WriteString("# Review Session - Action Required\n\n")
	b.WriteString(fmt.Sprintf("Label: %s\n\n", m.labelName))
	b.WriteString("The following issues have been reviewed and require updates.\n")
	b.WriteString("Please update each bead based on its review status and notes.\n\n")
	b.WriteString("---\n\n")

	for _, action := range actions {
		b.WriteString(fmt.Sprintf("## %s\n\n", action.IssueID))
		b.WriteString(fmt.Sprintf("**Review Status:** %s\n", action.Status))

		// Add issue context if available
		if issue := m.issueMap[action.IssueID]; issue != nil {
			b.WriteString(fmt.Sprintf("**Title:** %s\n", issue.Title))
			b.WriteString(fmt.Sprintf("**Type:** %s | **Priority:** %d\n", issue.IssueType, issue.Priority))
		}

		if action.Notes != "" {
			b.WriteString(fmt.Sprintf("\n**Review Notes:**\n%s\n", action.Notes))
		}

		// Add recommended action based on status
		b.WriteString("\n**Recommended Action:**\n")
		switch action.Status {
		case model.ReviewStatusApproved:
			b.WriteString("- Issue approved. Mark as ready for implementation or close if complete.\n")
			b.WriteString(fmt.Sprintf("- Run: `bd update %s --status=in_progress` to begin work, or `bd close %s` if done.\n", action.IssueID, action.IssueID))
		case model.ReviewStatusNeedsRevision:
			b.WriteString("- Issue needs revision based on review feedback.\n")
			b.WriteString("- Address the review notes above, then re-submit for review.\n")
			b.WriteString(fmt.Sprintf("- Run: `bd show %s` to see full details.\n", action.IssueID))
		case model.ReviewStatusDeferred:
			b.WriteString("- Issue deferred for later consideration.\n")
			b.WriteString("- No immediate action required.\n")
		}
		b.WriteString("\n---\n\n")
	}

	return b.String()
}

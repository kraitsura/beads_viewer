package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"

	"github.com/charmbracelet/lipgloss"
)

// DepthOption represents dependency tree expansion depth
type DepthOption int

const (
	Depth1   DepthOption = 1
	Depth2   DepthOption = 2
	Depth3   DepthOption = 3
	DepthAll DepthOption = -1 // Unlimited
)

// String returns display string for depth
func (d DepthOption) String() string {
	if d == DepthAll {
		return "All"
	}
	return fmt.Sprintf("%d", d)
}

// TreeNode represents a node in the dependency tree
type TreeNode struct {
	Issue       model.Issue
	IsPrimary   bool        // true if has the label
	Children    []*TreeNode // downstream issues (what this unblocks)
	Depth       int         // depth in tree (0 = root)
	IsLastChild bool        // for rendering tree lines
	ParentPath  []bool      // track which ancestors are last children (for tree lines)
}

// FlatNode is a flattened tree node for display/navigation
type FlatNode struct {
	Node       *TreeNode
	TreePrefix string // rendered tree prefix (├─►, └─►, etc.)
	Status     string // ready, blocked, in_progress, closed
	BlockedBy  string // ID of blocker if blocked
}

// LabelDashboardModel represents the label dashboard view
type LabelDashboardModel struct {
	// Data
	labelName string
	viewMode  string // "label" or "epic"
	epicID    string // Only set if viewMode == "epic"

	// Tree data
	roots       []*TreeNode          // Root nodes (ready issues or all primaries at depth 1)
	flatNodes   []FlatNode           // Flattened for display
	allIssues   []model.Issue        // Reference to all issues
	issueMap    map[string]*model.Issue
	primaryIDs  map[string]bool      // Issues that have the label
	blockedByMap map[string]string   // issue ID -> blocking issue ID

	// Dependency graphs
	downstream map[string][]string // issue ID -> issues it unblocks
	upstream   map[string][]string // issue ID -> issues that block it

	// Dependency expansion
	dependencyDepth DepthOption

	// UI State
	cursor          int
	selectedIssueID string
	scroll          int // scroll offset for long lists

	// Stats
	totalCount   int
	primaryCount int
	contextCount int
	readyCount   int
	blockedCount int
	closedCount  int

	// Dimensions
	width  int
	height int
	theme  Theme
}

// NewLabelDashboardModel creates a new label dashboard for the given label
func NewLabelDashboardModel(labelName string, allIssues []model.Issue, issueMap map[string]*model.Issue, theme Theme) LabelDashboardModel {
	m := LabelDashboardModel{
		labelName:       labelName,
		viewMode:        "label",
		allIssues:       allIssues,
		issueMap:        issueMap,
		theme:           theme,
		dependencyDepth: Depth2, // Default to 2 levels (shows immediate deps)
		width:           80,
		height:          24,
		primaryIDs:      make(map[string]bool),
	}

	// Find primary issues (have this label)
	for _, issue := range allIssues {
		for _, label := range issue.Labels {
			if label == labelName {
				m.primaryIDs[issue.ID] = true
				break
			}
		}
	}

	m.buildGraphs()
	m.buildTree()

	return m
}

// NewEpicDashboardModel creates a dashboard for an epic's children
func NewEpicDashboardModel(epicID string, epicTitle string, allIssues []model.Issue, issueMap map[string]*model.Issue, theme Theme) LabelDashboardModel {
	m := LabelDashboardModel{
		labelName:       epicTitle,
		viewMode:        "epic",
		epicID:          epicID,
		allIssues:       allIssues,
		issueMap:        issueMap,
		theme:           theme,
		dependencyDepth: Depth2,
		width:           80,
		height:          24,
		primaryIDs:      make(map[string]bool),
	}

	// Find children of this epic
	for _, issue := range allIssues {
		for _, dep := range issue.Dependencies {
			if dep.DependsOnID == epicID && dep.Type == model.DepParentChild {
				m.primaryIDs[issue.ID] = true
				break
			}
		}
	}

	m.buildGraphs()
	m.buildTree()

	return m
}

// buildGraphs builds the upstream and downstream dependency graphs
func (m *LabelDashboardModel) buildGraphs() {
	m.downstream = make(map[string][]string)
	m.upstream = make(map[string][]string)
	m.blockedByMap = make(map[string]string)

	// Build set of open issues
	openIssues := make(map[string]bool)
	for _, issue := range m.allIssues {
		if issue.Status != model.StatusClosed {
			openIssues[issue.ID] = true
		}
	}

	// Build graphs from dependencies
	for _, issue := range m.allIssues {
		for _, dep := range issue.Dependencies {
			if dep.Type == model.DepBlocks {
				// issue depends on dep.DependsOnID (dep.DependsOnID blocks issue)
				// So: dep.DependsOnID -> issue (downstream)
				// And: issue <- dep.DependsOnID (upstream)
				m.downstream[dep.DependsOnID] = append(m.downstream[dep.DependsOnID], issue.ID)
				m.upstream[issue.ID] = append(m.upstream[issue.ID], dep.DependsOnID)

				// Track first open blocker
				if openIssues[dep.DependsOnID] && m.blockedByMap[issue.ID] == "" {
					m.blockedByMap[issue.ID] = dep.DependsOnID
				}
			}
		}
	}
}

// buildTree builds the tree structure based on current depth
func (m *LabelDashboardModel) buildTree() {
	m.roots = nil
	m.flatNodes = nil
	m.totalCount = 0
	m.primaryCount = 0
	m.contextCount = 0
	m.readyCount = 0
	m.blockedCount = 0
	m.closedCount = 0

	seen := make(map[string]bool)

	// Find root nodes: primary issues that are "ready" (not blocked by open issues)
	// Or at depth 1, just show all primary issues flat
	var rootIssues []model.Issue

	if m.dependencyDepth == Depth1 {
		// Depth 1: flat list of primary issues only
		for _, issue := range m.allIssues {
			if m.primaryIDs[issue.ID] {
				rootIssues = append(rootIssues, issue)
			}
		}
	} else {
		// Depth 2+: find ready roots and build trees
		// Roots are primary issues with no open blockers (or blockers outside primary set)
		for _, issue := range m.allIssues {
			if !m.primaryIDs[issue.ID] {
				continue
			}

			// Check if blocked by another primary issue
			isBlockedByPrimary := false
			for _, blockerID := range m.upstream[issue.ID] {
				if blocker, ok := m.issueMap[blockerID]; ok {
					if blocker.Status != model.StatusClosed && m.primaryIDs[blockerID] {
						isBlockedByPrimary = true
						break
					}
				}
			}

			if !isBlockedByPrimary {
				rootIssues = append(rootIssues, issue)
			}
		}

		// If no roots found (all blocked), just use all primary issues
		if len(rootIssues) == 0 {
			for _, issue := range m.allIssues {
				if m.primaryIDs[issue.ID] {
					rootIssues = append(rootIssues, issue)
				}
			}
		}
	}

	// Sort roots by status (ready first) then priority
	sort.Slice(rootIssues, func(i, j int) bool {
		si := m.getStatusOrder(rootIssues[i])
		sj := m.getStatusOrder(rootIssues[j])
		if si != sj {
			return si < sj
		}
		return rootIssues[i].Priority < rootIssues[j].Priority
	})

	// Build tree from each root
	maxDepth := int(m.dependencyDepth)
	if m.dependencyDepth == DepthAll {
		maxDepth = 100
	}

	for i, issue := range rootIssues {
		if seen[issue.ID] {
			continue
		}
		isLast := i == len(rootIssues)-1
		node := m.buildTreeNode(issue, 0, maxDepth, seen, isLast, nil)
		if node != nil {
			m.roots = append(m.roots, node)
		}
	}

	// Flatten tree for display
	m.flattenTree()

	// Update selected issue
	if len(m.flatNodes) > 0 {
		if m.cursor >= len(m.flatNodes) {
			m.cursor = len(m.flatNodes) - 1
		}
		m.selectedIssueID = m.flatNodes[m.cursor].Node.Issue.ID
	} else {
		m.selectedIssueID = ""
	}
}

// buildTreeNode recursively builds a tree node
func (m *LabelDashboardModel) buildTreeNode(issue model.Issue, depth, maxDepth int, seen map[string]bool, isLast bool, parentPath []bool) *TreeNode {
	if seen[issue.ID] {
		return nil
	}
	seen[issue.ID] = true

	node := &TreeNode{
		Issue:       issue,
		IsPrimary:   m.primaryIDs[issue.ID],
		Depth:       depth,
		IsLastChild: isLast,
		ParentPath:  append([]bool{}, parentPath...),
	}

	// Update stats
	m.totalCount++
	if node.IsPrimary {
		m.primaryCount++
	} else {
		m.contextCount++
	}

	status := m.getIssueStatus(issue)
	switch status {
	case "ready":
		m.readyCount++
	case "blocked":
		m.blockedCount++
	case "closed":
		m.closedCount++
	}

	// Add children (downstream issues) if within depth
	if depth < maxDepth-1 {
		var childIssues []model.Issue
		for _, childID := range m.downstream[issue.ID] {
			if child, ok := m.issueMap[childID]; ok {
				if !seen[childID] {
					childIssues = append(childIssues, *child)
				}
			}
		}

		// Sort children by status then priority
		sort.Slice(childIssues, func(i, j int) bool {
			si := m.getStatusOrder(childIssues[i])
			sj := m.getStatusOrder(childIssues[j])
			if si != sj {
				return si < sj
			}
			return childIssues[i].Priority < childIssues[j].Priority
		})

		newParentPath := append(parentPath, isLast)
		for i, child := range childIssues {
			childIsLast := i == len(childIssues)-1
			childNode := m.buildTreeNode(child, depth+1, maxDepth, seen, childIsLast, newParentPath)
			if childNode != nil {
				node.Children = append(node.Children, childNode)
			}
		}
	}

	return node
}

// flattenTree converts the tree to a flat list for display
func (m *LabelDashboardModel) flattenTree() {
	m.flatNodes = nil
	for _, root := range m.roots {
		m.flattenNode(root)
	}
}

// flattenNode recursively flattens a node and its children
func (m *LabelDashboardModel) flattenNode(node *TreeNode) {
	prefix := m.buildTreePrefix(node)
	status := m.getIssueStatus(node.Issue)

	fn := FlatNode{
		Node:       node,
		TreePrefix: prefix,
		Status:     status,
		BlockedBy:  m.blockedByMap[node.Issue.ID],
	}
	m.flatNodes = append(m.flatNodes, fn)

	for _, child := range node.Children {
		m.flattenNode(child)
	}
}

// buildTreePrefix builds the tree line prefix for a node
func (m *LabelDashboardModel) buildTreePrefix(node *TreeNode) string {
	if node.Depth == 0 {
		return ""
	}

	var prefix strings.Builder

	// Build prefix from parent path
	for i := 0; i < len(node.ParentPath); i++ {
		if node.ParentPath[i] {
			prefix.WriteString("   ") // parent was last child, no line
		} else {
			prefix.WriteString("│  ") // parent has siblings, continue line
		}
	}

	// Add connector for this node
	if node.IsLastChild {
		prefix.WriteString("└─►")
	} else {
		prefix.WriteString("├─►")
	}

	return prefix.String()
}

// getIssueStatus returns the effective status of an issue
func (m *LabelDashboardModel) getIssueStatus(issue model.Issue) string {
	if issue.Status == model.StatusClosed {
		return "closed"
	}
	if issue.Status == model.StatusInProgress {
		return "in_progress"
	}
	if m.blockedByMap[issue.ID] != "" {
		return "blocked"
	}
	return "ready"
}

// getStatusOrder returns sort order for status (ready first)
func (m *LabelDashboardModel) getStatusOrder(issue model.Issue) int {
	status := m.getIssueStatus(issue)
	switch status {
	case "ready":
		return 0
	case "in_progress":
		return 1
	case "blocked":
		return 2
	case "closed":
		return 3
	default:
		return 4
	}
}

// CycleDepth cycles through depth options
func (m *LabelDashboardModel) CycleDepth() {
	switch m.dependencyDepth {
	case Depth1:
		m.dependencyDepth = Depth2
	case Depth2:
		m.dependencyDepth = Depth3
	case Depth3:
		m.dependencyDepth = DepthAll
	case DepthAll:
		m.dependencyDepth = Depth1
	default:
		m.dependencyDepth = Depth2
	}

	// Rebuild tree with new depth
	m.buildTree()
}

// GetDepth returns the current depth setting
func (m *LabelDashboardModel) GetDepth() DepthOption {
	return m.dependencyDepth
}

// SetSize updates the dashboard dimensions
func (m *LabelDashboardModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// MoveUp moves cursor up
func (m *LabelDashboardModel) MoveUp() {
	if m.cursor > 0 {
		m.cursor--
		m.selectedIssueID = m.flatNodes[m.cursor].Node.Issue.ID
		m.ensureVisible()
	}
}

// MoveDown moves cursor down
func (m *LabelDashboardModel) MoveDown() {
	if m.cursor < len(m.flatNodes)-1 {
		m.cursor++
		m.selectedIssueID = m.flatNodes[m.cursor].Node.Issue.ID
		m.ensureVisible()
	}
}

// ensureVisible adjusts scroll to keep cursor visible
func (m *LabelDashboardModel) ensureVisible() {
	visibleLines := m.height - 8 // header, stats, footer
	if visibleLines < 5 {
		visibleLines = 5
	}

	if m.cursor < m.scroll {
		m.scroll = m.cursor
	} else if m.cursor >= m.scroll+visibleLines {
		m.scroll = m.cursor - visibleLines + 1
	}
}

// NextSection jumps to next status group
func (m *LabelDashboardModel) NextSection() {
	if len(m.flatNodes) == 0 {
		return
	}

	currentStatus := m.flatNodes[m.cursor].Status
	for i := m.cursor + 1; i < len(m.flatNodes); i++ {
		if m.flatNodes[i].Status != currentStatus {
			m.cursor = i
			m.selectedIssueID = m.flatNodes[m.cursor].Node.Issue.ID
			m.ensureVisible()
			return
		}
	}
}

// PrevSection jumps to previous status group
func (m *LabelDashboardModel) PrevSection() {
	if len(m.flatNodes) == 0 {
		return
	}

	currentStatus := m.flatNodes[m.cursor].Status

	// Find start of current section
	sectionStart := m.cursor
	for sectionStart > 0 && m.flatNodes[sectionStart-1].Status == currentStatus {
		sectionStart--
	}

	// If at start of section, go to previous section
	if m.cursor == sectionStart && sectionStart > 0 {
		m.cursor = sectionStart - 1
		m.selectedIssueID = m.flatNodes[m.cursor].Node.Issue.ID
		m.ensureVisible()
	} else {
		// Go to start of current section
		m.cursor = sectionStart
		m.selectedIssueID = m.flatNodes[m.cursor].Node.Issue.ID
		m.ensureVisible()
	}
}

// SelectedIssueID returns the ID of the currently selected issue
func (m *LabelDashboardModel) SelectedIssueID() string {
	return m.selectedIssueID
}

// LabelName returns the current label name
func (m *LabelDashboardModel) LabelName() string {
	return m.labelName
}

// IssueCount returns the total number of issues
func (m *LabelDashboardModel) IssueCount() int {
	return m.totalCount
}

// ContextCount returns the number of context issues
func (m *LabelDashboardModel) ContextCount() int {
	return m.contextCount
}

// PrimaryCount returns the number of primary issues
func (m *LabelDashboardModel) PrimaryCount() int {
	return m.primaryCount
}

// View renders the dashboard
func (m *LabelDashboardModel) View() string {
	t := m.theme

	var lines []string

	contentWidth := m.width - 4
	if contentWidth < 40 {
		contentWidth = 40
	}

	// Header
	headerStyle := t.Renderer.NewStyle().
		Foreground(t.Primary).
		Bold(true)

	modeIcon := ""
	if m.viewMode == "epic" {
		modeIcon = "📋 "
	}

	// Progress bar
	progress := 0.0
	if m.totalCount > 0 {
		progress = float64(m.closedCount) / float64(m.totalCount)
	}
	progressBar := m.renderProgressBar(progress, 10)
	progressText := fmt.Sprintf("%d/%d done", m.closedCount, m.totalCount)

	header := fmt.Sprintf("%s%s", modeIcon, m.labelName)
	headerLine := headerStyle.Render(header) + "  " + progressBar + " " + progressText
	lines = append(lines, headerLine)

	// Stats line
	statsStyle := t.Renderer.NewStyle().Foreground(t.Subtext)
	primaryIcon := t.Renderer.NewStyle().Foreground(t.Primary).Render("●")
	contextIcon := t.Renderer.NewStyle().Foreground(t.Secondary).Render("○")
	depthStyle := t.Renderer.NewStyle().Foreground(t.InProgress).Bold(true)

	statsLine := fmt.Sprintf("%s %d labeled  %s %d context  Depth: [%s]",
		primaryIcon, m.primaryCount, contextIcon, m.contextCount,
		depthStyle.Render(m.dependencyDepth.String()))
	lines = append(lines, statsStyle.Render(statsLine))

	// Status summary
	readyStyle := t.Renderer.NewStyle().Foreground(t.Open)
	blockedStyle := t.Renderer.NewStyle().Foreground(t.Blocked)
	inProgStyle := t.Renderer.NewStyle().Foreground(t.InProgress)
	summaryLine := fmt.Sprintf("%s ready  %s blocked  %s in progress",
		readyStyle.Render(fmt.Sprintf("%d", m.readyCount)),
		blockedStyle.Render(fmt.Sprintf("%d", m.blockedCount)),
		inProgStyle.Render(fmt.Sprintf("%d", m.totalCount-m.readyCount-m.blockedCount-m.closedCount)))
	lines = append(lines, statsStyle.Render(summaryLine))
	lines = append(lines, "")

	// Calculate visible area
	visibleLines := m.height - 8
	if visibleLines < 5 {
		visibleLines = 5
	}

	// Render tree
	if len(m.flatNodes) == 0 {
		emptyStyle := t.Renderer.NewStyle().Foreground(t.Subtext).Italic(true)
		lines = append(lines, emptyStyle.Render("  No issues found"))
	} else {
		// Render visible portion
		endIdx := m.scroll + visibleLines
		if endIdx > len(m.flatNodes) {
			endIdx = len(m.flatNodes)
		}

		lastStatus := ""
		for i := m.scroll; i < endIdx; i++ {
			fn := m.flatNodes[i]

			// Show status header when status changes
			if fn.Status != lastStatus {
				statusHeader := m.renderStatusHeader(fn.Status)
				lines = append(lines, statusHeader)
				lastStatus = fn.Status
			}

			isSelected := i == m.cursor
			line := m.renderTreeNode(fn, isSelected, contentWidth)
			lines = append(lines, line)
		}

		// Show scroll indicator if needed
		if len(m.flatNodes) > visibleLines {
			scrollInfo := fmt.Sprintf("  [%d-%d of %d]", m.scroll+1, endIdx, len(m.flatNodes))
			lines = append(lines, statsStyle.Render(scrollInfo))
		}
	}

	// Footer
	lines = append(lines, "")
	footerStyle := t.Renderer.NewStyle().Foreground(t.Subtext).Italic(true)
	lines = append(lines, footerStyle.Render("j/k: nav • d: depth • p: copy • tab: detail • n/N: section • enter: jump • esc: back"))

	return strings.Join(lines, "\n")
}

// renderStatusHeader renders a status section header
func (m *LabelDashboardModel) renderStatusHeader(status string) string {
	t := m.theme

	var color lipgloss.AdaptiveColor
	var label string

	switch status {
	case "ready":
		color = t.Open
		label = "READY"
	case "blocked":
		color = t.Blocked
		label = "BLOCKED"
	case "in_progress":
		color = t.InProgress
		label = "IN PROGRESS"
	case "closed":
		color = t.Closed
		label = "CLOSED"
	default:
		color = t.Subtext
		label = strings.ToUpper(status)
	}

	style := t.Renderer.NewStyle().Foreground(color).Bold(true)
	return style.Render(label)
}

// renderTreeNode renders a single tree node
func (m *LabelDashboardModel) renderTreeNode(fn FlatNode, isSelected bool, maxWidth int) string {
	t := m.theme
	node := fn.Node

	// Selection indicator
	selectPrefix := "  "
	if isSelected {
		selectPrefix = "▸ "
	}

	// Primary/context indicator
	var indicator string
	if node.IsPrimary {
		indicator = t.Renderer.NewStyle().Foreground(t.Primary).Render("●")
	} else {
		indicator = t.Renderer.NewStyle().Foreground(t.Secondary).Render("○")
	}

	// Tree prefix (styled dimmer)
	treePrefix := ""
	if fn.TreePrefix != "" {
		treePrefix = t.Renderer.NewStyle().Foreground(t.Subtext).Render(fn.TreePrefix) + " "
	}

	// Issue ID and title
	idStyle := t.Renderer.NewStyle()
	titleStyle := t.Renderer.NewStyle()

	if isSelected {
		idStyle = idStyle.Foreground(t.Primary).Bold(true)
		titleStyle = titleStyle.Foreground(t.Primary).Bold(true)
	} else if !node.IsPrimary {
		idStyle = idStyle.Foreground(t.Subtext)
		titleStyle = titleStyle.Foreground(t.Subtext)
	} else {
		idStyle = idStyle.Foreground(t.Base.GetForeground())
		titleStyle = titleStyle.Foreground(t.Base.GetForeground())
	}

	// Calculate max title length
	prefixLen := len(selectPrefix) + 2 + len(fn.TreePrefix) + len(node.Issue.ID) + 3
	maxTitleLen := maxWidth - prefixLen
	if maxTitleLen < 15 {
		maxTitleLen = 15
	}
	title := truncateRunesHelper(node.Issue.Title, maxTitleLen, "…")

	// Status indicator for blocked items
	statusSuffix := ""
	if fn.Status == "blocked" && fn.BlockedBy != "" {
		blockerStyle := t.Renderer.NewStyle().Foreground(t.Blocked)
		statusSuffix = blockerStyle.Render(" ◄ " + fn.BlockedBy)
	}

	return fmt.Sprintf("%s%s %s%s %s%s",
		selectPrefix,
		indicator,
		treePrefix,
		idStyle.Render(node.Issue.ID),
		titleStyle.Render(title),
		statusSuffix)
}

func (m *LabelDashboardModel) renderProgressBar(progress float64, width int) string {
	t := m.theme

	filled := int(progress * float64(width))
	if filled > width {
		filled = width
	}

	var barColor lipgloss.AdaptiveColor
	if progress >= 1.0 {
		barColor = t.Closed
	} else if progress >= 0.5 {
		barColor = t.InProgress
	} else {
		barColor = t.Open
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	return t.Renderer.NewStyle().Foreground(barColor).Render("[" + bar + "]")
}

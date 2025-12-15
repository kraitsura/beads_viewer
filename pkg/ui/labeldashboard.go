package ui

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
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

// ViewType represents the display mode for workstreams
type ViewType int

const (
	ViewTypeFlat       ViewType = 0
	ViewTypeWorkstream ViewType = 1
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
	Issue        model.Issue
	IsPrimary    bool        // true if has the label
	IsEntryEpic  bool        // true if this is the entry point epic (when viewing an epic)
	Children     []*TreeNode // downstream issues (what this unblocks)
	Depth        int         // depth in tree (0 = root)
	IsLastChild  bool        // for rendering tree lines
	ParentPath   []bool      // track which ancestors are last children (for tree lines)
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
	primaryIDs  map[string]bool      // Issues that have the label (expanded via parent-child)
	directPrimaryIDs map[string]bool // Issues that directly have the label (not expanded)
	blockedByMap map[string]string   // issue ID -> blocking issue ID

	// Epic mode: depth-specific descendant maps
	// For epic mode, depth semantics differ from label mode:
	// - Depth1 = direct children of epic
	// - Depth2 = children + grandchildren
	// - Depth3 = children + grandchildren + great-grandchildren
	// - DepthAll = all descendants
	epicDescendantsByDepth map[DepthOption]map[string]bool

	// Dependency graphs
	downstream      map[string][]string // issue ID -> issues it unblocks (blocks + parent-child)
	upstream        map[string][]string // issue ID -> issues that block it
	parentChildDown map[string][]string // issue ID -> children (parent-child only, no blocking)

	// Dependency expansion
	dependencyDepth DepthOption

	// View type (flat vs workstream)
	viewType        ViewType
	workstreamCount int
	workstreams     []analysis.Workstream

	// Workstream view cursor
	wsCursor      int // Which workstream is selected
	wsIssueCursor int // Which issue within workstream (-1 = header)

	// Workstream expansion state
	wsExpanded map[int]bool // Which workstreams are expanded
	wsScroll   int          // Scroll offset for workstream view
	wsTreeView bool         // Show dependency tree within workstreams

	// Sub-workstream support
	workstreamPtrs []*analysis.Workstream // Pointers for mutation during subdivision
	wsSubdivided   bool                   // Whether subdivision is active
	subWSExpanded  map[int]map[int]bool   // wsIndex -> subIndex -> expanded
	subWsCursor    map[int]int            // wsIndex -> subWsCursor

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
		labelName:        labelName,
		viewMode:         "label",
		allIssues:        allIssues,
		issueMap:         issueMap,
		theme:            theme,
		dependencyDepth:  Depth2, // Default to 2 levels (shows immediate deps)
		width:            80,
		height:           24,
		primaryIDs:       make(map[string]bool),
		directPrimaryIDs: make(map[string]bool),
	}

	// Find direct primary issues (have this label directly)
	for _, issue := range allIssues {
		for _, label := range issue.Labels {
			if label == labelName {
				m.directPrimaryIDs[issue.ID] = true
				break
			}
		}
	}

	// Expand to include all descendants via parent-child
	// This ensures children of labeled epics appear in the view
	m.primaryIDs = expandToDescendants(m.directPrimaryIDs, allIssues)

	m.buildGraphs()
	m.buildTree()
	m.recomputeWorkstreams() // Ensure workstreams use same issue set as flat view

	return m
}

// NewBeadDashboardModel creates a dashboard for any issue and its descendants/blocked issues.
// Unlike epic mode which only shows parent-child descendants, bead mode also includes
// issues that are blocked by the entry issue (downstream dependency graph).
func NewBeadDashboardModel(issueID string, allIssues []model.Issue, issueMap map[string]*model.Issue, theme Theme) LabelDashboardModel {
	issue, exists := issueMap[issueID]
	if !exists {
		// Return empty dashboard if issue not found
		return LabelDashboardModel{
			labelName:        "Not Found: " + issueID,
			viewMode:         "bead",
			allIssues:        allIssues,
			issueMap:         issueMap,
			theme:            theme,
			dependencyDepth:  Depth2,
			width:            80,
			height:           24,
			primaryIDs:       make(map[string]bool),
			directPrimaryIDs: make(map[string]bool),
		}
	}

	m := LabelDashboardModel{
		labelName:        issue.Title,
		viewMode:         "bead",
		epicID:           issueID, // Reuse epicID field for entry point
		allIssues:        allIssues,
		issueMap:         issueMap,
		theme:            theme,
		dependencyDepth:  Depth2,
		width:            80,
		height:           24,
		primaryIDs:       make(map[string]bool),
		directPrimaryIDs: make(map[string]bool),
	}

	// For bead mode, directPrimaryIDs contains DIRECT CHILDREN of the entry bead
	directChildren := getDirectChildren(issueID, allIssues)
	for childID := range directChildren {
		m.directPrimaryIDs[childID] = true
	}

	// primaryIDs contains the entry issue + all descendants via parent-child + all blocked issues
	entrySet := map[string]bool{issueID: true}
	m.primaryIDs = expandToDescendantsAndBlocked(entrySet, allIssues)

	// Build depth-specific descendant maps (reuse epic logic)
	m.epicDescendantsByDepth = buildBeadDescendantsByDepth(issueID, allIssues)

	m.buildGraphs()
	m.buildTree()
	m.recomputeWorkstreams()

	return m
}

// NewEpicDashboardModel creates a dashboard for an epic's children
func NewEpicDashboardModel(epicID string, epicTitle string, allIssues []model.Issue, issueMap map[string]*model.Issue, theme Theme) LabelDashboardModel {
	m := LabelDashboardModel{
		labelName:        epicTitle,
		viewMode:         "epic",
		epicID:           epicID,
		allIssues:        allIssues,
		issueMap:         issueMap,
		theme:            theme,
		dependencyDepth:  Depth2,
		width:            80,
		height:           24,
		primaryIDs:       make(map[string]bool),
		directPrimaryIDs: make(map[string]bool),
	}

	// For epic mode, directPrimaryIDs contains DIRECT CHILDREN of the epic (not the epic itself)
	// This matches the intended behavior: Depth1 = direct children
	directChildren := getDirectChildren(epicID, allIssues)
	for childID := range directChildren {
		m.directPrimaryIDs[childID] = true
	}

	// primaryIDs contains ALL descendants (for DepthAll)
	// Start with epic itself for tree building
	epicSet := map[string]bool{epicID: true}
	m.primaryIDs = expandToDescendants(epicSet, allIssues)

	// Build depth-specific descendant maps for epic mode
	m.epicDescendantsByDepth = buildEpicDescendantsByDepth(epicID, allIssues)

	m.buildGraphs()
	m.buildTree()
	m.recomputeWorkstreams() // Ensure workstreams use same issue set as flat view

	return m
}

// getDirectChildren returns the direct children of an issue via parent-child relationships
func getDirectChildren(parentID string, issues []model.Issue) map[string]bool {
	children := make(map[string]bool)
	for _, issue := range issues {
		for _, dep := range issue.Dependencies {
			if dep.Type == model.DepParentChild && dep.DependsOnID == parentID {
				children[issue.ID] = true
				break
			}
		}
	}
	return children
}

// buildEpicDescendantsByDepth builds maps of descendants at each depth level for epic mode.
// Depth1 = direct children only
// Depth2 = children + grandchildren
// Depth3 = children + grandchildren + great-grandchildren
// DepthAll = all descendants
func buildEpicDescendantsByDepth(epicID string, issues []model.Issue) map[DepthOption]map[string]bool {
	// Build parent -> children map
	children := make(map[string][]string)
	for _, issue := range issues {
		for _, dep := range issue.Dependencies {
			if dep.Type == model.DepParentChild {
				children[dep.DependsOnID] = append(children[dep.DependsOnID], issue.ID)
			}
		}
	}

	result := make(map[DepthOption]map[string]bool)

	// BFS with depth tracking
	type queueItem struct {
		id    string
		depth int
	}

	// Collect descendants at each level
	descendantsByLevel := make(map[int]map[string]bool)
	visited := make(map[string]bool)
	queue := []queueItem{{id: epicID, depth: 0}}
	visited[epicID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, childID := range children[current.id] {
			if !visited[childID] {
				visited[childID] = true
				childDepth := current.depth + 1

				if descendantsByLevel[childDepth] == nil {
					descendantsByLevel[childDepth] = make(map[string]bool)
				}
				descendantsByLevel[childDepth][childID] = true

				queue = append(queue, queueItem{id: childID, depth: childDepth})
			}
		}
	}

	// Build cumulative sets for each DepthOption
	// Depth1 = level 1 only (direct children)
	result[Depth1] = make(map[string]bool)
	for id := range descendantsByLevel[1] {
		result[Depth1][id] = true
	}

	// Depth2 = levels 1-2
	result[Depth2] = make(map[string]bool)
	for level := 1; level <= 2; level++ {
		for id := range descendantsByLevel[level] {
			result[Depth2][id] = true
		}
	}

	// Depth3 = levels 1-3
	result[Depth3] = make(map[string]bool)
	for level := 1; level <= 3; level++ {
		for id := range descendantsByLevel[level] {
			result[Depth3][id] = true
		}
	}

	// DepthAll = all levels
	result[DepthAll] = make(map[string]bool)
	for level := range descendantsByLevel {
		for id := range descendantsByLevel[level] {
			result[DepthAll][id] = true
		}
	}

	return result
}

// expandToDescendants expands a set of issue IDs to include all descendants
// via parent-child relationships. Uses BFS to find all children recursively.
func expandToDescendants(primaryIDs map[string]bool, issues []model.Issue) map[string]bool {
	if len(primaryIDs) == 0 {
		return primaryIDs
	}

	// Build parent -> children map
	children := make(map[string][]string)
	for _, issue := range issues {
		for _, dep := range issue.Dependencies {
			if dep.Type == model.DepParentChild {
				// issue is a child of dep.DependsOnID
				children[dep.DependsOnID] = append(children[dep.DependsOnID], issue.ID)
			}
		}
	}

	// BFS to find all descendants
	expanded := make(map[string]bool)
	for id := range primaryIDs {
		expanded[id] = true
	}

	queue := make([]string, 0, len(primaryIDs))
	for id := range primaryIDs {
		queue = append(queue, id)
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, childID := range children[current] {
			if !expanded[childID] {
				expanded[childID] = true
				queue = append(queue, childID)
			}
		}
	}

	return expanded
}

// expandToDescendantsAndBlocked expands a set of issue IDs to include:
// 1. All descendants via parent-child relationships (like expandToDescendants)
// 2. All issues that the primary set blocks (downstream dependency graph)
// This is used for bead mode where we want to show what an issue unblocks.
func expandToDescendantsAndBlocked(primaryIDs map[string]bool, issues []model.Issue) map[string]bool {
	if len(primaryIDs) == 0 {
		return primaryIDs
	}

	// First expand via parent-child
	expanded := expandToDescendants(primaryIDs, issues)

	// Build blocks graph: issue ID -> issues it blocks (issues that depend on it)
	blocks := make(map[string][]string)
	for _, issue := range issues {
		for _, dep := range issue.Dependencies {
			if dep.Type == model.DepBlocks {
				// issue depends on dep.DependsOnID, meaning dep.DependsOnID blocks issue
				blocks[dep.DependsOnID] = append(blocks[dep.DependsOnID], issue.ID)
			}
		}
	}

	// BFS to find all blocked issues (downstream from primary set)
	queue := make([]string, 0, len(expanded))
	for id := range expanded {
		queue = append(queue, id)
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, blockedID := range blocks[current] {
			if !expanded[blockedID] {
				expanded[blockedID] = true
				queue = append(queue, blockedID)
			}
		}
	}

	return expanded
}

// buildBeadDescendantsByDepth builds maps of descendants at each depth level for bead mode.
// Similar to buildEpicDescendantsByDepth but includes both parent-child AND blocking relationships.
// Depth1 = direct children + directly blocked issues
// Depth2 = above + grandchildren + transitively blocked
// etc.
func buildBeadDescendantsByDepth(beadID string, issues []model.Issue) map[DepthOption]map[string]bool {
	// Build parent -> children map AND blocker -> blocked map
	children := make(map[string][]string)
	blocks := make(map[string][]string)
	for _, issue := range issues {
		for _, dep := range issue.Dependencies {
			switch dep.Type {
			case model.DepParentChild:
				children[dep.DependsOnID] = append(children[dep.DependsOnID], issue.ID)
			case model.DepBlocks:
				blocks[dep.DependsOnID] = append(blocks[dep.DependsOnID], issue.ID)
			}
		}
	}

	result := make(map[DepthOption]map[string]bool)

	// BFS with depth tracking - follow both parent-child and blocking edges
	type queueItem struct {
		id    string
		depth int
	}

	// Collect descendants at each level
	descendantsByLevel := make(map[int]map[string]bool)
	visited := make(map[string]bool)
	queue := []queueItem{{id: beadID, depth: 0}}
	visited[beadID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		// Follow parent-child edges
		for _, childID := range children[current.id] {
			if !visited[childID] {
				visited[childID] = true
				childDepth := current.depth + 1

				if descendantsByLevel[childDepth] == nil {
					descendantsByLevel[childDepth] = make(map[string]bool)
				}
				descendantsByLevel[childDepth][childID] = true

				queue = append(queue, queueItem{id: childID, depth: childDepth})
			}
		}

		// Follow blocking edges
		for _, blockedID := range blocks[current.id] {
			if !visited[blockedID] {
				visited[blockedID] = true
				blockedDepth := current.depth + 1

				if descendantsByLevel[blockedDepth] == nil {
					descendantsByLevel[blockedDepth] = make(map[string]bool)
				}
				descendantsByLevel[blockedDepth][blockedID] = true

				queue = append(queue, queueItem{id: blockedID, depth: blockedDepth})
			}
		}
	}

	// Build cumulative sets for each DepthOption
	// Depth1 = level 1 only (direct children + directly blocked)
	result[Depth1] = make(map[string]bool)
	for id := range descendantsByLevel[1] {
		result[Depth1][id] = true
	}

	// Depth2 = levels 1-2
	result[Depth2] = make(map[string]bool)
	for level := 1; level <= 2; level++ {
		for id := range descendantsByLevel[level] {
			result[Depth2][id] = true
		}
	}

	// Depth3 = levels 1-3
	result[Depth3] = make(map[string]bool)
	for level := 1; level <= 3; level++ {
		for id := range descendantsByLevel[level] {
			result[Depth3][id] = true
		}
	}

	// DepthAll = all levels
	result[DepthAll] = make(map[string]bool)
	for level := range descendantsByLevel {
		for id := range descendantsByLevel[level] {
			result[DepthAll][id] = true
		}
	}

	return result
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
			switch dep.Type {
			case model.DepBlocks:
				// issue depends on dep.DependsOnID (dep.DependsOnID blocks issue)
				// So: dep.DependsOnID -> issue (downstream)
				// And: issue <- dep.DependsOnID (upstream)
				m.downstream[dep.DependsOnID] = append(m.downstream[dep.DependsOnID], issue.ID)
				m.upstream[issue.ID] = append(m.upstream[issue.ID], dep.DependsOnID)

				// Track first open blocker
				if openIssues[dep.DependsOnID] && m.blockedByMap[issue.ID] == "" {
					m.blockedByMap[issue.ID] = dep.DependsOnID
				}

			case model.DepParentChild:
				// issue is a child of dep.DependsOnID (parent -> child relationship)
				// So: dep.DependsOnID -> issue (downstream/children)
				// And: issue <- dep.DependsOnID (upstream/parent)
				m.downstream[dep.DependsOnID] = append(m.downstream[dep.DependsOnID], issue.ID)
				m.upstream[issue.ID] = append(m.upstream[issue.ID], dep.DependsOnID)
				// Note: parent-child doesn't create blocking relationships
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

	// Get the depth-appropriate primary IDs
	// This ensures Depth1 uses directPrimaryIDs for label mode,
	// and depth-specific descendants for epic mode
	depthPrimaryIDs := m.GetPrimaryIDsForDepth()

	// Find root nodes: primary issues that are "ready" (not blocked by open issues)
	// Or at depth 1, just show all primary issues flat
	var rootIssues []model.Issue

	if m.dependencyDepth == Depth1 {
		// Depth 1: flat list of primary issues only (depth-appropriate)
		for _, issue := range m.allIssues {
			if depthPrimaryIDs[issue.ID] {
				rootIssues = append(rootIssues, issue)
			}
		}
	} else {
		// Depth 2+: find ready roots and build trees
		// Roots are primary issues with no open blockers (or blockers outside primary set)
		for _, issue := range m.allIssues {
			if !depthPrimaryIDs[issue.ID] {
				continue
			}

			// Check if blocked by another primary issue
			isBlockedByPrimary := false
			for _, blockerID := range m.upstream[issue.ID] {
				if blocker, ok := m.issueMap[blockerID]; ok {
					if blocker.Status != model.StatusClosed && depthPrimaryIDs[blockerID] {
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
				if depthPrimaryIDs[issue.ID] {
					rootIssues = append(rootIssues, issue)
				}
			}
		}
	}

	// Sort roots: entry point first (when in epic or bead mode), then by status, then priority
	sort.Slice(rootIssues, func(i, j int) bool {
		// Entry point (epic or bead) always comes first
		if (m.viewMode == "epic" || m.viewMode == "bead") && m.epicID != "" {
			if rootIssues[i].ID == m.epicID {
				return true
			}
			if rootIssues[j].ID == m.epicID {
				return false
			}
		}
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

	// Add upstream context blockers that weren't reached via downstream traversal
	// These are context issues that block primary issues but aren't downstream of any primary
	if m.dependencyDepth != Depth1 {
		m.addUpstreamContextBlockers(seen, maxDepth)
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

	// Use depth-appropriate primary IDs for IsPrimary determination
	depthPrimaryIDs := m.GetPrimaryIDsForDepth()

	node := &TreeNode{
		Issue:       issue,
		IsPrimary:   depthPrimaryIDs[issue.ID],
		IsEntryEpic: (m.viewMode == "epic" || m.viewMode == "bead") && issue.ID == m.epicID,
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

// addUpstreamContextBlockers finds context issues that block primaries but weren't
// included via downstream traversal, and adds them to the tree.
// This ensures parity with workstream view which includes both directions.
func (m *LabelDashboardModel) addUpstreamContextBlockers(seen map[string]bool, maxDepth int) {
	// Use depth-appropriate primary IDs
	depthPrimaryIDs := m.GetPrimaryIDsForDepth()

	// Find all context issues that block any primary issue (directly or transitively)
	var contextBlockers []model.Issue

	// First pass: find direct context blockers of primary issues
	directBlockers := make(map[string]bool)
	for _, issue := range m.allIssues {
		if depthPrimaryIDs[issue.ID] {
			// This is a primary - check its blockers
			for _, blockerID := range m.upstream[issue.ID] {
				if !depthPrimaryIDs[blockerID] && !seen[blockerID] {
					// This is a context blocker not yet in the tree
					directBlockers[blockerID] = true
				}
			}
		}
	}

	// BFS to find transitive context blockers (blockers of blockers)
	// AND their parent-child descendants (children of context blockers)
	toVisit := make([]string, 0, len(directBlockers))
	for id := range directBlockers {
		toVisit = append(toVisit, id)
	}

	allContextBlockers := make(map[string]bool)
	for id := range directBlockers {
		allContextBlockers[id] = true
	}

	for len(toVisit) > 0 {
		current := toVisit[0]
		toVisit = toVisit[1:]

		// Find upstream blockers of this context issue
		for _, blockerID := range m.upstream[current] {
			if !depthPrimaryIDs[blockerID] && !seen[blockerID] && !allContextBlockers[blockerID] {
				allContextBlockers[blockerID] = true
				toVisit = append(toVisit, blockerID)
			}
		}

	}

	// Collect context blocker issues
	for _, issue := range m.allIssues {
		if allContextBlockers[issue.ID] {
			contextBlockers = append(contextBlockers, issue)
		}
	}

	if len(contextBlockers) == 0 {
		return
	}

	// Sort by status (ready first) then priority
	sort.Slice(contextBlockers, func(i, j int) bool {
		si := m.getStatusOrder(contextBlockers[i])
		sj := m.getStatusOrder(contextBlockers[j])
		if si != sj {
			return si < sj
		}
		return contextBlockers[i].Priority < contextBlockers[j].Priority
	})

	// Find context blockers that are "roots" (not blocked by other unseen context blockers)
	contextRoots := make([]model.Issue, 0)
	for _, issue := range contextBlockers {
		if seen[issue.ID] {
			continue
		}

		isBlockedByUnseen := false
		for _, blockerID := range m.upstream[issue.ID] {
			if allContextBlockers[blockerID] && !seen[blockerID] {
				isBlockedByUnseen = true
				break
			}
		}

		if !isBlockedByUnseen {
			contextRoots = append(contextRoots, issue)
		}
	}

	// If no roots found, just use all unseen context blockers
	if len(contextRoots) == 0 {
		for _, issue := range contextBlockers {
			if !seen[issue.ID] {
				contextRoots = append(contextRoots, issue)
			}
		}
	}

	// Build tree nodes for context blockers
	// These will follow downstream within the context blocker set
	numExistingRoots := len(m.roots)
	for i, issue := range contextRoots {
		if seen[issue.ID] {
			continue
		}
		isLast := (numExistingRoots == 0) && (i == len(contextRoots)-1)
		node := m.buildContextBlockerNode(issue, 0, maxDepth, seen, isLast, nil, allContextBlockers)
		if node != nil {
			m.roots = append(m.roots, node)
		}
	}
}

// buildContextBlockerNode builds a tree node for context blockers,
// following downstream within the context blocker set
func (m *LabelDashboardModel) buildContextBlockerNode(issue model.Issue, depth, maxDepth int, seen map[string]bool, isLast bool, parentPath []bool, contextBlockerSet map[string]bool) *TreeNode {
	if seen[issue.ID] {
		return nil
	}
	seen[issue.ID] = true

	// Use depth-appropriate primary IDs for IsPrimary determination
	depthPrimaryIDs := m.GetPrimaryIDsForDepth()

	node := &TreeNode{
		Issue:       issue,
		IsPrimary:   depthPrimaryIDs[issue.ID],
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

	// Add children (downstream issues within context blocker set) if within depth
	if depth < maxDepth-1 {
		var childIssues []model.Issue
		for _, childID := range m.downstream[issue.ID] {
			if child, ok := m.issueMap[childID]; ok {
				// Only include if it's a context blocker and not yet seen
				if contextBlockerSet[childID] && !seen[childID] {
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
			childNode := m.buildContextBlockerNode(child, depth+1, maxDepth, seen, childIsLast, newParentPath, contextBlockerSet)
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
	// Check explicit blocked status first, then blocking dependencies
	if issue.Status == model.StatusBlocked {
		return "blocked"
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

	// Recompute workstreams with depth-appropriate primaryIDs
	m.recomputeWorkstreams()
}

// GetPrimaryIDsForDepth returns the appropriate primaryIDs for the current depth.
// At Depth1, returns only direct label matches.
// At Depth2+, returns expanded set including descendants.
func (m *LabelDashboardModel) GetPrimaryIDsForDepth() map[string]bool {
	// Epic and bead modes: use depth-specific descendant maps
	if (m.viewMode == "epic" || m.viewMode == "bead") && m.epicDescendantsByDepth != nil {
		if depthSet, ok := m.epicDescendantsByDepth[m.dependencyDepth]; ok {
			return depthSet
		}
		// Fallback to all descendants for unknown depth
		return m.primaryIDs
	}

	// Label mode:
	// - Depth1: only issues with the label directly applied
	// - Depth2+: expanded set including descendants
	if m.dependencyDepth == Depth1 {
		return m.directPrimaryIDs
	}
	return m.primaryIDs
}

// recomputeWorkstreams detects workstreams using depth-appropriate primaryIDs
// and the same issue set that flat view shows (primary + context blockers)
func (m *LabelDashboardModel) recomputeWorkstreams() {
	selectedLabel := m.labelName
	primaryIDs := m.GetPrimaryIDsForDepth()

	// Get the same issue set that flat view shows
	// This ensures flat and workstream views display the same issues
	displayIssues := m.getDisplayIssues()

	workstreams := analysis.DetectWorkstreams(displayIssues, primaryIDs, selectedLabel)
	m.SetWorkstreams(workstreams)
}

// getDisplayIssues returns the issues that should be displayed in the current view.
// This is the union of primary issues (depth-appropriate) and context blockers.
// Used to ensure flat and workstream views show the same issue set.
func (m *LabelDashboardModel) getDisplayIssues() []model.Issue {
	// If flatNodes is populated, extract issues from there
	// This ensures we get exactly what flat view would show
	if len(m.flatNodes) > 0 {
		seen := make(map[string]bool)
		issues := make([]model.Issue, 0, len(m.flatNodes))
		for _, fn := range m.flatNodes {
			if !seen[fn.Node.Issue.ID] {
				seen[fn.Node.Issue.ID] = true
				issues = append(issues, fn.Node.Issue)
			}
		}
		return issues
	}

	// Fallback: return depth-appropriate primary issues only
	depthPrimaryIDs := m.GetPrimaryIDsForDepth()
	issues := make([]model.Issue, 0)
	for _, issue := range m.allIssues {
		if depthPrimaryIDs[issue.ID] {
			issues = append(issues, issue)
		}
	}
	return issues
}

// GetDepth returns the current depth setting
func (m *LabelDashboardModel) GetDepth() DepthOption {
	return m.dependencyDepth
}

// SetDepth sets the dependency depth and rebuilds the tree
func (m *LabelDashboardModel) SetDepth(depth DepthOption) {
	m.dependencyDepth = depth
	m.buildTree()
	m.recomputeWorkstreams()
}

// SetSize updates the dashboard dimensions
func (m *LabelDashboardModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// MoveUp moves cursor up
func (m *LabelDashboardModel) MoveUp() {
	if m.viewType == ViewTypeWorkstream && len(m.workstreams) > 1 {
		m.moveUpWS()
		return
	}
	if m.cursor > 0 {
		m.cursor--
		m.selectedIssueID = m.flatNodes[m.cursor].Node.Issue.ID
		m.ensureVisible()
	}
}

// MoveDown moves cursor down
func (m *LabelDashboardModel) MoveDown() {
	if m.viewType == ViewTypeWorkstream && len(m.workstreams) > 1 {
		m.moveDownWS()
		return
	}
	if m.cursor < len(m.flatNodes)-1 {
		m.cursor++
		m.selectedIssueID = m.flatNodes[m.cursor].Node.Issue.ID
		m.ensureVisible()
	}
}

// moveUpWS moves cursor up in workstream view
func (m *LabelDashboardModel) moveUpWS() {
	if len(m.workstreams) == 0 {
		return
	}

	if m.wsIssueCursor > 0 {
		// Move up within current workstream's issues
		m.wsIssueCursor--
	} else if m.wsIssueCursor == 0 {
		// At first issue, go to header
		m.wsIssueCursor = -1
	} else if m.wsCursor > 0 {
		// At header, go to previous workstream's last issue
		m.wsCursor--
		issueCount := m.getVisibleIssueCount(m.wsCursor)
		if issueCount > 0 {
			m.wsIssueCursor = issueCount - 1
		} else {
			m.wsIssueCursor = -1
		}
	}

	m.updateSelectedIssueFromWS()
}

// getVisibleIssueCount returns the number of visible issues for a workstream
func (m *LabelDashboardModel) getVisibleIssueCount(wsIdx int) int {
	if wsIdx >= len(m.workstreams) {
		return 0
	}
	ws := m.workstreams[wsIdx]
	isExpanded := m.wsExpanded[wsIdx]

	// In tree view with expansion, count tree nodes
	if m.wsTreeView && isExpanded {
		wsCopy := ws
		treeRoots := m.buildWorkstreamTree(&wsCopy)
		flatNodes := m.flattenWSTree(treeRoots)
		return len(flatNodes)
	}

	// Flat view
	issueCount := len(ws.Issues)
	if !isExpanded && issueCount > 3 {
		return 3 // Collapsed: show max 3
	}
	return issueCount // Expanded: show all
}

// moveDownWS moves cursor down in workstream view
func (m *LabelDashboardModel) moveDownWS() {
	if len(m.workstreams) == 0 {
		return
	}

	maxIssues := m.getVisibleIssueCount(m.wsCursor)

	if m.wsIssueCursor < 0 {
		// At header, move to first issue (if any)
		if maxIssues > 0 {
			m.wsIssueCursor = 0
		} else if m.wsCursor < len(m.workstreams)-1 {
			// No issues, go to next workstream
			m.wsCursor++
			m.wsIssueCursor = -1
		}
	} else if m.wsIssueCursor < maxIssues-1 {
		// Move down within issues
		m.wsIssueCursor++
	} else if m.wsCursor < len(m.workstreams)-1 {
		// At last issue, go to next workstream header
		m.wsCursor++
		m.wsIssueCursor = -1
	}

	m.updateSelectedIssueFromWS()
}

// updateSelectedIssueFromWS updates selectedIssueID based on workstream cursor
func (m *LabelDashboardModel) updateSelectedIssueFromWS() {
	if len(m.workstreams) == 0 {
		m.selectedIssueID = ""
		return
	}

	ws := m.workstreams[m.wsCursor]
	isExpanded := m.wsExpanded[m.wsCursor]

	if m.wsIssueCursor >= 0 {
		// Get the issue at cursor position
		if m.wsTreeView && isExpanded {
			// Tree view - get from flattened tree
			wsCopy := ws
			treeRoots := m.buildWorkstreamTree(&wsCopy)
			flatNodes := m.flattenWSTree(treeRoots)
			if m.wsIssueCursor < len(flatNodes) {
				m.selectedIssueID = flatNodes[m.wsIssueCursor].Node.Issue.ID
			} else if len(flatNodes) > 0 {
				m.selectedIssueID = flatNodes[len(flatNodes)-1].Node.Issue.ID
			} else {
				m.selectedIssueID = ""
			}
		} else {
			// Flat view
			visibleCount := m.getVisibleIssueCount(m.wsCursor)
			if m.wsIssueCursor < visibleCount && m.wsIssueCursor < len(ws.Issues) {
				m.selectedIssueID = ws.Issues[m.wsIssueCursor].ID
			} else if len(ws.Issues) > 0 {
				m.selectedIssueID = ws.Issues[0].ID
			} else {
				m.selectedIssueID = ""
			}
		}
	} else if len(ws.Issues) > 0 {
		// Header selected, use first issue ID
		m.selectedIssueID = ws.Issues[0].ID
	} else {
		m.selectedIssueID = ""
	}

	// Ensure current position is visible
	m.ensureVisibleWS()
}

// ensureVisibleWS adjusts wsScroll to keep cursor visible
func (m *LabelDashboardModel) ensureVisibleWS() {
	// Calculate the line number of the current cursor position
	cursorLine := m.getWSCursorLine()

	// Match the visible lines calculation from View() and renderWorkstreamView()
	// View uses: visibleLines = m.height - 8
	// renderWorkstreamView uses: endIdx = wsScroll + visibleLines - 4
	// So effective visible content lines = (m.height - 8) - 4 = m.height - 12
	visibleLines := m.height - 12
	if visibleLines < 3 {
		visibleLines = 3
	}

	// Scroll up if cursor is above visible area
	if cursorLine < m.wsScroll {
		m.wsScroll = cursorLine
	}

	// Scroll down if cursor is below visible area (keep 1 line margin)
	if cursorLine >= m.wsScroll+visibleLines-1 {
		m.wsScroll = cursorLine - visibleLines + 2
	}

	// Clamp scroll to valid range
	totalLines := m.getTotalWSLines()
	maxScroll := totalLines - visibleLines + 1
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.wsScroll > maxScroll {
		m.wsScroll = maxScroll
	}
	if m.wsScroll < 0 {
		m.wsScroll = 0
	}
}

// getWSCursorLine calculates the line number of the current cursor in workstream view
func (m *LabelDashboardModel) getWSCursorLine() int {
	line := 0
	for wsIdx := 0; wsIdx < len(m.workstreams); wsIdx++ {
		ws := m.workstreams[wsIdx]
		isExpanded := m.wsExpanded[wsIdx]

		// Header line
		if wsIdx == m.wsCursor && m.wsIssueCursor < 0 {
			return line
		}
		line++

		// Calculate issue lines based on view mode
		var issueLineCount int
		if m.wsTreeView && isExpanded {
			// Tree view - count tree nodes
			wsCopy := ws
			treeRoots := m.buildWorkstreamTree(&wsCopy)
			flatNodes := m.flattenWSTree(treeRoots)
			issueLineCount = len(flatNodes)
		} else {
			// Flat view
			issueLineCount = m.getVisibleIssueCount(wsIdx)
		}

		if wsIdx == m.wsCursor && m.wsIssueCursor >= 0 {
			// Clamp cursor to valid range
			if m.wsIssueCursor >= issueLineCount {
				return line + issueLineCount - 1
			}
			return line + m.wsIssueCursor
		}
		line += issueLineCount

		// "+N more" line if collapsed with hidden issues (only in flat view)
		if !isExpanded && !m.wsTreeView && len(ws.Issues) > 3 {
			line++
		}

		// Empty line between workstreams
		line++
	}
	return line
}

// getTotalWSLines calculates total lines in workstream view
func (m *LabelDashboardModel) getTotalWSLines() int {
	line := 0
	for wsIdx := range m.workstreams {
		ws := m.workstreams[wsIdx]
		isExpanded := m.wsExpanded[wsIdx]

		line++ // header

		if m.wsTreeView && isExpanded {
			wsCopy := ws
			treeRoots := m.buildWorkstreamTree(&wsCopy)
			flatNodes := m.flattenWSTree(treeRoots)
			line += len(flatNodes)
		} else {
			line += m.getVisibleIssueCount(wsIdx)
			if !isExpanded && len(ws.Issues) > 3 {
				line++ // "+N more" line
			}
		}

		line++ // empty line
	}
	return line
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

// GetViewType returns the current view type
func (m *LabelDashboardModel) GetViewType() ViewType {
	return m.viewType
}

// IsWorkstreamView returns true if in workstream view mode
func (m *LabelDashboardModel) IsWorkstreamView() bool {
	return m.viewType == ViewTypeWorkstream
}

// ToggleViewType toggles between flat and workstream view
func (m *LabelDashboardModel) ToggleViewType() {
	if m.viewType == ViewTypeFlat {
		m.viewType = ViewTypeWorkstream
		// Initialize workstream cursor to first workstream header
		m.wsCursor = 0
		m.wsIssueCursor = -1
		m.updateSelectedIssueFromWS()
	} else {
		m.viewType = ViewTypeFlat
	}
}

// WorkstreamCount returns the number of workstreams detected
func (m *LabelDashboardModel) WorkstreamCount() int {
	return m.workstreamCount
}

// SetWorkstreams sets the detected workstreams
func (m *LabelDashboardModel) SetWorkstreams(ws []analysis.Workstream) {
	// In epic mode, sort workstream containing the entry epic to the front
	if m.viewMode == "epic" && m.epicID != "" && len(ws) > 1 {
		sort.SliceStable(ws, func(i, j int) bool {
			// Check if workstream i contains the entry epic
			for _, issue := range ws[i].Issues {
				if issue.ID == m.epicID {
					return true
				}
			}
			// Check if workstream j contains the entry epic
			for _, issue := range ws[j].Issues {
				if issue.ID == m.epicID {
					return false
				}
			}
			return false // Keep original order otherwise
		})
	}

	m.workstreams = ws
	m.workstreamCount = len(ws)
	m.wsExpanded = make(map[int]bool)   // Reset expansion state
	m.subWSExpanded = make(map[int]map[int]bool) // Reset sub-workstream expansion
	m.subWsCursor = make(map[int]int)   // Reset sub-workstream cursors
	m.wsSubdivided = false              // Reset subdivision state
	m.workstreamPtrs = analysis.WorkstreamPointers(ws) // Create pointers for mutation
}

// isEntryEpic checks if an issue ID is the entry point (for epic or bead view modes)
func (m *LabelDashboardModel) isEntryEpic(issueID string) bool {
	return (m.viewMode == "epic" || m.viewMode == "bead") && m.epicID != "" && issueID == m.epicID
}

// ToggleWorkstreamExpand toggles expansion of the current workstream
func (m *LabelDashboardModel) ToggleWorkstreamExpand() {
	if len(m.workstreams) == 0 {
		return
	}
	wasExpanded := m.wsExpanded[m.wsCursor]
	m.wsExpanded[m.wsCursor] = !wasExpanded

	// When collapsing, adjust cursor if it's beyond visible range
	if wasExpanded && m.wsIssueCursor >= 0 {
		newVisibleCount := m.getVisibleIssueCount(m.wsCursor)
		if m.wsIssueCursor >= newVisibleCount {
			// Move to last visible issue when collapsing
			if newVisibleCount > 0 {
				m.wsIssueCursor = newVisibleCount - 1
			} else {
				m.wsIssueCursor = -1
			}
		}
		m.updateSelectedIssueFromWS()
	}
}

// IsWorkstreamExpanded returns whether the given workstream is expanded
func (m *LabelDashboardModel) IsWorkstreamExpanded(wsIdx int) bool {
	return m.wsExpanded[wsIdx]
}

// CurrentWorkstreamName returns the name of the currently selected workstream
func (m *LabelDashboardModel) CurrentWorkstreamName() string {
	if len(m.workstreams) == 0 || m.wsCursor >= len(m.workstreams) {
		return ""
	}
	return m.workstreams[m.wsCursor].Name
}

// IsOnWorkstreamHeader returns true if cursor is on a workstream header (not an issue)
func (m *LabelDashboardModel) IsOnWorkstreamHeader() bool {
	return m.wsIssueCursor < 0
}

// ToggleWSTreeView toggles dependency tree view within workstreams
func (m *LabelDashboardModel) ToggleWSTreeView() {
	m.wsTreeView = !m.wsTreeView
}

// IsWSTreeView returns true if showing dependency tree in workstream view
func (m *LabelDashboardModel) IsWSTreeView() bool {
	return m.wsTreeView
}

// === Sub-Workstream Support ===

// ToggleSubdivision toggles subdivision mode on/off
func (m *LabelDashboardModel) ToggleSubdivision() {
	m.wsSubdivided = !m.wsSubdivided
	if m.wsSubdivided && len(m.workstreamPtrs) > 0 {
		// Apply subdivision to all workstreams
		opts := analysis.DefaultGroupingOptions()
		analysis.SubdivideAll(m.workstreamPtrs, m.primaryIDs, opts)
	}
	// Reset sub-workstream cursor when toggling
	m.subWsCursor = make(map[int]int)
	m.subWSExpanded = make(map[int]map[int]bool)
}

// IsSubdivided returns true if subdivision is active
func (m *LabelDashboardModel) IsSubdivided() bool {
	return m.wsSubdivided
}

// GetWorkstreamPointers returns pointers to workstreams for mutation
func (m *LabelDashboardModel) GetWorkstreamPointers() []*analysis.Workstream {
	return m.workstreamPtrs
}

// HasSubWorkstreams returns true if the given workstream has children
func (m *LabelDashboardModel) HasSubWorkstreams(wsIdx int) bool {
	if wsIdx >= len(m.workstreamPtrs) || m.workstreamPtrs[wsIdx] == nil {
		return false
	}
	return len(m.workstreamPtrs[wsIdx].SubWorkstreams) > 0
}

// IsSubWorkstreamExpanded returns whether a sub-workstream is expanded
func (m *LabelDashboardModel) IsSubWorkstreamExpanded(wsIdx, subIdx int) bool {
	if m.subWSExpanded[wsIdx] == nil {
		return false
	}
	return m.subWSExpanded[wsIdx][subIdx]
}

// ToggleSubWorkstreamExpand toggles expansion of a sub-workstream
func (m *LabelDashboardModel) ToggleSubWorkstreamExpand(wsIdx, subIdx int) {
	if m.subWSExpanded[wsIdx] == nil {
		m.subWSExpanded[wsIdx] = make(map[int]bool)
	}
	m.subWSExpanded[wsIdx][subIdx] = !m.subWSExpanded[wsIdx][subIdx]
}

// GetSubWorkstreamCursor returns the sub-workstream cursor for a workstream
func (m *LabelDashboardModel) GetSubWorkstreamCursor(wsIdx int) int {
	return m.subWsCursor[wsIdx]
}

// SetSubWorkstreamCursor sets the sub-workstream cursor for a workstream
func (m *LabelDashboardModel) SetSubWorkstreamCursor(wsIdx, cursor int) {
	m.subWsCursor[wsIdx] = cursor
}

// GetWsCursor returns the current workstream cursor position
func (m *LabelDashboardModel) GetWsCursor() int {
	return m.wsCursor
}

// NextWorkstream moves to the next workstream
func (m *LabelDashboardModel) NextWorkstream() {
	if len(m.workstreams) == 0 {
		return
	}
	if m.wsCursor < len(m.workstreams)-1 {
		m.wsCursor++
		m.wsIssueCursor = -1 // Go to header
		m.updateSelectedIssueFromWS()
	}
}

// PrevWorkstream moves to the previous workstream
func (m *LabelDashboardModel) PrevWorkstream() {
	if len(m.workstreams) == 0 {
		return
	}
	if m.wsCursor > 0 {
		m.wsCursor--
		m.wsIssueCursor = -1 // Go to header
		m.updateSelectedIssueFromWS()
	}
}

// GoToTop moves cursor to the first item
func (m *LabelDashboardModel) GoToTop() {
	if m.viewType == ViewTypeWorkstream && len(m.workstreams) > 1 {
		m.wsCursor = 0
		m.wsIssueCursor = -1 // Go to first workstream header
		m.updateSelectedIssueFromWS()
		return
	}
	// Flat view
	if len(m.flatNodes) > 0 {
		m.cursor = 0
		m.selectedIssueID = m.flatNodes[m.cursor].Node.Issue.ID
		m.scroll = 0
	}
}

// GoToBottom moves cursor to the last item
func (m *LabelDashboardModel) GoToBottom() {
	if m.viewType == ViewTypeWorkstream && len(m.workstreams) > 1 {
		m.wsCursor = len(m.workstreams) - 1
		// Go to last visible issue in last workstream
		issueCount := m.getVisibleIssueCount(m.wsCursor)
		if issueCount > 0 {
			m.wsIssueCursor = issueCount - 1
		} else {
			m.wsIssueCursor = -1
		}
		m.updateSelectedIssueFromWS() // This calls ensureVisibleWS which handles scroll
		return
	}
	// Flat view
	if len(m.flatNodes) > 0 {
		m.cursor = len(m.flatNodes) - 1
		m.selectedIssueID = m.flatNodes[m.cursor].Node.Issue.ID
		m.ensureVisible()
	}
}

// GetWorkstreams returns the detected workstreams
func (m *LabelDashboardModel) GetWorkstreams() []analysis.Workstream {
	return m.workstreams
}

// buildWorkstreamTree builds a dependency tree for issues within a workstream
func (m *LabelDashboardModel) buildWorkstreamTree(ws *analysis.Workstream) []*TreeNode {
	if len(ws.Issues) == 0 {
		return nil
	}

	// Build a set of issue IDs in this workstream
	wsIssueIDs := make(map[string]bool)
	wsIssueMap := make(map[string]*model.Issue)
	for i := range ws.Issues {
		wsIssueIDs[ws.Issues[i].ID] = true
		wsIssueMap[ws.Issues[i].ID] = &ws.Issues[i]
	}

	// Build downstream graph (what each issue unblocks) within workstream
	downstream := make(map[string][]string)
	hasUpstream := make(map[string]bool) // issues that are blocked by something in workstream

	for _, issue := range ws.Issues {
		for _, dep := range issue.Dependencies {
			if dep.Type == model.DepBlocks && wsIssueIDs[dep.DependsOnID] {
				// dep.DependsOnID blocks this issue
				downstream[dep.DependsOnID] = append(downstream[dep.DependsOnID], issue.ID)
				hasUpstream[issue.ID] = true
			}
		}
	}

	// Find root issues (not blocked by anything in workstream)
	var rootIssues []*model.Issue
	for _, issue := range ws.Issues {
		if !hasUpstream[issue.ID] {
			issueCopy := issue
			rootIssues = append(rootIssues, &issueCopy)
		}
	}

	// If no roots found (circular deps?), use all issues
	if len(rootIssues) == 0 {
		for i := range ws.Issues {
			rootIssues = append(rootIssues, &ws.Issues[i])
		}
	}

	// Build tree nodes
	seen := make(map[string]bool)
	maxDepth := int(m.dependencyDepth)
	if m.dependencyDepth == DepthAll {
		maxDepth = 100
	}

	var roots []*TreeNode
	for i, issue := range rootIssues {
		if seen[issue.ID] {
			continue
		}
		isLast := i == len(rootIssues)-1
		node := m.buildWSTreeNode(issue, 0, maxDepth, seen, isLast, nil, downstream, wsIssueMap)
		if node != nil {
			roots = append(roots, node)
		}
	}

	return roots
}

// buildWSTreeNode recursively builds a tree node for workstream tree view
func (m *LabelDashboardModel) buildWSTreeNode(issue *model.Issue, depth, maxDepth int, seen map[string]bool, isLast bool, parentPath []bool, downstream map[string][]string, wsIssueMap map[string]*model.Issue) *TreeNode {
	if seen[issue.ID] {
		return nil
	}
	seen[issue.ID] = true

	node := &TreeNode{
		Issue:       *issue,
		IsPrimary:   true, // All workstream issues are "primary"
		Depth:       depth,
		IsLastChild: isLast,
		ParentPath:  append([]bool{}, parentPath...),
	}

	// Add children (downstream issues) if within depth
	if depth < maxDepth-1 {
		childIDs := downstream[issue.ID]
		var children []*model.Issue
		for _, childID := range childIDs {
			if child, ok := wsIssueMap[childID]; ok && !seen[childID] {
				children = append(children, child)
			}
		}

		newParentPath := append(parentPath, isLast)
		for i, child := range children {
			childIsLast := i == len(children)-1
			childNode := m.buildWSTreeNode(child, depth+1, maxDepth, seen, childIsLast, newParentPath, downstream, wsIssueMap)
			if childNode != nil {
				node.Children = append(node.Children, childNode)
			}
		}
	}

	return node
}

// flattenWSTree converts workstream tree to flat list for display
func (m *LabelDashboardModel) flattenWSTree(roots []*TreeNode) []FlatNode {
	var flatNodes []FlatNode
	for _, root := range roots {
		m.flattenWSTreeNode(root, &flatNodes)
	}
	return flatNodes
}

func (m *LabelDashboardModel) flattenWSTreeNode(node *TreeNode, flatNodes *[]FlatNode) {
	prefix := m.buildTreePrefix(node)
	status := m.getIssueStatus(node.Issue)

	fn := FlatNode{
		Node:       node,
		TreePrefix: prefix,
		Status:     status,
		BlockedBy:  m.blockedByMap[node.Issue.ID],
	}
	*flatNodes = append(*flatNodes, fn)

	for _, child := range node.Children {
		m.flattenWSTreeNode(child, flatNodes)
	}
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
	switch m.viewMode {
	case "epic":
		modeIcon = "📋 "
	case "bead":
		modeIcon = "🔷 "
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

	// Render based on view type
	if m.viewType == ViewTypeWorkstream && len(m.workstreams) > 1 {
		// Render workstream view
		lines = append(lines, m.renderWorkstreamView(contentWidth, visibleLines, statsStyle)...)
	} else {
		// Render flat tree view
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
	}

	// Footer
	lines = append(lines, "")
	footerStyle := t.Renderer.NewStyle().Foreground(t.Subtext).Italic(true)

	// Show view type indicator and toggle hint
	viewIndicator := "[flat]"
	toggleHint := "w: workstreams"
	navHint := "n/N: section"
	enterHint := "enter: jump"
	treeHint := ""
	if m.viewType == ViewTypeWorkstream && len(m.workstreams) > 1 {
		viewIndicator = fmt.Sprintf("[workstreams: %d]", m.workstreamCount)
		toggleHint = "w: flat view"
		navHint = "n/N: workstream"
		enterHint = "enter: expand"
		if m.wsTreeView {
			treeHint = " • D: list • d: depth"
		} else {
			treeHint = " • d: tree"
		}
	}

	lines = append(lines, footerStyle.Render(fmt.Sprintf("%s • j/k: nav • g/G: top/bottom • %s • %s • %s%s • esc: back", viewIndicator, toggleHint, navHint, enterHint, treeHint)))

	return strings.Join(lines, "\n")
}

// renderWorkstreamView renders issues grouped by workstream
func (m *LabelDashboardModel) renderWorkstreamView(contentWidth, visibleLines int, statsStyle lipgloss.Style) []string {
	t := m.theme
	var allLines []string

	wsHeaderStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)
	wsHeaderSelectedStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true).Background(t.Highlight)
	wsSubStyle := t.Renderer.NewStyle().Foreground(t.Subtext)
	issueStyle := t.Renderer.NewStyle()
	issueSelectedStyle := t.Renderer.NewStyle().Bold(true)
	readyStyle := t.Renderer.NewStyle().Foreground(t.Open)
	blockedStyle := t.Renderer.NewStyle().Foreground(t.Blocked)
	closedStyle := t.Renderer.NewStyle().Foreground(t.Closed)
	inProgStyle := t.Renderer.NewStyle().Foreground(t.InProgress)

	// Build all lines first, then apply scroll
	for wsIdx, ws := range m.workstreams {
		// Check if this workstream header is selected
		isHeaderSelected := wsIdx == m.wsCursor && m.wsIssueCursor < 0
		isExpanded := m.wsExpanded[wsIdx]

		// Workstream header with progress
		progressPct := int(ws.Progress * 100)
		progressBar := m.renderMiniProgressBar(ws.Progress, 8)

		// Status counts
		statusCounts := fmt.Sprintf("○%d ●%d ◈%d ✓%d",
			ws.ReadyCount, ws.InProgressCount, ws.BlockedCount, ws.ClosedCount)

		// Expand/collapse indicator
		expandIcon := "▶"
		if isExpanded {
			expandIcon = "▼"
		}

		// Selection indicator
		selectPrefix := "  "
		headerStyle := wsHeaderStyle
		if isHeaderSelected {
			selectPrefix = "▸ "
			headerStyle = wsHeaderSelectedStyle
		}

		// Show sub-workstream indicator if present
		subWsIndicator := ""
		if m.wsSubdivided && wsIdx < len(m.workstreamPtrs) && m.workstreamPtrs[wsIdx] != nil {
			subCount := len(m.workstreamPtrs[wsIdx].SubWorkstreams)
			if subCount > 0 {
				subWsIndicator = fmt.Sprintf(" [%d sub]", subCount)
			}
		}

		wsLine := fmt.Sprintf("%s%s %s %s %d%% %s%s",
			selectPrefix,
			expandIcon,
			headerStyle.Render(ws.Name),
			progressBar,
			progressPct,
			wsSubStyle.Render(statusCounts),
			wsSubStyle.Render(subWsIndicator))
		allLines = append(allLines, wsLine)

		// Render sub-workstreams when subdivision is active and expanded
		if m.wsSubdivided && isExpanded && wsIdx < len(m.workstreamPtrs) && m.workstreamPtrs[wsIdx] != nil {
			for subIdx, subWs := range m.workstreamPtrs[wsIdx].SubWorkstreams {
				if subWs == nil {
					continue
				}
				subProgress := int(subWs.Progress * 100)
				subStatusCounts := fmt.Sprintf("○%d ●%d ◈%d ✓%d",
					subWs.ReadyCount, subWs.InProgressCount, subWs.BlockedCount, subWs.ClosedCount)
				subLine := fmt.Sprintf("      %s (%d%%) %s",
					wsSubStyle.Render("├─ "+subWs.Name),
					subProgress,
					wsSubStyle.Render(subStatusCounts))
				_ = subIdx // Will be used for sub-workstream selection in future
				allLines = append(allLines, subLine)
			}
		}

		// Render issues - either as tree or flat list
		if m.wsTreeView && isExpanded {
			// Tree view for expanded workstreams
			wsCopy := ws
			treeRoots := m.buildWorkstreamTree(&wsCopy)
			flatNodes := m.flattenWSTree(treeRoots)

			for i, fn := range flatNodes {
				// Check if this issue is selected
				isIssueSelected := wsIdx == m.wsCursor && i == m.wsIssueCursor
				isEpicEntry := m.isEntryEpic(fn.Node.Issue.ID)

				// Format issue line with tree prefix
				var statusIcon string
				var style lipgloss.Style
				if isEpicEntry {
					// Entry epic gets distinct diamond icon
					statusIcon = "◆"
					style = t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)
				} else {
					switch fn.Node.Issue.Status {
					case model.StatusClosed:
						statusIcon = "✓"
						style = closedStyle
					case model.StatusBlocked:
						statusIcon = "◈"
						style = blockedStyle
					case model.StatusInProgress:
						statusIcon = "●"
						style = inProgStyle
					default:
						statusIcon = "○"
						style = readyStyle
					}
				}

				// Selection indicator
				issuePrefix := "    "
				idStyle := issueStyle
				titleStyle := issueStyle
				if isEpicEntry {
					// Entry epic: always bold primary
					idStyle = issueSelectedStyle.Foreground(t.Primary)
					titleStyle = issueSelectedStyle.Foreground(t.Primary)
				}
				if isIssueSelected {
					issuePrefix = "  ▸ "
					idStyle = issueSelectedStyle.Foreground(t.Primary)
					titleStyle = issueSelectedStyle
				}

				// Tree prefix
				treePrefix := ""
				if fn.TreePrefix != "" {
					treePrefix = wsSubStyle.Render(fn.TreePrefix) + " "
				}

				title := truncateRunesHelper(fn.Node.Issue.Title, contentWidth-25-len(fn.TreePrefix), "…")
				epicBadge := ""
				if isEpicEntry {
					epicBadge = wsSubStyle.Render(" [EPIC]")
				}
				issueLine := fmt.Sprintf("%s%s %s%s %s%s",
					issuePrefix,
					style.Render(statusIcon),
					treePrefix,
					idStyle.Render(fn.Node.Issue.ID),
					titleStyle.Render(title),
					epicBadge)
				allLines = append(allLines, issueLine)
			}
		} else {
			// Flat list view
			maxIssues := m.getVisibleIssueCount(wsIdx)

			for i, issue := range ws.Issues {
				if i >= maxIssues {
					break
				}

				// Check if this issue is selected
				isIssueSelected := wsIdx == m.wsCursor && i == m.wsIssueCursor
				isEpicEntry := m.isEntryEpic(issue.ID)

				// Format issue line
				var statusIcon string
				var style lipgloss.Style
				if isEpicEntry {
					// Entry epic gets distinct diamond icon
					statusIcon = "◆"
					style = t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)
				} else {
					switch issue.Status {
					case model.StatusClosed:
						statusIcon = "✓"
						style = closedStyle
					case model.StatusBlocked:
						statusIcon = "◈"
						style = blockedStyle
					case model.StatusInProgress:
						statusIcon = "●"
						style = inProgStyle
					default:
						statusIcon = "○"
						style = readyStyle
					}
				}

				// Selection indicator
				issuePrefix := "    "
				idStyle := issueStyle
				titleStyle := issueStyle
				if isEpicEntry {
					// Entry epic: always bold primary
					idStyle = issueSelectedStyle.Foreground(t.Primary)
					titleStyle = issueSelectedStyle.Foreground(t.Primary)
				}
				if isIssueSelected {
					issuePrefix = "  ▸ "
					idStyle = issueSelectedStyle.Foreground(t.Primary)
					titleStyle = issueSelectedStyle
				}

				title := truncateRunesHelper(issue.Title, contentWidth-20, "…")
				epicBadge := ""
				if isEpicEntry {
					epicBadge = wsSubStyle.Render(" [EPIC]")
				}
				issueLine := fmt.Sprintf("%s%s %s %s%s",
					issuePrefix,
					style.Render(statusIcon),
					idStyle.Render(issue.ID),
					titleStyle.Render(title),
					epicBadge)
				allLines = append(allLines, issueLine)
			}

			// Show "+N more" hint for collapsed workstreams
			if !isExpanded && len(ws.Issues) > 3 {
				remaining := len(ws.Issues) - 3
				allLines = append(allLines, wsSubStyle.Render(fmt.Sprintf("        ... +%d more (enter to expand)", remaining)))
			}
		}

		allLines = append(allLines, "") // Empty line between workstreams
	}

	// Apply scroll offset
	var lines []string
	viewModeStr := "list"
	if m.wsTreeView {
		viewModeStr = fmt.Sprintf("tree [depth:%s]", m.dependencyDepth.String())
	}
	if m.wsSubdivided {
		viewModeStr += " [subdivided]"
	}
	lines = append(lines, wsSubStyle.Render(fmt.Sprintf("  %d workstreams (%s):", len(m.workstreams), viewModeStr)))
	lines = append(lines, "")

	// Calculate visible window
	startIdx := m.wsScroll
	endIdx := m.wsScroll + visibleLines - 4 // Account for header lines
	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx > len(allLines) {
		endIdx = len(allLines)
	}
	if startIdx > len(allLines) {
		startIdx = len(allLines)
	}

	// Add visible lines
	for i := startIdx; i < endIdx; i++ {
		lines = append(lines, allLines[i])
	}

	// Show scroll indicator if needed
	totalLines := len(allLines)
	if totalLines > visibleLines-4 {
		scrollInfo := fmt.Sprintf("  [%d-%d of %d lines]", startIdx+1, endIdx, totalLines)
		lines = append(lines, wsSubStyle.Render(scrollInfo))
	}

	return lines
}

// renderMiniProgressBar renders a small progress bar
func (m *LabelDashboardModel) renderMiniProgressBar(progress float64, width int) string {
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

	// Primary/context/entry-epic indicator
	var indicator string
	if node.IsEntryEpic {
		// Entry epic gets a distinct diamond icon
		indicator = t.Renderer.NewStyle().Foreground(t.Primary).Bold(true).Render("◆")
	} else if node.IsPrimary {
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

	if node.IsEntryEpic {
		// Entry epic: bold with primary color, stands out
		idStyle = idStyle.Foreground(t.Primary).Bold(true)
		titleStyle = titleStyle.Foreground(t.Primary).Bold(true)
	} else if isSelected {
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

	// Entry epic badge
	epicBadge := ""
	if node.IsEntryEpic {
		epicBadge = t.Renderer.NewStyle().Foreground(t.Subtext).Render(" [EPIC]")
	}

	// Status indicator for blocked items
	statusSuffix := ""
	if fn.Status == "blocked" && fn.BlockedBy != "" {
		blockerStyle := t.Renderer.NewStyle().Foreground(t.Blocked)
		statusSuffix = blockerStyle.Render(" ◄ " + fn.BlockedBy)
	}

	return fmt.Sprintf("%s%s %s%s %s%s%s",
		selectPrefix,
		indicator,
		treePrefix,
		idStyle.Render(node.Issue.ID),
		titleStyle.Render(title),
		epicBadge,
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

// DumpToFile writes workstream information to a text file
func (m *LabelDashboardModel) DumpToFile() (string, error) {
	filename := fmt.Sprintf("%s-dump.txt", m.labelName)

	var buf strings.Builder

	// Header
	buf.WriteString(fmt.Sprintf("Label Dashboard Dump: %s\n", m.labelName))
	buf.WriteString(fmt.Sprintf("Generated: %s\n", time.Now().Format(time.RFC3339)))
	buf.WriteString(strings.Repeat("=", 60) + "\n\n")

	// Summary stats
	buf.WriteString("SUMMARY\n")
	buf.WriteString(strings.Repeat("-", 40) + "\n")
	buf.WriteString(fmt.Sprintf("  Total: %d issues (%d primary, %d context)\n",
		m.totalCount, m.primaryCount, m.contextCount))
	buf.WriteString(fmt.Sprintf("  Ready: %d, Blocked: %d, In Progress: %d, Closed: %d\n",
		m.readyCount, m.blockedCount,
		m.totalCount-m.readyCount-m.blockedCount-m.closedCount, m.closedCount))
	progress := 0.0
	if m.totalCount > 0 {
		progress = float64(m.closedCount) / float64(m.totalCount)
	}
	buf.WriteString(fmt.Sprintf("  Progress: %d%%\n", int(progress*100)))
	buf.WriteString(fmt.Sprintf("  Dependency Depth: %s\n\n", m.dependencyDepth.String()))

	// Workstream hierarchy (if workstreams exist)
	if len(m.workstreamPtrs) > 0 {
		buf.WriteString("WORKSTREAMS (Hierarchical)\n")
		buf.WriteString(strings.Repeat("-", 40) + "\n")
		for _, ws := range m.workstreamPtrs {
			if ws != nil {
				buf.WriteString(m.dumpWorkstreamTree(ws, 0))
			}
		}
		buf.WriteString("\n")
	}

	// Flat output by depth
	buf.WriteString("ISSUES BY DEPTH\n")
	buf.WriteString(strings.Repeat("-", 40) + "\n")
	buf.WriteString(m.dumpFlatByDepth())

	return filename, os.WriteFile(filename, []byte(buf.String()), 0644)
}

// dumpWorkstreamTree recursively dumps a workstream and its sub-workstreams
func (m *LabelDashboardModel) dumpWorkstreamTree(ws *analysis.Workstream, indent int) string {
	var buf strings.Builder
	prefix := strings.Repeat("  ", indent)

	// Workstream header
	buf.WriteString(fmt.Sprintf("%s[%s] %s (%d issues, %d%% done)\n",
		prefix, ws.ID, ws.Name, len(ws.Issues), int(ws.Progress*100)))
	buf.WriteString(fmt.Sprintf("%s  Ready: %d, Blocked: %d, In Progress: %d, Closed: %d\n",
		prefix, ws.ReadyCount, ws.BlockedCount, ws.InProgressCount, ws.ClosedCount))

	if ws.GroupedBy != "" {
		buf.WriteString(fmt.Sprintf("%s  Grouped by: %s\n", prefix, ws.GroupedBy))
	}

	// Issues in this workstream
	if len(ws.Issues) > 0 {
		buf.WriteString(fmt.Sprintf("%s  Issues:\n", prefix))
		for _, issue := range ws.Issues {
			buf.WriteString(fmt.Sprintf("%s    - [%s] %s (%s)\n",
				prefix, issue.ID, issue.Title, issue.Status))
		}
	}

	// Recurse into sub-workstreams
	if len(ws.SubWorkstreams) > 0 {
		buf.WriteString(fmt.Sprintf("%s  Sub-workstreams (%d):\n", prefix, len(ws.SubWorkstreams)))
		for _, subWs := range ws.SubWorkstreams {
			buf.WriteString(m.dumpWorkstreamTree(subWs, indent+1))
		}
	}

	buf.WriteString("\n")
	return buf.String()
}

// dumpFlatByDepth groups issues by their depth in the tree
func (m *LabelDashboardModel) dumpFlatByDepth() string {
	var buf strings.Builder

	// Group all issues by their depth
	depthMap := make(map[int][]model.Issue)
	maxDepth := 0

	for _, fn := range m.flatNodes {
		depth := fn.Node.Depth
		depthMap[depth] = append(depthMap[depth], fn.Node.Issue)
		if depth > maxDepth {
			maxDepth = depth
		}
	}

	for depth := 0; depth <= maxDepth; depth++ {
		issues := depthMap[depth]
		if len(issues) > 0 {
			buf.WriteString(fmt.Sprintf("\nDepth %d (%d issues):\n", depth, len(issues)))
			for _, issue := range issues {
				statusStr := issue.Status
				if statusStr == "" {
					statusStr = "open"
				}
				buf.WriteString(fmt.Sprintf("  [%s] %s (%s)\n", issue.ID, issue.Title, statusStr))
			}
		}
	}

	if len(m.flatNodes) == 0 {
		buf.WriteString("\n  No issues in current view\n")
	}

	return buf.String()
}

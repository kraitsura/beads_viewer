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

// ScopeMode represents how multiple scope labels are combined
type ScopeMode int

const (
	ScopeModeUnion        ScopeMode = iota // Issue appears if it has ANY of the scope labels
	ScopeModeIntersection                  // Issue appears only if it has ALL scope labels
)

// String returns display string for scope mode
func (s ScopeMode) String() string {
	if s == ScopeModeIntersection {
		return "Intersection (ALL)"
	}
	return "Union (ANY)"
}

// ShortString returns a short display string for scope mode
func (s ScopeMode) ShortString() string {
	if s == ScopeModeIntersection {
		return "∩ ALL"
	}
	return "∪ ANY"
}

// String returns display string for depth
func (d DepthOption) String() string {
	if d == DepthAll {
		return "All"
	}
	return fmt.Sprintf("%d", d)
}

// LensTreeNode represents a node in the dependency tree
type LensTreeNode struct {
	Issue         model.Issue
	IsPrimary     bool             // true if has the label
	IsEntryEpic   bool             // true if this is the entry point epic (when viewing an epic)
	Children      []*LensTreeNode  // downstream issues (what this unblocks)
	Depth         int              // depth in tree (0 = root)
	RelativeDepth int              // depth relative to entry point: -N upstream, 0 center, +N downstream
	IsLastChild   bool             // for rendering tree lines
	ParentPath    []bool           // track which ancestors are last children (for tree lines)
	IsUpstream    bool             // true if this is a blocker of the entry point
}

// LensFlatNode is a flattened tree node for display/navigation
type LensFlatNode struct {
	Node       *LensTreeNode
	TreePrefix string // rendered tree prefix (├─►, └─►, etc.)
	Status     string // ready, blocked, in_progress, closed
	BlockedBy  string // ID of blocker if blocked
}

// LensDashboardModel represents the label dashboard view
type LensDashboardModel struct {
	// Data
	labelName string
	viewMode  string // "label" or "epic"
	epicID    string // Only set if viewMode == "epic"

	// Tree data
	roots       []*LensTreeNode          // Root nodes (ready issues or all primaries at depth 1)
	flatNodes   []LensFlatNode           // Flattened for display
	allIssues   []model.Issue        // Reference to all issues
	issueMap    map[string]*model.Issue
	primaryIDs  map[string]bool      // Issues that have the label (expanded via parent-child)
	directPrimaryIDs map[string]bool // Issues that directly have the label (not expanded)
	blockedByMap map[string]string   // issue ID -> blocking issue ID

	// Ego-centered view (for epic/bead modes)
	centeredMode  bool        // true = ego-centered layout, false = flat tree
	upstreamNodes []LensFlatNode  // Blockers of the entry point (shown above)
	egoNode       *LensFlatNode   // The entry point itself (center)
	// roots/flatNodes used for downstream (shown below)

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

	// Scope filtering (multi-label selection)
	scopeLabels []string  // Currently selected scope labels (empty = no scope)
	scopeMode   ScopeMode // Union (ANY) or Intersection (ALL) mode

	// Scope input modal
	showScopeInput bool   // True when scope input modal is visible
	scopeInput     string // Current text in scope input
}

// NewLensDashboardModel creates a new label dashboard for the given label
func NewLensDashboardModel(labelName string, allIssues []model.Issue, issueMap map[string]*model.Issue, theme Theme) LensDashboardModel {
	m := LensDashboardModel{
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

// NewBeadLensModel creates a dashboard for any issue and its descendants/blocked issues.
// Unlike epic mode which only shows parent-child descendants, bead mode also includes
// issues that are blocked by the entry issue (downstream dependency graph).
func NewBeadLensModel(issueID string, allIssues []model.Issue, issueMap map[string]*model.Issue, theme Theme) LensDashboardModel {
	issue, exists := issueMap[issueID]
	if !exists {
		// Return empty dashboard if issue not found
		return LensDashboardModel{
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

	m := LensDashboardModel{
		labelName:        issue.Title,
		viewMode:         "bead",
		epicID:           issueID, // Reuse epicID field for entry point
		centeredMode:     true,    // Enable ego-centered view by default for bead mode
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

// NewEpicLensModel creates a dashboard for an epic's children
func NewEpicLensModel(epicID string, epicTitle string, allIssues []model.Issue, issueMap map[string]*model.Issue, theme Theme) LensDashboardModel {
	m := LensDashboardModel{
		labelName:        epicTitle,
		viewMode:         "epic",
		epicID:           epicID,
		centeredMode:     true, // Enable ego-centered view by default for epic mode
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
	// IMPORTANT: Always include the entry epic itself at ALL depth levels
	// This ensures the entry point is visible and highlighted in the view

	// Depth1 = entry epic + direct children
	result[Depth1] = make(map[string]bool)
	result[Depth1][epicID] = true // Entry point always included
	for id := range descendantsByLevel[1] {
		result[Depth1][id] = true
	}

	// Depth2 = entry epic + levels 1-2
	result[Depth2] = make(map[string]bool)
	result[Depth2][epicID] = true // Entry point always included
	for level := 1; level <= 2; level++ {
		for id := range descendantsByLevel[level] {
			result[Depth2][id] = true
		}
	}

	// Depth3 = entry epic + levels 1-3
	result[Depth3] = make(map[string]bool)
	result[Depth3][epicID] = true // Entry point always included
	for level := 1; level <= 3; level++ {
		for id := range descendantsByLevel[level] {
			result[Depth3][id] = true
		}
	}

	// DepthAll = entry epic + all levels
	result[DepthAll] = make(map[string]bool)
	result[DepthAll][epicID] = true // Entry point always included
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
	// IMPORTANT: Always include the entry bead itself at ALL depth levels
	// This ensures the entry point is visible and highlighted in the view

	// Depth1 = entry bead + direct children + directly blocked
	result[Depth1] = make(map[string]bool)
	result[Depth1][beadID] = true // Entry point always included
	for id := range descendantsByLevel[1] {
		result[Depth1][id] = true
	}

	// Depth2 = entry bead + levels 1-2
	result[Depth2] = make(map[string]bool)
	result[Depth2][beadID] = true // Entry point always included
	for level := 1; level <= 2; level++ {
		for id := range descendantsByLevel[level] {
			result[Depth2][id] = true
		}
	}

	// Depth3 = entry bead + levels 1-3
	result[Depth3] = make(map[string]bool)
	result[Depth3][beadID] = true // Entry point always included
	for level := 1; level <= 3; level++ {
		for id := range descendantsByLevel[level] {
			result[Depth3][id] = true
		}
	}

	// DepthAll = entry bead + all levels
	result[DepthAll] = make(map[string]bool)
	result[DepthAll][beadID] = true // Entry point always included
	for level := range descendantsByLevel {
		for id := range descendantsByLevel[level] {
			result[DepthAll][id] = true
		}
	}

	return result
}

// buildGraphs builds the upstream and downstream dependency graphs
func (m *LensDashboardModel) buildGraphs() {
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
func (m *LensDashboardModel) buildTree() {
	m.roots = nil
	m.flatNodes = nil
	m.upstreamNodes = nil
	m.egoNode = nil
	m.totalCount = 0
	m.primaryCount = 0
	m.contextCount = 0
	m.readyCount = 0
	m.blockedCount = 0
	m.closedCount = 0

	// For epic/bead modes with centered view, use ego-centered tree building
	if m.centeredMode && (m.viewMode == "epic" || m.viewMode == "bead") && m.epicID != "" {
		m.buildEgoCenteredTree()
		return
	}

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
func (m *LensDashboardModel) buildTreeNode(issue model.Issue, depth, maxDepth int, seen map[string]bool, isLast bool, parentPath []bool) *LensTreeNode {
	if seen[issue.ID] {
		return nil
	}
	seen[issue.ID] = true

	// Use depth-appropriate primary IDs for IsPrimary determination
	depthPrimaryIDs := m.GetPrimaryIDsForDepth()

	node := &LensTreeNode{
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
func (m *LensDashboardModel) addUpstreamContextBlockers(seen map[string]bool, maxDepth int) {
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
func (m *LensDashboardModel) buildContextBlockerNode(issue model.Issue, depth, maxDepth int, seen map[string]bool, isLast bool, parentPath []bool, contextBlockerSet map[string]bool) *LensTreeNode {
	if seen[issue.ID] {
		return nil
	}
	seen[issue.ID] = true

	// Use depth-appropriate primary IDs for IsPrimary determination
	depthPrimaryIDs := m.GetPrimaryIDsForDepth()

	node := &LensTreeNode{
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
func (m *LensDashboardModel) flattenTree() {
	m.flatNodes = nil
	for _, root := range m.roots {
		m.flattenNode(root)
	}
}

// flattenNode recursively flattens a node and its children
func (m *LensDashboardModel) flattenNode(node *LensTreeNode) {
	prefix := m.buildTreePrefix(node)
	status := m.getIssueStatus(node.Issue)

	fn := LensFlatNode{
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

// buildEgoCenteredTree builds a tree structure centered on the entry point (epic/bead).
// Layout: Upstream blockers → Entry point (center) → Downstream descendants
func (m *LensDashboardModel) buildEgoCenteredTree() {
	entryIssue, exists := m.issueMap[m.epicID]
	if !exists {
		return
	}

	depthPrimaryIDs := m.GetPrimaryIDsForDepth()
	maxDepth := int(m.dependencyDepth)
	if m.dependencyDepth == DepthAll {
		maxDepth = 100
	}

	// Track what we've seen
	seen := make(map[string]bool)

	// 1. Build the ego/center node
	egoTreeNode := &LensTreeNode{
		Issue:         *entryIssue,
		IsPrimary:     true,
		IsEntryEpic:   true,
		Depth:         0,
		RelativeDepth: 0,
		IsLastChild:   true,
		ParentPath:    nil,
	}
	m.egoNode = &LensFlatNode{
		Node:       egoTreeNode,
		TreePrefix: "",
		Status:     m.getIssueStatus(*entryIssue),
		BlockedBy:  m.blockedByMap[entryIssue.ID],
	}
	seen[m.epicID] = true
	m.totalCount++
	m.primaryCount++
	if m.egoNode.Status == "ready" {
		m.readyCount++
	} else if m.egoNode.Status == "blocked" {
		m.blockedCount++
	} else if m.egoNode.Status == "closed" {
		m.closedCount++
	}

	// 2. Build upstream blockers (issues that block the entry point)
	// These are shown ABOVE the center
	m.upstreamNodes = nil
	blockerIDs := m.upstream[m.epicID]
	var blockerIssues []model.Issue
	for _, blockerID := range blockerIDs {
		if blocker, ok := m.issueMap[blockerID]; ok {
			if blocker.Status != model.StatusClosed { // Only show open blockers
				blockerIssues = append(blockerIssues, *blocker)
			}
		}
	}

	// Sort blockers by status then priority
	sort.Slice(blockerIssues, func(i, j int) bool {
		si := m.getStatusOrder(blockerIssues[i])
		sj := m.getStatusOrder(blockerIssues[j])
		if si != sj {
			return si < sj
		}
		return blockerIssues[i].Priority < blockerIssues[j].Priority
	})

	for i, blocker := range blockerIssues {
		if seen[blocker.ID] {
			continue
		}
		seen[blocker.ID] = true

		node := &LensTreeNode{
			Issue:         blocker,
			IsPrimary:     depthPrimaryIDs[blocker.ID],
			IsEntryEpic:   false,
			Depth:         0,
			RelativeDepth: -1, // Upstream = negative depth
			IsLastChild:   i == len(blockerIssues)-1,
			IsUpstream:    true,
		}

		fn := LensFlatNode{
			Node:       node,
			TreePrefix: "",
			Status:     m.getIssueStatus(blocker),
			BlockedBy:  m.blockedByMap[blocker.ID],
		}
		m.upstreamNodes = append(m.upstreamNodes, fn)

		m.totalCount++
		if node.IsPrimary {
			m.primaryCount++
		} else {
			m.contextCount++
		}
		if fn.Status == "ready" {
			m.readyCount++
		} else if fn.Status == "blocked" {
			m.blockedCount++
		} else if fn.Status == "closed" {
			m.closedCount++
		}
	}

	// 3. Build downstream tree (children and dependents)
	// These are shown BELOW the center
	m.roots = nil
	m.flatNodes = nil

	// Get all direct children/dependents of the entry point
	downstreamIDs := m.downstream[m.epicID]
	var downstreamIssues []model.Issue
	for _, childID := range downstreamIDs {
		if child, ok := m.issueMap[childID]; ok {
			if !seen[childID] {
				downstreamIssues = append(downstreamIssues, *child)
			}
		}
	}

	// Sort by status then priority
	sort.Slice(downstreamIssues, func(i, j int) bool {
		si := m.getStatusOrder(downstreamIssues[i])
		sj := m.getStatusOrder(downstreamIssues[j])
		if si != sj {
			return si < sj
		}
		return downstreamIssues[i].Priority < downstreamIssues[j].Priority
	})

	// Build tree from each downstream issue
	for i, issue := range downstreamIssues {
		if seen[issue.ID] {
			continue
		}
		isLast := i == len(downstreamIssues)-1
		node := m.buildCenteredTreeNode(issue, 1, maxDepth, seen, isLast, nil, depthPrimaryIDs)
		if node != nil {
			m.roots = append(m.roots, node)
		}
	}

	// Flatten downstream tree
	for _, root := range m.roots {
		m.flattenNode(root)
	}

	// Update selected issue
	totalNodes := len(m.upstreamNodes) + 1 + len(m.flatNodes) // upstream + ego + downstream
	if totalNodes > 0 {
		// Default cursor to the ego node (after upstream section)
		if m.cursor < 0 || m.cursor >= totalNodes {
			m.cursor = len(m.upstreamNodes) // Point to ego node
		}
		m.selectedIssueID = m.getSelectedIDForCenteredMode()
	}
}

// buildCenteredTreeNode builds a tree node with relative depth tracking
func (m *LensDashboardModel) buildCenteredTreeNode(issue model.Issue, relDepth, maxDepth int, seen map[string]bool, isLast bool, parentPath []bool, depthPrimaryIDs map[string]bool) *LensTreeNode {
	if seen[issue.ID] {
		return nil
	}
	seen[issue.ID] = true

	node := &LensTreeNode{
		Issue:         issue,
		IsPrimary:     depthPrimaryIDs[issue.ID],
		IsEntryEpic:   false,
		Depth:         relDepth,
		RelativeDepth: relDepth,
		IsLastChild:   isLast,
		ParentPath:    append([]bool{}, parentPath...),
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

	// Add children if within depth
	if relDepth < maxDepth {
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
			childNode := m.buildCenteredTreeNode(child, relDepth+1, maxDepth, seen, childIsLast, newParentPath, depthPrimaryIDs)
			if childNode != nil {
				node.Children = append(node.Children, childNode)
			}
		}
	}

	return node
}

// getSelectedIDForCenteredMode returns the selected issue ID based on cursor position in centered mode
func (m *LensDashboardModel) getSelectedIDForCenteredMode() string {
	upstreamLen := len(m.upstreamNodes)

	if m.cursor < upstreamLen {
		// In upstream section
		return m.upstreamNodes[m.cursor].Node.Issue.ID
	} else if m.cursor == upstreamLen {
		// On ego node
		if m.egoNode != nil {
			return m.egoNode.Node.Issue.ID
		}
		return ""
	} else {
		// In downstream section
		downstreamIdx := m.cursor - upstreamLen - 1
		if downstreamIdx < len(m.flatNodes) {
			return m.flatNodes[downstreamIdx].Node.Issue.ID
		}
		return ""
	}
}

// getTotalCenteredNodeCount returns total navigable nodes in centered mode
func (m *LensDashboardModel) getTotalCenteredNodeCount() int {
	egoCount := 0
	if m.egoNode != nil {
		egoCount = 1
	}
	return len(m.upstreamNodes) + egoCount + len(m.flatNodes)
}

// buildTreePrefix builds the tree line prefix for a node
// Uses refined minimal connectors: ├─ └─ │
func (m *LensDashboardModel) buildTreePrefix(node *LensTreeNode) string {
	if node.Depth == 0 {
		return ""
	}

	var prefix strings.Builder

	// Build prefix from parent path with refined spacing
	for i := 0; i < len(node.ParentPath); i++ {
		if node.ParentPath[i] {
			prefix.WriteString("  ") // parent was last child, no line
		} else {
			prefix.WriteString("│ ") // parent has siblings, continue line
		}
	}

	// Refined minimal connectors
	if node.IsLastChild {
		prefix.WriteString("└─")
	} else {
		prefix.WriteString("├─")
	}

	return prefix.String()
}

// getIssueStatus returns the effective status of an issue
func (m *LensDashboardModel) getIssueStatus(issue model.Issue) string {
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
func (m *LensDashboardModel) getStatusOrder(issue model.Issue) int {
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
func (m *LensDashboardModel) CycleDepth() {
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

// ToggleCenteredMode toggles between ego-centered and flat view modes
// Only applicable for epic/bead modes
func (m *LensDashboardModel) ToggleCenteredMode() {
	if m.viewMode != "epic" && m.viewMode != "bead" {
		return // Only toggle for epic/bead modes
	}

	m.centeredMode = !m.centeredMode
	m.cursor = 0 // Reset cursor when switching modes
	m.scroll = 0

	// Rebuild tree with new mode
	m.buildTree()

	// Recompute workstreams
	m.recomputeWorkstreams()
}

// IsCenteredMode returns whether the dashboard is in ego-centered mode
func (m *LensDashboardModel) IsCenteredMode() bool {
	return m.centeredMode && (m.viewMode == "epic" || m.viewMode == "bead")
}

// GetPrimaryIDsForDepth returns the appropriate primaryIDs for the current depth.
// At Depth1, returns only direct label matches.
// At Depth2+, returns expanded set including descendants.
func (m *LensDashboardModel) GetPrimaryIDsForDepth() map[string]bool {
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
func (m *LensDashboardModel) recomputeWorkstreams() {
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
func (m *LensDashboardModel) getDisplayIssues() []model.Issue {
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
func (m *LensDashboardModel) GetDepth() DepthOption {
	return m.dependencyDepth
}

// SetDepth sets the dependency depth and rebuilds the tree
func (m *LensDashboardModel) SetDepth(depth DepthOption) {
	m.dependencyDepth = depth
	m.buildTree()
	m.recomputeWorkstreams()
}

// ══════════════════════════════════════════════════════════════════════════════
// SCOPE MANAGEMENT - Multi-label filtering with union/intersection
// ══════════════════════════════════════════════════════════════════════════════

// GetScopeLabels returns the currently selected scope labels
func (m *LensDashboardModel) GetScopeLabels() []string {
	return m.scopeLabels
}

// GetScopeMode returns the current scope mode (union or intersection)
func (m *LensDashboardModel) GetScopeMode() ScopeMode {
	return m.scopeMode
}

// HasScope returns true if scope filtering is active
func (m *LensDashboardModel) HasScope() bool {
	return len(m.scopeLabels) > 0
}

// AddScopeLabel adds a label to the scope (if not already present)
func (m *LensDashboardModel) AddScopeLabel(label string) {
	// Check if already in scope
	for _, l := range m.scopeLabels {
		if l == label {
			return // Already present
		}
	}
	m.scopeLabels = append(m.scopeLabels, label)
	m.rebuildWithScope()
}

// RemoveScopeLabel removes a specific label from the scope
func (m *LensDashboardModel) RemoveScopeLabel(label string) {
	for i, l := range m.scopeLabels {
		if l == label {
			m.scopeLabels = append(m.scopeLabels[:i], m.scopeLabels[i+1:]...)
			m.rebuildWithScope()
			return
		}
	}
}

// RemoveLastScopeLabel removes the most recently added scope label
func (m *LensDashboardModel) RemoveLastScopeLabel() bool {
	if len(m.scopeLabels) == 0 {
		return false
	}
	m.scopeLabels = m.scopeLabels[:len(m.scopeLabels)-1]
	m.rebuildWithScope()
	return true
}

// ClearScope clears all scope labels
func (m *LensDashboardModel) ClearScope() {
	m.scopeLabels = nil
	m.rebuildWithScope()
}

// ToggleScopeMode toggles between union (ANY) and intersection (ALL) mode
func (m *LensDashboardModel) ToggleScopeMode() {
	if m.scopeMode == ScopeModeUnion {
		m.scopeMode = ScopeModeIntersection
	} else {
		m.scopeMode = ScopeModeUnion
	}
	// Rebuild only if scope is active
	if len(m.scopeLabels) > 0 {
		m.rebuildWithScope()
	}
}

// rebuildWithScope rebuilds primaryIDs based on scope and rebuilds tree
func (m *LensDashboardModel) rebuildWithScope() {
	// If no scope, reset to original behavior
	if len(m.scopeLabels) == 0 {
		// Reset primaryIDs based on view mode
		if m.viewMode == "label" {
			// Rebuild directPrimaryIDs from labelName
			m.directPrimaryIDs = make(map[string]bool)
			for _, issue := range m.allIssues {
				for _, label := range issue.Labels {
					if label == m.labelName {
						m.directPrimaryIDs[issue.ID] = true
						break
					}
				}
			}
			m.primaryIDs = expandToDescendants(m.directPrimaryIDs, m.allIssues)
		}
		// For epic/bead modes, scope doesn't change primary logic (epic children stay)
	} else {
		// Apply scope filtering
		m.applyScopeFilter()
	}

	m.buildGraphs()
	m.buildTree()
	m.recomputeWorkstreams()
}

// applyScopeFilter filters primaryIDs to only include issues matching scope criteria
func (m *LensDashboardModel) applyScopeFilter() {
	if len(m.scopeLabels) == 0 {
		return
	}

	// Build set of issues that match scope criteria
	scopeMatchingIDs := make(map[string]bool)

	for _, issue := range m.allIssues {
		matches := m.issueMatchesScope(issue)
		if matches {
			scopeMatchingIDs[issue.ID] = true
		}
	}

	// For label mode: intersection of original primaryIDs and scope-matching IDs
	if m.viewMode == "label" {
		// First get the original primary IDs (issues with the labelName)
		originalPrimaryIDs := make(map[string]bool)
		for _, issue := range m.allIssues {
			for _, label := range issue.Labels {
				if label == m.labelName {
					originalPrimaryIDs[issue.ID] = true
					break
				}
			}
		}

		// Intersect with scope matching
		m.directPrimaryIDs = make(map[string]bool)
		for id := range originalPrimaryIDs {
			if scopeMatchingIDs[id] {
				m.directPrimaryIDs[id] = true
			}
		}
		m.primaryIDs = expandToDescendants(m.directPrimaryIDs, m.allIssues)
	} else {
		// For epic/bead modes, filter the existing primaryIDs
		filteredPrimary := make(map[string]bool)
		for id := range m.primaryIDs {
			if scopeMatchingIDs[id] {
				filteredPrimary[id] = true
			}
		}
		// Always keep the entry point visible
		if m.epicID != "" {
			filteredPrimary[m.epicID] = true
		}
		m.primaryIDs = filteredPrimary
	}
}

// issueMatchesScope checks if an issue matches the current scope criteria
func (m *LensDashboardModel) issueMatchesScope(issue model.Issue) bool {
	if len(m.scopeLabels) == 0 {
		return true
	}

	// Build set of issue's labels for quick lookup
	issueLabels := make(map[string]bool)
	for _, label := range issue.Labels {
		issueLabels[label] = true
	}

	if m.scopeMode == ScopeModeUnion {
		// Union: issue has ANY of the scope labels
		for _, scopeLabel := range m.scopeLabels {
			if issueLabels[scopeLabel] {
				return true
			}
		}
		return false
	}

	// Intersection: issue has ALL of the scope labels
	for _, scopeLabel := range m.scopeLabels {
		if !issueLabels[scopeLabel] {
			return false
		}
	}
	return true
}

// GetAvailableScopeLabels returns labels that co-occur with current scope
// Useful for suggesting additional scope labels
func (m *LensDashboardModel) GetAvailableScopeLabels() []string {
	if len(m.scopeLabels) == 0 {
		// Return all unique labels
		labelSet := make(map[string]bool)
		for _, issue := range m.allIssues {
			for _, label := range issue.Labels {
				labelSet[label] = true
			}
		}
		var labels []string
		for label := range labelSet {
			labels = append(labels, label)
		}
		sort.Strings(labels)
		return labels
	}

	// Return labels that co-occur with current scope (excluding already selected)
	scopeSet := make(map[string]bool)
	for _, l := range m.scopeLabels {
		scopeSet[l] = true
	}

	cooccurring := make(map[string]int)
	for _, issue := range m.allIssues {
		if m.issueMatchesScope(issue) {
			for _, label := range issue.Labels {
				if !scopeSet[label] {
					cooccurring[label]++
				}
			}
		}
	}

	// Sort by count (descending)
	type labelCount struct {
		label string
		count int
	}
	var sorted []labelCount
	for label, count := range cooccurring {
		sorted = append(sorted, labelCount{label, count})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].count > sorted[j].count
	})

	var result []string
	for _, lc := range sorted {
		result = append(result, lc.label)
	}
	return result
}

// ══════════════════════════════════════════════════════════════════════════════
// SCOPE INPUT MODAL - Inline label input for scope filtering
// ══════════════════════════════════════════════════════════════════════════════

// ShowScopeInput returns true if the scope input modal is visible
func (m *LensDashboardModel) ShowScopeInput() bool {
	return m.showScopeInput
}

// OpenScopeInput opens the scope input modal
func (m *LensDashboardModel) OpenScopeInput() {
	m.showScopeInput = true
	m.scopeInput = ""
}

// CloseScopeInput closes the scope input modal
func (m *LensDashboardModel) CloseScopeInput() {
	m.showScopeInput = false
	m.scopeInput = ""
}

// GetScopeInput returns the current scope input text
func (m *LensDashboardModel) GetScopeInput() string {
	return m.scopeInput
}

// HandleScopeInputKey handles a key press when the scope input modal is open
// Returns true if the key was handled, false if modal should close
func (m *LensDashboardModel) HandleScopeInputKey(key string) (handled bool, statusMsg string) {
	switch key {
	case "esc":
		m.CloseScopeInput()
		return true, "Scope input cancelled"
	case "enter":
		// Add the label to scope if it's a valid label
		if m.scopeInput != "" {
			label := strings.TrimSpace(m.scopeInput)
			// Check if it's a valid label (exists in the data)
			isValid := false
			for _, issue := range m.allIssues {
				for _, l := range issue.Labels {
					if strings.EqualFold(l, label) {
						label = l // Use exact case from data
						isValid = true
						break
					}
				}
				if isValid {
					break
				}
			}
			if isValid {
				// Check if already in scope
				alreadyInScope := false
				for _, l := range m.scopeLabels {
					if l == label {
						alreadyInScope = true
						break
					}
				}
				if !alreadyInScope {
					m.AddScopeLabel(label)
					m.CloseScopeInput()
					return true, fmt.Sprintf("Added '%s' to scope (%s)", label, m.scopeMode.ShortString())
				}
				m.CloseScopeInput()
				return true, fmt.Sprintf("'%s' already in scope", label)
			}
			m.scopeInput = ""
			return true, fmt.Sprintf("Label '%s' not found", label)
		}
		m.CloseScopeInput()
		return true, ""
	case "backspace", "ctrl+h":
		if len(m.scopeInput) > 0 {
			m.scopeInput = m.scopeInput[:len(m.scopeInput)-1]
		}
		return true, ""
	case "tab":
		// Auto-complete with first matching label
		if m.scopeInput != "" {
			query := strings.ToLower(m.scopeInput)
			for _, label := range m.GetAvailableScopeLabels() {
				if strings.HasPrefix(strings.ToLower(label), query) {
					m.scopeInput = label
					return true, ""
				}
			}
		}
		return true, ""
	default:
		// Add printable characters to input
		if len(key) == 1 && key[0] >= 32 && key[0] < 127 {
			m.scopeInput += key
			return true, ""
		}
	}
	return false, ""
}

// SetSize updates the dashboard dimensions
func (m *LensDashboardModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// MoveUp moves cursor up
func (m *LensDashboardModel) MoveUp() {
	if m.viewType == ViewTypeWorkstream && len(m.workstreams) > 1 {
		m.moveUpWS()
		return
	}

	// Handle centered mode navigation
	if m.centeredMode && (m.viewMode == "epic" || m.viewMode == "bead") && m.egoNode != nil {
		if m.cursor > 0 {
			m.cursor--
			m.selectedIssueID = m.getSelectedIDForCenteredMode()
		}
		return
	}

	// Standard flat view navigation
	if m.cursor > 0 {
		m.cursor--
		m.selectedIssueID = m.flatNodes[m.cursor].Node.Issue.ID
		m.ensureVisible()
	}
}

// MoveDown moves cursor down
func (m *LensDashboardModel) MoveDown() {
	if m.viewType == ViewTypeWorkstream && len(m.workstreams) > 1 {
		m.moveDownWS()
		return
	}

	// Handle centered mode navigation
	if m.centeredMode && (m.viewMode == "epic" || m.viewMode == "bead") && m.egoNode != nil {
		totalNodes := m.getTotalCenteredNodeCount()
		if m.cursor < totalNodes-1 {
			m.cursor++
			m.selectedIssueID = m.getSelectedIDForCenteredMode()
		}
		return
	}

	// Standard flat view navigation
	if m.cursor < len(m.flatNodes)-1 {
		m.cursor++
		m.selectedIssueID = m.flatNodes[m.cursor].Node.Issue.ID
		m.ensureVisible()
	}
}

// moveUpWS moves cursor up in workstream view
func (m *LensDashboardModel) moveUpWS() {
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
func (m *LensDashboardModel) getVisibleIssueCount(wsIdx int) int {
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
func (m *LensDashboardModel) moveDownWS() {
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
func (m *LensDashboardModel) updateSelectedIssueFromWS() {
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
func (m *LensDashboardModel) ensureVisibleWS() {
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
func (m *LensDashboardModel) getWSCursorLine() int {
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
func (m *LensDashboardModel) getTotalWSLines() int {
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
func (m *LensDashboardModel) ensureVisible() {
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
func (m *LensDashboardModel) NextSection() {
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
func (m *LensDashboardModel) PrevSection() {
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
func (m *LensDashboardModel) SelectedIssueID() string {
	return m.selectedIssueID
}

// LabelName returns the current label name
func (m *LensDashboardModel) LabelName() string {
	return m.labelName
}

// IssueCount returns the total number of issues
func (m *LensDashboardModel) IssueCount() int {
	return m.totalCount
}

// ContextCount returns the number of context issues
func (m *LensDashboardModel) ContextCount() int {
	return m.contextCount
}

// PrimaryCount returns the number of primary issues
func (m *LensDashboardModel) PrimaryCount() int {
	return m.primaryCount
}

// GetViewType returns the current view type
func (m *LensDashboardModel) GetViewType() ViewType {
	return m.viewType
}

// IsWorkstreamView returns true if in workstream view mode
func (m *LensDashboardModel) IsWorkstreamView() bool {
	return m.viewType == ViewTypeWorkstream
}

// ToggleViewType toggles between flat and workstream view
func (m *LensDashboardModel) ToggleViewType() {
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
func (m *LensDashboardModel) WorkstreamCount() int {
	return m.workstreamCount
}

// SetWorkstreams sets the detected workstreams
func (m *LensDashboardModel) SetWorkstreams(ws []analysis.Workstream) {
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
func (m *LensDashboardModel) isEntryEpic(issueID string) bool {
	return (m.viewMode == "epic" || m.viewMode == "bead") && m.epicID != "" && issueID == m.epicID
}

// ToggleWorkstreamExpand toggles expansion of the current workstream
func (m *LensDashboardModel) ToggleWorkstreamExpand() {
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
func (m *LensDashboardModel) IsWorkstreamExpanded(wsIdx int) bool {
	return m.wsExpanded[wsIdx]
}

// ExpandWorkstream expands the current workstream (does nothing if already expanded)
func (m *LensDashboardModel) ExpandWorkstream() {
	if len(m.workstreams) == 0 {
		return
	}
	m.wsExpanded[m.wsCursor] = true
}

// CurrentWorkstreamName returns the name of the currently selected workstream
func (m *LensDashboardModel) CurrentWorkstreamName() string {
	if len(m.workstreams) == 0 || m.wsCursor >= len(m.workstreams) {
		return ""
	}
	return m.workstreams[m.wsCursor].Name
}

// IsOnWorkstreamHeader returns true if cursor is on a workstream header (not an issue)
func (m *LensDashboardModel) IsOnWorkstreamHeader() bool {
	return m.wsIssueCursor < 0
}

// ToggleWSTreeView toggles dependency tree view within workstreams
func (m *LensDashboardModel) ToggleWSTreeView() {
	m.wsTreeView = !m.wsTreeView
}

// IsWSTreeView returns true if showing dependency tree in workstream view
func (m *LensDashboardModel) IsWSTreeView() bool {
	return m.wsTreeView
}

// === Sub-Workstream Support ===

// ToggleSubdivision toggles subdivision mode on/off
func (m *LensDashboardModel) ToggleSubdivision() {
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
func (m *LensDashboardModel) IsSubdivided() bool {
	return m.wsSubdivided
}

// GetWorkstreamPointers returns pointers to workstreams for mutation
func (m *LensDashboardModel) GetWorkstreamPointers() []*analysis.Workstream {
	return m.workstreamPtrs
}

// HasSubWorkstreams returns true if the given workstream has children
func (m *LensDashboardModel) HasSubWorkstreams(wsIdx int) bool {
	if wsIdx >= len(m.workstreamPtrs) || m.workstreamPtrs[wsIdx] == nil {
		return false
	}
	return len(m.workstreamPtrs[wsIdx].SubWorkstreams) > 0
}

// IsSubWorkstreamExpanded returns whether a sub-workstream is expanded
func (m *LensDashboardModel) IsSubWorkstreamExpanded(wsIdx, subIdx int) bool {
	if m.subWSExpanded[wsIdx] == nil {
		return false
	}
	return m.subWSExpanded[wsIdx][subIdx]
}

// ToggleSubWorkstreamExpand toggles expansion of a sub-workstream
func (m *LensDashboardModel) ToggleSubWorkstreamExpand(wsIdx, subIdx int) {
	if m.subWSExpanded[wsIdx] == nil {
		m.subWSExpanded[wsIdx] = make(map[int]bool)
	}
	m.subWSExpanded[wsIdx][subIdx] = !m.subWSExpanded[wsIdx][subIdx]
}

// GetSubWorkstreamCursor returns the sub-workstream cursor for a workstream
func (m *LensDashboardModel) GetSubWorkstreamCursor(wsIdx int) int {
	return m.subWsCursor[wsIdx]
}

// SetSubWorkstreamCursor sets the sub-workstream cursor for a workstream
func (m *LensDashboardModel) SetSubWorkstreamCursor(wsIdx, cursor int) {
	m.subWsCursor[wsIdx] = cursor
}

// GetWsCursor returns the current workstream cursor position
func (m *LensDashboardModel) GetWsCursor() int {
	return m.wsCursor
}

// NextWorkstream moves to the next workstream
func (m *LensDashboardModel) NextWorkstream() {
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
func (m *LensDashboardModel) PrevWorkstream() {
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
func (m *LensDashboardModel) GoToTop() {
	if m.viewType == ViewTypeWorkstream && len(m.workstreams) > 1 {
		m.wsCursor = 0
		m.wsIssueCursor = -1 // Go to first workstream header
		m.updateSelectedIssueFromWS()
		return
	}

	// Centered mode
	if m.centeredMode && (m.viewMode == "epic" || m.viewMode == "bead") && m.egoNode != nil {
		m.cursor = 0
		m.selectedIssueID = m.getSelectedIDForCenteredMode()
		m.scroll = 0
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
func (m *LensDashboardModel) GoToBottom() {
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

	// Centered mode
	if m.centeredMode && (m.viewMode == "epic" || m.viewMode == "bead") && m.egoNode != nil {
		totalNodes := m.getTotalCenteredNodeCount()
		m.cursor = totalNodes - 1
		m.selectedIssueID = m.getSelectedIDForCenteredMode()
		return
	}

	// Flat view
	if len(m.flatNodes) > 0 {
		m.cursor = len(m.flatNodes) - 1
		m.selectedIssueID = m.flatNodes[m.cursor].Node.Issue.ID
		m.ensureVisible()
	}
}

// PageDown moves cursor down by half a page
func (m *LensDashboardModel) PageDown() {
	pageSize := (m.height - 8) / 2
	if pageSize < 3 {
		pageSize = 3
	}

	if m.viewType == ViewTypeWorkstream && len(m.workstreams) > 1 {
		// Move multiple issues down in workstream view
		for i := 0; i < pageSize; i++ {
			m.moveDownWS()
		}
		return
	}

	// Centered mode
	if m.centeredMode && (m.viewMode == "epic" || m.viewMode == "bead") && m.egoNode != nil {
		totalNodes := m.getTotalCenteredNodeCount()
		m.cursor += pageSize
		if m.cursor >= totalNodes {
			m.cursor = totalNodes - 1
		}
		m.selectedIssueID = m.getSelectedIDForCenteredMode()
		return
	}

	// Flat view
	if len(m.flatNodes) > 0 {
		m.cursor += pageSize
		if m.cursor >= len(m.flatNodes) {
			m.cursor = len(m.flatNodes) - 1
		}
		m.selectedIssueID = m.flatNodes[m.cursor].Node.Issue.ID
		m.ensureVisible()
	}
}

// PageUp moves cursor up by half a page
func (m *LensDashboardModel) PageUp() {
	pageSize := (m.height - 8) / 2
	if pageSize < 3 {
		pageSize = 3
	}

	if m.viewType == ViewTypeWorkstream && len(m.workstreams) > 1 {
		// Move multiple issues up in workstream view
		for i := 0; i < pageSize; i++ {
			m.moveUpWS()
		}
		return
	}

	// Centered mode
	if m.centeredMode && (m.viewMode == "epic" || m.viewMode == "bead") && m.egoNode != nil {
		m.cursor -= pageSize
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.selectedIssueID = m.getSelectedIDForCenteredMode()
		return
	}

	// Flat view
	if len(m.flatNodes) > 0 {
		m.cursor -= pageSize
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.selectedIssueID = m.flatNodes[m.cursor].Node.Issue.ID
		m.ensureVisible()
	}
}

// GetWorkstreams returns the detected workstreams
func (m *LensDashboardModel) GetWorkstreams() []analysis.Workstream {
	return m.workstreams
}

// buildWorkstreamTree builds a dependency tree for issues within a workstream
func (m *LensDashboardModel) buildWorkstreamTree(ws *analysis.Workstream) []*LensTreeNode {
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

	var roots []*LensTreeNode
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
func (m *LensDashboardModel) buildWSTreeNode(issue *model.Issue, depth, maxDepth int, seen map[string]bool, isLast bool, parentPath []bool, downstream map[string][]string, wsIssueMap map[string]*model.Issue) *LensTreeNode {
	if seen[issue.ID] {
		return nil
	}
	seen[issue.ID] = true

	node := &LensTreeNode{
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
func (m *LensDashboardModel) flattenWSTree(roots []*LensTreeNode) []LensFlatNode {
	var flatNodes []LensFlatNode
	for _, root := range roots {
		m.flattenWSTreeNode(root, &flatNodes)
	}
	return flatNodes
}

func (m *LensDashboardModel) flattenWSTreeNode(node *LensTreeNode, flatNodes *[]LensFlatNode) {
	prefix := m.buildTreePrefix(node)
	status := m.getIssueStatus(node.Issue)

	fn := LensFlatNode{
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
func (m *LensDashboardModel) View() string {
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

	modeIcon := "🔭 " // Default lens icon for label mode
	switch m.viewMode {
	case "epic":
		modeIcon = "◈ " // Epic icon
	case "bead":
		modeIcon = "◇ " // Bead icon
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

	statsLine := fmt.Sprintf("%s %d in lens  %s %d context  Depth: [%s]",
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

	// Scope indicator (if scope is active)
	if len(m.scopeLabels) > 0 {
		scopeStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)
		modeStyle := t.Renderer.NewStyle().Foreground(t.InProgress)
		tagStyle := t.Renderer.NewStyle().Foreground(t.Secondary)

		// Build scope tags
		var tags []string
		for _, label := range m.scopeLabels {
			tags = append(tags, tagStyle.Render("["+label+"]"))
		}

		// Mode indicator
		modeIndicator := m.scopeMode.ShortString()

		scopeLine := scopeStyle.Render("Scope: ") + strings.Join(tags, " ") + "  " + modeStyle.Render(modeIndicator)
		lines = append(lines, scopeLine)
	}

	// Scope input field (inline, appears when adding scope)
	if m.showScopeInput {
		inputStyle := t.Renderer.NewStyle().Foreground(t.Primary)
		promptStyle := t.Renderer.NewStyle().Foreground(t.Secondary)
		hintStyle := t.Renderer.NewStyle().Faint(true)

		inputLine := promptStyle.Render("+ Scope: ") + inputStyle.Render(m.scopeInput) + inputStyle.Render("█")
		lines = append(lines, inputLine)

		// Show matching labels on separate line to avoid breaking layout
		if m.scopeInput != "" {
			query := strings.ToLower(m.scopeInput)
			var matches []string
			for _, label := range m.GetAvailableScopeLabels() {
				if strings.Contains(strings.ToLower(label), query) {
					matches = append(matches, label)
					if len(matches) >= 5 {
						break
					}
				}
			}
			if len(matches) > 0 {
				// Truncate matches to fit width
				matchText := strings.Join(matches, ", ")
				maxLen := contentWidth - 6
				if maxLen > 0 && len(matchText) > maxLen {
					matchText = matchText[:maxLen-3] + "..."
				}
				lines = append(lines, hintStyle.Render("  → "+matchText))
			}
		}
	}

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
	} else if m.centeredMode && (m.viewMode == "epic" || m.viewMode == "bead") && m.egoNode != nil {
		// Render ego-centered view for epic/bead modes
		lines = append(lines, m.renderCenteredView(contentWidth, visibleLines, statsStyle)...)
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
	viewIndicator := "[flat view]"
	toggleHint := "w: streams"
	navHint := "n/N: section"
	enterHint := "enter: focus"
	treeHint := ""
	centeredHint := ""
	if m.centeredMode && (m.viewMode == "epic" || m.viewMode == "bead") {
		viewIndicator = "[centered]"
		centeredHint = " • c: flat"
	} else if m.viewMode == "epic" || m.viewMode == "bead" {
		centeredHint = " • c: centered"
	}
	if m.viewType == ViewTypeWorkstream && len(m.workstreams) > 1 {
		viewIndicator = fmt.Sprintf("[%d streams]", m.workstreamCount)
		toggleHint = "w: flat"
		navHint = "n/N: stream"
		enterHint = "enter: expand"
		if m.wsTreeView {
			treeHint = " • T: list • d: depth"
		} else {
			treeHint = " • T: tree • d: depth"
		}
	}

	lines = append(lines, footerStyle.Render(fmt.Sprintf("%s • j/k: nav • g/G: top/bottom • %s • %s • %s%s%s • esc: back", viewIndicator, toggleHint, navHint, enterHint, treeHint, centeredHint)))

	return strings.Join(lines, "\n")
}

// renderCenteredView renders the ego-centered layout for epic/bead modes:
// Upstream blockers → Entry point (center) → Downstream descendants
func (m *LensDashboardModel) renderCenteredView(contentWidth, visibleLines int, statsStyle lipgloss.Style) []string {
	t := m.theme
	var allLines []string

	upstreamLen := len(m.upstreamNodes)
	totalNodes := m.getTotalCenteredNodeCount()

	// Elegant section header styles with gradient colors
	upstreamIconStyle := t.Renderer.NewStyle().Foreground(t.Blocked).Bold(true)
	downstreamIconStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)
	sectionLabelStyle := t.Renderer.NewStyle().Foreground(t.Subtext).Bold(true)
	separatorStyle := t.Renderer.NewStyle().Foreground(t.Subtext)
	boxStyle := t.Renderer.NewStyle().Foreground(t.Primary)

	// Helper to render elegant header with decorative line
	renderSectionHeader := func(icon, iconStyled, label string, lineWidth int) string {
		lineLen := lineWidth - len(icon) - len(label) - 2
		if lineLen < 5 {
			lineLen = 5
		}
		line := strings.Repeat("─", lineLen)
		return iconStyled + " " + sectionLabelStyle.Render(label) + " " + separatorStyle.Render(line)
	}

	// === UPSTREAM SECTION (blockers) ===
	if len(m.upstreamNodes) > 0 {
		header := renderSectionHeader("◇", upstreamIconStyle.Render("◇"), "BLOCKERS", min(contentWidth, 50))
		allLines = append(allLines, header)

		for i, fn := range m.upstreamNodes {
			isSelected := i == m.cursor
			line := m.renderCenteredNode(fn, isSelected, contentWidth, -1)
			allLines = append(allLines, line)
		}
		allLines = append(allLines, "")
	}

	// === CENTER SECTION (entry point/ego) with elegant top/bottom lines ===
	if m.egoNode != nil {
		// Simple elegant lines - no side borders
		lineWidth := min(contentWidth-4, 50)
		topLine := boxStyle.Render("═" + strings.Repeat("═", lineWidth) + "═")
		bottomLine := boxStyle.Render("─" + strings.Repeat("─", lineWidth) + "─")

		allLines = append(allLines, topLine)

		isSelected := m.cursor == upstreamLen
		line := m.renderEgoNodeLine(*m.egoNode, isSelected, contentWidth)
		allLines = append(allLines, line)

		allLines = append(allLines, bottomLine)
		allLines = append(allLines, "")
	}

	// === DOWNSTREAM SECTION (children/dependents) ===
	if len(m.flatNodes) > 0 {
		header := renderSectionHeader("◆", downstreamIconStyle.Render("◆"), "DESCENDANTS", min(contentWidth, 50))
		allLines = append(allLines, header)

		lastStatus := ""
		for i, fn := range m.flatNodes {
			// Calculate actual cursor position (offset by upstream + ego)
			cursorPos := upstreamLen + 1 + i
			isSelected := cursorPos == m.cursor

			// Show status header when status changes
			if fn.Status != lastStatus {
				statusHeader := m.renderStatusHeader(fn.Status)
				allLines = append(allLines, statusHeader)
				lastStatus = fn.Status
			}

			line := m.renderCenteredNode(fn, isSelected, contentWidth, fn.Node.RelativeDepth)
			allLines = append(allLines, line)
		}
	} else if len(m.upstreamNodes) == 0 {
		emptyStyle := t.Renderer.NewStyle().Foreground(t.Subtext).Italic(true)
		allLines = append(allLines, emptyStyle.Render("  No descendants found"))
	}

	// Show scroll indicator if needed
	if totalNodes > visibleLines {
		scrollInfo := fmt.Sprintf("  [cursor %d of %d]", m.cursor+1, totalNodes)
		allLines = append(allLines, statsStyle.Render(scrollInfo))
	}

	return allLines
}

// renderEgoNodeLine renders the center/ego node with prominent styling
// No [CENTER] badge - the framed box already indicates this is the center
func (m *LensDashboardModel) renderEgoNodeLine(fn LensFlatNode, isSelected bool, maxWidth int) string {
	t := m.theme
	node := fn.Node

	// Selection indicator
	selectPrefix := "  "
	if isSelected {
		selectPrefix = "▸ "
	}

	// Diamond icon for center - elegant ◈ instead of filled ◆
	indicator := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true).Render("◈")

	// Issue ID and title with prominent styling
	idStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)
	titleStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)

	// Calculate max title length (more room without badge)
	prefixLen := len(selectPrefix) + 2 + len(node.Issue.ID) + 3
	maxTitleLen := maxWidth - prefixLen
	if maxTitleLen < 15 {
		maxTitleLen = 15
	}
	title := truncateRunesHelper(node.Issue.Title, maxTitleLen, "…")

	// Status indicator
	statusSuffix := ""
	if fn.Status == "blocked" && fn.BlockedBy != "" {
		blockerStyle := t.Renderer.NewStyle().Foreground(t.Blocked)
		statusSuffix = blockerStyle.Render(" ◄ " + fn.BlockedBy)
	}

	return fmt.Sprintf("%s%s %s %s%s",
		selectPrefix,
		indicator,
		idStyle.Render(node.Issue.ID),
		titleStyle.Render(title),
		statusSuffix)
}

// renderCenteredNode renders a node with elegant depth-based styling
func (m *LensDashboardModel) renderCenteredNode(fn LensFlatNode, isSelected bool, maxWidth int, relDepth int) string {
	t := m.theme
	node := fn.Node

	// Selection indicator
	selectPrefix := "  "
	if isSelected {
		selectPrefix = "▸ "
	}

	// Primary/context indicator - use smaller ▴ for upstream blockers
	var indicator string
	if node.IsUpstream {
		// Upstream blocker - use smaller up triangle
		indicator = t.Renderer.NewStyle().Foreground(t.Blocked).Render("▴")
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

	// Gradient depth coloring: deeper nodes fade toward subtext
	// Depth 1 = full color, Depth 2+ = progressively dimmer
	idStyle := t.Renderer.NewStyle()
	titleStyle := t.Renderer.NewStyle()

	absDepth := relDepth
	if absDepth < 0 {
		absDepth = -absDepth
	}

	if isSelected {
		idStyle = idStyle.Foreground(t.Primary).Bold(true)
		titleStyle = titleStyle.Foreground(t.Primary).Bold(true)
	} else if node.IsUpstream {
		idStyle = idStyle.Foreground(t.Blocked)
		titleStyle = titleStyle.Foreground(t.Blocked)
	} else if absDepth >= 3 {
		// Deep nodes (depth 3+): dimmer subtext color
		idStyle = idStyle.Foreground(t.Subtext)
		titleStyle = titleStyle.Foreground(t.Subtext)
	} else if absDepth == 2 {
		// Medium depth: secondary/subdued color
		if node.IsPrimary {
			idStyle = idStyle.Foreground(t.Secondary)
			titleStyle = titleStyle.Foreground(t.Secondary)
		} else {
			idStyle = idStyle.Foreground(t.Subtext)
			titleStyle = titleStyle.Foreground(t.Subtext)
		}
	} else if !node.IsPrimary {
		// Depth 1, context node
		idStyle = idStyle.Foreground(t.Subtext)
		titleStyle = titleStyle.Foreground(t.Subtext)
	} else {
		// Depth 1, primary node: full brightness
		idStyle = idStyle.Foreground(t.Base.GetForeground())
		titleStyle = titleStyle.Foreground(t.Base.GetForeground())
	}

	// Calculate max title length (no depth badge = more room for title)
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

// renderWorkstreamView renders issues grouped by workstream
func (m *LensDashboardModel) renderWorkstreamView(contentWidth, visibleLines int, statsStyle lipgloss.Style) []string {
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
				subLine := fmt.Sprintf("     %s (%d%%) %s",
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
func (m *LensDashboardModel) renderMiniProgressBar(progress float64, width int) string {
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

// renderStatusHeader renders a status section header with elegant dotted dividers
func (m *LensDashboardModel) renderStatusHeader(status string) string {
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

	// Elegant dotted divider: ┄ LABEL ┄┄┄┄┄┄
	labelStyle := t.Renderer.NewStyle().Foreground(color).Bold(true)
	dividerStyle := t.Renderer.NewStyle().Foreground(t.Subtext)

	dotCount := 20 - len(label)
	if dotCount < 3 {
		dotCount = 3
	}

	return dividerStyle.Render("┄ ") + labelStyle.Render(label) + " " + dividerStyle.Render(strings.Repeat("┄", dotCount))
}

// renderTreeNode renders a single tree node
func (m *LensDashboardModel) renderTreeNode(fn LensFlatNode, isSelected bool, maxWidth int) string {
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

func (m *LensDashboardModel) renderProgressBar(progress float64, width int) string {
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
func (m *LensDashboardModel) DumpToFile() (string, error) {
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
func (m *LensDashboardModel) dumpWorkstreamTree(ws *analysis.Workstream, indent int) string {
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
func (m *LensDashboardModel) dumpFlatByDepth() string {
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

package ui

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"

	"github.com/charmbracelet/bubbles/viewport"
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
	ViewTypeGrouped    ViewType = 2
)

// GroupByMode determines how issues are grouped in grouped view
type GroupByMode int

const (
	GroupByLabel    GroupByMode = iota // Group by most popular labels
	GroupByPriority                    // Group by priority (P0, P1, P2, P3+)
	GroupByStatus                      // Group by status (Open, In Progress, Blocked, Closed)
)

// String returns display name for the group-by mode
func (g GroupByMode) String() string {
	switch g {
	case GroupByLabel:
		return "Label"
	case GroupByPriority:
		return "Priority"
	case GroupByStatus:
		return "Status"
	default:
		return "Label"
	}
}

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

// Viewport constants for consistent layout calculations
const (
	lensHeaderMinLines   = 4 // title + stats + summary + blank
	lensFooterLines      = 2 // blank + keybinds
	lensMinContentHeight = 5
)

// ViewportConfig holds calculated viewport dimensions
type ViewportConfig struct {
	HeaderLines   int
	FooterLines   int
	ContentHeight int
}

// calculateViewport returns the current viewport configuration
func (m *LensDashboardModel) calculateViewport() ViewportConfig {
	headerLines := lensHeaderMinLines
	if len(m.scopeLabels) > 0 {
		headerLines++
	}
	if m.showScopeInput {
		headerLines += 2
	}

	contentHeight := m.height - headerLines - lensFooterLines
	if contentHeight < lensMinContentHeight {
		contentHeight = lensMinContentHeight
	}

	return ViewportConfig{
		HeaderLines:   headerLines,
		FooterLines:   lensFooterLines,
		ContentHeight: contentHeight,
	}
}

// applyScrollWindow slices content by scroll offset and visible lines
func (m *LensDashboardModel) applyScrollWindow(allLines []string, scroll, visibleLines int) []string {
	if len(allLines) == 0 {
		return nil
	}

	startIdx := scroll
	endIdx := scroll + visibleLines

	if startIdx < 0 {
		startIdx = 0
	}
	if startIdx >= len(allLines) {
		startIdx = len(allLines) - 1
	}
	if endIdx > len(allLines) {
		endIdx = len(allLines)
	}

	return allLines[startIdx:endIdx]
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
	Node          *LensTreeNode
	TreePrefix    string // rendered tree prefix (├─►, └─►, etc.)
	Status        string // ready, blocked, in_progress, closed
	BlockedBy     string // ID of blocker if blocked
	BlockerInTree bool   // true if BlockedBy is visible as ancestor in tree
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
	downstream map[string][]string // issue ID -> issues it unblocks (blocks + parent-child)
	upstream   map[string][]string // issue ID -> issues that block it

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

	// Grouped view state
	groupByMode        GroupByMode           // Current grouping mode (Label, Priority, Status)
	groupedSections    []analysis.Workstream // Grouped sections (reusing Workstream struct)
	groupedExpanded    map[int]bool          // Expansion state per group
	groupedSubExpanded map[int]map[int]bool  // groupIndex -> subIndex -> expanded
	groupedCursor      int                   // Which group is selected
	groupedSubCursor   int                   // Which sub-group is selected (-1 = on group level)
	groupedIssueCursor int                   // Which issue within group/sub-group (-1 = header)
	groupedScroll      int                   // Scroll offset for grouped view
	groupedTreeView    bool                  // Show dependency tree within groups

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

	// Split view (bead detail panel)
	detailViewport viewport.Model // Viewport for bead details on the right
	detailFocus    bool           // True when detail panel has focus
	splitViewMode  bool           // True when in split view mode (wide terminal)
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
	m.initDetailViewport()

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
	m.initDetailViewport()

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
	m.initDetailViewport()

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
	// Collect descendants at each level
	descendantsByLevel := make(map[int]map[string]bool)
	visited := make(map[string]bool)
	queue := []BFSQueueItem{{ID: epicID, Depth: 0}}
	visited[epicID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, childID := range children[current.ID] {
			if !visited[childID] {
				visited[childID] = true
				childDepth := current.Depth + 1

				if descendantsByLevel[childDepth] == nil {
					descendantsByLevel[childDepth] = make(map[string]bool)
				}
				descendantsByLevel[childDepth][childID] = true

				queue = append(queue, BFSQueueItem{ID: childID, Depth: childDepth})
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
	// Collect descendants at each level
	descendantsByLevel := make(map[int]map[string]bool)
	visited := make(map[string]bool)
	queue := []BFSQueueItem{{ID: beadID, Depth: 0}}
	visited[beadID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		// Follow parent-child edges
		for _, childID := range children[current.ID] {
			if !visited[childID] {
				visited[childID] = true
				childDepth := current.Depth + 1

				if descendantsByLevel[childDepth] == nil {
					descendantsByLevel[childDepth] = make(map[string]bool)
				}
				descendantsByLevel[childDepth][childID] = true

				queue = append(queue, BFSQueueItem{ID: childID, Depth: childDepth})
			}
		}

		// Follow blocking edges
		for _, blockedID := range blocks[current.ID] {
			if !visited[blockedID] {
				visited[blockedID] = true
				blockedDepth := current.Depth + 1

				if descendantsByLevel[blockedDepth] == nil {
					descendantsByLevel[blockedDepth] = make(map[string]bool)
				}
				descendantsByLevel[blockedDepth][blockedID] = true

				queue = append(queue, BFSQueueItem{ID: blockedID, Depth: blockedDepth})
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

	// Fix tree structure (correct IsLastChild/ParentPath after siblings may have been claimed)
	m.fixTreeStructure()

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

// fixTreeStructure corrects IsLastChild and ParentPath values after tree construction.
// This is needed because during recursive building, siblings may get "claimed" by earlier
// branches, leaving incorrect IsLastChild values that cause tree lines to extend too far.
func (m *LensDashboardModel) fixTreeStructure() {
	for i, root := range m.roots {
		isLast := i == len(m.roots)-1
		root.IsLastChild = isLast
		root.ParentPath = nil
		m.fixNodeChildren(root, nil)
	}
}

// fixNodeChildren recursively fixes IsLastChild and ParentPath for all children
func (m *LensDashboardModel) fixNodeChildren(node *LensTreeNode, parentPath []bool) {
	if len(node.Children) == 0 {
		return
	}

	// Build the parent path for children (includes this node's IsLastChild)
	childParentPath := append([]bool{}, parentPath...)
	childParentPath = append(childParentPath, node.IsLastChild)

	// Fix each child's IsLastChild and ParentPath
	for i, child := range node.Children {
		child.IsLastChild = i == len(node.Children)-1
		child.ParentPath = append([]bool{}, childParentPath...)
		m.fixNodeChildren(child, childParentPath)
	}
}

// flattenTree converts the tree to a flat list for display
func (m *LensDashboardModel) flattenTree() {
	m.flatNodes = nil
	for _, root := range m.roots {
		m.flattenNode(root, make(map[string]bool))
	}
}

// flattenNode recursively flattens a node and its children.
// ancestors tracks issue IDs visible above this node in the tree, used to detect
// if a blocker is already visible (so we can suppress the redundant indicator).
func (m *LensDashboardModel) flattenNode(node *LensTreeNode, ancestors map[string]bool) {
	prefix := m.buildTreePrefix(node)
	status := m.getIssueStatus(node.Issue)
	blockerID := m.blockedByMap[node.Issue.ID]

	fn := LensFlatNode{
		Node:          node,
		TreePrefix:    prefix,
		Status:        status,
		BlockedBy:     blockerID,
		BlockerInTree: ancestors[blockerID],
	}
	m.flatNodes = append(m.flatNodes, fn)

	// Build child ancestors (include current node)
	childAncestors := make(map[string]bool, len(ancestors)+1)
	for k := range ancestors {
		childAncestors[k] = true
	}
	childAncestors[node.Issue.ID] = true

	for _, child := range node.Children {
		m.flattenNode(child, childAncestors)
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

	// Fix tree structure (correct IsLastChild/ParentPath after siblings may have been claimed)
	m.fixTreeStructure()

	// Flatten downstream tree
	// In centered mode, ego and upstream nodes are visible above downstream,
	// so include them as "ancestors" for blocker visibility detection
	centeredAncestors := m.getCenteredAncestors()
	for _, root := range m.roots {
		m.flattenNode(root, centeredAncestors)
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

// getCenteredAncestors returns a map of issue IDs that are visible "above" the
// downstream tree in centered mode: the ego node and all upstream blockers.
// Used to detect when a blocker is already visible in the tree structure.
func (m *LensDashboardModel) getCenteredAncestors() map[string]bool {
	ancestors := make(map[string]bool)
	// Ego node is visible above all downstream
	if m.egoNode != nil {
		ancestors[m.egoNode.Node.Issue.ID] = true
	}
	// Upstream blockers are also visible in the tree
	for _, up := range m.upstreamNodes {
		ancestors[up.Node.Issue.ID] = true
	}
	return ancestors
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

// isIssueBlockedByDeps checks if an issue is blocked by dependencies
func (m *LensDashboardModel) isIssueBlockedByDeps(issueID string) bool {
	return m.blockedByMap[issueID] != ""
}

// buildWorkstreamFromIssues creates a Workstream struct with computed stats
func (m *LensDashboardModel) buildWorkstreamFromIssues(name string, issues []model.Issue) analysis.Workstream {
	ws := analysis.Workstream{
		ID:       "group:" + name,
		Name:     name,
		Issues:   issues,
		IssueIDs: make([]string, len(issues)),
	}

	for i, issue := range issues {
		ws.IssueIDs[i] = issue.ID
	}

	// Compute stats
	for _, issue := range issues {
		switch issue.Status {
		case model.StatusClosed:
			ws.ClosedCount++
		case model.StatusBlocked:
			ws.BlockedCount++
		case model.StatusInProgress:
			ws.InProgressCount++
		case model.StatusOpen:
			if m.isIssueBlockedByDeps(issue.ID) {
				ws.BlockedCount++
			} else {
				ws.ReadyCount++
			}
		default:
			ws.ReadyCount++
		}
	}

	// Progress = closed / total
	if len(issues) > 0 {
		ws.Progress = float64(ws.ClosedCount) / float64(len(issues))
	}

	ws.PrimaryCount = len(issues)
	return ws
}

// buildGroupedByLabel groups issues by their most popular label (no repeats)
// Level 1: Group by most popular label globally
// Level 2: Sub-group by next-most-popular label within each group
func (m *LensDashboardModel) buildGroupedByLabel() []analysis.Workstream {
	// 1. Calculate label popularity across all primary issues
	labelCounts := make(map[string]int)
	for _, issue := range m.allIssues {
		if !m.primaryIDs[issue.ID] {
			continue
		}
		for _, label := range issue.Labels {
			labelCounts[label]++
		}
	}

	// 2. Sort labels by popularity (descending)
	var sortedLabels []LabelCount
	for label, count := range labelCounts {
		sortedLabels = append(sortedLabels, LabelCount{Label: label, Count: count})
	}
	SortLabelCountsDescending(sortedLabels)

	// 3. Build label rank map (lower = more popular)
	labelRank := make(map[string]int)
	for i, lc := range sortedLabels {
		labelRank[lc.Label] = i
	}

	// 4. Assign each issue to its most popular label
	groups := make(map[string][]model.Issue)
	var unlabeled []model.Issue

	for _, issue := range m.allIssues {
		if !m.primaryIDs[issue.ID] {
			continue
		}

		if len(issue.Labels) == 0 {
			unlabeled = append(unlabeled, issue)
			continue
		}

		// Find the most popular label on this issue
		bestLabel := issue.Labels[0]
		bestRank := labelRank[bestLabel]
		for _, label := range issue.Labels[1:] {
			if rank, ok := labelRank[label]; ok && rank < bestRank {
				bestLabel = label
				bestRank = rank
			}
		}

		groups[bestLabel] = append(groups[bestLabel], issue)
	}

	// 5. Convert to Workstream slices, ordered by popularity
	var result []analysis.Workstream
	for _, lc := range sortedLabels {
		if issues, ok := groups[lc.Label]; ok && len(issues) > 0 {
			ws := m.buildWorkstreamFromIssues(lc.Label, issues)
			// Build sub-groups for 2-level hierarchy
			m.buildLabelSubGroups(&ws, labelRank, lc.Label)
			result = append(result, ws)
		}
	}

	// 6. Add unlabeled group at the end
	if len(unlabeled) > 0 {
		ws := m.buildWorkstreamFromIssues("Unlabeled", unlabeled)
		result = append(result, ws)
	}

	return result
}

// buildLabelSubGroups creates sub-groups within a label group by secondary label
func (m *LensDashboardModel) buildLabelSubGroups(parent *analysis.Workstream, labelRank map[string]int, primaryLabel string) {
	if len(parent.Issues) < 4 { // Don't sub-divide tiny groups
		return
	}

	// Find second-most-popular label for each issue (excluding primary)
	subGroups := make(map[string][]model.Issue)
	var noSecondary []model.Issue

	for _, issue := range parent.Issues {
		var secondLabel string
		secondRank := -1

		for _, label := range issue.Labels {
			if label == primaryLabel {
				continue
			}
			rank, ok := labelRank[label]
			if !ok {
				continue
			}
			if secondRank < 0 || rank < secondRank {
				secondLabel = label
				secondRank = rank
			}
		}

		if secondLabel != "" {
			subGroups[secondLabel] = append(subGroups[secondLabel], issue)
		} else {
			noSecondary = append(noSecondary, issue)
		}
	}

	// Only create sub-groups if we have meaningful partitioning
	if len(subGroups) < 2 {
		return
	}

	// Sort sub-groups by size (descending)
	type subGroup struct {
		label  string
		issues []model.Issue
	}
	var sortedSubs []subGroup
	for label, issues := range subGroups {
		if len(issues) >= 2 {
			sortedSubs = append(sortedSubs, subGroup{label, issues})
		} else {
			// Add small groups to noSecondary
			noSecondary = append(noSecondary, issues...)
		}
	}
	sort.Slice(sortedSubs, func(i, j int) bool {
		return len(sortedSubs[i].issues) > len(sortedSubs[j].issues)
	})

	// Convert to sub-workstreams
	for _, sg := range sortedSubs {
		sub := m.buildWorkstreamFromIssues(sg.label, sg.issues)
		sub.Depth = 1
		parent.SubWorkstreams = append(parent.SubWorkstreams, &sub)
	}

	if len(noSecondary) > 0 {
		sub := m.buildWorkstreamFromIssues("Core", noSecondary)
		sub.Depth = 1
		parent.SubWorkstreams = append(parent.SubWorkstreams, &sub)
	}
}

// buildGroupedByPriority groups issues by priority level
func (m *LensDashboardModel) buildGroupedByPriority() []analysis.Workstream {
	priorityNames := []string{"P0 Critical", "P1 High", "P2 Medium", "P3+ Other"}
	groups := make([][]model.Issue, 4)

	for _, issue := range m.allIssues {
		if !m.primaryIDs[issue.ID] {
			continue
		}

		var idx int
		switch {
		case issue.Priority == 0:
			idx = 0
		case issue.Priority == 1:
			idx = 1
		case issue.Priority == 2:
			idx = 2
		default:
			idx = 3
		}
		groups[idx] = append(groups[idx], issue)
	}

	var result []analysis.Workstream
	for i, issues := range groups {
		if len(issues) > 0 {
			ws := m.buildWorkstreamFromIssues(priorityNames[i], issues)
			result = append(result, ws)
		}
	}
	return result
}

// buildGroupedByStatus groups issues by status
func (m *LensDashboardModel) buildGroupedByStatus() []analysis.Workstream {
	statusNames := map[model.Status]string{
		model.StatusOpen:       "Open",
		model.StatusInProgress: "In Progress",
		model.StatusBlocked:    "Blocked",
		model.StatusClosed:     "Closed",
	}
	statusOrder := []model.Status{model.StatusOpen, model.StatusInProgress, model.StatusBlocked, model.StatusClosed}
	groups := make(map[model.Status][]model.Issue)

	for _, issue := range m.allIssues {
		if !m.primaryIDs[issue.ID] {
			continue
		}
		groups[issue.Status] = append(groups[issue.Status], issue)
	}

	var result []analysis.Workstream
	for _, status := range statusOrder {
		if issues, ok := groups[status]; ok && len(issues) > 0 {
			ws := m.buildWorkstreamFromIssues(statusNames[status], issues)
			result = append(result, ws)
		}
	}
	return result
}

// buildGroupedSections builds the grouped sections based on current groupByMode
func (m *LensDashboardModel) buildGroupedSections() {
	switch m.groupByMode {
	case GroupByLabel:
		m.groupedSections = m.buildGroupedByLabel()
	case GroupByPriority:
		m.groupedSections = m.buildGroupedByPriority()
	case GroupByStatus:
		m.groupedSections = m.buildGroupedByStatus()
	default:
		m.groupedSections = m.buildGroupedByLabel()
	}

	// Initialize expansion state - expand first group by default
	if m.groupedExpanded == nil {
		m.groupedExpanded = make(map[int]bool)
	}
	if len(m.groupedSections) > 0 && len(m.groupedExpanded) == 0 {
		m.groupedExpanded[0] = true
	}

	// Initialize sub-expansion state
	if m.groupedSubExpanded == nil {
		m.groupedSubExpanded = make(map[int]map[int]bool)
	}
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
	var sorted []LabelCount
	for label, count := range cooccurring {
		sorted = append(sorted, LabelCount{Label: label, Count: count})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Count > sorted[j].Count
	})

	var result []string
	for _, lc := range sorted {
		result = append(result, lc.Label)
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
	// Enable split view mode for wide terminals
	m.splitViewMode = width >= LensSplitViewThreshold
}

// MoveUp moves cursor up
func (m *LensDashboardModel) MoveUp() {
	if m.viewType == ViewTypeGrouped && len(m.groupedSections) > 0 {
		m.moveUpGrouped()
		m.updateDetailContent()
		return
	}

	if m.viewType == ViewTypeWorkstream && len(m.workstreams) > 1 {
		m.moveUpWS()
		m.updateDetailContent()
		return
	}

	// Handle centered mode navigation
	if m.centeredMode && (m.viewMode == "epic" || m.viewMode == "bead") && m.egoNode != nil {
		if m.cursor > 0 {
			m.cursor--
			m.selectedIssueID = m.getSelectedIDForCenteredMode()
			m.ensureCenteredVisible()
			m.updateDetailContent()
		}
		return
	}

	// Standard flat view navigation
	if m.cursor > 0 {
		m.cursor--
		m.selectedIssueID = m.flatNodes[m.cursor].Node.Issue.ID
		m.ensureVisible()
		m.updateDetailContent()
	}
}

// MoveDown moves cursor down
func (m *LensDashboardModel) MoveDown() {
	if m.viewType == ViewTypeGrouped && len(m.groupedSections) > 0 {
		m.moveDownGrouped()
		m.updateDetailContent()
		return
	}

	if m.viewType == ViewTypeWorkstream && len(m.workstreams) > 1 {
		m.moveDownWS()
		m.updateDetailContent()
		return
	}

	// Handle centered mode navigation
	if m.centeredMode && (m.viewMode == "epic" || m.viewMode == "bead") && m.egoNode != nil {
		totalNodes := m.getTotalCenteredNodeCount()
		if m.cursor < totalNodes-1 {
			m.cursor++
			m.selectedIssueID = m.getSelectedIDForCenteredMode()
			m.ensureCenteredVisible()
			m.updateDetailContent()
		}
		return
	}

	// Standard flat view navigation
	if m.cursor < len(m.flatNodes)-1 {
		m.cursor++
		m.selectedIssueID = m.flatNodes[m.cursor].Node.Issue.ID
		m.ensureVisible()
		m.updateDetailContent()
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

// moveUpGrouped moves cursor up in grouped view
func (m *LensDashboardModel) moveUpGrouped() {
	if len(m.groupedSections) == 0 {
		return
	}

	group := m.groupedSections[m.groupedCursor]
	_ = len(group.SubWorkstreams) > 0 && m.groupedExpanded[m.groupedCursor] // hasSubGroups used below

	if m.groupedIssueCursor > 0 {
		// Move up within current issues
		m.groupedIssueCursor--
	} else if m.groupedIssueCursor == 0 {
		// At first issue, go to sub-group header (if in sub-group) or group header
		m.groupedIssueCursor = -1
	} else if m.groupedSubCursor >= 0 && len(group.SubWorkstreams) > 0 {
		// At sub-group header, check previous sub-group or go to group header
		if m.groupedSubCursor > 0 && m.groupedSubCursor <= len(group.SubWorkstreams) {
			// Go to previous sub-group's last issue or header
			m.groupedSubCursor--
			subGroup := group.SubWorkstreams[m.groupedSubCursor]
			if m.groupedSubExpanded[m.groupedCursor] != nil && m.groupedSubExpanded[m.groupedCursor][m.groupedSubCursor] && subGroup != nil {
				m.groupedIssueCursor = len(subGroup.Issues) - 1
				if m.groupedIssueCursor < 0 {
					m.groupedIssueCursor = -1
				}
			} else {
				m.groupedIssueCursor = -1
			}
		} else {
			// At first sub-group, go to group header
			m.groupedSubCursor = -1
			m.groupedIssueCursor = -1
		}
	} else if m.groupedSubCursor >= 0 {
		// Invalid state: sub-cursor set but no sub-groups, reset
		m.groupedSubCursor = -1
		m.groupedIssueCursor = -1
	} else if m.groupedCursor > 0 {
		// At group header, go to previous group
		m.groupedCursor--
		prevGroup := m.groupedSections[m.groupedCursor]
		prevHasSubGroups := len(prevGroup.SubWorkstreams) > 0 && m.groupedExpanded[m.groupedCursor]

		if prevHasSubGroups {
			// Go to last sub-group
			m.groupedSubCursor = len(prevGroup.SubWorkstreams) - 1
			subGroup := prevGroup.SubWorkstreams[m.groupedSubCursor]
			if m.groupedSubExpanded[m.groupedCursor] != nil && m.groupedSubExpanded[m.groupedCursor][m.groupedSubCursor] && subGroup != nil {
				m.groupedIssueCursor = len(subGroup.Issues) - 1
				if m.groupedIssueCursor < 0 {
					m.groupedIssueCursor = -1
				}
			} else {
				m.groupedIssueCursor = -1
			}
		} else if m.groupedExpanded[m.groupedCursor] && len(prevGroup.Issues) > 0 {
			// Go to last issue in previous group
			m.groupedSubCursor = -1
			m.groupedIssueCursor = len(prevGroup.Issues) - 1
		} else {
			// Go to previous group header
			m.groupedSubCursor = -1
			m.groupedIssueCursor = -1
		}
	}

	m.updateSelectedIssueFromGrouped()
	m.ensureGroupedVisible()
}

// moveDownGrouped moves cursor down in grouped view
func (m *LensDashboardModel) moveDownGrouped() {
	if len(m.groupedSections) == 0 {
		return
	}

	group := m.groupedSections[m.groupedCursor]
	hasSubGroups := len(group.SubWorkstreams) > 0
	isGroupExpanded := m.groupedExpanded[m.groupedCursor]

	if m.groupedSubCursor >= 0 && len(group.SubWorkstreams) > m.groupedSubCursor {
		// We're in a sub-group
		subGroup := group.SubWorkstreams[m.groupedSubCursor]
		isSubExpanded := m.groupedSubExpanded[m.groupedCursor] != nil && m.groupedSubExpanded[m.groupedCursor][m.groupedSubCursor]
		subIssueCount := 0
		if subGroup != nil {
			subIssueCount = len(subGroup.Issues)
		}

		if m.groupedIssueCursor < 0 {
			// At sub-group header
			if isSubExpanded && subIssueCount > 0 {
				m.groupedIssueCursor = 0
			} else if m.groupedSubCursor < len(group.SubWorkstreams)-1 {
				// Go to next sub-group
				m.groupedSubCursor++
				m.groupedIssueCursor = -1
			} else if m.groupedCursor < len(m.groupedSections)-1 {
				// Go to next group
				m.groupedCursor++
				m.groupedSubCursor = -1
				m.groupedIssueCursor = -1
			}
		} else if m.groupedIssueCursor < subIssueCount-1 {
			// Move down within sub-group issues
			m.groupedIssueCursor++
		} else if m.groupedSubCursor < len(group.SubWorkstreams)-1 {
			// Go to next sub-group
			m.groupedSubCursor++
			m.groupedIssueCursor = -1
		} else if m.groupedCursor < len(m.groupedSections)-1 {
			// Go to next group
			m.groupedCursor++
			m.groupedSubCursor = -1
			m.groupedIssueCursor = -1
		}
	} else if m.groupedSubCursor >= 0 {
		// Invalid state: sub-cursor set but no sub-groups, reset and go to next group
		m.groupedSubCursor = -1
		m.groupedIssueCursor = -1
		if m.groupedCursor < len(m.groupedSections)-1 {
			m.groupedCursor++
		}
	} else if m.groupedIssueCursor >= 0 {
		// We're in group issues (no sub-groups)
		if m.groupedIssueCursor < len(group.Issues)-1 {
			m.groupedIssueCursor++
		} else if m.groupedCursor < len(m.groupedSections)-1 {
			// Go to next group
			m.groupedCursor++
			m.groupedSubCursor = -1
			m.groupedIssueCursor = -1
		}
	} else {
		// At group header
		if isGroupExpanded && hasSubGroups {
			// Go to first sub-group
			m.groupedSubCursor = 0
			m.groupedIssueCursor = -1
		} else if isGroupExpanded && len(group.Issues) > 0 {
			// Go to first issue
			m.groupedIssueCursor = 0
		} else if m.groupedCursor < len(m.groupedSections)-1 {
			// Go to next group
			m.groupedCursor++
			m.groupedSubCursor = -1
			m.groupedIssueCursor = -1
		}
	}

	m.updateSelectedIssueFromGrouped()
	m.ensureGroupedVisible()
}

// getVisibleGroupedIssueCount returns the number of visible issues for a grouped section
func (m *LensDashboardModel) getVisibleGroupedIssueCount(gIdx int) int {
	if gIdx >= len(m.groupedSections) {
		return 0
	}
	group := m.groupedSections[gIdx]
	isExpanded := m.groupedExpanded[gIdx]

	if !isExpanded {
		return 0 // Collapsed: no issues visible
	}

	// If there are sub-groups, issues are shown within sub-groups
	if len(group.SubWorkstreams) > 0 {
		return 0 // Issues in sub-groups, not directly navigable at group level
	}

	return len(group.Issues)
}

// getTotalGroupedLines calculates total lines in grouped view
func (m *LensDashboardModel) getTotalGroupedLines() int {
	totalLines := 0
	for i, group := range m.groupedSections {
		totalLines++ // Header line
		if m.groupedExpanded[i] {
			if len(group.SubWorkstreams) == 0 {
				totalLines += len(group.Issues)
			} else {
				for j, sub := range group.SubWorkstreams {
					if sub == nil {
						continue
					}
					totalLines++ // Sub-group header
					if m.groupedSubExpanded[i] != nil && m.groupedSubExpanded[i][j] {
						totalLines += len(sub.Issues)
					}
				}
			}
		}
		totalLines++ // Empty line between groups
	}
	return totalLines
}

// ensureGroupedVisible ensures the current cursor position is visible
func (m *LensDashboardModel) ensureGroupedVisible() {
	// Calculate line position for current cursor
	// This must match the rendering logic in renderGroupedView()
	linePos := 0

	for i := 0; i < m.groupedCursor; i++ {
		linePos++ // Header line
		if m.groupedExpanded[i] {
			group := m.groupedSections[i]
			if len(group.SubWorkstreams) == 0 {
				// No sub-groups: just add issue count
				linePos += len(group.Issues)
			} else {
				// Has sub-groups: add each sub-group header + expanded issues
				for j, sub := range group.SubWorkstreams {
					if sub == nil {
						continue
					}
					linePos++ // Sub-group header
					if m.groupedSubExpanded[i] != nil && m.groupedSubExpanded[i][j] {
						linePos += len(sub.Issues)
					}
				}
			}
		}
		linePos++ // Empty line between groups
	}

	// Add current group header
	linePos++

	// Handle position within current group
	if m.groupedCursor >= 0 && m.groupedCursor < len(m.groupedSections) {
		group := m.groupedSections[m.groupedCursor]

		if m.groupedExpanded[m.groupedCursor] {
			if len(group.SubWorkstreams) == 0 {
				// No sub-groups: issue cursor directly under group
				if m.groupedIssueCursor >= 0 {
					linePos += m.groupedIssueCursor + 1
				}
			} else {
				// Has sub-groups
				if m.groupedSubCursor >= 0 {
					// Add lines for sub-groups before current sub-cursor
					for j := 0; j < m.groupedSubCursor && j < len(group.SubWorkstreams); j++ {
						sub := group.SubWorkstreams[j]
						if sub == nil {
							continue
						}
						linePos++ // Sub-group header
						if m.groupedSubExpanded[m.groupedCursor] != nil && m.groupedSubExpanded[m.groupedCursor][j] {
							linePos += len(sub.Issues)
						}
					}
					// Add current sub-group header
					linePos++
					// Add issue position within sub-group
					if m.groupedIssueCursor >= 0 {
						linePos += m.groupedIssueCursor + 1
					}
				}
			}
		}
	}

	// Calculate visible lines using viewport config
	// renderGroupedView adds 2 lines for header, rest is content
	vp := m.calculateViewport()
	visibleLines := vp.ContentHeight - 2
	if visibleLines < 5 {
		visibleLines = 5
	}

	// Center cursor in viewport (scrolloff = half viewport height)
	scrolloff := visibleLines / 2
	targetScroll := linePos - scrolloff
	if targetScroll < 0 {
		targetScroll = 0
	}

	// NOTE: We intentionally do NOT clamp to maxScroll here.
	// This allows the last items to be centered with empty space below them.
	// The render function handles padding when scroll goes past content.

	m.groupedScroll = targetScroll
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

// ensureVisibleWS adjusts wsScroll to keep cursor centered in viewport
func (m *LensDashboardModel) ensureVisibleWS() {
	// Calculate the line number of the current cursor position
	cursorLine := m.getWSCursorLine()

	// Calculate visible lines using viewport config
	// renderWorkstreamView adds 2 lines for header, rest is content
	vp := m.calculateViewport()
	visibleLines := vp.ContentHeight - 2
	if visibleLines < 3 {
		visibleLines = 3
	}

	// Center cursor in viewport (scrolloff = half viewport height)
	scrolloff := visibleLines / 2
	targetScroll := cursorLine - scrolloff
	if targetScroll < 0 {
		targetScroll = 0
	}

	// NOTE: We intentionally do NOT clamp to maxScroll here.
	// This allows the last items to be centered with empty space below them.
	// The render function handles padding when scroll goes past content.

	m.wsScroll = targetScroll
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

// getFlatLinePosition returns the line position for a given flatNodes index
// accounting for status headers that appear when status changes
func (m *LensDashboardModel) getFlatLinePosition(nodeIdx int) int {
	if nodeIdx < 0 || len(m.flatNodes) == 0 {
		return 0
	}
	if nodeIdx >= len(m.flatNodes) {
		nodeIdx = len(m.flatNodes) - 1
	}

	linePos := 0
	lastStatus := ""
	for i := 0; i <= nodeIdx; i++ {
		if m.flatNodes[i].Status != lastStatus {
			linePos++ // status header
			lastStatus = m.flatNodes[i].Status
		}
		if i < nodeIdx {
			linePos++ // node line (don't count target node itself)
		}
	}
	return linePos
}

// getTotalFlatLines returns total lines for flat view including status headers
func (m *LensDashboardModel) getTotalFlatLines() int {
	if len(m.flatNodes) == 0 {
		return 0
	}
	lines := 0
	lastStatus := ""
	for _, fn := range m.flatNodes {
		if fn.Status != lastStatus {
			lines++ // status header
			lastStatus = fn.Status
		}
		lines++ // node line
	}
	return lines
}

// ensureVisible adjusts scroll to keep cursor centered in viewport
// NOTE: For flat view, m.scroll stores LINE position (not node index)
func (m *LensDashboardModel) ensureVisible() {
	if len(m.flatNodes) == 0 {
		m.scroll = 0
		return
	}

	vp := m.calculateViewport()
	visibleLines := vp.ContentHeight
	if visibleLines < 5 {
		visibleLines = 5
	}

	// Get line position of cursor (accounting for status headers)
	cursorLine := m.getFlatLinePosition(m.cursor)

	// Center cursor in viewport (scrolloff = half viewport height)
	scrolloff := visibleLines / 2
	targetScrollLine := cursorLine - scrolloff
	if targetScrollLine < 0 {
		targetScrollLine = 0
	}

	// NOTE: We intentionally do NOT clamp to maxScrollLine here.
	// This allows the last items to be centered with empty space below them.
	// The render function handles padding when scroll goes past content.

	m.scroll = targetScrollLine
}

// findNodeForLine finds the flatNodes index for a given line position
func (m *LensDashboardModel) findNodeForLine(targetLine int) int {
	if len(m.flatNodes) == 0 || targetLine <= 0 {
		return 0
	}

	linePos := 0
	lastStatus := ""
	for i, fn := range m.flatNodes {
		if fn.Status != lastStatus {
			linePos++ // status header
			lastStatus = fn.Status
		}
		if linePos >= targetLine {
			return i
		}
		linePos++ // node line
	}
	return len(m.flatNodes) - 1
}

// ensureCenteredVisible adjusts scroll to keep cursor visible in centered mode
func (m *LensDashboardModel) ensureCenteredVisible() {
	if !m.centeredMode || m.egoNode == nil {
		return
	}

	vp := m.calculateViewport()
	visibleLines := vp.ContentHeight

	// Center cursor in viewport (scrolloff = half viewport height)
	scrolloff := visibleLines / 2
	targetScroll := m.cursor - scrolloff
	if targetScroll < 0 {
		targetScroll = 0
	}

	// NOTE: We intentionally do NOT clamp to maxScroll here.
	// This allows the last items to be centered with empty space below them.
	// The render function handles padding when scroll goes past content.

	m.scroll = targetScroll
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
	} else if m.viewType == ViewTypeGrouped {
		// From grouped view, go to workstream view
		m.viewType = ViewTypeWorkstream
		m.wsCursor = 0
		m.wsIssueCursor = -1
		m.updateSelectedIssueFromWS()
	} else {
		// From workstream view, go to flat view
		m.viewType = ViewTypeFlat
	}
}

// IsGroupedView returns true if in grouped view mode
func (m *LensDashboardModel) IsGroupedView() bool {
	return m.viewType == ViewTypeGrouped
}

// EnterGroupedView switches to grouped view mode
func (m *LensDashboardModel) EnterGroupedView() {
	m.viewType = ViewTypeGrouped
	// Build grouped sections
	m.buildGroupedSections()
	// Initialize cursor to first group header
	m.groupedCursor = 0
	m.groupedSubCursor = -1
	m.groupedIssueCursor = -1
	m.groupedScroll = 0
	// Update selected issue
	m.updateSelectedIssueFromGrouped()
}

// ExitGroupedView switches back to flat view mode
func (m *LensDashboardModel) ExitGroupedView() {
	m.viewType = ViewTypeFlat
}

// CycleGroupByMode cycles through grouping modes: Label -> Priority -> Status -> Label
func (m *LensDashboardModel) CycleGroupByMode() {
	switch m.groupByMode {
	case GroupByLabel:
		m.groupByMode = GroupByPriority
	case GroupByPriority:
		m.groupByMode = GroupByStatus
	case GroupByStatus:
		m.groupByMode = GroupByLabel
	default:
		m.groupByMode = GroupByLabel
	}
	// Rebuild grouped sections with new mode
	m.buildGroupedSections()
	// Reset cursor
	m.groupedCursor = 0
	m.groupedIssueCursor = -1
	m.groupedScroll = 0
	m.updateSelectedIssueFromGrouped()
}

// GetGroupByMode returns the current grouping mode
func (m *LensDashboardModel) GetGroupByMode() GroupByMode {
	return m.groupByMode
}

// updateSelectedIssueFromGrouped updates the selected issue ID based on grouped view cursor
func (m *LensDashboardModel) updateSelectedIssueFromGrouped() {
	if m.groupedIssueCursor < 0 {
		// On group or sub-group header, no specific issue selected
		m.selectedIssueID = ""
		return
	}

	if m.groupedCursor < 0 || m.groupedCursor >= len(m.groupedSections) {
		m.selectedIssueID = ""
		return
	}

	group := m.groupedSections[m.groupedCursor]

	// Check if we're in a sub-group
	if m.groupedSubCursor >= 0 && m.groupedSubCursor < len(group.SubWorkstreams) {
		subGroup := group.SubWorkstreams[m.groupedSubCursor]
		if subGroup != nil && m.groupedIssueCursor < len(subGroup.Issues) {
			m.selectedIssueID = subGroup.Issues[m.groupedIssueCursor].ID
		} else {
			m.selectedIssueID = ""
		}
		return
	}

	// Not in sub-group, check group issues
	if m.groupedIssueCursor < len(group.Issues) {
		m.selectedIssueID = group.Issues[m.groupedIssueCursor].ID
	} else {
		m.selectedIssueID = ""
	}
}

// ToggleGroupedExpand toggles expansion of the current grouped section or sub-group
func (m *LensDashboardModel) ToggleGroupedExpand() {
	if m.groupedCursor < 0 || m.groupedCursor >= len(m.groupedSections) {
		return
	}

	group := m.groupedSections[m.groupedCursor]

	// Check if we're on a sub-group header
	if m.groupedSubCursor >= 0 && m.groupedSubCursor < len(group.SubWorkstreams) && m.groupedIssueCursor < 0 {
		// Toggle sub-group expansion
		if m.groupedSubExpanded[m.groupedCursor] == nil {
			m.groupedSubExpanded[m.groupedCursor] = make(map[int]bool)
		}
		m.groupedSubExpanded[m.groupedCursor][m.groupedSubCursor] = !m.groupedSubExpanded[m.groupedCursor][m.groupedSubCursor]
		return
	}

	// Toggle group expansion
	if m.groupedIssueCursor < 0 && m.groupedSubCursor < 0 {
		m.groupedExpanded[m.groupedCursor] = !m.groupedExpanded[m.groupedCursor]
		// If collapsing, reset sub-cursor
		if !m.groupedExpanded[m.groupedCursor] {
			m.groupedIssueCursor = -1
		}
	}
}

// ToggleGroupedTreeView toggles tree view within grouped sections
func (m *LensDashboardModel) ToggleGroupedTreeView() {
	m.groupedTreeView = !m.groupedTreeView
}

// IsGroupedTreeView returns true if tree view is enabled for grouped sections
func (m *LensDashboardModel) IsGroupedTreeView() bool {
	return m.groupedTreeView
}

// IsGroupExpanded returns true if the group at the given index is expanded
func (m *LensDashboardModel) IsGroupExpanded(idx int) bool {
	return m.groupedExpanded[idx]
}

// GetGroupedCursor returns the current grouped view cursor position
func (m *LensDashboardModel) GetGroupedCursor() int {
	return m.groupedCursor
}

// CurrentGroupName returns the name of the currently selected group
func (m *LensDashboardModel) CurrentGroupName() string {
	if m.groupedCursor >= 0 && m.groupedCursor < len(m.groupedSections) {
		return m.groupedSections[m.groupedCursor].Name
	}
	return ""
}

// NextGroup moves to the next group or sub-group with auto-expand/collapse
func (m *LensDashboardModel) NextGroup() {
	if m.groupedCursor < 0 || m.groupedCursor >= len(m.groupedSections) {
		return
	}

	group := m.groupedSections[m.groupedCursor]

	// If on an issue, jump to the header (sub-group or group)
	if m.groupedIssueCursor >= 0 {
		m.groupedIssueCursor = -1
		m.updateSelectedIssueFromGrouped()
		m.ensureGroupedVisible()
		return
	}

	// If on a sub-group header
	if m.groupedSubCursor >= 0 && len(group.SubWorkstreams) > 0 {
		// Collapse current sub-group
		if m.groupedSubExpanded[m.groupedCursor] != nil {
			m.groupedSubExpanded[m.groupedCursor][m.groupedSubCursor] = false
		}

		// Try to go to next sub-group
		if m.groupedSubCursor < len(group.SubWorkstreams)-1 {
			m.groupedSubCursor++
			// Expand new sub-group
			if m.groupedSubExpanded[m.groupedCursor] == nil {
				m.groupedSubExpanded[m.groupedCursor] = make(map[int]bool)
			}
			m.groupedSubExpanded[m.groupedCursor][m.groupedSubCursor] = true
			m.groupedIssueCursor = -1
			m.updateSelectedIssueFromGrouped()
			m.ensureGroupedVisible()
			return
		}
		// At last sub-group, go to next group
		m.groupedSubCursor = -1
	}

	// On group header - check if we should enter sub-groups
	if !m.groupedExpanded[m.groupedCursor] {
		// Expand current group and enter first sub-group (if any)
		m.groupedExpanded[m.groupedCursor] = true
		if len(group.SubWorkstreams) > 0 {
			m.groupedSubCursor = 0
			if m.groupedSubExpanded[m.groupedCursor] == nil {
				m.groupedSubExpanded[m.groupedCursor] = make(map[int]bool)
			}
			m.groupedSubExpanded[m.groupedCursor][0] = true
		}
		m.updateSelectedIssueFromGrouped()
		m.ensureGroupedVisible()
		return
	}

	// Already expanded - if has sub-groups and not in them yet, enter first sub-group
	if m.groupedSubCursor < 0 && len(group.SubWorkstreams) > 0 && m.groupedExpanded[m.groupedCursor] {
		m.groupedSubCursor = 0
		if m.groupedSubExpanded[m.groupedCursor] == nil {
			m.groupedSubExpanded[m.groupedCursor] = make(map[int]bool)
		}
		m.groupedSubExpanded[m.groupedCursor][0] = true
		m.updateSelectedIssueFromGrouped()
		m.ensureGroupedVisible()
		return
	}

	// Move to next group
	if m.groupedCursor < len(m.groupedSections)-1 {
		m.groupedCursor++
		m.groupedIssueCursor = -1
		m.groupedSubCursor = -1
		m.updateSelectedIssueFromGrouped()
		m.ensureGroupedVisible()
	}
}

// PrevGroup moves to the previous group or sub-group with auto-expand/collapse
func (m *LensDashboardModel) PrevGroup() {
	if m.groupedCursor < 0 || m.groupedCursor >= len(m.groupedSections) {
		return
	}

	group := m.groupedSections[m.groupedCursor]

	// If on an issue, jump to the header (sub-group or group)
	if m.groupedIssueCursor >= 0 {
		m.groupedIssueCursor = -1
		m.updateSelectedIssueFromGrouped()
		m.ensureGroupedVisible()
		return
	}

	// If on a sub-group header
	if m.groupedSubCursor >= 0 && len(group.SubWorkstreams) > 0 {
		// Collapse current sub-group
		if m.groupedSubExpanded[m.groupedCursor] != nil {
			m.groupedSubExpanded[m.groupedCursor][m.groupedSubCursor] = false
		}

		// Try to go to previous sub-group
		if m.groupedSubCursor > 0 {
			m.groupedSubCursor--
			// Expand new sub-group
			if m.groupedSubExpanded[m.groupedCursor] == nil {
				m.groupedSubExpanded[m.groupedCursor] = make(map[int]bool)
			}
			m.groupedSubExpanded[m.groupedCursor][m.groupedSubCursor] = true
			m.groupedIssueCursor = -1
			m.updateSelectedIssueFromGrouped()
			m.ensureGroupedVisible()
			return
		}
		// At first sub-group, go to group header
		m.groupedSubCursor = -1
		m.groupedIssueCursor = -1
		m.updateSelectedIssueFromGrouped()
		m.ensureGroupedVisible()
		return
	}

	// On group header - move to previous group
	if m.groupedCursor > 0 {
		m.groupedCursor--
		m.groupedIssueCursor = -1
		// If previous group has sub-groups and is expanded, go to last sub-group
		prevGroup := m.groupedSections[m.groupedCursor]
		if m.groupedExpanded[m.groupedCursor] && len(prevGroup.SubWorkstreams) > 0 {
			m.groupedSubCursor = len(prevGroup.SubWorkstreams) - 1
			// Expand the last sub-group
			if m.groupedSubExpanded[m.groupedCursor] == nil {
				m.groupedSubExpanded[m.groupedCursor] = make(map[int]bool)
			}
			m.groupedSubExpanded[m.groupedCursor][m.groupedSubCursor] = true
		} else {
			m.groupedSubCursor = -1
		}
		m.updateSelectedIssueFromGrouped()
		m.ensureGroupedVisible()
	}
}

// ExpandGroup expands the current group (does not collapse)
func (m *LensDashboardModel) ExpandGroup() {
	if m.groupedCursor >= 0 && m.groupedCursor < len(m.groupedSections) {
		m.groupedExpanded[m.groupedCursor] = true
	}
}

// ExpandAllGroups expands all groups and sub-groups
func (m *LensDashboardModel) ExpandAllGroups() {
	for i := range m.groupedSections {
		m.groupedExpanded[i] = true
		// Also expand all sub-groups
		if m.groupedSubExpanded[i] == nil {
			m.groupedSubExpanded[i] = make(map[int]bool)
		}
		for j := range m.groupedSections[i].SubWorkstreams {
			m.groupedSubExpanded[i][j] = true
		}
	}
}

// CollapseAllGroups collapses all groups and sub-groups
func (m *LensDashboardModel) CollapseAllGroups() {
	for i := range m.groupedSections {
		m.groupedExpanded[i] = false
		// Also collapse all sub-groups
		if m.groupedSubExpanded[i] != nil {
			for j := range m.groupedSubExpanded[i] {
				m.groupedSubExpanded[i][j] = false
			}
		}
	}
	// Reset cursor to group level
	m.groupedIssueCursor = -1
	m.groupedSubCursor = -1
}

// ExpandAllWorkstreams expands all workstreams
func (m *LensDashboardModel) ExpandAllWorkstreams() {
	for i := range m.workstreams {
		m.wsExpanded[i] = true
		// Also expand all sub-workstreams
		if m.subWSExpanded[i] == nil {
			m.subWSExpanded[i] = make(map[int]bool)
		}
		if len(m.workstreams[i].SubWorkstreams) > 0 {
			for j := range m.workstreams[i].SubWorkstreams {
				m.subWSExpanded[i][j] = true
			}
		}
	}
}

// CollapseAllWorkstreams collapses all workstreams
func (m *LensDashboardModel) CollapseAllWorkstreams() {
	for i := range m.workstreams {
		m.wsExpanded[i] = false
		// Also collapse all sub-workstreams
		if m.subWSExpanded[i] != nil {
			for j := range m.subWSExpanded[i] {
				m.subWSExpanded[i][j] = false
			}
		}
	}
	// Reset cursor to workstream level
	m.wsIssueCursor = -1
	if m.subWsCursor != nil {
		for k := range m.subWsCursor {
			m.subWsCursor[k] = -1
		}
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

// NextWorkstream moves to the next workstream with auto-expand/collapse
func (m *LensDashboardModel) NextWorkstream() {
	if len(m.workstreams) == 0 {
		return
	}

	// If on an issue, jump to the workstream header first
	if m.wsIssueCursor >= 0 {
		m.wsIssueCursor = -1
		m.updateSelectedIssueFromWS()
		return
	}

	// Move to next workstream
	if m.wsCursor < len(m.workstreams)-1 {
		// Collapse current workstream
		m.wsExpanded[m.wsCursor] = false

		m.wsCursor++
		m.wsIssueCursor = -1 // Go to header

		// Expand new workstream
		m.wsExpanded[m.wsCursor] = true

		m.updateSelectedIssueFromWS()
	}
}

// PrevWorkstream moves to the previous workstream with auto-expand/collapse
func (m *LensDashboardModel) PrevWorkstream() {
	if len(m.workstreams) == 0 {
		return
	}

	// If on an issue, jump to the workstream header first
	if m.wsIssueCursor >= 0 {
		m.wsIssueCursor = -1
		m.updateSelectedIssueFromWS()
		return
	}

	// Move to previous workstream
	if m.wsCursor > 0 {
		// Collapse current workstream
		m.wsExpanded[m.wsCursor] = false

		m.wsCursor--
		m.wsIssueCursor = -1 // Go to header

		// Expand new workstream
		m.wsExpanded[m.wsCursor] = true

		m.updateSelectedIssueFromWS()
	}
}

// GoToTop moves cursor to the first item
func (m *LensDashboardModel) GoToTop() {
	// Grouped view
	if m.viewType == ViewTypeGrouped && len(m.groupedSections) > 0 {
		m.groupedCursor = 0
		m.groupedSubCursor = -1
		m.groupedIssueCursor = -1
		m.groupedScroll = 0
		m.updateSelectedIssueFromGrouped()
		return
	}

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
	// Grouped view
	if m.viewType == ViewTypeGrouped && len(m.groupedSections) > 0 {
		m.groupedCursor = len(m.groupedSections) - 1
		group := m.groupedSections[m.groupedCursor]

		// Navigate to last item in last group
		if m.groupedExpanded[m.groupedCursor] {
			if len(group.SubWorkstreams) > 0 {
				m.groupedSubCursor = len(group.SubWorkstreams) - 1
				// Navigate to last issue in last subgroup if expanded
				if m.groupedSubExpanded[m.groupedCursor] != nil && m.groupedSubExpanded[m.groupedCursor][m.groupedSubCursor] {
					subGroup := group.SubWorkstreams[m.groupedSubCursor]
					if subGroup != nil && len(subGroup.Issues) > 0 {
						m.groupedIssueCursor = len(subGroup.Issues) - 1
					} else {
						m.groupedIssueCursor = -1
					}
				} else {
					m.groupedIssueCursor = -1
				}
			} else if len(group.Issues) > 0 {
				m.groupedSubCursor = -1
				m.groupedIssueCursor = len(group.Issues) - 1
			} else {
				m.groupedSubCursor = -1
				m.groupedIssueCursor = -1
			}
		} else {
			m.groupedSubCursor = -1
			m.groupedIssueCursor = -1
		}
		m.ensureGroupedVisible()
		m.updateSelectedIssueFromGrouped()
		return
	}

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
		m.ensureCenteredVisible()
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
		m.ensureCenteredVisible()
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
		m.ensureCenteredVisible()
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

// fixRootsStructure corrects IsLastChild and ParentPath values for a set of roots.
// Same as fixTreeStructure but works on a provided slice instead of m.roots.
func (m *LensDashboardModel) fixRootsStructure(roots []*LensTreeNode) {
	for i, root := range roots {
		isLast := i == len(roots)-1
		root.IsLastChild = isLast
		root.ParentPath = nil
		m.fixNodeChildren(root, nil)
	}
}

// flattenWSTree converts workstream tree to flat list for display
func (m *LensDashboardModel) flattenWSTree(roots []*LensTreeNode) []LensFlatNode {
	// Fix tree structure before flattening
	m.fixRootsStructure(roots)

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
	// Use split view for wide terminals
	if m.splitViewMode {
		return m.renderSplitView()
	}

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

	// Calculate visible area using viewport config
	vp := m.calculateViewport()
	visibleLines := vp.ContentHeight

	// Render based on view type
	var contentLines []string
	if m.viewType == ViewTypeGrouped && len(m.groupedSections) > 0 {
		// Render grouped view
		contentLines = m.renderGroupedView(contentWidth, visibleLines, statsStyle)
	} else if m.viewType == ViewTypeWorkstream && len(m.workstreams) > 1 {
		// Render workstream view
		contentLines = m.renderWorkstreamView(contentWidth, visibleLines, statsStyle)
	} else if m.centeredMode && (m.viewMode == "epic" || m.viewMode == "bead") && m.egoNode != nil {
		// Render ego-centered view for epic/bead modes
		contentLines = m.renderCenteredView(contentWidth, visibleLines, statsStyle)
	} else {
		// Render flat tree view
		contentLines = m.renderFlatView(contentWidth, visibleLines, statsStyle)
	}

	// Truncate content to exactly ContentHeight for fixed footer positioning
	if len(contentLines) > visibleLines {
		contentLines = contentLines[:visibleLines]
	}
	// Pad if needed
	for len(contentLines) < visibleLines {
		contentLines = append(contentLines, "")
	}
	lines = append(lines, contentLines...)

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

// renderFlatView renders the flat tree view using line-based scrolling
func (m *LensDashboardModel) renderFlatView(contentWidth, visibleLines int, statsStyle lipgloss.Style) []string {
	t := m.theme

	if len(m.flatNodes) == 0 {
		emptyStyle := t.Renderer.NewStyle().Foreground(t.Subtext).Italic(true)
		return []string{emptyStyle.Render("  No issues found")}
	}

	// Build ALL lines first (including status headers)
	var allLines []string
	lastStatus := ""
	for i, fn := range m.flatNodes {
		// Add status header when status changes
		if fn.Status != lastStatus {
			statusHeader := m.renderStatusHeader(fn.Status)
			allLines = append(allLines, statusHeader)
			lastStatus = fn.Status
		}

		isSelected := i == m.cursor
		line := m.renderTreeNode(fn, isSelected, contentWidth)
		allLines = append(allLines, line)
	}

	// Add header lines (matching workstream/grouped views pattern)
	var lines []string
	viewModeStr := fmt.Sprintf("tree [depth:%s]", m.dependencyDepth.String())
	lines = append(lines, statsStyle.Render(fmt.Sprintf("  %d issues (%s):", len(m.flatNodes), viewModeStr)))
	lines = append(lines, "")

	// Calculate visible window (2 lines for header above, rest for content)
	contentLines := visibleLines - 2
	if contentLines < 1 {
		contentLines = 1
	}

	// m.scroll is already a LINE position (set by ensureVisible)
	scrollLine := m.scroll
	if scrollLine < 0 {
		scrollLine = 0
	}

	// Add visible content lines
	if scrollLine < len(allLines) {
		endLine := scrollLine + contentLines
		if endLine > len(allLines) {
			endLine = len(allLines)
		}
		for i := scrollLine; i < endLine; i++ {
			lines = append(lines, allLines[i])
		}
	}

	// Pad to exactly visibleLines
	for len(lines) < visibleLines {
		lines = append(lines, "")
	}

	return lines
}

// renderCenteredView renders the ego-centered layout for epic/bead modes:
// Upstream blockers → Entry point (center) → Downstream descendants
func (m *LensDashboardModel) renderCenteredView(contentWidth, visibleLines int, statsStyle lipgloss.Style) []string {
	t := m.theme

	upstreamLen := len(m.upstreamNodes)

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

	// Build ALL lines first (including headers, decorations, etc.)
	var allLines []string
	nodeIdx := 0

	// === UPSTREAM SECTION (blockers) ===
	if len(m.upstreamNodes) > 0 {
		header := renderSectionHeader("◇", upstreamIconStyle.Render("◇"), "BLOCKERS", min(contentWidth, 50))
		allLines = append(allLines, header)

		for _, fn := range m.upstreamNodes {
			isSelected := nodeIdx == m.cursor
			line := m.renderCenteredNode(fn, isSelected, contentWidth, -1)
			allLines = append(allLines, line)
			nodeIdx++
		}
		allLines = append(allLines, "")
	}

	// === CENTER SECTION (entry point/ego) with elegant top/bottom lines ===
	if m.egoNode != nil {
		lineWidth := min(contentWidth-4, 50)
		topLine := boxStyle.Render("═" + strings.Repeat("═", lineWidth) + "═")
		bottomLine := boxStyle.Render("─" + strings.Repeat("─", lineWidth) + "─")

		allLines = append(allLines, topLine)

		egoNodeIdx := upstreamLen
		isSelected := m.cursor == egoNodeIdx
		line := m.renderEgoNodeLine(*m.egoNode, isSelected, contentWidth)
		allLines = append(allLines, line)

		allLines = append(allLines, bottomLine)
		allLines = append(allLines, "")
		nodeIdx = upstreamLen + 1
	}

	// === DOWNSTREAM SECTION (children/dependents) ===
	if len(m.flatNodes) > 0 {
		header := renderSectionHeader("◆", downstreamIconStyle.Render("◆"), "DESCENDANTS", min(contentWidth, 50))
		allLines = append(allLines, header)

		lastStatus := ""
		for i, fn := range m.flatNodes {
			cursorPos := upstreamLen + 1 + i

			// Show status header when status changes
			if fn.Status != lastStatus {
				statusHeader := m.renderStatusHeader(fn.Status)
				allLines = append(allLines, statusHeader)
				lastStatus = fn.Status
			}

			isSelected := cursorPos == m.cursor
			line := m.renderCenteredNode(fn, isSelected, contentWidth, fn.Node.RelativeDepth)
			allLines = append(allLines, line)
		}
	} else if len(m.upstreamNodes) == 0 {
		emptyStyle := t.Renderer.NewStyle().Foreground(t.Subtext).Italic(true)
		allLines = append(allLines, emptyStyle.Render("  No descendants found"))
	}

	// Now apply scroll and buffer (matching flat view pattern)
	var lines []string
	contentLines := visibleLines - 2
	if contentLines < 1 {
		contentLines = 1
	}

	scrollLine := m.scroll
	if scrollLine < 0 {
		scrollLine = 0
	}

	// Add visible content lines
	if scrollLine < len(allLines) {
		endLine := scrollLine + contentLines
		if endLine > len(allLines) {
			endLine = len(allLines)
		}
		for i := scrollLine; i < endLine; i++ {
			lines = append(lines, allLines[i])
		}
	}

	// Pad to exactly visibleLines
	for len(lines) < visibleLines {
		lines = append(lines, "")
	}

	return lines
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

	// Status indicator (only show if blocker not already visible in tree)
	statusSuffix := ""
	if fn.Status == "blocked" && fn.BlockedBy != "" && !fn.BlockerInTree {
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

	// Status indicator for blocked items (only show if blocker not already visible in tree)
	statusSuffix := ""
	if fn.Status == "blocked" && fn.BlockedBy != "" && !fn.BlockerInTree {
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

	// Calculate visible window (2 lines for header above, rest for content)
	totalLines := len(allLines)
	contentLines := visibleLines - 2

	startIdx := m.wsScroll
	if startIdx < 0 {
		startIdx = 0
	}

	// Add visible content lines
	if startIdx < totalLines {
		endIdx := startIdx + contentLines
		if endIdx > totalLines {
			endIdx = totalLines
		}
		for i := startIdx; i < endIdx; i++ {
			lines = append(lines, allLines[i])
		}
	}

	// Pad with empty lines to allow empty space after list end
	// This ensures the last items can be centered with empty space below
	for len(lines) < visibleLines {
		lines = append(lines, "")
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

// renderGroupedView renders the grouped view with workstream-like styling
func (m *LensDashboardModel) renderGroupedView(contentWidth, visibleLines int, statsStyle lipgloss.Style) []string {
	t := m.theme
	var allLines []string

	// Same styles as workstream view for consistency
	groupHeaderStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)
	groupHeaderSelectedStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true).Background(t.Highlight)
	subStyle := t.Renderer.NewStyle().Foreground(t.Subtext)
	issueStyle := t.Renderer.NewStyle()
	issueSelectedStyle := t.Renderer.NewStyle().Bold(true)
	readyStyle := t.Renderer.NewStyle().Foreground(t.Open)
	blockedStyle := t.Renderer.NewStyle().Foreground(t.Blocked)
	closedStyle := t.Renderer.NewStyle().Foreground(t.Closed)
	inProgStyle := t.Renderer.NewStyle().Foreground(t.InProgress)

	// Build all lines first, then apply scroll
	for gIdx, group := range m.groupedSections {
		// Check if this group header is selected (on group header, not in sub-group)
		isHeaderSelected := gIdx == m.groupedCursor && m.groupedSubCursor < 0 && m.groupedIssueCursor < 0
		isExpanded := m.groupedExpanded[gIdx]

		// Group header with progress
		progressPct := int(group.Progress * 100)
		progressBar := m.renderMiniProgressBar(group.Progress, 8)

		// Status counts
		statusCounts := fmt.Sprintf("○%d ●%d ◈%d ✓%d",
			group.ReadyCount, group.InProgressCount, group.BlockedCount, group.ClosedCount)

		// Expand/collapse indicator
		expandIcon := "▶"
		if isExpanded {
			expandIcon = "▼"
		}

		// Selection indicator
		selectPrefix := "  "
		headerStyle := groupHeaderStyle
		if isHeaderSelected {
			selectPrefix = "▸ "
			headerStyle = groupHeaderSelectedStyle
		}

		// Sub-group indicator
		subGroupIndicator := ""
		if len(group.SubWorkstreams) > 0 {
			subGroupIndicator = fmt.Sprintf(" [%d sub]", len(group.SubWorkstreams))
		}

		groupLine := fmt.Sprintf("%s%s %s %s %d%% %s (%d)%s",
			selectPrefix,
			expandIcon,
			headerStyle.Render(group.Name),
			progressBar,
			progressPct,
			subStyle.Render(statusCounts),
			len(group.Issues),
			subStyle.Render(subGroupIndicator))
		allLines = append(allLines, groupLine)

		// Render sub-groups if expanded and present
		if isExpanded && len(group.SubWorkstreams) > 0 {
			for subIdx, subGroup := range group.SubWorkstreams {
				if subGroup == nil {
					continue
				}
				subProgress := int(subGroup.Progress * 100)
				subStatusCounts := fmt.Sprintf("○%d ●%d ◈%d ✓%d",
					subGroup.ReadyCount, subGroup.InProgressCount, subGroup.BlockedCount, subGroup.ClosedCount)

				// Check sub-group expansion
				subExpanded := m.groupedSubExpanded[gIdx] != nil && m.groupedSubExpanded[gIdx][subIdx]
				subExpandIcon := "▶"
				if subExpanded {
					subExpandIcon = "▼"
				}

				// Check if this sub-group header is selected
				isSubHeaderSelected := gIdx == m.groupedCursor && subIdx == m.groupedSubCursor && m.groupedIssueCursor < 0
				subSelectPrefix := "     "
				subHeaderStyle := subStyle
				if isSubHeaderSelected {
					subSelectPrefix = "   ▸ "
					subHeaderStyle = groupHeaderSelectedStyle
				}

				subLine := fmt.Sprintf("%s%s %s (%d%%) %s (%d)",
					subSelectPrefix,
					subExpandIcon,
					subHeaderStyle.Render(subGroup.Name),
					subProgress,
					subStyle.Render(subStatusCounts),
					len(subGroup.Issues))
				allLines = append(allLines, subLine)

				// Render sub-group issues if expanded
				if subExpanded {
					if m.groupedTreeView {
						// Tree view for sub-group issues
						subGroupCopy := *subGroup
						treeRoots := m.buildWorkstreamTree(&subGroupCopy)
						flatNodes := m.flattenWSTree(treeRoots)
						for i, fn := range flatNodes {
							isIssueSelected := gIdx == m.groupedCursor && subIdx == m.groupedSubCursor && i == m.groupedIssueCursor
							allLines = append(allLines, m.renderGroupedTreeIssue(fn, isIssueSelected, contentWidth, "        ", issueStyle, issueSelectedStyle, readyStyle, blockedStyle, closedStyle, inProgStyle, subStyle))
						}
					} else {
						for i, issue := range subGroup.Issues {
							isIssueSelected := gIdx == m.groupedCursor && subIdx == m.groupedSubCursor && i == m.groupedIssueCursor
							allLines = append(allLines, m.renderGroupedIssue(issue, isIssueSelected, contentWidth, "        ", issueStyle, issueSelectedStyle, readyStyle, blockedStyle, closedStyle, inProgStyle, subStyle))
						}
					}
				}
			}
		}

		// Render issues when expanded (only if no sub-groups, otherwise they're shown in sub-groups)
		if isExpanded && len(group.SubWorkstreams) == 0 {
			if m.groupedTreeView {
				// Tree view for group issues
				groupCopy := group
				treeRoots := m.buildWorkstreamTree(&groupCopy)
				flatNodes := m.flattenWSTree(treeRoots)
				for i, fn := range flatNodes {
					isIssueSelected := gIdx == m.groupedCursor && m.groupedSubCursor < 0 && i == m.groupedIssueCursor
					allLines = append(allLines, m.renderGroupedTreeIssue(fn, isIssueSelected, contentWidth, "    ", issueStyle, issueSelectedStyle, readyStyle, blockedStyle, closedStyle, inProgStyle, subStyle))
				}
			} else {
				maxIssues := len(group.Issues)
				for i := 0; i < maxIssues && i < len(group.Issues); i++ {
					issue := group.Issues[i]
					isIssueSelected := gIdx == m.groupedCursor && m.groupedSubCursor < 0 && i == m.groupedIssueCursor
					allLines = append(allLines, m.renderGroupedIssue(issue, isIssueSelected, contentWidth, "    ", issueStyle, issueSelectedStyle, readyStyle, blockedStyle, closedStyle, inProgStyle, subStyle))
				}
			}
		}

		allLines = append(allLines, "") // Empty line between groups
	}

	// Apply scroll offset
	var lines []string
	viewModeStr := "list"
	if m.groupedTreeView {
		viewModeStr = fmt.Sprintf("tree [depth:%s]", m.dependencyDepth.String())
	}
	lines = append(lines, subStyle.Render(fmt.Sprintf("  Grouped by %s (%d groups, %s):", m.groupByMode.String(), len(m.groupedSections), viewModeStr)))
	lines = append(lines, "")

	// Calculate visible window (2 lines for header above, rest for content)
	totalLines := len(allLines)
	contentLines := visibleLines - 2

	startIdx := m.groupedScroll
	if startIdx < 0 {
		startIdx = 0
	}

	// Add visible content lines
	if startIdx < totalLines {
		endIdx := startIdx + contentLines
		if endIdx > totalLines {
			endIdx = totalLines
		}
		for i := startIdx; i < endIdx; i++ {
			lines = append(lines, allLines[i])
		}
	}

	// Pad with empty lines to allow empty space after list end
	// This ensures the last items can be centered with empty space below
	for len(lines) < visibleLines {
		lines = append(lines, "")
	}

	return lines
}

// renderGroupedIssue renders a single issue in grouped view
func (m *LensDashboardModel) renderGroupedIssue(issue model.Issue, isSelected bool, contentWidth int, indent string, issueStyle, issueSelectedStyle, readyStyle, blockedStyle, closedStyle, inProgStyle, subStyle lipgloss.Style) string {
	t := m.theme

	// Determine status icon and style
	var statusIcon string
	var style lipgloss.Style
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
		// Check if blocked by dependencies
		if m.isIssueBlockedByDeps(issue.ID) {
			statusIcon = "◈"
			style = blockedStyle
		} else {
			statusIcon = "○"
			style = readyStyle
		}
	}

	// Selection indicator
	issuePrefix := indent
	idStyle := issueStyle
	titleStyle := issueStyle
	if isSelected {
		issuePrefix = indent[:len(indent)-2] + "▸ "
		idStyle = issueSelectedStyle.Foreground(t.Primary)
		titleStyle = issueSelectedStyle
	}

	title := truncateRunesHelper(issue.Title, contentWidth-20-len(indent), "…")
	return fmt.Sprintf("%s%s %s %s",
		issuePrefix,
		style.Render(statusIcon),
		idStyle.Render(issue.ID),
		titleStyle.Render(title))
}

// renderGroupedTreeIssue renders a single issue with tree prefix in grouped view
func (m *LensDashboardModel) renderGroupedTreeIssue(fn LensFlatNode, isSelected bool, contentWidth int, indent string, issueStyle, issueSelectedStyle, readyStyle, blockedStyle, closedStyle, inProgStyle, subStyle lipgloss.Style) string {
	t := m.theme
	issue := fn.Node.Issue

	// Determine status icon and style
	var statusIcon string
	var style lipgloss.Style
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
		// Check if blocked by dependencies
		if m.isIssueBlockedByDeps(issue.ID) {
			statusIcon = "◈"
			style = blockedStyle
		} else {
			statusIcon = "○"
			style = readyStyle
		}
	}

	// Build tree prefix
	treePrefix := fn.TreePrefix

	// Selection indicator
	issuePrefix := indent + treePrefix
	idStyle := issueStyle
	titleStyle := issueStyle
	if isSelected {
		// For tree view, highlight the whole line
		idStyle = issueSelectedStyle.Foreground(t.Primary)
		titleStyle = issueSelectedStyle
		// Add selection marker at the beginning
		issuePrefix = indent[:len(indent)-2] + "▸ " + treePrefix
	}

	title := truncateRunesHelper(issue.Title, contentWidth-20-len(indent)-len(treePrefix), "…")
	return fmt.Sprintf("%s%s %s %s",
		issuePrefix,
		style.Render(statusIcon),
		idStyle.Render(issue.ID),
		titleStyle.Render(title))
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

	// Status indicator for blocked items (only show if blocker not already visible in tree)
	statusSuffix := ""
	if fn.Status == "blocked" && fn.BlockedBy != "" && !fn.BlockerInTree {
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

// ══════════════════════════════════════════════════════════════════════════════
// SPLIT VIEW - Bead detail panel on the right
// ══════════════════════════════════════════════════════════════════════════════

const LensSplitViewThreshold = 120 // Minimum width for split view

// initDetailViewport initializes the detail viewport for split view
func (m *LensDashboardModel) initDetailViewport() {
	m.detailViewport = viewport.New(40, 20)
	m.detailViewport.Style = lipgloss.NewStyle()
	m.updateDetailContent()
}

// IsSplitView returns true if split view is active
func (m *LensDashboardModel) IsSplitView() bool {
	return m.splitViewMode
}

// IsDetailFocused returns true if the detail panel has focus
func (m *LensDashboardModel) IsDetailFocused() bool {
	return m.detailFocus
}

// ToggleDetailFocus switches focus between tree and detail panels
func (m *LensDashboardModel) ToggleDetailFocus() {
	if m.splitViewMode {
		m.detailFocus = !m.detailFocus
	}
}

// SetDetailFocus sets the detail panel focus state
func (m *LensDashboardModel) SetDetailFocus(focused bool) {
	m.detailFocus = focused
}

// ScrollDetailUp scrolls the detail viewport up
func (m *LensDashboardModel) ScrollDetailUp() {
	if m.detailFocus {
		m.detailViewport.LineUp(1)
	}
}

// ScrollDetailDown scrolls the detail viewport down
func (m *LensDashboardModel) ScrollDetailDown() {
	if m.detailFocus {
		m.detailViewport.LineDown(1)
	}
}

// ScrollDetailPageUp scrolls the detail viewport up by a page
func (m *LensDashboardModel) ScrollDetailPageUp() {
	if m.detailFocus {
		m.detailViewport.HalfViewUp()
	}
}

// ScrollDetailPageDown scrolls the detail viewport down by a page
func (m *LensDashboardModel) ScrollDetailPageDown() {
	if m.detailFocus {
		m.detailViewport.HalfViewDown()
	}
}

// updateDetailContent updates the detail viewport content based on selected issue
func (m *LensDashboardModel) updateDetailContent() {
	if m.selectedIssueID == "" {
		m.detailViewport.SetContent("No issue selected")
		return
	}

	issue, exists := m.issueMap[m.selectedIssueID]
	if !exists {
		m.detailViewport.SetContent("Issue not found: " + m.selectedIssueID)
		return
	}

	content := m.renderIssueDetail(issue)
	m.detailViewport.SetContent(content)
	m.detailViewport.GotoTop()
}

// renderIssueDetail renders the detailed view of an issue for the viewport
func (m *LensDashboardModel) renderIssueDetail(issue *model.Issue) string {
	t := m.theme
	var sb strings.Builder

	// Title with type icon
	titleStyle := t.Renderer.NewStyle().Bold(true).Foreground(t.Primary)
	typeIcon, typeColor := t.GetTypeIcon(string(issue.IssueType))
	typeStyle := t.Renderer.NewStyle().Foreground(typeColor)

	sb.WriteString(typeStyle.Render(typeIcon) + " ")
	sb.WriteString(titleStyle.Render(issue.Title))
	sb.WriteString("\n\n")

	// ID and metadata
	labelStyle := t.Renderer.NewStyle().Foreground(t.Subtext)
	valueStyle := t.Renderer.NewStyle().Foreground(t.Base.GetForeground())

	sb.WriteString(labelStyle.Render("ID:       "))
	sb.WriteString(valueStyle.Render(issue.ID))
	sb.WriteString("\n")

	sb.WriteString(labelStyle.Render("Status:   "))
	sb.WriteString(RenderStatusBadge(string(issue.Status)))
	sb.WriteString("\n")

	sb.WriteString(labelStyle.Render("Priority: "))
	sb.WriteString(RenderPriorityBadge(issue.Priority))
	sb.WriteString("\n")

	sb.WriteString(labelStyle.Render("Type:     "))
	sb.WriteString(typeStyle.Render(string(issue.IssueType)))
	sb.WriteString("\n")

	if issue.Assignee != "" {
		sb.WriteString(labelStyle.Render("Assignee: "))
		sb.WriteString(valueStyle.Render("@"+issue.Assignee))
		sb.WriteString("\n")
	}

	sb.WriteString(labelStyle.Render("Created:  "))
	sb.WriteString(valueStyle.Render(issue.CreatedAt.Format("2006-01-02 15:04")))
	sb.WriteString("\n")

	if !issue.UpdatedAt.IsZero() && issue.UpdatedAt != issue.CreatedAt {
		sb.WriteString(labelStyle.Render("Updated:  "))
		sb.WriteString(valueStyle.Render(issue.UpdatedAt.Format("2006-01-02 15:04")))
		sb.WriteString("\n")
	}

	// Labels
	if len(issue.Labels) > 0 {
		sb.WriteString("\n")
		sectionStyle := t.Renderer.NewStyle().Bold(true).Foreground(t.Secondary)
		sb.WriteString(sectionStyle.Render("🏷 Labels"))
		sb.WriteString("\n")

		chipStyle := t.Renderer.NewStyle().Foreground(t.Primary)
		for _, label := range issue.Labels {
			sb.WriteString("  ")
			sb.WriteString(chipStyle.Render(label))
			sb.WriteString("\n")
		}
	}

	// Dependencies
	blockers := m.upstream[issue.ID]
	dependents := m.downstream[issue.ID]

	if len(blockers) > 0 || len(dependents) > 0 {
		sb.WriteString("\n")
		sectionStyle := t.Renderer.NewStyle().Bold(true).Foreground(t.Secondary)
		sb.WriteString(sectionStyle.Render("🔗 Dependencies"))
		sb.WriteString("\n")

		if len(blockers) > 0 {
			blockerStyle := t.Renderer.NewStyle().Foreground(t.Blocked)
			sb.WriteString(blockerStyle.Render(fmt.Sprintf("  ↓ Blocked by (%d):", len(blockers))))
			sb.WriteString("\n")
			for _, blockerID := range blockers {
				if blocker, ok := m.issueMap[blockerID]; ok {
					title := blocker.Title
					if len(title) > 30 {
						title = title[:27] + "..."
					}
					sb.WriteString(fmt.Sprintf("    %s %s\n", blockerID, labelStyle.Render(title)))
				}
			}
		}

		if len(dependents) > 0 {
			dependentStyle := t.Renderer.NewStyle().Foreground(t.Open)
			sb.WriteString(dependentStyle.Render(fmt.Sprintf("  ↑ Blocks (%d):", len(dependents))))
			sb.WriteString("\n")
			for _, depID := range dependents {
				if dep, ok := m.issueMap[depID]; ok {
					title := dep.Title
					if len(title) > 30 {
						title = title[:27] + "..."
					}
					sb.WriteString(fmt.Sprintf("    %s %s\n", depID, labelStyle.Render(title)))
				}
			}
		}
	}

	// Description
	if issue.Description != "" {
		sb.WriteString("\n")
		sectionStyle := t.Renderer.NewStyle().Bold(true).Foreground(t.Secondary)
		sb.WriteString(sectionStyle.Render("📝 Description"))
		sb.WriteString("\n\n")
		sb.WriteString(issue.Description)
		sb.WriteString("\n")
	}

	// Design
	if issue.Design != "" {
		sb.WriteString("\n")
		sectionStyle := t.Renderer.NewStyle().Bold(true).Foreground(t.Secondary)
		sb.WriteString(sectionStyle.Render("🎨 Design"))
		sb.WriteString("\n\n")
		sb.WriteString(issue.Design)
		sb.WriteString("\n")
	}

	// Acceptance Criteria
	if issue.AcceptanceCriteria != "" {
		sb.WriteString("\n")
		sectionStyle := t.Renderer.NewStyle().Bold(true).Foreground(t.Secondary)
		sb.WriteString(sectionStyle.Render("✅ Acceptance Criteria"))
		sb.WriteString("\n\n")
		sb.WriteString(issue.AcceptanceCriteria)
		sb.WriteString("\n")
	}

	// Notes
	if issue.Notes != "" {
		sb.WriteString("\n")
		sectionStyle := t.Renderer.NewStyle().Bold(true).Foreground(t.Secondary)
		sb.WriteString(sectionStyle.Render("📋 Notes"))
		sb.WriteString("\n\n")
		sb.WriteString(issue.Notes)
		sb.WriteString("\n")
	}

	return sb.String()
}

// renderSplitView renders the split layout with tree on left and detail on right
func (m *LensDashboardModel) renderSplitView() string {
	t := m.theme

	// Calculate panel widths (45% tree, 55% detail)
	leftWidth := (m.width * 45) / 100
	rightWidth := m.width - leftWidth - 1 // 1 for separator

	if leftWidth < 40 {
		leftWidth = 40
	}
	if rightWidth < 30 {
		rightWidth = 30
	}

	// Panel styles based on focus
	var leftStyle, rightStyle lipgloss.Style
	borderColor := t.Border
	focusBorderColor := t.Primary

	// Use full height given to us - the parent wraps everything with Height/MaxHeight
	panelHeight := m.height
	if m.detailFocus {
		leftStyle = t.Renderer.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Width(leftWidth - 2).
			Height(panelHeight).
			MaxHeight(panelHeight)
		rightStyle = t.Renderer.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(focusBorderColor).
			Width(rightWidth - 2).
			Height(panelHeight).
			MaxHeight(panelHeight)
	} else {
		leftStyle = t.Renderer.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(focusBorderColor).
			Width(leftWidth - 2).
			Height(panelHeight).
			MaxHeight(panelHeight)
		rightStyle = t.Renderer.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Width(rightWidth - 2).
			Height(panelHeight).
			MaxHeight(panelHeight)
	}

	// Render left panel (tree content)
	leftContent := m.renderTreeContent(leftWidth - 4)

	// Render right panel (detail viewport)
	// Viewport height = panelHeight - 1 (for header line)
	m.detailViewport.Width = rightWidth - 4
	m.detailViewport.Height = panelHeight - 1
	rightContent := m.detailViewport.View()

	// Add panel headers
	leftHeader := t.Renderer.NewStyle().Bold(true).Foreground(t.Primary).Render("◆ " + m.labelName)
	rightHeader := t.Renderer.NewStyle().Bold(true).Foreground(t.Secondary).Render("📋 Details")

	if m.detailFocus {
		rightHeader = t.Renderer.NewStyle().Bold(true).Foreground(t.Primary).Render("📋 Details")
	}

	leftPanel := lipgloss.JoinVertical(lipgloss.Left, leftHeader, leftContent)
	rightPanel := lipgloss.JoinVertical(lipgloss.Left, rightHeader, rightContent)

	// Apply styles
	leftView := leftStyle.Render(leftPanel)
	rightView := rightStyle.Render(rightPanel)

	// Join horizontally
	return lipgloss.JoinHorizontal(lipgloss.Top, leftView, rightView)
}

// renderTreeContent renders just the tree portion for split view
func (m *LensDashboardModel) renderTreeContent(contentWidth int) string {
	t := m.theme
	var lines []string

	statsStyle := t.Renderer.NewStyle().Foreground(t.Subtext)

	// Stats line
	primaryIcon := t.Renderer.NewStyle().Foreground(t.Primary).Render("●")
	contextIcon := t.Renderer.NewStyle().Foreground(t.Secondary).Render("○")
	depthStyle := t.Renderer.NewStyle().Foreground(t.InProgress).Bold(true)

	statsLine := fmt.Sprintf("%s %d  %s %d  [%s]",
		primaryIcon, m.primaryCount, contextIcon, m.contextCount,
		depthStyle.Render(m.dependencyDepth.String()))
	lines = append(lines, statsStyle.Render(statsLine))

	// Status summary
	readyStyle := t.Renderer.NewStyle().Foreground(t.Open)
	blockedStyle := t.Renderer.NewStyle().Foreground(t.Blocked)
	summaryLine := fmt.Sprintf("%s ready  %s blocked",
		readyStyle.Render(fmt.Sprintf("%d", m.readyCount)),
		blockedStyle.Render(fmt.Sprintf("%d", m.blockedCount)))
	lines = append(lines, statsStyle.Render(summaryLine))
	lines = append(lines, "")

	// Calculate visible area
	visibleLines := m.height - 10
	if visibleLines < 5 {
		visibleLines = 5
	}

	// Render based on view type
	if m.viewType == ViewTypeGrouped && len(m.groupedSections) > 0 {
		lines = append(lines, m.renderGroupedView(contentWidth, visibleLines, statsStyle)...)
	} else if m.viewType == ViewTypeWorkstream && len(m.workstreams) > 1 {
		lines = append(lines, m.renderWorkstreamView(contentWidth, visibleLines, statsStyle)...)
	} else if m.centeredMode && (m.viewMode == "epic" || m.viewMode == "bead") && m.egoNode != nil {
		lines = append(lines, m.renderCenteredView(contentWidth, visibleLines, statsStyle)...)
	} else {
		// Render flat tree view (reuse the main render function)
		flatLines := m.renderFlatView(contentWidth, visibleLines, statsStyle)
		lines = append(lines, flatLines...)
	}

	return strings.Join(lines, "\n")
}

package ui

import (
	"fmt"
	"sort"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"

	"github.com/charmbracelet/bubbles/viewport"
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


// buildWorkstreamFromIssues creates a Workstream struct with computed stats

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

// SetSize updates the dashboard dimensions
func (m *LensDashboardModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	// Enable split view mode for wide terminals
	m.splitViewMode = width >= LensSplitViewThreshold
}


// ══════════════════════════════════════════════════════════════════════════════
// ACCESSORS - Simple getters for model state
// Navigation methods moved to lensdashboard_nav.go
// ══════════════════════════════════════════════════════════════════════════════

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


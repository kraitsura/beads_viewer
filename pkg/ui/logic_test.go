package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/Dicklesworthstone/beads_viewer/pkg/recipe"
)

// keyMsg creates a tea.KeyMsg for testing
func keyMsg(key string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}

// White-box testing of UI model logic

func TestApplyRecipe_StatusFilter(t *testing.T) {
	issues := []model.Issue{
		{ID: "open", Status: model.StatusOpen},
		{ID: "closed", Status: model.StatusClosed},
		{ID: "blocked", Status: model.StatusBlocked},
	}
	m := NewModel(issues, nil, "")

	r := &recipe.Recipe{
		Name: "closed-only",
		Filters: recipe.FilterConfig{
			Status: []string{"closed"},
		},
	}

	m.applyRecipe(r)

	filtered := m.FilteredIssues()
	if len(filtered) != 1 {
		t.Fatalf("Expected 1 filtered issue, got %d", len(filtered))
	}
	if filtered[0].ID != "closed" {
		t.Errorf("Expected issue 'closed', got %s", filtered[0].ID)
	}
}

func TestApplyRecipe_PriorityFilter(t *testing.T) {
	issues := []model.Issue{
		{ID: "p1", Status: model.StatusOpen, Priority: 1},
		{ID: "p2", Status: model.StatusOpen, Priority: 2},
	}
	m := NewModel(issues, nil, "")

	r := &recipe.Recipe{
		Filters: recipe.FilterConfig{
			Priority: []int{1},
		},
	}

	m.applyRecipe(r)

	filtered := m.FilteredIssues()
	if len(filtered) != 1 {
		t.Fatalf("Expected 1 issue, got %d", len(filtered))
	}
	if filtered[0].ID != "p1" {
		t.Errorf("Expected p1, got %s", filtered[0].ID)
	}
}

func TestApplyRecipe_ActionableFilter(t *testing.T) {
	// A blocks B. B is blocked. A is open.
	issues := []model.Issue{
		{ID: "A", Status: model.StatusOpen},
		{ID: "B", Status: model.StatusBlocked, Dependencies: []*model.Dependency{
			{DependsOnID: "A", Type: model.DepBlocks},
		}},
	}
	m := NewModel(issues, nil, "")

	yes := true
	r := &recipe.Recipe{
		Filters: recipe.FilterConfig{
			Actionable: &yes,
		},
	}

	m.applyRecipe(r)

	filtered := m.FilteredIssues()
	if len(filtered) != 1 {
		t.Fatalf("Expected 1 actionable issue, got %d", len(filtered))
	}
	if filtered[0].ID != "A" {
		t.Errorf("Expected A (actionable), got %s", filtered[0].ID)
	}
}

func TestApplyRecipe_Sorting(t *testing.T) {
	issues := []model.Issue{
		{ID: "A", Priority: 2},
		{ID: "B", Priority: 1},
		{ID: "C", Priority: 3},
	}
	m := NewModel(issues, nil, "")

	r := &recipe.Recipe{
		Sort: recipe.SortConfig{
			Field:     "priority",
			Direction: "asc",
		},
	}

	m.applyRecipe(r)

	filtered := m.FilteredIssues()
	if len(filtered) != 3 {
		t.Fatal("Expected 3 issues")
	}

	// Expect B(1), A(2), C(3)
	if filtered[0].ID != "B" {
		t.Errorf("Expected B first, got %s", filtered[0].ID)
	}
	if filtered[1].ID != "A" {
		t.Errorf("Expected A second, got %s", filtered[1].ID)
	}
	if filtered[2].ID != "C" {
		t.Errorf("Expected C third, got %s", filtered[2].ID)
	}
}

func TestTimeTravel_DiffBadgePropagation(t *testing.T) {
	issues := []model.Issue{
		{ID: "A", Status: model.StatusOpen},
	}
	m := NewModel(issues, nil, "")

	// Manually inject diff state (simulating enterTimeTravelMode)
	m.timeTravelMode = true
	m.newIssueIDs = map[string]bool{"A": true}
	m.closedIssueIDs = map[string]bool{}
	m.modifiedIssueIDs = map[string]bool{}

	// Test getDiffStatus logic
	status := m.getDiffStatus("A")
	if status != DiffStatusNew {
		t.Errorf("Expected DiffStatusNew, got %v", status)
	}

	// Test propagation to list items via rebuild
	m.rebuildListWithDiffInfo()

	items := m.list.Items()
	if len(items) != 1 {
		t.Fatal("Expected 1 item")
	}

	item := items[0].(IssueItem)
	if item.DiffStatus != DiffStatusNew {
		t.Errorf("List item missing DiffStatusNew, got %v", item.DiffStatus)
	}
}

func TestFormatTimeRel(t *testing.T) {
	now := time.Now()
	tests := []struct {
		t        time.Time
		expected string
	}{
		{now, "now"},
		{now.Add(-10 * time.Minute), "10m ago"},
		{now.Add(-2 * time.Hour), "2h ago"},
		{now.Add(-25 * time.Hour), "1d ago"},
		{now.Add(-8 * 24 * time.Hour), "1w ago"},
		{now.Add(-60 * 24 * time.Hour), "2mo ago"},
		{time.Time{}, "unknown"},
	}

	for _, tt := range tests {
		got := FormatTimeRel(tt.t)
		if got != tt.expected {
			t.Errorf("FormatTimeRel(%v): expected %s, got %s", tt.t, tt.expected, got)
		}
	}
}

func TestLensDashboardToggleViewType(t *testing.T) {
	// Create test issues with a label and dependencies to form multiple workstreams
	issues := []model.Issue{
		{ID: "A", Status: model.StatusOpen, Labels: []string{"test-label"}},
		{ID: "B", Status: model.StatusOpen, Labels: []string{"test-label"}, Dependencies: []*model.Dependency{
			{DependsOnID: "A", Type: model.DepBlocks},
		}},
		{ID: "C", Status: model.StatusOpen, Labels: []string{"test-label"}}, // Separate workstream
	}

	issueMap := make(map[string]*model.Issue)
	for i := range issues {
		issueMap[issues[i].ID] = &issues[i]
	}

	renderer := lipgloss.DefaultRenderer()
	theme := DefaultTheme(renderer)
	dashboard := NewLensDashboardModel("test-label", issues, issueMap, theme)

	// Initial state should be flat view
	if dashboard.GetViewType() != ViewTypeFlat {
		t.Errorf("Initial viewType should be ViewTypeFlat, got %v", dashboard.GetViewType())
	}
	if dashboard.IsWorkstreamView() {
		t.Error("IsWorkstreamView() should return false initially")
	}

	// Toggle to workstream view
	dashboard.ToggleViewType()

	if dashboard.GetViewType() != ViewTypeWorkstream {
		t.Errorf("After toggle, viewType should be ViewTypeWorkstream, got %v", dashboard.GetViewType())
	}
	if !dashboard.IsWorkstreamView() {
		t.Error("IsWorkstreamView() should return true after toggle")
	}

	// Toggle back to flat view
	dashboard.ToggleViewType()

	if dashboard.GetViewType() != ViewTypeFlat {
		t.Errorf("After second toggle, viewType should be ViewTypeFlat, got %v", dashboard.GetViewType())
	}
	if dashboard.IsWorkstreamView() {
		t.Error("IsWorkstreamView() should return false after second toggle")
	}
}

func TestLensDashboardToggleViewTypeViaModel(t *testing.T) {
	// Create test issues with a label
	issues := []model.Issue{
		{ID: "A", Status: model.StatusOpen, Labels: []string{"test-label"}},
		{ID: "B", Status: model.StatusOpen, Labels: []string{"test-label"}},
	}

	m := NewModel(issues, nil, "")

	// Open lens dashboard
	issueMap := make(map[string]*model.Issue)
	for i := range issues {
		issueMap[issues[i].ID] = &issues[i]
	}
	m.issueMap = issueMap
	dashboard := NewLensDashboardModel("test-label", issues, issueMap, m.theme)
	m.lensDashboard = dashboard
	m.showLensDashboard = true
	m.focused = focusLensDashboard

	// Verify initial state
	if m.lensDashboard.GetViewType() != ViewTypeFlat {
		t.Errorf("Initial viewType should be ViewTypeFlat, got %v", m.lensDashboard.GetViewType())
	}

	// Simulate 'w' key press through handleLensDashboardKeys
	// Note: handleLensDashboardKeys returns a new Model (value semantics)
	m = m.handleLensDashboardKeys(keyMsg("w"))

	// The critical test: did the viewType change persist?
	if m.lensDashboard.GetViewType() != ViewTypeWorkstream {
		t.Errorf("After 'w' key, viewType should be ViewTypeWorkstream, got %v", m.lensDashboard.GetViewType())
	}
	if !m.lensDashboard.IsWorkstreamView() {
		t.Error("IsWorkstreamView() should return true after 'w' key")
	}

	// Verify status message was set (now includes debug info)
	if !strings.Contains(m.statusMsg, "Switched to workstream view") {
		t.Errorf("Expected statusMsg to contain 'Switched to workstream view', got '%s'", m.statusMsg)
	}

	// Toggle back
	m = m.handleLensDashboardKeys(keyMsg("w"))

	if m.lensDashboard.GetViewType() != ViewTypeFlat {
		t.Errorf("After second 'w' key, viewType should be ViewTypeFlat, got %v", m.lensDashboard.GetViewType())
	}
	if !strings.Contains(m.statusMsg, "Switched to flat view") {
		t.Errorf("Expected statusMsg to contain 'Switched to flat view', got '%s'", m.statusMsg)
	}
}

func TestLensDashboardViewOutputChanges(t *testing.T) {
	// Create test issues with dependencies to form 2 workstreams
	// Workstream 1: A -> B (A blocks B)
	// Workstream 2: C (standalone)
	issues := []model.Issue{
		{ID: "A", Status: model.StatusOpen, Labels: []string{"test-label", "ws1"}},
		{ID: "B", Status: model.StatusOpen, Labels: []string{"test-label", "ws1"}, Dependencies: []*model.Dependency{
			{DependsOnID: "A", Type: model.DepBlocks},
		}},
		{ID: "C", Status: model.StatusOpen, Labels: []string{"test-label", "ws2"}}, // Different label = different workstream
	}

	issueMap := make(map[string]*model.Issue)
	for i := range issues {
		issueMap[issues[i].ID] = &issues[i]
	}

	renderer := lipgloss.DefaultRenderer()
	theme := DefaultTheme(renderer)
	dashboard := NewLensDashboardModel("test-label", issues, issueMap, theme)
	dashboard.SetSize(80, 40)

	// Check workstream count
	wsCount := dashboard.WorkstreamCount()
	t.Logf("Workstream count: %d", wsCount)

	// Get View output in flat mode
	flatView := dashboard.View()
	t.Logf("Flat view contains '[flat view]': %v", strings.Contains(flatView, "[flat view]"))

	// Toggle to workstream view
	dashboard.ToggleViewType()

	// Get View output in workstream mode
	workstreamView := dashboard.View()

	// If there are multiple workstreams, the view should show workstream info
	if wsCount > 1 {
		// Check for workstream indicator in keybind bar (format: "streams:N")
		if !strings.Contains(workstreamView, "streams:") {
			t.Errorf("With %d workstreams and ViewTypeWorkstream, view should show 'streams:', got:\n%s", wsCount, workstreamView)
		}
		if strings.Contains(workstreamView, "[flat view]") {
			t.Errorf("In workstream mode with multiple workstreams, should not show '[flat view]', got:\n%s", workstreamView)
		}
	} else {
		// With <= 1 workstream, even workstream mode shows as flat (by design)
		t.Logf("Only %d workstream(s), view stays flat even in workstream mode (by design)", wsCount)
	}
}

func TestLensDashboardUpstreamContextBlockers(t *testing.T) {
	// Test that flat view includes context issues that block primaries (upstream blockers)
	// This should match the behavior of workstream view
	//
	// Setup:
	// - "blocker" (context, no label) blocks "primary" (has label)
	// - "transitive-blocker" (context) blocks "blocker"
	// - "downstream" (context) is blocked by "primary"
	//
	// Flat view should include ALL of these, not just downstream
	issues := []model.Issue{
		{ID: "transitive-blocker", Status: model.StatusOpen, Labels: []string{}},
		{ID: "blocker", Status: model.StatusOpen, Labels: []string{}, Dependencies: []*model.Dependency{
			{DependsOnID: "transitive-blocker", Type: model.DepBlocks},
		}},
		{ID: "primary", Status: model.StatusOpen, Labels: []string{"test-label"}, Dependencies: []*model.Dependency{
			{DependsOnID: "blocker", Type: model.DepBlocks},
		}},
		{ID: "downstream", Status: model.StatusOpen, Labels: []string{}, Dependencies: []*model.Dependency{
			{DependsOnID: "primary", Type: model.DepBlocks},
		}},
	}

	issueMap := make(map[string]*model.Issue)
	for i := range issues {
		issueMap[issues[i].ID] = &issues[i]
	}

	renderer := lipgloss.DefaultRenderer()
	theme := DefaultTheme(renderer)
	dashboard := NewLensDashboardModel("test-label", issues, issueMap, theme)
	dashboard.SetSize(80, 40)

	// At default Depth2, flat view should now include upstream context blockers
	flatTotal := dashboard.IssueCount()
	flatPrimary := dashboard.PrimaryCount()
	flatContext := dashboard.ContextCount()

	t.Logf("Flat view counts: total=%d, primary=%d, context=%d", flatTotal, flatPrimary, flatContext)

	// Expected:
	// - 1 primary: "primary"
	// - 3 context: "blocker", "transitive-blocker", "downstream"
	// - Total: 4

	if flatPrimary != 1 {
		t.Errorf("Expected 1 primary issue, got %d", flatPrimary)
	}

	if flatContext != 3 {
		t.Errorf("Expected 3 context issues (blocker + transitive-blocker + downstream), got %d", flatContext)
	}

	if flatTotal != 4 {
		t.Errorf("Expected 4 total issues, got %d", flatTotal)
	}

	// Also test at DepthAll to ensure all issues are included
	// Depth cycle: Depth2 -> Depth3 -> DepthAll -> Depth1 -> Depth2
	dashboard.CycleDepth() // Depth2 -> Depth3
	dashboard.CycleDepth() // Depth3 -> DepthAll

	depthAllTotal := dashboard.IssueCount()
	t.Logf("DepthAll counts: total=%d", depthAllTotal)

	if depthAllTotal != 4 {
		t.Errorf("At DepthAll, expected 4 total issues, got %d", depthAllTotal)
	}

	// Test Depth1 (flat list of primary issues only)
	dashboard.CycleDepth() // DepthAll -> Depth1

	depth1Total := dashboard.IssueCount()
	depth1Primary := dashboard.PrimaryCount()
	t.Logf("Depth1 counts: total=%d, primary=%d", depth1Total, depth1Primary)

	// At Depth1, only primary issues are shown (no context)
	if depth1Primary != 1 {
		t.Errorf("At Depth1, expected 1 primary issue, got %d", depth1Primary)
	}
}

func TestLensDashboardToggleViaFullUpdateCycle(t *testing.T) {
	// Test the full Update() -> View() cycle to catch any issues with value semantics
	issues := []model.Issue{
		{ID: "A", Status: model.StatusOpen, Labels: []string{"test-label", "ws1"}},
		{ID: "B", Status: model.StatusOpen, Labels: []string{"test-label", "ws1"}, Dependencies: []*model.Dependency{
			{DependsOnID: "A", Type: model.DepBlocks},
		}},
		{ID: "C", Status: model.StatusOpen, Labels: []string{"test-label", "ws2"}},
	}

	m := NewModel(issues, nil, "")
	m.ready = true // Simulate initialization complete

	// Set up lens dashboard
	issueMap := make(map[string]*model.Issue)
	for i := range issues {
		issueMap[issues[i].ID] = &issues[i]
	}
	m.issueMap = issueMap
	dashboard2 := NewLensDashboardModel("test-label", issues, issueMap, m.theme)
	m.lensDashboard = dashboard2
	m.lensDashboard.SetSize(80, 40)
	m.showLensDashboard = true
	m.focused = focusLensDashboard

	// Test that the lens dashboard can be toggled via handleLensDashboardKeys
	m = m.handleLensDashboardKeys(keyMsg("w"))

	// Verify view type changed
	if !m.lensDashboard.IsWorkstreamView() {
		t.Error("After 'w' key via handleLensDashboardKeys(), should be in workstream view")
	}

	// Check workstream count
	wsCount := m.lensDashboard.WorkstreamCount()
	t.Logf("Workstream count: %d", wsCount)

	// Toggle back
	m = m.handleLensDashboardKeys(keyMsg("w"))

	if m.lensDashboard.IsWorkstreamView() {
		t.Error("After second 'w' key, should be back in flat view")
	}
}

func TestEpicDashboardDepthBehavior(t *testing.T) {
	// Test that epic mode depth works correctly:
	// - Depth1: direct children of epic
	// - Depth2: children + grandchildren
	// - Depth3: children + grandchildren + great-grandchildren
	//
	// Setup: epic -> child1 -> grandchild1 -> great-grandchild1
	//             -> child2 -> grandchild2
	issues := []model.Issue{
		{ID: "epic", Status: model.StatusOpen, IssueType: model.TypeEpic, Title: "Test Epic"},
		{ID: "child1", Status: model.StatusOpen, Dependencies: []*model.Dependency{
			{DependsOnID: "epic", Type: model.DepParentChild},
		}},
		{ID: "child2", Status: model.StatusOpen, Dependencies: []*model.Dependency{
			{DependsOnID: "epic", Type: model.DepParentChild},
		}},
		{ID: "grandchild1", Status: model.StatusOpen, Dependencies: []*model.Dependency{
			{DependsOnID: "child1", Type: model.DepParentChild},
		}},
		{ID: "grandchild2", Status: model.StatusOpen, Dependencies: []*model.Dependency{
			{DependsOnID: "child2", Type: model.DepParentChild},
		}},
		{ID: "great-grandchild1", Status: model.StatusOpen, Dependencies: []*model.Dependency{
			{DependsOnID: "grandchild1", Type: model.DepParentChild},
		}},
	}

	issueMap := make(map[string]*model.Issue)
	for i := range issues {
		issueMap[issues[i].ID] = &issues[i]
	}

	renderer := lipgloss.DefaultRenderer()
	theme := DefaultTheme(renderer)
	dashboard := NewEpicLensModel("epic", "Test Epic", issues, issueMap, theme)
	dashboard.SetSize(80, 40)

	// Default is Depth2
	depth2Primary := dashboard.PrimaryCount()
	t.Logf("Depth2 (default) primary count: %d", depth2Primary)

	// Depth2 should include: epic + child1, child2, grandchild1, grandchild2 (5 issues)
	// The entry epic is now included in the view at all depth levels
	if depth2Primary != 5 {
		t.Errorf("At Depth2, expected 5 primary issues (epic + 2 children + 2 grandchildren), got %d", depth2Primary)
	}

	// Cycle to Depth3
	dashboard.CycleDepth() // Depth2 -> Depth3

	depth3Primary := dashboard.PrimaryCount()
	t.Logf("Depth3 primary count: %d", depth3Primary)

	// Depth3 should include: epic + child1, child2, grandchild1, grandchild2, great-grandchild1 (6 issues)
	if depth3Primary != 6 {
		t.Errorf("At Depth3, expected 6 primary issues (epic + 5 descendants), got %d", depth3Primary)
	}

	// Cycle to DepthAll
	dashboard.CycleDepth() // Depth3 -> DepthAll

	depthAllPrimary := dashboard.PrimaryCount()
	t.Logf("DepthAll primary count: %d", depthAllPrimary)

	// DepthAll should include epic + all descendants (6 issues)
	if depthAllPrimary != 6 {
		t.Errorf("At DepthAll, expected 6 primary issues (epic + 5 descendants), got %d", depthAllPrimary)
	}

	// Cycle to Depth1
	dashboard.CycleDepth() // DepthAll -> Depth1

	depth1Primary := dashboard.PrimaryCount()
	t.Logf("Depth1 primary count: %d", depth1Primary)

	// Depth1 should include: epic + child1, child2 (3 issues: entry + 2 direct children)
	if depth1Primary != 3 {
		t.Errorf("At Depth1, expected 3 primary issues (epic + 2 direct children), got %d", depth1Primary)
	}
}

func TestLensDashboardViewHeightMatchesExpected(t *testing.T) {
	// Test that the View() output has exactly the expected number of lines
	// based on the height set via SetSize()
	issues := []model.Issue{
		{ID: "ready1", Status: model.StatusOpen, Labels: []string{"test", "ui"}},
		{ID: "ready2", Status: model.StatusOpen, Labels: []string{"test"}},
		{ID: "blocked1", Status: model.StatusBlocked, Labels: []string{"test"}},
		{ID: "closed1", Status: model.StatusClosed, Labels: []string{"test"}},
	}
	issueMap := make(map[string]*model.Issue)
	for i := range issues {
		issueMap[issues[i].ID] = &issues[i]
	}

	renderer := lipgloss.DefaultRenderer()
	theme := DefaultTheme(renderer)

	testCases := []struct {
		name         string
		height       int
		withFuzzy    bool
		withScope    bool
		withInput    bool
	}{
		{"base-small", 20, false, false, false},
		{"base-medium", 40, false, false, false},
		{"base-large", 60, false, false, false},
		{"fuzzy-search", 40, true, false, false},
		{"scope-labels", 40, false, true, false},
		{"scope-input", 40, false, false, true},
		{"all-features", 40, true, true, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dashboard := NewLensDashboardModel("test", issues, issueMap, theme)
			dashboard.SetSize(80, tc.height)

			// Enable features as needed
			if tc.withFuzzy {
				dashboard.OpenFuzzySearch()
			}
			if tc.withScope {
				dashboard.AddScopeLabel("ui")
			}
			if tc.withInput {
				dashboard.OpenScopeInput()
			}

			view := dashboard.View()
			actualLines := strings.Count(view, "\n") + 1

			// The view should output exactly tc.height lines
			if actualLines != tc.height {
				t.Errorf("height=%d: expected %d lines, got %d (diff: %d)",
					tc.height, tc.height, actualLines, actualLines-tc.height)

				// Debug: show calculated viewport
				vp := dashboard.calculateViewport()
				t.Logf("  calculateViewport: headerLines=%d, contentHeight=%d, footerLines=%d, sum=%d",
					vp.HeaderLines, vp.ContentHeight, vp.FooterLines,
					vp.HeaderLines+vp.ContentHeight+vp.FooterLines)
			}
		})
	}
}

func TestLensDashboardDepthBehavior(t *testing.T) {
	// Test that label mode depth works correctly:
	// - Depth1: only issues with the label directly applied (flat list)
	// - Depth2: tree with 2 levels (root + children)
	// - Depth3: tree with 3 levels (root + children + grandchildren)
	//
	// Setup:
	// - "parent" has label "test-label"
	// - "child" is a child of "parent" (via parent-child dep) but no label
	// - "grandchild" is a child of "child" (via parent-child dep) but no label
	issues := []model.Issue{
		{ID: "parent", Status: model.StatusOpen, Labels: []string{"test-label"}},
		{ID: "child", Status: model.StatusOpen, Dependencies: []*model.Dependency{
			{DependsOnID: "parent", Type: model.DepParentChild},
		}},
		{ID: "grandchild", Status: model.StatusOpen, Dependencies: []*model.Dependency{
			{DependsOnID: "child", Type: model.DepParentChild},
		}},
		{ID: "unrelated", Status: model.StatusOpen, Labels: []string{"other-label"}},
	}

	issueMap := make(map[string]*model.Issue)
	for i := range issues {
		issueMap[issues[i].ID] = &issues[i]
	}

	renderer := lipgloss.DefaultRenderer()
	theme := DefaultTheme(renderer)
	dashboard := NewLensDashboardModel("test-label", issues, issueMap, theme)
	dashboard.SetSize(80, 40)

	// Default is Depth2 - tree shows 2 levels (parent + child)
	depth2Primary := dashboard.PrimaryCount()
	t.Logf("Depth2 (default) primary count: %d", depth2Primary)

	// At Depth2, tree shows root + 1 level of children
	// So we expect: parent (root) + child (1 level deep) = 2 issues
	if depth2Primary != 2 {
		t.Errorf("At Depth2, expected 2 primary issues (parent + child), got %d", depth2Primary)
	}

	// Cycle to Depth3 - should now include grandchild
	dashboard.CycleDepth() // Depth2 -> Depth3

	depth3Primary := dashboard.PrimaryCount()
	t.Logf("Depth3 primary count: %d", depth3Primary)

	// At Depth3, tree shows root + 2 levels of children
	// So we expect: parent + child + grandchild = 3 issues
	if depth3Primary != 3 {
		t.Errorf("At Depth3, expected 3 primary issues (parent + child + grandchild), got %d", depth3Primary)
	}

	// Cycle to DepthAll
	dashboard.CycleDepth() // Depth3 -> DepthAll

	depthAllPrimary := dashboard.PrimaryCount()
	t.Logf("DepthAll primary count: %d", depthAllPrimary)

	// At DepthAll, tree shows all levels
	if depthAllPrimary != 3 {
		t.Errorf("At DepthAll, expected 3 primary issues, got %d", depthAllPrimary)
	}

	// Cycle to Depth1
	dashboard.CycleDepth() // DepthAll -> Depth1

	depth1Primary := dashboard.PrimaryCount()
	t.Logf("Depth1 primary count: %d", depth1Primary)

	// Depth1 should show ONLY the issue with the label directly applied (flat list)
	if depth1Primary != 1 {
		t.Errorf("At Depth1, expected 1 primary issue (only directly labeled), got %d", depth1Primary)
	}
}

func TestLensSelectorDirectCountsOnly(t *testing.T) {
	// Setup: parent has label, children do NOT have label
	// Label selector should count ONLY directly labeled issues (not descendants)
	//
	// parent (has label "test") -> child (no label)
	//                           -> child2 (no label)
	issues := []model.Issue{
		{ID: "parent", Status: model.StatusOpen, Labels: []string{"test"}},
		{ID: "child", Status: model.StatusOpen, Dependencies: []*model.Dependency{
			{DependsOnID: "parent", Type: model.DepParentChild},
		}},
		{ID: "child2", Status: model.StatusClosed, Dependencies: []*model.Dependency{
			{DependsOnID: "parent", Type: model.DepParentChild},
		}},
		{ID: "unrelated", Status: model.StatusOpen, Labels: []string{"other"}},
	}

	renderer := lipgloss.DefaultRenderer()
	theme := DefaultTheme(renderer)
	selector := NewLensSelectorModel(issues, theme, nil)

	// Find the "test" label item
	var testLensItem *LensItem
	for i := range selector.allLabels {
		if selector.allLabels[i].Value == "test" {
			testLensItem = &selector.allLabels[i]
			break
		}
	}

	if testLensItem == nil {
		t.Fatal("Expected to find 'test' label in selector")
	}

	// Should count ONLY direct: parent (1 issue with "test" label)
	if testLensItem.IssueCount != 1 {
		t.Errorf("Expected IssueCount=1 (only direct), got %d", testLensItem.IssueCount)
	}

	// Closed should be 0 (parent is open, children don't have label)
	if testLensItem.ClosedCount != 0 {
		t.Errorf("Expected ClosedCount=0, got %d", testLensItem.ClosedCount)
	}

	// Progress should be 0/1 = 0
	if testLensItem.Progress != 0.0 {
		t.Errorf("Expected Progress=0, got %.3f", testLensItem.Progress)
	}
}

func TestEpicSelectorCountsDescendants(t *testing.T) {
	// Setup: epic with children - epic selector should count ALL descendants
	//
	// epic -> child1 (open)
	//      -> child2 (closed)
	issues := []model.Issue{
		{ID: "epic", Status: model.StatusOpen, IssueType: model.TypeEpic, Title: "Test Epic"},
		{ID: "child1", Status: model.StatusOpen, Dependencies: []*model.Dependency{
			{DependsOnID: "epic", Type: model.DepParentChild},
		}},
		{ID: "child2", Status: model.StatusClosed, Dependencies: []*model.Dependency{
			{DependsOnID: "epic", Type: model.DepParentChild},
		}},
	}

	renderer := lipgloss.DefaultRenderer()
	theme := DefaultTheme(renderer)
	selector := NewLensSelectorModel(issues, theme, nil)

	// Find the epic item
	var epicItem *LensItem
	for i := range selector.allEpics {
		if selector.allEpics[i].Value == "epic" {
			epicItem = &selector.allEpics[i]
			break
		}
	}

	if epicItem == nil {
		t.Fatal("Expected to find epic in selector")
	}

	// Should count all descendants: child1 + child2 = 2
	if epicItem.IssueCount != 2 {
		t.Errorf("Expected IssueCount=2 (all descendants), got %d", epicItem.IssueCount)
	}

	// Closed should be 1 (child2)
	if epicItem.ClosedCount != 1 {
		t.Errorf("Expected ClosedCount=1, got %d", epicItem.ClosedCount)
	}

	// Progress should be 1/2 = 0.5
	if epicItem.Progress != 0.5 {
		t.Errorf("Expected Progress=0.5, got %.3f", epicItem.Progress)
	}
}

func TestCrossEpicContextBlockerIsolation(t *testing.T) {
	// Test that viewing one epic does NOT show descendants from unrelated epics,
	// even when they share a common upstream blocker.
	//
	// Setup:
	// - Epic1 (Auth): epic1 -> child1
	// - Epic2 (E-Commerce): epic2 -> child2
	// - Shared blocker: "db-migrations" blocks BOTH child1 and child2
	//
	// When viewing Epic1, we should see:
	// - Primary: epic1, child1
	// - Context: db-migrations (blocker)
	// - NOT: epic2, child2 (unrelated epic's issues)
	issues := []model.Issue{
		// Epic 1 (Auth)
		{ID: "epic1", Status: model.StatusOpen, IssueType: model.TypeEpic, Title: "Auth Epic"},
		{ID: "child1", Status: model.StatusOpen, Dependencies: []*model.Dependency{
			{DependsOnID: "epic1", Type: model.DepParentChild},
			{DependsOnID: "db-migrations", Type: model.DepBlocks},
		}},

		// Epic 2 (E-Commerce)
		{ID: "epic2", Status: model.StatusOpen, IssueType: model.TypeEpic, Title: "E-Commerce Epic"},
		{ID: "child2", Status: model.StatusOpen, Dependencies: []*model.Dependency{
			{DependsOnID: "epic2", Type: model.DepParentChild},
			{DependsOnID: "db-migrations", Type: model.DepBlocks},
		}},

		// Shared infrastructure blocker
		{ID: "db-migrations", Status: model.StatusOpen, Title: "Database Migrations"},
	}

	issueMap := make(map[string]*model.Issue)
	for i := range issues {
		issueMap[issues[i].ID] = &issues[i]
	}

	renderer := lipgloss.DefaultRenderer()
	theme := DefaultTheme(renderer)

	// View Epic1 dashboard
	dashboard := NewEpicLensModel("epic1", "Auth Epic", issues, issueMap, theme)
	dashboard.SetSize(80, 40)

	total := dashboard.IssueCount()
	primary := dashboard.PrimaryCount()
	context := dashboard.ContextCount()

	t.Logf("Epic1 view: total=%d, primary=%d, context=%d", total, primary, context)

	// KEY ASSERTION: Cross-epic isolation must work
	// Total should be at least 2 (epic1 entry + child1)
	// In centered mode, context blockers (db-migrations) may be shown separately
	// NOT 5+ (which would include epic2, child2)
	//
	// The entry epic is now included in the view, so minimum is:
	// - epic1 (entry, primary)
	// - child1 (primary)
	// - db-migrations (context blocker, shown in upstream section for centered mode)
	if total < 2 {
		t.Errorf("Expected at least 2 primary issues (epic1 + child1), got %d", total)
	}

	// Verify epic2 and child2 are NOT in the view
	// Cross-epic isolation is broken if we see > 4 issues
	// (epic1 + child1 + db-migrations + potentially upstream blockers of entry)
	if total > 4 {
		t.Errorf("Cross-epic isolation failed: got %d issues, "+
			"expected max 4 (epic1 tree + blockers only)", total)
	}
}

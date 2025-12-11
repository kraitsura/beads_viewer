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

func TestLabelDashboardToggleViewType(t *testing.T) {
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
	dashboard := NewLabelDashboardModel("test-label", issues, issueMap, theme)

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

func TestLabelDashboardToggleViewTypeViaModel(t *testing.T) {
	// Create test issues with a label
	issues := []model.Issue{
		{ID: "A", Status: model.StatusOpen, Labels: []string{"test-label"}},
		{ID: "B", Status: model.StatusOpen, Labels: []string{"test-label"}},
	}

	m := NewModel(issues, nil, "")

	// Open label dashboard
	issueMap := make(map[string]*model.Issue)
	for i := range issues {
		issueMap[issues[i].ID] = &issues[i]
	}
	m.issueMap = issueMap
	dashboard := NewLabelDashboardModel("test-label", issues, issueMap, m.theme)
	m.labelDashboard = dashboard
	m.showLabelDashboard = true
	m.focused = focusLabelDashboard

	// Verify initial state
	if m.labelDashboard.GetViewType() != ViewTypeFlat {
		t.Errorf("Initial viewType should be ViewTypeFlat, got %v", m.labelDashboard.GetViewType())
	}

	// Simulate 'w' key press through handleLabelDashboardKeys
	// Note: handleLabelDashboardKeys returns a new Model (value semantics)
	m = m.handleLabelDashboardKeys(keyMsg("w"))

	// The critical test: did the viewType change persist?
	if m.labelDashboard.GetViewType() != ViewTypeWorkstream {
		t.Errorf("After 'w' key, viewType should be ViewTypeWorkstream, got %v", m.labelDashboard.GetViewType())
	}
	if !m.labelDashboard.IsWorkstreamView() {
		t.Error("IsWorkstreamView() should return true after 'w' key")
	}

	// Verify status message was set (now includes debug info)
	if !strings.Contains(m.statusMsg, "Switched to workstream view") {
		t.Errorf("Expected statusMsg to contain 'Switched to workstream view', got '%s'", m.statusMsg)
	}

	// Toggle back
	m = m.handleLabelDashboardKeys(keyMsg("w"))

	if m.labelDashboard.GetViewType() != ViewTypeFlat {
		t.Errorf("After second 'w' key, viewType should be ViewTypeFlat, got %v", m.labelDashboard.GetViewType())
	}
	if !strings.Contains(m.statusMsg, "Switched to flat view") {
		t.Errorf("Expected statusMsg to contain 'Switched to flat view', got '%s'", m.statusMsg)
	}
}

func TestLabelDashboardViewOutputChanges(t *testing.T) {
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
	dashboard := NewLabelDashboardModel("test-label", issues, issueMap, theme)
	dashboard.SetSize(80, 40)

	// Check workstream count
	wsCount := dashboard.WorkstreamCount()
	t.Logf("Workstream count: %d", wsCount)

	// Get View output in flat mode
	flatView := dashboard.View()
	t.Logf("Flat view contains '[flat]': %v", strings.Contains(flatView, "[flat]"))

	// Toggle to workstream view
	dashboard.ToggleViewType()

	// Get View output in workstream mode
	workstreamView := dashboard.View()

	// If there are multiple workstreams, the view should show workstream info
	if wsCount > 1 {
		if !strings.Contains(workstreamView, "workstreams:") {
			t.Errorf("With %d workstreams and ViewTypeWorkstream, view should show 'workstreams:', got:\n%s", wsCount, workstreamView)
		}
		if strings.Contains(workstreamView, "[flat]") {
			t.Errorf("In workstream mode with multiple workstreams, should not show '[flat]', got:\n%s", workstreamView)
		}
	} else {
		// With <= 1 workstream, even workstream mode shows as flat (by design)
		t.Logf("Only %d workstream(s), view stays flat even in workstream mode (by design)", wsCount)
	}
}

func TestLabelDashboardToggleViaFullUpdateCycle(t *testing.T) {
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

	// Set up label dashboard
	issueMap := make(map[string]*model.Issue)
	for i := range issues {
		issueMap[issues[i].ID] = &issues[i]
	}
	m.issueMap = issueMap
	dashboard2 := NewLabelDashboardModel("test-label", issues, issueMap, m.theme)
	m.labelDashboard = dashboard2
	m.labelDashboard.SetSize(80, 40)
	m.showLabelDashboard = true
	m.focused = focusLabelDashboard

	// Verify initial flat view
	flatView := m.View()
	if !strings.Contains(flatView, "[flat]") {
		t.Errorf("Initial view should contain '[flat]', got:\n%s", flatView)
	}
	// Flat view footer should suggest "w: workstreams"
	if !strings.Contains(flatView, "w: workstreams") {
		t.Errorf("Initial flat view footer should suggest 'w: workstreams', got:\n%s", flatView)
	}

	// Send 'w' key through Update()
	updatedAny, _ := m.Update(keyMsg("w"))
	m = updatedAny.(Model)

	// Verify view type changed
	if !m.labelDashboard.IsWorkstreamView() {
		t.Error("After 'w' key via Update(), should be in workstream view")
	}

	// Check View() output
	workstreamView := m.View()
	wsCount := m.labelDashboard.WorkstreamCount()
	t.Logf("Workstream count: %d", wsCount)

	if wsCount > 1 {
		if !strings.Contains(workstreamView, "workstreams:") {
			t.Errorf("After toggle via Update(), view should contain 'workstreams:', got:\n%s", workstreamView)
		}
		if strings.Contains(workstreamView, "[flat]") {
			t.Errorf("After toggle via Update(), view should NOT contain '[flat]', got:\n%s", workstreamView)
		}
		// Workstream view footer should suggest "w: flat view"
		if !strings.Contains(workstreamView, "w: flat view") {
			t.Errorf("Workstream view footer should suggest 'w: flat view', got:\n%s", workstreamView)
		}
	}

	// Toggle back
	updatedAny, _ = m.Update(keyMsg("w"))
	m = updatedAny.(Model)

	if m.labelDashboard.IsWorkstreamView() {
		t.Error("After second 'w' key, should be back in flat view")
	}

	backToFlatView := m.View()
	if !strings.Contains(backToFlatView, "[flat]") {
		t.Errorf("After toggling back, view should contain '[flat]', got:\n%s", backToFlatView)
	}
}

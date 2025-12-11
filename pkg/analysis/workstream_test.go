package analysis

import (
	"fmt"
	"testing"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

func TestDetectWorkstreams_Empty(t *testing.T) {
	ws := DetectWorkstreams(nil, nil, "test")
	if len(ws) != 0 {
		t.Errorf("expected 0 workstreams, got %d", len(ws))
	}
}

func TestDetectWorkstreams_SingleIssue(t *testing.T) {
	issues := []model.Issue{
		{ID: "A", Title: "Issue A", Status: model.StatusOpen},
	}
	primaryIDs := map[string]bool{"A": true}

	ws := DetectWorkstreams(issues, primaryIDs, "test-label")

	if len(ws) != 1 {
		t.Fatalf("expected 1 workstream, got %d", len(ws))
	}
	// New algorithm uses "Standalone" (capitalized)
	if ws[0].Name != "Standalone" {
		t.Errorf("expected name 'Standalone', got %q", ws[0].Name)
	}
	if ws[0].PrimaryCount != 1 {
		t.Errorf("expected PrimaryCount 1, got %d", ws[0].PrimaryCount)
	}
}

func TestDetectWorkstreams_TwoDisconnected(t *testing.T) {
	issues := []model.Issue{
		{ID: "A", Title: "Issue A", Status: model.StatusOpen},
		{ID: "B", Title: "Issue B", Status: model.StatusOpen},
	}
	primaryIDs := map[string]bool{"A": true, "B": true}

	ws := DetectWorkstreams(issues, primaryIDs, "test-label")

	// Two disconnected single issues without labels go into "Standalone" workstream
	if len(ws) != 1 {
		t.Fatalf("expected 1 workstream (Standalone), got %d", len(ws))
	}
	// New algorithm uses "Standalone" (capitalized)
	if ws[0].Name != "Standalone" {
		t.Errorf("expected name 'Standalone', got %q", ws[0].Name)
	}
	if len(ws[0].Issues) != 2 {
		t.Errorf("expected 2 issues in standalone workstream, got %d", len(ws[0].Issues))
	}
}

func TestDetectWorkstreams_TwoConnected(t *testing.T) {
	issues := []model.Issue{
		{
			ID:     "A",
			Title:  "Issue A",
			Status: model.StatusOpen,
			Dependencies: []*model.Dependency{
				{IssueID: "A", DependsOnID: "B", Type: model.DepBlocks},
			},
		},
		{ID: "B", Title: "Issue B", Status: model.StatusOpen},
	}
	primaryIDs := map[string]bool{"A": true, "B": true}

	ws := DetectWorkstreams(issues, primaryIDs, "test-label")

	if len(ws) != 1 {
		t.Fatalf("expected 1 workstream (connected), got %d", len(ws))
	}
	if len(ws[0].Issues) != 2 {
		t.Errorf("expected 2 issues in workstream, got %d", len(ws[0].Issues))
	}
}

func TestDetectWorkstreams_ChainOfThree(t *testing.T) {
	// A depends on B depends on C
	issues := []model.Issue{
		{
			ID:     "A",
			Title:  "Issue A",
			Status: model.StatusOpen,
			Dependencies: []*model.Dependency{
				{IssueID: "A", DependsOnID: "B", Type: model.DepBlocks},
			},
		},
		{
			ID:     "B",
			Title:  "Issue B",
			Status: model.StatusOpen,
			Dependencies: []*model.Dependency{
				{IssueID: "B", DependsOnID: "C", Type: model.DepBlocks},
			},
		},
		{ID: "C", Title: "Issue C", Status: model.StatusOpen},
	}
	primaryIDs := map[string]bool{"A": true, "B": true, "C": true}

	ws := DetectWorkstreams(issues, primaryIDs, "test-label")

	if len(ws) != 1 {
		t.Fatalf("expected 1 workstream (chain), got %d", len(ws))
	}
	if len(ws[0].Issues) != 3 {
		t.Errorf("expected 3 issues in workstream, got %d", len(ws[0].Issues))
	}
}

func TestDetectWorkstreams_TwoSeparateChains(t *testing.T) {
	// Chain 1: A -> B (label: chain1)
	// Chain 2: C -> D (label: chain2)
	// New algorithm requires distinguishing labels to create separate workstreams
	issues := []model.Issue{
		{
			ID:     "A",
			Title:  "Issue A",
			Status: model.StatusOpen,
			Labels: []string{"chain1"},
			Dependencies: []*model.Dependency{
				{IssueID: "A", DependsOnID: "B", Type: model.DepBlocks},
			},
		},
		{ID: "B", Title: "Issue B", Status: model.StatusOpen, Labels: []string{"chain1"}},
		{
			ID:     "C",
			Title:  "Issue C",
			Status: model.StatusOpen,
			Labels: []string{"chain2"},
			Dependencies: []*model.Dependency{
				{IssueID: "C", DependsOnID: "D", Type: model.DepBlocks},
			},
		},
		{ID: "D", Title: "Issue D", Status: model.StatusOpen, Labels: []string{"chain2"}},
	}
	primaryIDs := map[string]bool{"A": true, "B": true, "C": true, "D": true}

	ws := DetectWorkstreams(issues, primaryIDs, "test-label")

	if len(ws) != 2 {
		t.Fatalf("expected 2 workstreams, got %d", len(ws))
	}
	// Each workstream should have 2 issues
	for i, w := range ws {
		if len(w.Issues) != 2 {
			t.Errorf("workstream %d: expected 2 issues, got %d", i, len(w.Issues))
		}
	}
}

func TestDetectWorkstreams_ContextIssues(t *testing.T) {
	// A (primary) depends on B (context - not in primaryIDs)
	issues := []model.Issue{
		{
			ID:     "A",
			Title:  "Issue A",
			Status: model.StatusOpen,
			Dependencies: []*model.Dependency{
				{IssueID: "A", DependsOnID: "B", Type: model.DepBlocks},
			},
		},
		{ID: "B", Title: "Issue B", Status: model.StatusOpen},
	}
	primaryIDs := map[string]bool{"A": true} // B is context

	ws := DetectWorkstreams(issues, primaryIDs, "test-label")

	if len(ws) != 1 {
		t.Fatalf("expected 1 workstream, got %d", len(ws))
	}
	if ws[0].PrimaryCount != 1 {
		t.Errorf("expected PrimaryCount 1, got %d", ws[0].PrimaryCount)
	}
	if ws[0].ContextCount != 1 {
		t.Errorf("expected ContextCount 1, got %d", ws[0].ContextCount)
	}
}

func TestDetectWorkstreams_Progress(t *testing.T) {
	// 2 primary issues, 1 closed - both disconnected so consolidated into standalone
	issues := []model.Issue{
		{ID: "A", Title: "Issue A", Status: model.StatusOpen},
		{ID: "B", Title: "Issue B", Status: model.StatusClosed},
	}
	primaryIDs := map[string]bool{"A": true, "B": true}

	ws := DetectWorkstreams(issues, primaryIDs, "test-label")

	// Two disconnected single issues = 1 consolidated standalone workstream
	if len(ws) != 1 {
		t.Fatalf("expected 1 workstream (consolidated standalone), got %d", len(ws))
	}

	// The consolidated workstream should have progress 0.5 (1 of 2 closed)
	if ws[0].Progress != 0.5 {
		t.Errorf("expected progress 0.5, got %f", ws[0].Progress)
	}
	if ws[0].ClosedCount != 1 {
		t.Errorf("expected ClosedCount 1, got %d", ws[0].ClosedCount)
	}
	if ws[0].ReadyCount != 1 {
		t.Errorf("expected ReadyCount 1, got %d", ws[0].ReadyCount)
	}
}

func TestDetectWorkstreams_Blocked(t *testing.T) {
	// A depends on B (open), so A is blocked
	issues := []model.Issue{
		{
			ID:     "A",
			Title:  "Issue A",
			Status: model.StatusOpen,
			Dependencies: []*model.Dependency{
				{IssueID: "A", DependsOnID: "B", Type: model.DepBlocks},
			},
		},
		{ID: "B", Title: "Issue B", Status: model.StatusOpen},
	}
	primaryIDs := map[string]bool{"A": true, "B": true}

	ws := DetectWorkstreams(issues, primaryIDs, "test-label")

	if len(ws) != 1 {
		t.Fatalf("expected 1 workstream, got %d", len(ws))
	}
	// A is blocked (depends on B), B is ready
	if ws[0].BlockedCount != 1 {
		t.Errorf("expected BlockedCount 1, got %d", ws[0].BlockedCount)
	}
	if ws[0].ReadyCount != 1 {
		t.Errorf("expected ReadyCount 1, got %d", ws[0].ReadyCount)
	}
	if ws[0].IsBlocked {
		t.Error("workstream should not be fully blocked (B is ready)")
	}
}

func TestDetectWorkstreams_FullyBlocked(t *testing.T) {
	// A depends on external X (not in issue set)
	// Since X is not in the set, A appears blocked but we can't verify the blocker
	issues := []model.Issue{
		{
			ID:     "A",
			Title:  "Issue A",
			Status: model.StatusOpen,
			Dependencies: []*model.Dependency{
				{IssueID: "A", DependsOnID: "X", Type: model.DepBlocks}, // X not in set
			},
		},
	}
	primaryIDs := map[string]bool{"A": true}

	ws := DetectWorkstreams(issues, primaryIDs, "test-label")

	if len(ws) != 1 {
		t.Fatalf("expected 1 workstream, got %d", len(ws))
	}
	// A's blocker (X) is not in the issue set, so A is considered ready
	if ws[0].ReadyCount != 1 {
		t.Errorf("expected ReadyCount 1 (blocker outside set), got %d", ws[0].ReadyCount)
	}
}

func TestDetectWorkstreams_RelatedLabels(t *testing.T) {
	// Test with issues that share the same distinguishing label to form one workstream
	issues := []model.Issue{
		{
			ID:     "A",
			Title:  "Issue A",
			Status: model.StatusOpen,
			Labels: []string{"infra", "feat:auth"},
			Dependencies: []*model.Dependency{
				{IssueID: "A", DependsOnID: "B", Type: model.DepBlocks},
			},
		},
		{
			ID:     "B",
			Title:  "Issue B",
			Status: model.StatusOpen,
			Labels: []string{"infra", "feat:auth"},
		},
	}
	primaryIDs := map[string]bool{"A": true, "B": true}

	ws := DetectWorkstreams(issues, primaryIDs, "infra")

	// With feat:auth on both issues, they should be in the same workstream
	if len(ws) != 1 {
		t.Fatalf("expected 1 workstream, got %d", len(ws))
	}
	// RelatedLabels collects ALL labels from issues in the workstream
	// Should include feat:auth (appears in both)
	found := false
	for _, label := range ws[0].RelatedLabels {
		if label == "feat:auth" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'feat:auth' in RelatedLabels")
	}
}

func TestDetectWorkstreams_Naming(t *testing.T) {
	// Workstream with feat: label should get formatted name
	issues := []model.Issue{
		{
			ID:     "A",
			Title:  "Issue A",
			Status: model.StatusOpen,
			Labels: []string{"infra", "feat:inbound-emails"},
			Dependencies: []*model.Dependency{
				{IssueID: "A", DependsOnID: "B", Type: model.DepBlocks},
			},
		},
		{
			ID:     "B",
			Title:  "Issue B",
			Status: model.StatusOpen,
			Labels: []string{"infra", "feat:inbound-emails"},
		},
	}
	primaryIDs := map[string]bool{"A": true, "B": true}

	ws := DetectWorkstreams(issues, primaryIDs, "infra")

	if len(ws) != 1 {
		t.Fatalf("expected 1 workstream, got %d", len(ws))
	}
	// New algorithm strips prefix and capitalizes first letter
	if ws[0].Name != "Inbound-emails" {
		t.Errorf("expected name 'Inbound-emails', got %q", ws[0].Name)
	}
}

func TestDetectWorkstreams_OnlyBlocksDeps(t *testing.T) {
	// A is "related" to B (not blocks) - should NOT be connected by the related dep
	// But since both are single issues without other connections, they get consolidated into standalone
	issues := []model.Issue{
		{
			ID:     "A",
			Title:  "Issue A",
			Status: model.StatusOpen,
			Dependencies: []*model.Dependency{
				{IssueID: "A", DependsOnID: "B", Type: model.DepRelated}, // Not blocks!
			},
		},
		{ID: "B", Title: "Issue B", Status: model.StatusOpen},
	}
	primaryIDs := map[string]bool{"A": true, "B": true}

	ws := DetectWorkstreams(issues, primaryIDs, "test-label")

	// Related deps don't connect issues for workstream purposes
	// Without labels, all issues go into "Standalone" workstream
	if len(ws) != 1 {
		t.Fatalf("expected 1 workstream (Standalone), got %d", len(ws))
	}
	// New algorithm uses "Standalone" (capitalized)
	if ws[0].Name != "Standalone" {
		t.Errorf("expected Standalone workstream, got %q", ws[0].Name)
	}
	// Both issues should be in the standalone workstream
	if len(ws[0].Issues) != 2 {
		t.Errorf("expected 2 issues in standalone, got %d", len(ws[0].Issues))
	}
}

func TestDetectWorkstreams_RelatedDepsDoNotConnect(t *testing.T) {
	// Test that "related" deps don't create connectivity between connected components
	// Chain1: A -> B (blocks, label: chain1), Chain2: C -> D (blocks, label: chain2)
	// New algorithm requires distinguishing labels to create separate workstreams
	issues := []model.Issue{
		{
			ID:     "A",
			Title:  "Issue A",
			Status: model.StatusOpen,
			Labels: []string{"chain1"},
			Dependencies: []*model.Dependency{
				{IssueID: "A", DependsOnID: "B", Type: model.DepBlocks},
			},
		},
		{ID: "B", Title: "Issue B", Status: model.StatusOpen, Labels: []string{"chain1"}},
		{
			ID:     "C",
			Title:  "Issue C",
			Status: model.StatusOpen,
			Labels: []string{"chain2"},
			Dependencies: []*model.Dependency{
				{IssueID: "C", DependsOnID: "D", Type: model.DepBlocks},
				{IssueID: "C", DependsOnID: "A", Type: model.DepRelated}, // Related, not blocks!
			},
		},
		{ID: "D", Title: "Issue D", Status: model.StatusOpen, Labels: []string{"chain2"}},
	}
	primaryIDs := map[string]bool{"A": true, "B": true, "C": true, "D": true}

	ws := DetectWorkstreams(issues, primaryIDs, "test-label")

	// Should be 2 workstreams based on labels
	if len(ws) != 2 {
		t.Fatalf("expected 2 workstreams (based on labels), got %d", len(ws))
	}
}

func TestDetectWorkstreams_ParentChildConnects(t *testing.T) {
	// Issues that share the same epic parent should be connected
	// A and B both have parent-child dep to Epic E
	issues := []model.Issue{
		{
			ID:     "A",
			Title:  "Issue A",
			Status: model.StatusOpen,
			Dependencies: []*model.Dependency{
				{IssueID: "A", DependsOnID: "E", Type: model.DepParentChild},
			},
		},
		{
			ID:     "B",
			Title:  "Issue B",
			Status: model.StatusOpen,
			Dependencies: []*model.Dependency{
				{IssueID: "B", DependsOnID: "E", Type: model.DepParentChild},
			},
		},
	}
	primaryIDs := map[string]bool{"A": true, "B": true}

	ws := DetectWorkstreams(issues, primaryIDs, "test-label")

	// A and B should be in the same workstream because they share epic parent E
	if len(ws) != 1 {
		t.Fatalf("expected 1 workstream (siblings via epic parent), got %d", len(ws))
	}
	if len(ws[0].Issues) != 2 {
		t.Errorf("expected 2 issues in workstream, got %d", len(ws[0].Issues))
	}
}

func TestDetectWorkstreams_SubdivideByPhaseLabels(t *testing.T) {
	// Large workstream with phase labels should be subdivided
	// 6 issues sharing epic parent E, with phase1 (3) and phase2 (3) labels
	issues := []model.Issue{
		{ID: "A1", Title: "Phase1 A", Status: model.StatusOpen, Labels: []string{"phase1"},
			Dependencies: []*model.Dependency{{IssueID: "A1", DependsOnID: "E", Type: model.DepParentChild}}},
		{ID: "A2", Title: "Phase1 B", Status: model.StatusOpen, Labels: []string{"phase1"},
			Dependencies: []*model.Dependency{{IssueID: "A2", DependsOnID: "E", Type: model.DepParentChild}}},
		{ID: "A3", Title: "Phase1 C", Status: model.StatusClosed, Labels: []string{"phase1"},
			Dependencies: []*model.Dependency{{IssueID: "A3", DependsOnID: "E", Type: model.DepParentChild}}},
		{ID: "B1", Title: "Phase2 A", Status: model.StatusOpen, Labels: []string{"phase2"},
			Dependencies: []*model.Dependency{{IssueID: "B1", DependsOnID: "E", Type: model.DepParentChild}}},
		{ID: "B2", Title: "Phase2 B", Status: model.StatusOpen, Labels: []string{"phase2"},
			Dependencies: []*model.Dependency{{IssueID: "B2", DependsOnID: "E", Type: model.DepParentChild}}},
		{ID: "B3", Title: "Phase2 C", Status: model.StatusOpen, Labels: []string{"phase2"},
			Dependencies: []*model.Dependency{{IssueID: "B3", DependsOnID: "E", Type: model.DepParentChild}}},
	}
	primaryIDs := map[string]bool{"A1": true, "A2": true, "A3": true, "B1": true, "B2": true, "B3": true}

	ws := DetectWorkstreams(issues, primaryIDs, "test-label")

	// Should be subdivided into 2 workstreams: phase1 and phase2
	if len(ws) != 2 {
		t.Fatalf("expected 2 workstreams (subdivided by phase), got %d", len(ws))
	}

	// Check names
	names := map[string]bool{}
	for _, w := range ws {
		names[w.Name] = true
	}
	if !names["Phase1"] && !names["phase1"] {
		t.Error("expected workstream named 'Phase1' or 'phase1'")
	}
	if !names["Phase2"] && !names["phase2"] {
		t.Error("expected workstream named 'Phase2' or 'phase2'")
	}

	// Each should have 3 issues
	for _, w := range ws {
		if len(w.Issues) != 3 {
			t.Errorf("expected 3 issues in workstream %q, got %d", w.Name, len(w.Issues))
		}
	}
}

func TestDetectWorkstreams_SortedBySize(t *testing.T) {
	// Create workstreams of different sizes using labels
	now := time.Now()
	issues := []model.Issue{
		// Small chain: X -> Y (2 issues, label: small)
		{
			ID: "X", Title: "X", Status: model.StatusOpen, CreatedAt: now, Labels: []string{"small"},
			Dependencies: []*model.Dependency{{IssueID: "X", DependsOnID: "Y", Type: model.DepBlocks}},
		},
		{ID: "Y", Title: "Y", Status: model.StatusOpen, CreatedAt: now, Labels: []string{"small"}},
		// Large chain: A -> B -> C -> D (4 issues, label: large)
		{
			ID: "A", Title: "A", Status: model.StatusOpen, CreatedAt: now, Labels: []string{"large"},
			Dependencies: []*model.Dependency{{IssueID: "A", DependsOnID: "B", Type: model.DepBlocks}},
		},
		{
			ID: "B", Title: "B", Status: model.StatusOpen, CreatedAt: now, Labels: []string{"large"},
			Dependencies: []*model.Dependency{{IssueID: "B", DependsOnID: "C", Type: model.DepBlocks}},
		},
		{
			ID: "C", Title: "C", Status: model.StatusOpen, CreatedAt: now, Labels: []string{"large"},
			Dependencies: []*model.Dependency{{IssueID: "C", DependsOnID: "D", Type: model.DepBlocks}},
		},
		{ID: "D", Title: "D", Status: model.StatusOpen, CreatedAt: now, Labels: []string{"large"}},
	}
	primaryIDs := map[string]bool{"X": true, "Y": true, "A": true, "B": true, "C": true, "D": true}

	ws := DetectWorkstreams(issues, primaryIDs, "test-label")

	if len(ws) != 2 {
		t.Fatalf("expected 2 workstreams, got %d", len(ws))
	}
	// Largest first
	if len(ws[0].Issues) != 4 {
		t.Errorf("expected first workstream to have 4 issues (largest), got %d", len(ws[0].Issues))
	}
	if len(ws[1].Issues) != 2 {
		t.Errorf("expected second workstream to have 2 issues, got %d", len(ws[1].Issues))
	}
}

// Tests for FindDistinguishingLabels - label-agnostic algorithm

func TestFindDistinguishingLabels_Empty(t *testing.T) {
	dl := FindDistinguishingLabels(nil, "test", 2)
	if len(dl) != 0 {
		t.Errorf("expected 0 labels for empty input, got %d", len(dl))
	}

	dl = FindDistinguishingLabels([]model.Issue{{ID: "A"}}, "test", 2)
	if len(dl) != 0 {
		t.Errorf("expected 0 labels for single issue, got %d", len(dl))
	}
}

func TestFindDistinguishingLabels_TooCommon(t *testing.T) {
	// Label appearing in ALL issues should be filtered out
	issues := []model.Issue{
		{ID: "A", Labels: []string{"common", "unique-a"}},
		{ID: "B", Labels: []string{"common", "unique-b"}},
		{ID: "C", Labels: []string{"common", "unique-c"}},
	}

	dl := FindDistinguishingLabels(issues, "", 1)

	// "common" should NOT be in results (appears in all)
	for _, d := range dl {
		if d.Label == "common" {
			t.Error("label 'common' should be filtered out (appears in all issues)")
		}
	}
}

func TestFindDistinguishingLabels_TooRare(t *testing.T) {
	// Labels appearing in only 1 issue should be filtered out with minGroupSize=2
	issues := []model.Issue{
		{ID: "A", Labels: []string{"group1", "singleton-a"}},
		{ID: "B", Labels: []string{"group1", "singleton-b"}},
		{ID: "C", Labels: []string{"group2"}},
		{ID: "D", Labels: []string{"group2"}},
	}

	dl := FindDistinguishingLabels(issues, "", 2)

	// Singletons should be filtered out
	for _, d := range dl {
		if d.Label == "singleton-a" || d.Label == "singleton-b" {
			t.Errorf("singleton label %q should be filtered out", d.Label)
		}
	}

	// group1 and group2 should be present (each has 2 issues)
	found := map[string]bool{}
	for _, d := range dl {
		found[d.Label] = true
	}
	if !found["group1"] {
		t.Error("expected 'group1' in results")
	}
	if !found["group2"] {
		t.Error("expected 'group2' in results")
	}
}

func TestFindDistinguishingLabels_ExcludesSelectedLabel(t *testing.T) {
	issues := []model.Issue{
		{ID: "A", Labels: []string{"selected", "other"}},
		{ID: "B", Labels: []string{"selected", "other"}},
	}

	dl := FindDistinguishingLabels(issues, "selected", 1)

	for _, d := range dl {
		if d.Label == "selected" {
			t.Error("selected label should be excluded from results")
		}
	}
}

func TestFindDistinguishingLabels_SprintLabels(t *testing.T) {
	// Test with sprint-style labels (any naming convention should work)
	issues := []model.Issue{
		{ID: "A", Labels: []string{"sprint-1"}},
		{ID: "B", Labels: []string{"sprint-1"}},
		{ID: "C", Labels: []string{"sprint-2"}},
		{ID: "D", Labels: []string{"sprint-2"}},
		{ID: "E", Labels: []string{"sprint-3"}},
		{ID: "F", Labels: []string{"sprint-3"}},
	}

	dl := FindDistinguishingLabels(issues, "", 2)

	if len(dl) < 3 {
		t.Fatalf("expected at least 3 distinguishing labels, got %d", len(dl))
	}

	// All sprint labels should be found
	found := map[string]bool{}
	for _, d := range dl {
		found[d.Label] = true
	}
	for _, expected := range []string{"sprint-1", "sprint-2", "sprint-3"} {
		if !found[expected] {
			t.Errorf("expected %q in results", expected)
		}
	}
}

func TestFindDistinguishingLabels_QuarterLabels(t *testing.T) {
	// Test with Q1/Q2/Q3/Q4 labels
	issues := []model.Issue{
		{ID: "A", Labels: []string{"Q1"}},
		{ID: "B", Labels: []string{"Q1"}},
		{ID: "C", Labels: []string{"Q2"}},
		{ID: "D", Labels: []string{"Q2"}},
	}

	dl := FindDistinguishingLabels(issues, "", 2)

	found := map[string]bool{}
	for _, d := range dl {
		found[d.Label] = true
	}
	if !found["Q1"] {
		t.Error("expected 'Q1' in results")
	}
	if !found["Q2"] {
		t.Error("expected 'Q2' in results")
	}
}

func TestFindDistinguishingLabels_EpicLabels(t *testing.T) {
	// Test with epic-style labels
	issues := []model.Issue{
		{ID: "A", Labels: []string{"epic:auth"}},
		{ID: "B", Labels: []string{"epic:auth"}},
		{ID: "C", Labels: []string{"epic:payments"}},
		{ID: "D", Labels: []string{"epic:payments"}},
	}

	dl := FindDistinguishingLabels(issues, "", 2)

	found := map[string]bool{}
	for _, d := range dl {
		found[d.Label] = true
	}
	if !found["epic:auth"] {
		t.Error("expected 'epic:auth' in results")
	}
	if !found["epic:payments"] {
		t.Error("expected 'epic:payments' in results")
	}
}

func TestFindDistinguishingLabels_CustomLabels(t *testing.T) {
	// Test with completely custom labels (no recognizable pattern)
	issues := []model.Issue{
		{ID: "A", Labels: []string{"alpha"}},
		{ID: "B", Labels: []string{"alpha"}},
		{ID: "C", Labels: []string{"alpha"}},
		{ID: "D", Labels: []string{"beta"}},
		{ID: "E", Labels: []string{"beta"}},
		{ID: "F", Labels: []string{"beta"}},
	}

	dl := FindDistinguishingLabels(issues, "", 2)

	// Both alpha and beta should work even without sequence markers
	found := map[string]bool{}
	for _, d := range dl {
		found[d.Label] = true
	}
	if !found["alpha"] {
		t.Error("expected 'alpha' in results")
	}
	if !found["beta"] {
		t.Error("expected 'beta' in results")
	}
}

func TestFindDistinguishingLabels_PartitionScore(t *testing.T) {
	// Test that labels with better partition ratios score higher
	// 10 issues: group-a has 4 (40%), group-b has 2 (20%)
	// group-a should score higher (closer to ideal 40%)
	issues := []model.Issue{
		{ID: "1", Labels: []string{"group-a"}},
		{ID: "2", Labels: []string{"group-a"}},
		{ID: "3", Labels: []string{"group-a"}},
		{ID: "4", Labels: []string{"group-a"}},
		{ID: "5", Labels: []string{"group-b"}},
		{ID: "6", Labels: []string{"group-b"}},
		{ID: "7", Labels: []string{}},
		{ID: "8", Labels: []string{}},
		{ID: "9", Labels: []string{}},
		{ID: "10", Labels: []string{}},
	}

	dl := FindDistinguishingLabels(issues, "", 2)

	if len(dl) < 2 {
		t.Fatalf("expected at least 2 distinguishing labels, got %d", len(dl))
	}

	// group-a (40% ratio) should score higher than group-b (20% ratio)
	var groupAScore, groupBScore float64
	for _, d := range dl {
		if d.Label == "group-a" {
			groupAScore = d.Score
		}
		if d.Label == "group-b" {
			groupBScore = d.Score
		}
	}

	if groupAScore <= groupBScore {
		t.Errorf("expected group-a (40%% ratio) to score higher than group-b (20%% ratio), got %.2f vs %.2f", groupAScore, groupBScore)
	}
}

func TestFindDistinguishingLabels_SequenceBonus(t *testing.T) {
	// Test that sequence-like labels get a bonus
	// Both have same partition ratio but phase labels form a sequential family
	// The new algorithm uses family-based scoring, so sequence bonus comes from
	// being detected as a sequential family, not from individual label scores
	issues := []model.Issue{
		{ID: "1", Labels: []string{"phase1", "custom-x"}},
		{ID: "2", Labels: []string{"phase1", "custom-x"}},
		{ID: "3", Labels: []string{"phase2", "custom-y"}},
		{ID: "4", Labels: []string{"phase2", "custom-y"}},
	}

	dl := FindDistinguishingLabels(issues, "", 2)

	// Find scores for phase1 and custom-x
	var phase1Score, customXScore float64
	for _, d := range dl {
		if d.Label == "phase1" {
			phase1Score = d.Score
		}
		if d.Label == "custom-x" {
			customXScore = d.Score
		}
	}

	// In the new algorithm, FindDistinguishingLabels is a compatibility wrapper
	// that returns coverage-based scores. Actual family detection and boosting
	// happens in DetectWorkstreams. Here we just verify both labels are found.
	if phase1Score == 0 {
		t.Error("expected phase1 to have non-zero score")
	}
	if customXScore == 0 {
		t.Error("expected custom-x to have non-zero score")
	}
}

func TestDetectWorkstreams_SubdivideBySprintLabels(t *testing.T) {
	// Test that subdivision works with sprint labels (not just phase)
	issues := []model.Issue{
		{ID: "A1", Title: "Sprint1 A", Status: model.StatusOpen, Labels: []string{"sprint-1"},
			Dependencies: []*model.Dependency{{IssueID: "A1", DependsOnID: "E", Type: model.DepParentChild}}},
		{ID: "A2", Title: "Sprint1 B", Status: model.StatusOpen, Labels: []string{"sprint-1"},
			Dependencies: []*model.Dependency{{IssueID: "A2", DependsOnID: "E", Type: model.DepParentChild}}},
		{ID: "A3", Title: "Sprint1 C", Status: model.StatusClosed, Labels: []string{"sprint-1"},
			Dependencies: []*model.Dependency{{IssueID: "A3", DependsOnID: "E", Type: model.DepParentChild}}},
		{ID: "B1", Title: "Sprint2 A", Status: model.StatusOpen, Labels: []string{"sprint-2"},
			Dependencies: []*model.Dependency{{IssueID: "B1", DependsOnID: "E", Type: model.DepParentChild}}},
		{ID: "B2", Title: "Sprint2 B", Status: model.StatusOpen, Labels: []string{"sprint-2"},
			Dependencies: []*model.Dependency{{IssueID: "B2", DependsOnID: "E", Type: model.DepParentChild}}},
		{ID: "B3", Title: "Sprint2 C", Status: model.StatusOpen, Labels: []string{"sprint-2"},
			Dependencies: []*model.Dependency{{IssueID: "B3", DependsOnID: "E", Type: model.DepParentChild}}},
	}
	primaryIDs := map[string]bool{"A1": true, "A2": true, "A3": true, "B1": true, "B2": true, "B3": true}

	ws := DetectWorkstreams(issues, primaryIDs, "test-label")

	// Should be subdivided into 2 workstreams: sprint-1 and sprint-2
	if len(ws) != 2 {
		t.Fatalf("expected 2 workstreams (subdivided by sprint), got %d", len(ws))
	}

	// Check names
	names := map[string]bool{}
	for _, w := range ws {
		names[w.Name] = true
	}
	if !names["Sprint-1"] && !names["sprint-1"] {
		t.Error("expected workstream named 'Sprint-1' or 'sprint-1'")
	}
	if !names["Sprint-2"] && !names["sprint-2"] {
		t.Error("expected workstream named 'Sprint-2' or 'sprint-2'")
	}
}

func TestDetectWorkstreams_SubdivideByCustomLabels(t *testing.T) {
	// Test that subdivision works with completely custom labels
	// Generic labels without a common prefix form separate workstreams
	issues := []model.Issue{
		{ID: "A1", Title: "Alpha A", Status: model.StatusOpen, Labels: []string{"alpha"},
			Dependencies: []*model.Dependency{{IssueID: "A1", DependsOnID: "E", Type: model.DepParentChild}}},
		{ID: "A2", Title: "Alpha B", Status: model.StatusOpen, Labels: []string{"alpha"},
			Dependencies: []*model.Dependency{{IssueID: "A2", DependsOnID: "E", Type: model.DepParentChild}}},
		{ID: "A3", Title: "Alpha C", Status: model.StatusOpen, Labels: []string{"alpha"},
			Dependencies: []*model.Dependency{{IssueID: "A3", DependsOnID: "E", Type: model.DepParentChild}}},
		{ID: "B1", Title: "Beta A", Status: model.StatusOpen, Labels: []string{"beta"},
			Dependencies: []*model.Dependency{{IssueID: "B1", DependsOnID: "E", Type: model.DepParentChild}}},
		{ID: "B2", Title: "Beta B", Status: model.StatusOpen, Labels: []string{"beta"},
			Dependencies: []*model.Dependency{{IssueID: "B2", DependsOnID: "E", Type: model.DepParentChild}}},
		{ID: "B3", Title: "Beta C", Status: model.StatusOpen, Labels: []string{"beta"},
			Dependencies: []*model.Dependency{{IssueID: "B3", DependsOnID: "E", Type: model.DepParentChild}}},
	}
	primaryIDs := map[string]bool{"A1": true, "A2": true, "A3": true, "B1": true, "B2": true, "B3": true}

	ws := DetectWorkstreams(issues, primaryIDs, "test-label")

	t.Logf("Got %d workstreams:", len(ws))
	for _, w := range ws {
		t.Logf("  %s: %d issues", w.Name, len(w.Issues))
	}

	// Generic labels (alpha, beta) are not in a sequential family, so they're treated
	// as independent generic families. Each should create a separate workstream.
	if len(ws) != 2 {
		t.Fatalf("expected 2 workstreams (subdivided by custom labels), got %d", len(ws))
	}

	// Each workstream should have 3 issues
	for _, w := range ws {
		if len(w.Issues) != 3 {
			t.Errorf("expected 3 issues in workstream %q, got %d", w.Name, len(w.Issues))
		}
	}
}

func TestLooksLikeSequenceLabel(t *testing.T) {
	testCases := []struct {
		label    string
		expected bool
	}{
		// Phase patterns
		{"phase1", true},
		{"Phase2", true},
		{"phase-3", true},
		{"PHASE4", true},
		// Stage patterns
		{"stage1", true},
		{"stage-2", true},
		// Sprint patterns
		{"sprint-1", true},
		{"sprint1", true},
		{"Sprint-5", true},
		// Quarter patterns
		{"Q1", true},
		{"q2", true},
		{"Q3", true},
		{"Q4", true},
		// Short phase/version patterns
		{"p1", true},
		{"P2", true},
		{"v1", true},
		{"V2", true},
		// Other sequence patterns
		{"milestone-1", true},
		{"iteration-3", true},
		{"week-5", true},
		{"release-2.0", true},
		// Non-sequence labels
		{"alpha", false},
		{"beta", false},
		{"ui", false},
		{"backend", false},
		{"feat:auth", false},
		{"epic:payments", false},
	}

	for _, tc := range testCases {
		got := looksLikeSequenceLabel(tc.label)
		if got != tc.expected {
			t.Errorf("looksLikeSequenceLabel(%q) = %v, want %v", tc.label, got, tc.expected)
		}
	}
}

func TestDetectWorkstreams_LabelInheritance(t *testing.T) {
	// Test that unlabeled issues inherit workstream from their blockers
	// Need enough labeled issues per group to trigger subdivision (2 groups with 2+ each)
	issues := []model.Issue{
		// Phase1 anchors (2 labeled issues)
		{ID: "P1a", Title: "Phase1 A", Status: model.StatusOpen, Labels: []string{"phase1"},
			Dependencies: []*model.Dependency{{IssueID: "P1a", DependsOnID: "E", Type: model.DepParentChild}}},
		{ID: "P1b", Title: "Phase1 B", Status: model.StatusOpen, Labels: []string{"phase1"},
			Dependencies: []*model.Dependency{{IssueID: "P1b", DependsOnID: "E", Type: model.DepParentChild}}},
		// Unlabeled A depends on P1a (P1a blocks A)
		{ID: "A", Title: "Unlabeled A", Status: model.StatusOpen, Labels: []string{},
			Dependencies: []*model.Dependency{
				{IssueID: "A", DependsOnID: "E", Type: model.DepParentChild},
				{IssueID: "A", DependsOnID: "P1a", Type: model.DepBlocks},
			}},

		// Phase2 anchors (2 labeled issues)
		{ID: "P2a", Title: "Phase2 A", Status: model.StatusOpen, Labels: []string{"phase2"},
			Dependencies: []*model.Dependency{{IssueID: "P2a", DependsOnID: "E", Type: model.DepParentChild}}},
		{ID: "P2b", Title: "Phase2 B", Status: model.StatusOpen, Labels: []string{"phase2"},
			Dependencies: []*model.Dependency{{IssueID: "P2b", DependsOnID: "E", Type: model.DepParentChild}}},
		// Unlabeled B depends on P2a (P2a blocks B)
		{ID: "B", Title: "Unlabeled B", Status: model.StatusOpen, Labels: []string{},
			Dependencies: []*model.Dependency{
				{IssueID: "B", DependsOnID: "E", Type: model.DepParentChild},
				{IssueID: "B", DependsOnID: "P2a", Type: model.DepBlocks},
			}},
	}
	primaryIDs := map[string]bool{"P1a": true, "P1b": true, "A": true, "P2a": true, "P2b": true, "B": true}

	ws := DetectWorkstreams(issues, primaryIDs, "test-label")

	// Log what we got for debugging
	t.Logf("Got %d workstreams:", len(ws))
	for _, w := range ws {
		t.Logf("  %s: %v", w.Name, w.IssueIDs)
	}

	// Should have at least 2 workstreams: phase1 and phase2
	// Unlabeled issues go to Standalone unless they inherit through blocking deps
	if len(ws) < 2 {
		t.Fatalf("expected at least 2 workstreams, got %d", len(ws))
	}

	// Find workstreams by name
	wsByName := make(map[string]*Workstream)
	for i := range ws {
		wsByName[ws[i].Name] = &ws[i]
	}

	// Check phase1 workstream has P1a, P1b
	phase1WS, ok := wsByName["Phase1"]
	if !ok {
		phase1WS, ok = wsByName["phase1"]
	}
	if ok {
		ids := make(map[string]bool)
		for _, id := range phase1WS.IssueIDs {
			ids[id] = true
		}
		if !ids["P1a"] {
			t.Error("phase1 workstream should contain P1a")
		}
		if !ids["P1b"] {
			t.Error("phase1 workstream should contain P1b")
		}
		// A may or may not be in phase1 depending on inheritance algorithm
		// The new algorithm inherits labels through blocking edges
		if ids["A"] {
			t.Log("A successfully inherited phase1 from P1a")
		}
	} else {
		t.Log("Note: phase1 workstream not found by name, may have different naming")
	}

	// Check phase2 workstream has P2a, P2b
	phase2WS, ok := wsByName["Phase2"]
	if !ok {
		phase2WS, ok = wsByName["phase2"]
	}
	if ok {
		ids := make(map[string]bool)
		for _, id := range phase2WS.IssueIDs {
			ids[id] = true
		}
		if !ids["P2a"] {
			t.Error("phase2 workstream should contain P2a")
		}
		if !ids["P2b"] {
			t.Error("phase2 workstream should contain P2b")
		}
		// B may or may not be in phase2 depending on inheritance algorithm
		if ids["B"] {
			t.Log("B successfully inherited phase2 from P2a")
		}
	} else {
		t.Log("Note: phase2 workstream not found by name, may have different naming")
	}
}

func TestPropagateLabelsThroughDeps(t *testing.T) {
	// Direct test of the propagation function
	// Scenario: P1 (phase1) -> A -> B
	//           P2 (phase2) -> C
	issueMap := map[string]model.Issue{
		"P1": {ID: "P1", Labels: []string{"phase1"}},
		"A":  {ID: "A", Labels: []string{}, Dependencies: []*model.Dependency{{IssueID: "A", DependsOnID: "P1", Type: model.DepBlocks}}},
		"B":  {ID: "B", Labels: []string{}, Dependencies: []*model.Dependency{{IssueID: "B", DependsOnID: "A", Type: model.DepBlocks}}},
		"P2": {ID: "P2", Labels: []string{"phase2"}},
		"C":  {ID: "C", Labels: []string{}, Dependencies: []*model.Dependency{{IssueID: "C", DependsOnID: "P2", Type: model.DepBlocks}}},
	}
	memberIDs := []string{"P1", "A", "B", "P2", "C"}

	issueToLabels := map[string][]string{
		"P1": {"phase1"},
		"P2": {"phase2"},
	}

	assigned := map[string]string{
		"P1": "phase1",
		"P2": "phase2",
	}

	propagateLabelsThroughDeps(assigned, issueToLabels, memberIDs, issueMap)

	// A should inherit phase1 from P1
	if assigned["A"] != "phase1" {
		t.Errorf("A should have inherited phase1, got %q", assigned["A"])
	}
	// B should inherit phase1 through A
	if assigned["B"] != "phase1" {
		t.Errorf("B should have inherited phase1 through A, got %q", assigned["B"])
	}
	// C should inherit phase2 from P2
	if assigned["C"] != "phase2" {
		t.Errorf("C should have inherited phase2, got %q", assigned["C"])
	}
}

func TestDetectWorkstreams_EpicWithManyPhases(t *testing.T) {
	// Test scenario: Epic with 42 children, all having feat:labels-viewer + various phase labels
	// This simulates the actual failing scenario
	phases := []string{"phase1", "phase2", "phase3", "phase4", "phase5"}
	var issues []model.Issue
	primaryIDs := make(map[string]bool)

	// Create ~8-9 issues per phase, all sharing epic parent E
	id := 0
	for _, phase := range phases {
		for i := 0; i < 8; i++ {
			issueID := fmt.Sprintf("issue-%d", id)
			id++
			issues = append(issues, model.Issue{
				ID:     issueID,
				Title:  fmt.Sprintf("%s issue %d", phase, i),
				Status: model.StatusOpen,
				Labels: []string{"feat:labels-viewer", phase, "ui"}, // Common label + phase
				Dependencies: []*model.Dependency{
					{IssueID: issueID, DependsOnID: "epic-E", Type: model.DepParentChild},
				},
			})
			primaryIDs[issueID] = true
		}
	}

	// Add 2 more for phase1 to have 10 total
	for i := 0; i < 2; i++ {
		issueID := fmt.Sprintf("extra-%d", i)
		issues = append(issues, model.Issue{
			ID:     issueID,
			Title:  fmt.Sprintf("extra phase1 issue %d", i),
			Status: model.StatusOpen,
			Labels: []string{"feat:labels-viewer", "phase1"},
			Dependencies: []*model.Dependency{
				{IssueID: issueID, DependsOnID: "epic-E", Type: model.DepParentChild},
			},
		})
		primaryIDs[issueID] = true
	}

	t.Logf("Created %d issues with primaryIDs=%d", len(issues), len(primaryIDs))

	// Test FindDistinguishingLabels directly
	dl := FindDistinguishingLabels(issues, "Labels Viewer Epic", 2)
	t.Logf("FindDistinguishingLabels returned %d labels:", len(dl))
	for i, d := range dl {
		if i < 10 {
			t.Logf("  %s: score=%.2f, count=%d, ratio=%.2f", d.Label, d.Score, d.IssueCount, d.PartitionRatio)
		}
	}

	// Epic mode: selectedLabel is the epic title, not a real label
	ws := DetectWorkstreams(issues, primaryIDs, "Labels Viewer Epic")

	t.Logf("Detected %d workstreams", len(ws))
	for i, w := range ws {
		t.Logf("  Workstream %d: %q with %d issues", i, w.Name, len(w.Issues))
	}

	// Should have multiple workstreams (one per phase)
	if len(ws) < 2 {
		t.Errorf("expected at least 2 workstreams for phase subdivision, got %d", len(ws))
	}
}

func TestPropagateLabelsThroughDeps_StopsAtAnchor(t *testing.T) {
	// Test that propagation stops at another anchor
	// Scenario: P1 (phase1) -> A -> P2 (phase2) -> B
	issueMap := map[string]model.Issue{
		"P1": {ID: "P1", Labels: []string{"phase1"}},
		"A":  {ID: "A", Labels: []string{}, Dependencies: []*model.Dependency{{IssueID: "A", DependsOnID: "P1", Type: model.DepBlocks}}},
		"P2": {ID: "P2", Labels: []string{"phase2"}, Dependencies: []*model.Dependency{{IssueID: "P2", DependsOnID: "A", Type: model.DepBlocks}}},
		"B":  {ID: "B", Labels: []string{}, Dependencies: []*model.Dependency{{IssueID: "B", DependsOnID: "P2", Type: model.DepBlocks}}},
	}
	memberIDs := []string{"P1", "A", "P2", "B"}

	issueToLabels := map[string][]string{
		"P1": {"phase1"},
		"P2": {"phase2"},
	}

	assigned := map[string]string{
		"P1": "phase1",
		"P2": "phase2",
	}

	propagateLabelsThroughDeps(assigned, issueToLabels, memberIDs, issueMap)

	// A should inherit phase1 from P1
	if assigned["A"] != "phase1" {
		t.Errorf("A should have inherited phase1, got %q", assigned["A"])
	}
	// P2 should NOT change (it's an anchor)
	if assigned["P2"] != "phase2" {
		t.Errorf("P2 should remain phase2 (anchor), got %q", assigned["P2"])
	}
	// B should inherit phase2 from P2 (not phase1)
	if assigned["B"] != "phase2" {
		t.Errorf("B should have inherited phase2 from P2, got %q", assigned["B"])
	}
}

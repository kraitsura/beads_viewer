package analysis

import (
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

func TestHealthLevelFromScore(t *testing.T) {
	tests := []struct {
		score    int
		expected string
	}{
		{100, HealthLevelHealthy},
		{70, HealthLevelHealthy},
		{69, HealthLevelWarning},
		{40, HealthLevelWarning},
		{39, HealthLevelCritical},
		{0, HealthLevelCritical},
	}

	for _, tt := range tests {
		result := HealthLevelFromScore(tt.score)
		if result != tt.expected {
			t.Errorf("HealthLevelFromScore(%d) = %s, want %s", tt.score, result, tt.expected)
		}
	}
}

func TestComputeCompositeHealth(t *testing.T) {
	cfg := DefaultLabelHealthConfig()

	// All components at 100 should give 100
	score := ComputeCompositeHealth(100, 100, 100, 100, cfg)
	if score != 100 {
		t.Errorf("All 100s should give 100, got %d", score)
	}

	// All components at 0 should give 0
	score = ComputeCompositeHealth(0, 0, 0, 0, cfg)
	if score != 0 {
		t.Errorf("All 0s should give 0, got %d", score)
	}

	// All components at 50 should give 50
	score = ComputeCompositeHealth(50, 50, 50, 50, cfg)
	if score != 50 {
		t.Errorf("All 50s should give 50, got %d", score)
	}

	// Test weighted average
	// velocity=100, freshness=0, flow=100, criticality=0
	// With equal weights: (100*0.25 + 0*0.25 + 100*0.25 + 0*0.25) = 50
	score = ComputeCompositeHealth(100, 0, 100, 0, cfg)
	if score != 50 {
		t.Errorf("Expected 50 for alternating 100/0, got %d", score)
	}
}

func TestDefaultLabelHealthConfig(t *testing.T) {
	cfg := DefaultLabelHealthConfig()

	// Check weights sum to 1.0
	totalWeight := cfg.VelocityWeight + cfg.FreshnessWeight + cfg.FlowWeight + cfg.CriticalityWeight
	if totalWeight != 1.0 {
		t.Errorf("Weights should sum to 1.0, got %f", totalWeight)
	}

	// Check reasonable defaults
	if cfg.StaleThresholdDays != 14 {
		t.Errorf("Expected stale threshold of 14 days, got %d", cfg.StaleThresholdDays)
	}

	if cfg.MinIssuesForHealth != 1 {
		t.Errorf("Expected min issues of 1, got %d", cfg.MinIssuesForHealth)
	}
}

func TestNewLabelHealth(t *testing.T) {
	health := NewLabelHealth("test-label")

	if health.Label != "test-label" {
		t.Errorf("Expected label 'test-label', got '%s'", health.Label)
	}

	if health.Health != 100 {
		t.Errorf("New label should start with health 100, got %d", health.Health)
	}

	if health.HealthLevel != HealthLevelHealthy {
		t.Errorf("New label should be healthy, got %s", health.HealthLevel)
	}

	if health.Velocity.TrendDirection != "stable" {
		t.Errorf("Expected stable trend, got %s", health.Velocity.TrendDirection)
	}

	if health.Freshness.StaleThresholdDays != DefaultStaleThresholdDays {
		t.Errorf("Expected stale threshold %d, got %d", DefaultStaleThresholdDays, health.Freshness.StaleThresholdDays)
	}
}

func TestNeedsAttention(t *testing.T) {
	healthyLabel := LabelHealth{Health: 80}
	warningLabel := LabelHealth{Health: 50}
	criticalLabel := LabelHealth{Health: 30}

	if NeedsAttention(healthyLabel) {
		t.Error("Healthy label (80) should not need attention")
	}

	if !NeedsAttention(warningLabel) {
		t.Error("Warning label (50) should need attention")
	}

	if !NeedsAttention(criticalLabel) {
		t.Error("Critical label (30) should need attention")
	}
}

func TestLabelHealthTypes(t *testing.T) {
	// Test that all types can be instantiated and have expected structure
	velocity := VelocityMetrics{
		ClosedLast7Days:  5,
		ClosedLast30Days: 20,
		AvgDaysToClose:   3.5,
		TrendDirection:   "improving",
		TrendPercent:     15.0,
		VelocityScore:    80,
	}

	if velocity.ClosedLast7Days != 5 {
		t.Errorf("VelocityMetrics field mismatch")
	}

	freshness := FreshnessMetrics{
		AvgDaysSinceUpdate: 5.5,
		StaleCount:         2,
		StaleThresholdDays: 14,
		FreshnessScore:     70,
	}

	if freshness.StaleCount != 2 {
		t.Errorf("FreshnessMetrics field mismatch")
	}

	flow := FlowMetrics{
		IncomingDeps:      3,
		OutgoingDeps:      2,
		IncomingLabels:    []string{"api", "core"},
		OutgoingLabels:    []string{"ui"},
		BlockedByExternal: 1,
		BlockingExternal:  1,
		FlowScore:         85,
	}

	if len(flow.IncomingLabels) != 2 {
		t.Errorf("FlowMetrics labels mismatch")
	}

	criticality := CriticalityMetrics{
		AvgPageRank:       0.05,
		AvgBetweenness:    0.15,
		MaxBetweenness:    0.35,
		CriticalPathCount: 3,
		BottleneckCount:   1,
		CriticalityScore:  75,
	}

	if criticality.BottleneckCount != 1 {
		t.Errorf("CriticalityMetrics field mismatch")
	}
}

func TestCrossLabelFlowTypes(t *testing.T) {
	dep := LabelDependency{
		FromLabel:  "api",
		ToLabel:    "ui",
		IssueCount: 3,
		IssueIDs:   []string{"bv-1", "bv-2", "bv-3"},
		BlockingPairs: []BlockingPair{
			{BlockerID: "bv-1", BlockedID: "bv-4", BlockerLabel: "api", BlockedLabel: "ui"},
		},
	}

	if dep.FromLabel != "api" {
		t.Errorf("LabelDependency FromLabel mismatch")
	}

	if len(dep.BlockingPairs) != 1 {
		t.Errorf("Expected 1 blocking pair, got %d", len(dep.BlockingPairs))
	}

	flow := CrossLabelFlow{
		Labels:             []string{"api", "ui", "core"},
		FlowMatrix:         [][]int{{0, 3, 1}, {0, 0, 2}, {0, 0, 0}},
		Dependencies:       []LabelDependency{dep},
		BottleneckLabels:   []string{"api"},
		TotalCrossLabelDeps: 6,
	}

	if len(flow.Labels) != 3 {
		t.Errorf("Expected 3 labels, got %d", len(flow.Labels))
	}

	if flow.TotalCrossLabelDeps != 6 {
		t.Errorf("Expected 6 cross-label deps, got %d", flow.TotalCrossLabelDeps)
	}
}

func TestLabelPath(t *testing.T) {
	path := LabelPath{
		Labels:      []string{"core", "api", "ui"},
		Length:      2,
		IssueCount:  5,
		TotalWeight: 12.5,
	}

	if path.Length != 2 {
		t.Errorf("Expected length 2, got %d", path.Length)
	}

	if len(path.Labels) != 3 {
		t.Errorf("Expected 3 labels in path, got %d", len(path.Labels))
	}
}

func TestLabelAnalysisResult(t *testing.T) {
	result := LabelAnalysisResult{
		TotalLabels:     5,
		HealthyCount:    3,
		WarningCount:    1,
		CriticalCount:   1,
		AttentionNeeded: []string{"blocked-label", "stale-label"},
	}

	if result.TotalLabels != 5 {
		t.Errorf("Expected 5 total labels, got %d", result.TotalLabels)
	}

	total := result.HealthyCount + result.WarningCount + result.CriticalCount
	if total != result.TotalLabels {
		t.Errorf("Health counts (%d) don't sum to total (%d)", total, result.TotalLabels)
	}

	if len(result.AttentionNeeded) != 2 {
		t.Errorf("Expected 2 labels needing attention, got %d", len(result.AttentionNeeded))
	}
}

// ============================================================================
// Label Extraction Tests (bv-101)
// ============================================================================

func TestExtractLabelsEmpty(t *testing.T) {
	result := ExtractLabels([]model.Issue{})

	if result.LabelCount != 0 {
		t.Errorf("Expected 0 labels for empty input, got %d", result.LabelCount)
	}
	if result.IssueCount != 0 {
		t.Errorf("Expected 0 issues for empty input, got %d", result.IssueCount)
	}
	if len(result.Stats) != 0 {
		t.Errorf("Expected empty stats map, got %d entries", len(result.Stats))
	}
}

func TestExtractLabelsBasic(t *testing.T) {
	issues := []model.Issue{
		{ID: "bv-1", Labels: []string{"api", "bug"}, Status: model.StatusOpen, Priority: 1},
		{ID: "bv-2", Labels: []string{"api", "feature"}, Status: model.StatusClosed, Priority: 2},
		{ID: "bv-3", Labels: []string{"ui"}, Status: model.StatusInProgress, Priority: 1},
		{ID: "bv-4", Labels: []string{}, Status: model.StatusOpen, Priority: 3}, // No labels
	}

	result := ExtractLabels(issues)

	// Check counts
	if result.IssueCount != 4 {
		t.Errorf("Expected 4 issues, got %d", result.IssueCount)
	}
	if result.UnlabeledCount != 1 {
		t.Errorf("Expected 1 unlabeled issue, got %d", result.UnlabeledCount)
	}
	if result.LabelCount != 4 {
		t.Errorf("Expected 4 unique labels, got %d", result.LabelCount)
	}

	// Check labels are sorted
	expectedLabels := []string{"api", "bug", "feature", "ui"}
	for i, label := range expectedLabels {
		if result.Labels[i] != label {
			t.Errorf("Label %d: expected %s, got %s", i, label, result.Labels[i])
		}
	}

	// Check api label stats
	apiStats := result.Stats["api"]
	if apiStats == nil {
		t.Fatal("api label stats missing")
	}
	if apiStats.TotalCount != 2 {
		t.Errorf("api: expected 2 total, got %d", apiStats.TotalCount)
	}
	if apiStats.OpenCount != 1 {
		t.Errorf("api: expected 1 open, got %d", apiStats.OpenCount)
	}
	if apiStats.ClosedCount != 1 {
		t.Errorf("api: expected 1 closed, got %d", apiStats.ClosedCount)
	}

	// Check ui label stats
	uiStats := result.Stats["ui"]
	if uiStats == nil {
		t.Fatal("ui label stats missing")
	}
	if uiStats.InProgress != 1 {
		t.Errorf("ui: expected 1 in_progress, got %d", uiStats.InProgress)
	}

	// Check top labels (should be api first with 2 issues)
	if len(result.TopLabels) < 1 || result.TopLabels[0] != "api" {
		t.Errorf("Expected api as top label, got %v", result.TopLabels)
	}
}

func TestExtractLabelsDuplicateLabelsOnIssue(t *testing.T) {
	// Edge case: same label appears twice on an issue (shouldn't happen, but handle gracefully)
	issues := []model.Issue{
		{ID: "bv-1", Labels: []string{"api", "api"}, Status: model.StatusOpen}, // Duplicate
	}

	result := ExtractLabels(issues)

	// Both occurrences should be counted (total reflects raw label count per issue)
	if result.LabelCount != 1 {
		t.Errorf("Expected 1 unique label, got %d", result.LabelCount)
	}

	apiStats := result.Stats["api"]
	if apiStats.TotalCount != 2 {
		t.Errorf("Expected 2 counts for duplicate label, got %d", apiStats.TotalCount)
	}
}

func TestExtractLabelsEmptyLabelString(t *testing.T) {
	// Edge case: empty string label (should be skipped)
	issues := []model.Issue{
		{ID: "bv-1", Labels: []string{"", "api", ""}, Status: model.StatusOpen},
	}

	result := ExtractLabels(issues)

	if result.LabelCount != 1 {
		t.Errorf("Expected 1 label (empty strings skipped), got %d", result.LabelCount)
	}
	if result.Labels[0] != "api" {
		t.Errorf("Expected api label, got %s", result.Labels[0])
	}
}

func TestGetLabelIssues(t *testing.T) {
	issues := []model.Issue{
		{ID: "bv-1", Labels: []string{"api", "bug"}},
		{ID: "bv-2", Labels: []string{"api"}},
		{ID: "bv-3", Labels: []string{"ui"}},
	}

	apiIssues := GetLabelIssues(issues, "api")
	if len(apiIssues) != 2 {
		t.Errorf("Expected 2 api issues, got %d", len(apiIssues))
	}

	uiIssues := GetLabelIssues(issues, "ui")
	if len(uiIssues) != 1 {
		t.Errorf("Expected 1 ui issue, got %d", len(uiIssues))
	}

	noIssues := GetLabelIssues(issues, "nonexistent")
	if len(noIssues) != 0 {
		t.Errorf("Expected 0 issues for nonexistent label, got %d", len(noIssues))
	}
}

func TestGetLabelsForIssue(t *testing.T) {
	issues := []model.Issue{
		{ID: "bv-1", Labels: []string{"api", "bug"}},
		{ID: "bv-2", Labels: []string{"ui"}},
	}

	labels := GetLabelsForIssue(issues, "bv-1")
	if len(labels) != 2 {
		t.Errorf("Expected 2 labels for bv-1, got %d", len(labels))
	}

	labels = GetLabelsForIssue(issues, "bv-999")
	if labels != nil {
		t.Errorf("Expected nil for nonexistent issue, got %v", labels)
	}
}

func TestGetCommonLabels(t *testing.T) {
	set1 := []string{"api", "bug", "feature"}
	set2 := []string{"api", "feature", "ui"}
	set3 := []string{"api", "core"}

	// Common to all three: only "api"
	common := GetCommonLabels(set1, set2, set3)
	if len(common) != 1 || common[0] != "api" {
		t.Errorf("Expected [api], got %v", common)
	}

	// Common to two: "api" and "feature"
	common = GetCommonLabels(set1, set2)
	if len(common) != 2 {
		t.Errorf("Expected 2 common labels, got %d", len(common))
	}

	// Empty input
	common = GetCommonLabels()
	if common != nil {
		t.Errorf("Expected nil for empty input, got %v", common)
	}
}

func TestGetLabelCooccurrence(t *testing.T) {
	issues := []model.Issue{
		{ID: "bv-1", Labels: []string{"api", "bug"}},    // api+bug
		{ID: "bv-2", Labels: []string{"api", "bug"}},    // api+bug again
		{ID: "bv-3", Labels: []string{"api", "feature"}}, // api+feature
		{ID: "bv-4", Labels: []string{"ui"}},             // single label, no co-occurrence
	}

	cooc := GetLabelCooccurrence(issues)

	// api+bug should appear twice
	if cooc["api"]["bug"] != 2 {
		t.Errorf("Expected api+bug co-occurrence of 2, got %d", cooc["api"]["bug"])
	}
	if cooc["bug"]["api"] != 2 {
		t.Errorf("Expected bug+api co-occurrence of 2, got %d", cooc["bug"]["api"])
	}

	// api+feature should appear once
	if cooc["api"]["feature"] != 1 {
		t.Errorf("Expected api+feature co-occurrence of 1, got %d", cooc["api"]["feature"])
	}

	// ui has no co-occurrences
	if len(cooc["ui"]) != 0 {
		t.Errorf("Expected no co-occurrences for ui, got %v", cooc["ui"])
	}
}

func TestSortLabelsByCount(t *testing.T) {
	stats := map[string]*LabelStats{
		"api":     {Label: "api", TotalCount: 10},
		"bug":     {Label: "bug", TotalCount: 5},
		"feature": {Label: "feature", TotalCount: 10}, // Same as api
		"ui":      {Label: "ui", TotalCount: 3},
	}

	sorted := sortLabelsByCount(stats)

	// Should be sorted by count descending, then alphabetically for ties
	expected := []string{"api", "feature", "bug", "ui"}
	for i, label := range expected {
		if sorted[i] != label {
			t.Errorf("Position %d: expected %s, got %s", i, label, sorted[i])
		}
	}
}

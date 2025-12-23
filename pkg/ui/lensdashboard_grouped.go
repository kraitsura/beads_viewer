package ui

import (
	"sort"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

// ══════════════════════════════════════════════════════════════════════════════
// GROUPED/WORKSTREAM VIEW - Building grouped and workstream structures
// ══════════════════════════════════════════════════════════════════════════════

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

// buildGroupedByStatus groups issues by computed status (considers implicit blocking)
func (m *LensDashboardModel) buildGroupedByStatus() []analysis.Workstream {
	statusNames := map[string]string{
		"open":        "Open",
		"in_progress": "In Progress",
		"blocked":     "Blocked",
		"closed":      "Closed",
	}
	statusOrder := []string{"open", "in_progress", "blocked", "closed"}
	groups := make(map[string][]model.Issue)

	for _, issue := range m.allIssues {
		if !m.primaryIDs[issue.ID] {
			continue
		}
		// Use computed status which checks blockedByMap for implicit blocking
		computedStatus := m.getIssueStatus(issue)
		groups[computedStatus] = append(groups[computedStatus], issue)
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

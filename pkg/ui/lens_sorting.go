package ui

import (
	"sort"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

// StatusOrderFunc returns a sort order for an issue based on its status.
// Lower values sort first.
type StatusOrderFunc func(model.Issue) int

// SortIssuesByStatusThenPriority sorts issues by status order (using the provided
// function) then by priority. This is the most common sorting pattern used
// throughout the lens dashboard ecosystem.
func SortIssuesByStatusThenPriority(issues []model.Issue, statusOrder StatusOrderFunc) {
	sort.Slice(issues, func(i, j int) bool {
		si := statusOrder(issues[i])
		sj := statusOrder(issues[j])
		if si != sj {
			return si < sj
		}
		return issues[i].Priority < issues[j].Priority
	})
}

// SortIssuesByPriorityOnly sorts issues by priority ascending (P0 first).
func SortIssuesByPriorityOnly(issues []model.Issue) {
	sort.Slice(issues, func(i, j int) bool {
		return issues[i].Priority < issues[j].Priority
	})
}

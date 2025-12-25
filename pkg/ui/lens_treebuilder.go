package ui

import (
	"sort"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

// BFSQueueItem represents an item in BFS traversal with depth tracking.
// Used by descendant map builders and tree traversal algorithms.
type BFSQueueItem struct {
	ID    string
	Depth int
}

// LabelCount associates a label with its count for sorting by popularity.
type LabelCount struct {
	Label string
	Count int
}

// SortLabelCountsDescending sorts label counts by count (descending),
// then alphabetically by label name for stable ordering.
func SortLabelCountsDescending(counts []LabelCount) {
	sort.Slice(counts, func(i, j int) bool {
		if counts[i].Count != counts[j].Count {
			return counts[i].Count > counts[j].Count
		}
		return counts[i].Label < counts[j].Label
	})
}

// BuildChildrenMap creates a parent -> children mapping from issues
// based on parent-child dependencies. This is used to efficiently count
// epic children without rebuilding the map for each epic.
func BuildChildrenMap(issues []model.Issue) map[string][]string {
	children := make(map[string][]string)
	for _, issue := range issues {
		for _, dep := range issue.Dependencies {
			if dep.Type == model.DepParentChild {
				children[dep.DependsOnID] = append(children[dep.DependsOnID], issue.ID)
			}
		}
	}
	return children
}

// BuildStatusMap creates an issue ID -> status mapping for efficient lookups.
func BuildStatusMap(issues []model.Issue) map[string]model.Status {
	status := make(map[string]model.Status, len(issues))
	for _, issue := range issues {
		status[issue.ID] = issue.Status
	}
	return status
}

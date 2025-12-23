package ui

import (
	"sort"
	"strconv"
	"strings"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

// CompareHierarchicalIDs compares two issue IDs that may have hierarchical numbering
// (e.g., bv-xyz.1, bv-xyz.1.1, bv-xyz.1.2). Returns:
//   - -1 if id1 < id2 (id1 should come first in ascending order)
//   - 0 if id1 == id2
//   - 1 if id1 > id2 (id2 should come first in ascending order)
//
// The comparison is hierarchical: it first compares the base prefix, then
// each numeric segment in order. For example:
// - bv-abc < bv-xyz (alphabetical prefix)
// - bv-xyz.1 < bv-xyz.2 (first level)
// - bv-xyz.1.1 < bv-xyz.1.2 (second level)
// - bv-xyz.1 < bv-xyz.1.1 (parent before children)
func CompareHierarchicalIDs(id1, id2 string) int {
	if id1 == id2 {
		return 0
	}

	// Split IDs by dots to get segments
	parts1 := strings.Split(id1, ".")
	parts2 := strings.Split(id2, ".")

	// First segment is the base ID (e.g., "bv-xyz")
	// Compare alphabetically
	if parts1[0] != parts2[0] {
		if parts1[0] < parts2[0] {
			return -1
		}
		return 1
	}

	// Same base ID - compare numeric suffixes
	// Skip the first segment (base ID)
	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}

	for i := 1; i < maxLen; i++ {
		// If one ID has fewer segments, it comes first (parent before children)
		if i >= len(parts1) {
			return -1 // id1 is shorter, comes first
		}
		if i >= len(parts2) {
			return 1 // id2 is shorter, comes first
		}

		// Try to parse as numbers for numeric comparison
		num1, err1 := strconv.Atoi(parts1[i])
		num2, err2 := strconv.Atoi(parts2[i])

		if err1 == nil && err2 == nil {
			// Both are numbers - compare numerically
			if num1 != num2 {
				if num1 < num2 {
					return -1
				}
				return 1
			}
		} else {
			// At least one is not a number - compare as strings
			if parts1[i] != parts2[i] {
				if parts1[i] < parts2[i] {
					return -1
				}
				return 1
			}
		}
	}

	return 0
}

// StatusOrderFunc returns a sort order for an issue based on its status.
// Lower values sort first.
type StatusOrderFunc func(model.Issue) int

// SortIssuesByStatusThenPriority sorts issues by status order (using the provided
// function) then by priority, then by hierarchical ID. This is the most common
// sorting pattern used throughout the lens dashboard ecosystem.
func SortIssuesByStatusThenPriority(issues []model.Issue, statusOrder StatusOrderFunc) {
	sort.Slice(issues, func(i, j int) bool {
		si := statusOrder(issues[i])
		sj := statusOrder(issues[j])
		if si != sj {
			return si < sj
		}
		if issues[i].Priority != issues[j].Priority {
			return issues[i].Priority < issues[j].Priority
		}
		// Within same priority, sort by hierarchical ID (ascending)
		return CompareHierarchicalIDs(issues[i].ID, issues[j].ID) < 0
	})
}

// SortIssuesByPriorityOnly sorts issues by priority ascending (P0 first),
// then by hierarchical ID for stable ordering.
func SortIssuesByPriorityOnly(issues []model.Issue) {
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Priority != issues[j].Priority {
			return issues[i].Priority < issues[j].Priority
		}
		// Within same priority, sort by hierarchical ID (ascending)
		return CompareHierarchicalIDs(issues[i].ID, issues[j].ID) < 0
	})
}

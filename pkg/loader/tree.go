package loader

import (
	"fmt"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

// ReviewTree contains an issue and all its descendants plus external blockers
type ReviewTree struct {
	Root        *model.Issue            // The root issue
	Descendants []*model.Issue          // All children recursively via parent-child deps
	Blockers    []*model.Issue          // External issues that block items in the tree
	IssueMap    map[string]*model.Issue // All issues by ID for O(1) lookup
}

// LoadReviewTree loads an issue tree starting from rootID
// It traverses parent-child dependencies to find all descendants,
// then identifies external blockers (issues outside the tree that block items in it)
func LoadReviewTree(rootID string, issues []model.Issue) (*ReviewTree, error) {
	// Build issue map for O(1) lookup
	issueMap := make(map[string]*model.Issue)
	for i := range issues {
		issueMap[issues[i].ID] = &issues[i]
	}

	root, exists := issueMap[rootID]
	if !exists {
		return nil, fmt.Errorf("issue not found: %s", rootID)
	}

	// Find all descendants via BFS on parent-child relationships
	descendants := make([]*model.Issue, 0)
	descendantIDs := make(map[string]bool)
	descendantIDs[rootID] = true

	// Build parent->children map
	childrenMap := make(map[string][]string)
	for _, issue := range issues {
		for _, dep := range issue.Dependencies {
			if dep.Type == model.DepParentChild {
				// issue depends on dep.DependsOnID as parent
				// So dep.DependsOnID is parent, issue.ID is child
				childrenMap[dep.DependsOnID] = append(childrenMap[dep.DependsOnID], issue.ID)
			}
		}
	}

	// BFS to find all descendants
	queue := []string{rootID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, childID := range childrenMap[current] {
			if !descendantIDs[childID] {
				descendantIDs[childID] = true
				if child, ok := issueMap[childID]; ok {
					descendants = append(descendants, child)
					queue = append(queue, childID)
				}
			}
		}
	}

	// Find external blockers - issues that block items in the tree but aren't descendants
	blockers := make([]*model.Issue, 0)
	blockerIDs := make(map[string]bool)

	for id := range descendantIDs {
		issue := issueMap[id]
		if issue == nil {
			continue
		}
		for _, dep := range issue.Dependencies {
			if dep.Type == model.DepBlocks {
				// This issue is blocked by dep.DependsOnID
				blockerID := dep.DependsOnID
				if !descendantIDs[blockerID] && !blockerIDs[blockerID] {
					if blocker, ok := issueMap[blockerID]; ok {
						blockers = append(blockers, blocker)
						blockerIDs[blockerID] = true
					}
				}
			}
		}
	}

	return &ReviewTree{
		Root:        root,
		Descendants: descendants,
		Blockers:    blockers,
		IssueMap:    issueMap,
	}, nil
}

// AllIssues returns root + all descendants as a flat slice
func (t *ReviewTree) AllIssues() []*model.Issue {
	result := make([]*model.Issue, 0, 1+len(t.Descendants))
	result = append(result, t.Root)
	result = append(result, t.Descendants...)
	return result
}

// TotalCount returns the total number of issues in the tree (root + descendants)
func (t *ReviewTree) TotalCount() int {
	return 1 + len(t.Descendants)
}

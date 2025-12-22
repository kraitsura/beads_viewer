package ui

import (
	"sort"
	"strings"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

// ══════════════════════════════════════════════════════════════════════════════
// TREE BUILDING - Dependency tree construction and traversal
// ══════════════════════════════════════════════════════════════════════════════

// getDirectChildren returns the direct children of an issue via parent-child relationships
func getDirectChildren(parentID string, issues []model.Issue) map[string]bool {
	children := make(map[string]bool)
	for _, issue := range issues {
		for _, dep := range issue.Dependencies {
			if dep.Type == model.DepParentChild && dep.DependsOnID == parentID {
				children[issue.ID] = true
				break
			}
		}
	}
	return children
}

// buildEpicDescendantsByDepth builds maps of descendants at each depth level for epic mode.
// Depth1 = direct children only
// Depth2 = children + grandchildren
// Depth3 = children + grandchildren + great-grandchildren
// DepthAll = all descendants
func buildEpicDescendantsByDepth(epicID string, issues []model.Issue) map[DepthOption]map[string]bool {
	// Build parent -> children map
	children := make(map[string][]string)
	for _, issue := range issues {
		for _, dep := range issue.Dependencies {
			if dep.Type == model.DepParentChild {
				children[dep.DependsOnID] = append(children[dep.DependsOnID], issue.ID)
			}
		}
	}

	result := make(map[DepthOption]map[string]bool)

	// BFS with depth tracking
	// Collect descendants at each level
	descendantsByLevel := make(map[int]map[string]bool)
	visited := make(map[string]bool)
	queue := []BFSQueueItem{{ID: epicID, Depth: 0}}
	visited[epicID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, childID := range children[current.ID] {
			if !visited[childID] {
				visited[childID] = true
				childDepth := current.Depth + 1

				if descendantsByLevel[childDepth] == nil {
					descendantsByLevel[childDepth] = make(map[string]bool)
				}
				descendantsByLevel[childDepth][childID] = true

				queue = append(queue, BFSQueueItem{ID: childID, Depth: childDepth})
			}
		}
	}

	// Build cumulative sets for each DepthOption
	// IMPORTANT: Always include the entry epic itself at ALL depth levels
	// This ensures the entry point is visible and highlighted in the view

	// Depth1 = entry epic + direct children
	result[Depth1] = make(map[string]bool)
	result[Depth1][epicID] = true // Entry point always included
	for id := range descendantsByLevel[1] {
		result[Depth1][id] = true
	}

	// Depth2 = entry epic + levels 1-2
	result[Depth2] = make(map[string]bool)
	result[Depth2][epicID] = true // Entry point always included
	for level := 1; level <= 2; level++ {
		for id := range descendantsByLevel[level] {
			result[Depth2][id] = true
		}
	}

	// Depth3 = entry epic + levels 1-3
	result[Depth3] = make(map[string]bool)
	result[Depth3][epicID] = true // Entry point always included
	for level := 1; level <= 3; level++ {
		for id := range descendantsByLevel[level] {
			result[Depth3][id] = true
		}
	}

	// DepthAll = entry epic + all levels
	result[DepthAll] = make(map[string]bool)
	result[DepthAll][epicID] = true // Entry point always included
	for level := range descendantsByLevel {
		for id := range descendantsByLevel[level] {
			result[DepthAll][id] = true
		}
	}

	return result
}

// expandToDescendants expands a set of issue IDs to include all descendants
// via parent-child relationships. Uses BFS to find all children recursively.
func expandToDescendants(primaryIDs map[string]bool, issues []model.Issue) map[string]bool {
	if len(primaryIDs) == 0 {
		return primaryIDs
	}

	// Build parent -> children map
	children := make(map[string][]string)
	for _, issue := range issues {
		for _, dep := range issue.Dependencies {
			if dep.Type == model.DepParentChild {
				// issue is a child of dep.DependsOnID
				children[dep.DependsOnID] = append(children[dep.DependsOnID], issue.ID)
			}
		}
	}

	// BFS to find all descendants
	expanded := make(map[string]bool)
	for id := range primaryIDs {
		expanded[id] = true
	}

	queue := make([]string, 0, len(primaryIDs))
	for id := range primaryIDs {
		queue = append(queue, id)
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, childID := range children[current] {
			if !expanded[childID] {
				expanded[childID] = true
				queue = append(queue, childID)
			}
		}
	}

	return expanded
}

// expandToDescendantsAndBlocked expands a set of issue IDs to include:
// 1. All descendants via parent-child relationships (like expandToDescendants)
// 2. All issues that the primary set blocks (downstream dependency graph)
// This is used for bead mode where we want to show what an issue unblocks.
func expandToDescendantsAndBlocked(primaryIDs map[string]bool, issues []model.Issue) map[string]bool {
	if len(primaryIDs) == 0 {
		return primaryIDs
	}

	// First expand via parent-child
	expanded := expandToDescendants(primaryIDs, issues)

	// Build blocks graph: issue ID -> issues it blocks (issues that depend on it)
	blocks := make(map[string][]string)
	for _, issue := range issues {
		for _, dep := range issue.Dependencies {
			if dep.Type == model.DepBlocks {
				// issue depends on dep.DependsOnID, meaning dep.DependsOnID blocks issue
				blocks[dep.DependsOnID] = append(blocks[dep.DependsOnID], issue.ID)
			}
		}
	}

	// BFS to find all blocked issues (downstream from primary set)
	queue := make([]string, 0, len(expanded))
	for id := range expanded {
		queue = append(queue, id)
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, blockedID := range blocks[current] {
			if !expanded[blockedID] {
				expanded[blockedID] = true
				queue = append(queue, blockedID)
			}
		}
	}

	return expanded
}

// buildBeadDescendantsByDepth builds maps of descendants at each depth level for bead mode.
// Similar to buildEpicDescendantsByDepth but includes both parent-child AND blocking relationships.
// Depth1 = direct children + directly blocked issues
// Depth2 = above + grandchildren + transitively blocked
// etc.
func buildBeadDescendantsByDepth(beadID string, issues []model.Issue) map[DepthOption]map[string]bool {
	// Build parent -> children map AND blocker -> blocked map
	children := make(map[string][]string)
	blocks := make(map[string][]string)
	for _, issue := range issues {
		for _, dep := range issue.Dependencies {
			switch dep.Type {
			case model.DepParentChild:
				children[dep.DependsOnID] = append(children[dep.DependsOnID], issue.ID)
			case model.DepBlocks:
				blocks[dep.DependsOnID] = append(blocks[dep.DependsOnID], issue.ID)
			}
		}
	}

	result := make(map[DepthOption]map[string]bool)

	// BFS with depth tracking - follow both parent-child and blocking edges
	// Collect descendants at each level
	descendantsByLevel := make(map[int]map[string]bool)
	visited := make(map[string]bool)
	queue := []BFSQueueItem{{ID: beadID, Depth: 0}}
	visited[beadID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		// Follow parent-child edges
		for _, childID := range children[current.ID] {
			if !visited[childID] {
				visited[childID] = true
				childDepth := current.Depth + 1

				if descendantsByLevel[childDepth] == nil {
					descendantsByLevel[childDepth] = make(map[string]bool)
				}
				descendantsByLevel[childDepth][childID] = true

				queue = append(queue, BFSQueueItem{ID: childID, Depth: childDepth})
			}
		}

		// Follow blocking edges
		for _, blockedID := range blocks[current.ID] {
			if !visited[blockedID] {
				visited[blockedID] = true
				blockedDepth := current.Depth + 1

				if descendantsByLevel[blockedDepth] == nil {
					descendantsByLevel[blockedDepth] = make(map[string]bool)
				}
				descendantsByLevel[blockedDepth][blockedID] = true

				queue = append(queue, BFSQueueItem{ID: blockedID, Depth: blockedDepth})
			}
		}
	}

	// Build cumulative sets for each DepthOption
	// IMPORTANT: Always include the entry bead itself at ALL depth levels
	// This ensures the entry point is visible and highlighted in the view

	// Depth1 = entry bead + direct children + directly blocked
	result[Depth1] = make(map[string]bool)
	result[Depth1][beadID] = true // Entry point always included
	for id := range descendantsByLevel[1] {
		result[Depth1][id] = true
	}

	// Depth2 = entry bead + levels 1-2
	result[Depth2] = make(map[string]bool)
	result[Depth2][beadID] = true // Entry point always included
	for level := 1; level <= 2; level++ {
		for id := range descendantsByLevel[level] {
			result[Depth2][id] = true
		}
	}

	// Depth3 = entry bead + levels 1-3
	result[Depth3] = make(map[string]bool)
	result[Depth3][beadID] = true // Entry point always included
	for level := 1; level <= 3; level++ {
		for id := range descendantsByLevel[level] {
			result[Depth3][id] = true
		}
	}

	// DepthAll = entry bead + all levels
	result[DepthAll] = make(map[string]bool)
	result[DepthAll][beadID] = true // Entry point always included
	for level := range descendantsByLevel {
		for id := range descendantsByLevel[level] {
			result[DepthAll][id] = true
		}
	}

	return result
}

// buildGraphs builds the upstream and downstream dependency graphs
func (m *LensDashboardModel) buildGraphs() {
	m.downstream = make(map[string][]string)
	m.upstream = make(map[string][]string)
	m.blockedByMap = make(map[string]string)

	// Build set of open issues
	openIssues := make(map[string]bool)
	for _, issue := range m.allIssues {
		if issue.Status != model.StatusClosed {
			openIssues[issue.ID] = true
		}
	}

	// Build graphs from dependencies
	for _, issue := range m.allIssues {
		for _, dep := range issue.Dependencies {
			switch dep.Type {
			case model.DepBlocks:
				// issue depends on dep.DependsOnID (dep.DependsOnID blocks issue)
				// So: dep.DependsOnID -> issue (downstream)
				// And: issue <- dep.DependsOnID (upstream)
				m.downstream[dep.DependsOnID] = append(m.downstream[dep.DependsOnID], issue.ID)
				m.upstream[issue.ID] = append(m.upstream[issue.ID], dep.DependsOnID)

				// Track first open blocker
				if openIssues[dep.DependsOnID] && m.blockedByMap[issue.ID] == "" {
					m.blockedByMap[issue.ID] = dep.DependsOnID
				}

			case model.DepParentChild:
				// issue is a child of dep.DependsOnID (parent -> child relationship)
				// So: dep.DependsOnID -> issue (downstream/children)
				// And: issue <- dep.DependsOnID (upstream/parent)
				m.downstream[dep.DependsOnID] = append(m.downstream[dep.DependsOnID], issue.ID)
				m.upstream[issue.ID] = append(m.upstream[issue.ID], dep.DependsOnID)
				// Note: parent-child doesn't create blocking relationships
			}
		}
	}
}

// buildTree builds the tree structure based on current depth
func (m *LensDashboardModel) buildTree() {
	m.roots = nil
	m.flatNodes = nil
	m.upstreamNodes = nil
	m.egoNode = nil
	m.totalCount = 0
	m.primaryCount = 0
	m.contextCount = 0
	m.readyCount = 0
	m.blockedCount = 0
	m.closedCount = 0

	// For epic/bead modes with centered view, use ego-centered tree building
	if m.centeredMode && (m.viewMode == "epic" || m.viewMode == "bead") && m.epicID != "" {
		m.buildEgoCenteredTree()
		return
	}

	seen := make(map[string]bool)

	// Get the depth-appropriate primary IDs
	// This ensures Depth1 uses directPrimaryIDs for label mode,
	// and depth-specific descendants for epic mode
	depthPrimaryIDs := m.GetPrimaryIDsForDepth()

	// Find root nodes: primary issues that are "ready" (not blocked by open issues)
	// Or at depth 1, just show all primary issues flat
	var rootIssues []model.Issue

	if m.dependencyDepth == Depth1 {
		// Depth 1: flat list of primary issues only (depth-appropriate)
		for _, issue := range m.allIssues {
			if depthPrimaryIDs[issue.ID] {
				rootIssues = append(rootIssues, issue)
			}
		}
	} else {
		// Depth 2+: find ready roots and build trees
		// Roots are primary issues with no open blockers (or blockers outside primary set)
		for _, issue := range m.allIssues {
			if !depthPrimaryIDs[issue.ID] {
				continue
			}

			// Check if blocked by another primary issue
			isBlockedByPrimary := false
			for _, blockerID := range m.upstream[issue.ID] {
				if blocker, ok := m.issueMap[blockerID]; ok {
					if blocker.Status != model.StatusClosed && depthPrimaryIDs[blockerID] {
						isBlockedByPrimary = true
						break
					}
				}
			}

			if !isBlockedByPrimary {
				rootIssues = append(rootIssues, issue)
			}
		}

		// If no roots found (all blocked), just use all primary issues
		if len(rootIssues) == 0 {
			for _, issue := range m.allIssues {
				if depthPrimaryIDs[issue.ID] {
					rootIssues = append(rootIssues, issue)
				}
			}
		}
	}

	// Sort roots: entry point first (when in epic or bead mode), then by status, then priority
	sort.Slice(rootIssues, func(i, j int) bool {
		// Entry point (epic or bead) always comes first
		if (m.viewMode == "epic" || m.viewMode == "bead") && m.epicID != "" {
			if rootIssues[i].ID == m.epicID {
				return true
			}
			if rootIssues[j].ID == m.epicID {
				return false
			}
		}
		si := m.getStatusOrder(rootIssues[i])
		sj := m.getStatusOrder(rootIssues[j])
		if si != sj {
			return si < sj
		}
		return rootIssues[i].Priority < rootIssues[j].Priority
	})

	// Build tree from each root
	maxDepth := int(m.dependencyDepth)
	if m.dependencyDepth == DepthAll {
		maxDepth = 100
	}

	for i, issue := range rootIssues {
		if seen[issue.ID] {
			continue
		}
		isLast := i == len(rootIssues)-1
		node := m.buildTreeNode(issue, 0, maxDepth, seen, isLast, nil)
		if node != nil {
			m.roots = append(m.roots, node)
		}
	}

	// Add upstream context blockers that weren't reached via downstream traversal
	// These are context issues that block primary issues but aren't downstream of any primary
	if m.dependencyDepth != Depth1 {
		m.addUpstreamContextBlockers(seen, maxDepth)
	}

	// Fix tree structure (correct IsLastChild/ParentPath after siblings may have been claimed)
	m.fixTreeStructure()

	// Flatten tree for display
	m.flattenTree()

	// Update selected issue
	if len(m.flatNodes) > 0 {
		if m.cursor >= len(m.flatNodes) {
			m.cursor = len(m.flatNodes) - 1
		}
		m.selectedIssueID = m.flatNodes[m.cursor].Node.Issue.ID
	} else {
		m.selectedIssueID = ""
	}
}

// buildTreeNode recursively builds a tree node
func (m *LensDashboardModel) buildTreeNode(issue model.Issue, depth, maxDepth int, seen map[string]bool, isLast bool, parentPath []bool) *LensTreeNode {
	if seen[issue.ID] {
		return nil
	}
	seen[issue.ID] = true

	// Use depth-appropriate primary IDs for IsPrimary determination
	depthPrimaryIDs := m.GetPrimaryIDsForDepth()

	node := &LensTreeNode{
		Issue:       issue,
		IsPrimary:   depthPrimaryIDs[issue.ID],
		IsEntryEpic: (m.viewMode == "epic" || m.viewMode == "bead") && issue.ID == m.epicID,
		Depth:       depth,
		IsLastChild: isLast,
		ParentPath:  append([]bool{}, parentPath...),
	}

	// Update stats
	m.totalCount++
	if node.IsPrimary {
		m.primaryCount++
	} else {
		m.contextCount++
	}

	status := m.getIssueStatus(issue)
	switch status {
	case "ready":
		m.readyCount++
	case "blocked":
		m.blockedCount++
	case "closed":
		m.closedCount++
	}

	// Add children (downstream issues) if within depth
	if depth < maxDepth-1 {
		var childIssues []model.Issue
		for _, childID := range m.downstream[issue.ID] {
			if child, ok := m.issueMap[childID]; ok {
				if !seen[childID] {
					childIssues = append(childIssues, *child)
				}
			}
		}

		// Sort children by status then priority
		sort.Slice(childIssues, func(i, j int) bool {
			si := m.getStatusOrder(childIssues[i])
			sj := m.getStatusOrder(childIssues[j])
			if si != sj {
				return si < sj
			}
			return childIssues[i].Priority < childIssues[j].Priority
		})

		newParentPath := append(parentPath, isLast)
		for i, child := range childIssues {
			childIsLast := i == len(childIssues)-1
			childNode := m.buildTreeNode(child, depth+1, maxDepth, seen, childIsLast, newParentPath)
			if childNode != nil {
				node.Children = append(node.Children, childNode)
			}
		}
	}

	return node
}

// addUpstreamContextBlockers finds context issues that block primaries but weren't
// included via downstream traversal, and adds them to the tree.
// This ensures parity with workstream view which includes both directions.
func (m *LensDashboardModel) addUpstreamContextBlockers(seen map[string]bool, maxDepth int) {
	// Use depth-appropriate primary IDs
	depthPrimaryIDs := m.GetPrimaryIDsForDepth()

	// Find all context issues that block any primary issue (directly or transitively)
	var contextBlockers []model.Issue

	// First pass: find direct context blockers of primary issues
	directBlockers := make(map[string]bool)
	for _, issue := range m.allIssues {
		if depthPrimaryIDs[issue.ID] {
			// This is a primary - check its blockers
			for _, blockerID := range m.upstream[issue.ID] {
				if !depthPrimaryIDs[blockerID] && !seen[blockerID] {
					// This is a context blocker not yet in the tree
					directBlockers[blockerID] = true
				}
			}
		}
	}

	// BFS to find transitive context blockers (blockers of blockers)
	// AND their parent-child descendants (children of context blockers)
	toVisit := make([]string, 0, len(directBlockers))
	for id := range directBlockers {
		toVisit = append(toVisit, id)
	}

	allContextBlockers := make(map[string]bool)
	for id := range directBlockers {
		allContextBlockers[id] = true
	}

	for len(toVisit) > 0 {
		current := toVisit[0]
		toVisit = toVisit[1:]

		// Find upstream blockers of this context issue
		for _, blockerID := range m.upstream[current] {
			if !depthPrimaryIDs[blockerID] && !seen[blockerID] && !allContextBlockers[blockerID] {
				allContextBlockers[blockerID] = true
				toVisit = append(toVisit, blockerID)
			}
		}

	}

	// Collect context blocker issues
	for _, issue := range m.allIssues {
		if allContextBlockers[issue.ID] {
			contextBlockers = append(contextBlockers, issue)
		}
	}

	if len(contextBlockers) == 0 {
		return
	}

	// Sort by status (ready first) then priority
	sort.Slice(contextBlockers, func(i, j int) bool {
		si := m.getStatusOrder(contextBlockers[i])
		sj := m.getStatusOrder(contextBlockers[j])
		if si != sj {
			return si < sj
		}
		return contextBlockers[i].Priority < contextBlockers[j].Priority
	})

	// Find context blockers that are "roots" (not blocked by other unseen context blockers)
	contextRoots := make([]model.Issue, 0)
	for _, issue := range contextBlockers {
		if seen[issue.ID] {
			continue
		}

		isBlockedByUnseen := false
		for _, blockerID := range m.upstream[issue.ID] {
			if allContextBlockers[blockerID] && !seen[blockerID] {
				isBlockedByUnseen = true
				break
			}
		}

		if !isBlockedByUnseen {
			contextRoots = append(contextRoots, issue)
		}
	}

	// If no roots found, just use all unseen context blockers
	if len(contextRoots) == 0 {
		for _, issue := range contextBlockers {
			if !seen[issue.ID] {
				contextRoots = append(contextRoots, issue)
			}
		}
	}

	// Build tree nodes for context blockers
	// These will follow downstream within the context blocker set
	numExistingRoots := len(m.roots)
	for i, issue := range contextRoots {
		if seen[issue.ID] {
			continue
		}
		isLast := (numExistingRoots == 0) && (i == len(contextRoots)-1)
		node := m.buildContextBlockerNode(issue, 0, maxDepth, seen, isLast, nil, allContextBlockers)
		if node != nil {
			m.roots = append(m.roots, node)
		}
	}
}

// buildContextBlockerNode builds a tree node for context blockers,
// following downstream within the context blocker set
func (m *LensDashboardModel) buildContextBlockerNode(issue model.Issue, depth, maxDepth int, seen map[string]bool, isLast bool, parentPath []bool, contextBlockerSet map[string]bool) *LensTreeNode {
	if seen[issue.ID] {
		return nil
	}
	seen[issue.ID] = true

	// Use depth-appropriate primary IDs for IsPrimary determination
	depthPrimaryIDs := m.GetPrimaryIDsForDepth()

	node := &LensTreeNode{
		Issue:       issue,
		IsPrimary:   depthPrimaryIDs[issue.ID],
		Depth:       depth,
		IsLastChild: isLast,
		ParentPath:  append([]bool{}, parentPath...),
	}

	// Update stats
	m.totalCount++
	if node.IsPrimary {
		m.primaryCount++
	} else {
		m.contextCount++
	}

	status := m.getIssueStatus(issue)
	switch status {
	case "ready":
		m.readyCount++
	case "blocked":
		m.blockedCount++
	case "closed":
		m.closedCount++
	}

	// Add children (downstream issues within context blocker set) if within depth
	if depth < maxDepth-1 {
		var childIssues []model.Issue
		for _, childID := range m.downstream[issue.ID] {
			if child, ok := m.issueMap[childID]; ok {
				// Only include if it's a context blocker and not yet seen
				if contextBlockerSet[childID] && !seen[childID] {
					childIssues = append(childIssues, *child)
				}
			}
		}

		// Sort children by status then priority
		sort.Slice(childIssues, func(i, j int) bool {
			si := m.getStatusOrder(childIssues[i])
			sj := m.getStatusOrder(childIssues[j])
			if si != sj {
				return si < sj
			}
			return childIssues[i].Priority < childIssues[j].Priority
		})

		newParentPath := append(parentPath, isLast)
		for i, child := range childIssues {
			childIsLast := i == len(childIssues)-1
			childNode := m.buildContextBlockerNode(child, depth+1, maxDepth, seen, childIsLast, newParentPath, contextBlockerSet)
			if childNode != nil {
				node.Children = append(node.Children, childNode)
			}
		}
	}

	return node
}

// fixTreeStructure corrects IsLastChild and ParentPath values after tree construction.
// This is needed because during recursive building, siblings may get "claimed" by earlier
// branches, leaving incorrect IsLastChild values that cause tree lines to extend too far.
func (m *LensDashboardModel) fixTreeStructure() {
	for i, root := range m.roots {
		isLast := i == len(m.roots)-1
		root.IsLastChild = isLast
		root.ParentPath = nil
		m.fixNodeChildren(root, nil)
	}
}

// fixNodeChildren recursively fixes IsLastChild and ParentPath for all children
func (m *LensDashboardModel) fixNodeChildren(node *LensTreeNode, parentPath []bool) {
	if len(node.Children) == 0 {
		return
	}

	// Build the parent path for children (includes this node's IsLastChild)
	childParentPath := append([]bool{}, parentPath...)
	childParentPath = append(childParentPath, node.IsLastChild)

	// Fix each child's IsLastChild and ParentPath
	for i, child := range node.Children {
		child.IsLastChild = i == len(node.Children)-1
		child.ParentPath = append([]bool{}, childParentPath...)
		m.fixNodeChildren(child, childParentPath)
	}
}

// flattenTree converts the tree to a flat list for display
func (m *LensDashboardModel) flattenTree() {
	m.flatNodes = nil
	for _, root := range m.roots {
		m.flattenNode(root, make(map[string]bool))
	}
}

// flattenNode recursively flattens a node and its children.
// ancestors tracks issue IDs visible above this node in the tree, used to detect
// if a blocker is already visible (so we can suppress the redundant indicator).
func (m *LensDashboardModel) flattenNode(node *LensTreeNode, ancestors map[string]bool) {
	prefix := m.buildTreePrefix(node)
	status := m.getIssueStatus(node.Issue)
	blockerID := m.blockedByMap[node.Issue.ID]

	fn := LensFlatNode{
		Node:          node,
		TreePrefix:    prefix,
		Status:        status,
		BlockedBy:     blockerID,
		BlockerInTree: ancestors[blockerID],
	}
	m.flatNodes = append(m.flatNodes, fn)

	// Build child ancestors (include current node)
	childAncestors := make(map[string]bool, len(ancestors)+1)
	for k := range ancestors {
		childAncestors[k] = true
	}
	childAncestors[node.Issue.ID] = true

	for _, child := range node.Children {
		m.flattenNode(child, childAncestors)
	}
}

// buildEgoCenteredTree builds a tree structure centered on the entry point (epic/bead).
// Layout: Upstream blockers → Entry point (center) → Downstream descendants
func (m *LensDashboardModel) buildEgoCenteredTree() {
	entryIssue, exists := m.issueMap[m.epicID]
	if !exists {
		return
	}

	depthPrimaryIDs := m.GetPrimaryIDsForDepth()
	maxDepth := int(m.dependencyDepth)
	if m.dependencyDepth == DepthAll {
		maxDepth = 100
	}

	// Track what we've seen
	seen := make(map[string]bool)

	// 1. Build the ego/center node
	egoTreeNode := &LensTreeNode{
		Issue:         *entryIssue,
		IsPrimary:     true,
		IsEntryEpic:   true,
		Depth:         0,
		RelativeDepth: 0,
		IsLastChild:   true,
		ParentPath:    nil,
	}
	m.egoNode = &LensFlatNode{
		Node:       egoTreeNode,
		TreePrefix: "",
		Status:     m.getIssueStatus(*entryIssue),
		BlockedBy:  m.blockedByMap[entryIssue.ID],
	}
	seen[m.epicID] = true
	m.totalCount++
	m.primaryCount++
	if m.egoNode.Status == "ready" {
		m.readyCount++
	} else if m.egoNode.Status == "blocked" {
		m.blockedCount++
	} else if m.egoNode.Status == "closed" {
		m.closedCount++
	}

	// 2. Build upstream blockers (issues that block the entry point)
	// These are shown ABOVE the center
	m.upstreamNodes = nil
	blockerIDs := m.upstream[m.epicID]
	var blockerIssues []model.Issue
	for _, blockerID := range blockerIDs {
		if blocker, ok := m.issueMap[blockerID]; ok {
			if blocker.Status != model.StatusClosed { // Only show open blockers
				blockerIssues = append(blockerIssues, *blocker)
			}
		}
	}

	// Sort blockers by status then priority
	sort.Slice(blockerIssues, func(i, j int) bool {
		si := m.getStatusOrder(blockerIssues[i])
		sj := m.getStatusOrder(blockerIssues[j])
		if si != sj {
			return si < sj
		}
		return blockerIssues[i].Priority < blockerIssues[j].Priority
	})

	for i, blocker := range blockerIssues {
		if seen[blocker.ID] {
			continue
		}
		seen[blocker.ID] = true

		node := &LensTreeNode{
			Issue:         blocker,
			IsPrimary:     depthPrimaryIDs[blocker.ID],
			IsEntryEpic:   false,
			Depth:         0,
			RelativeDepth: -1, // Upstream = negative depth
			IsLastChild:   i == len(blockerIssues)-1,
			IsUpstream:    true,
		}

		fn := LensFlatNode{
			Node:       node,
			TreePrefix: "",
			Status:     m.getIssueStatus(blocker),
			BlockedBy:  m.blockedByMap[blocker.ID],
		}
		m.upstreamNodes = append(m.upstreamNodes, fn)

		m.totalCount++
		if node.IsPrimary {
			m.primaryCount++
		} else {
			m.contextCount++
		}
		if fn.Status == "ready" {
			m.readyCount++
		} else if fn.Status == "blocked" {
			m.blockedCount++
		} else if fn.Status == "closed" {
			m.closedCount++
		}
	}

	// 3. Build downstream tree (children and dependents)
	// These are shown BELOW the center
	m.roots = nil
	m.flatNodes = nil

	// Get all direct children/dependents of the entry point
	downstreamIDs := m.downstream[m.epicID]
	var downstreamIssues []model.Issue
	for _, childID := range downstreamIDs {
		if child, ok := m.issueMap[childID]; ok {
			if !seen[childID] {
				downstreamIssues = append(downstreamIssues, *child)
			}
		}
	}

	// Sort by status then priority
	sort.Slice(downstreamIssues, func(i, j int) bool {
		si := m.getStatusOrder(downstreamIssues[i])
		sj := m.getStatusOrder(downstreamIssues[j])
		if si != sj {
			return si < sj
		}
		return downstreamIssues[i].Priority < downstreamIssues[j].Priority
	})

	// Build tree from each downstream issue
	for i, issue := range downstreamIssues {
		if seen[issue.ID] {
			continue
		}
		isLast := i == len(downstreamIssues)-1
		node := m.buildCenteredTreeNode(issue, 1, maxDepth, seen, isLast, nil, depthPrimaryIDs)
		if node != nil {
			m.roots = append(m.roots, node)
		}
	}

	// Fix tree structure (correct IsLastChild/ParentPath after siblings may have been claimed)
	m.fixTreeStructure()

	// Flatten downstream tree
	// In centered mode, ego and upstream nodes are visible above downstream,
	// so include them as "ancestors" for blocker visibility detection
	centeredAncestors := m.getCenteredAncestors()
	for _, root := range m.roots {
		m.flattenNode(root, centeredAncestors)
	}

	// Update selected issue
	totalNodes := len(m.upstreamNodes) + 1 + len(m.flatNodes) // upstream + ego + downstream
	if totalNodes > 0 {
		// Default cursor to the ego node (after upstream section)
		if m.cursor < 0 || m.cursor >= totalNodes {
			m.cursor = len(m.upstreamNodes) // Point to ego node
		}
		m.selectedIssueID = m.getSelectedIDForCenteredMode()
	}
}

// getCenteredAncestors returns a map of issue IDs that are visible "above" the
// downstream tree in centered mode: the ego node and all upstream blockers.
// Used to detect when a blocker is already visible in the tree structure.
func (m *LensDashboardModel) getCenteredAncestors() map[string]bool {
	ancestors := make(map[string]bool)
	// Ego node is visible above all downstream
	if m.egoNode != nil {
		ancestors[m.egoNode.Node.Issue.ID] = true
	}
	// Upstream blockers are also visible in the tree
	for _, up := range m.upstreamNodes {
		ancestors[up.Node.Issue.ID] = true
	}
	return ancestors
}

// buildCenteredTreeNode builds a tree node with relative depth tracking
func (m *LensDashboardModel) buildCenteredTreeNode(issue model.Issue, relDepth, maxDepth int, seen map[string]bool, isLast bool, parentPath []bool, depthPrimaryIDs map[string]bool) *LensTreeNode {
	if seen[issue.ID] {
		return nil
	}
	seen[issue.ID] = true

	node := &LensTreeNode{
		Issue:         issue,
		IsPrimary:     depthPrimaryIDs[issue.ID],
		IsEntryEpic:   false,
		Depth:         relDepth,
		RelativeDepth: relDepth,
		IsLastChild:   isLast,
		ParentPath:    append([]bool{}, parentPath...),
	}

	// Update stats
	m.totalCount++
	if node.IsPrimary {
		m.primaryCount++
	} else {
		m.contextCount++
	}

	status := m.getIssueStatus(issue)
	switch status {
	case "ready":
		m.readyCount++
	case "blocked":
		m.blockedCount++
	case "closed":
		m.closedCount++
	}

	// Add children if within depth
	if relDepth < maxDepth {
		var childIssues []model.Issue
		for _, childID := range m.downstream[issue.ID] {
			if child, ok := m.issueMap[childID]; ok {
				if !seen[childID] {
					childIssues = append(childIssues, *child)
				}
			}
		}

		// Sort children by status then priority
		sort.Slice(childIssues, func(i, j int) bool {
			si := m.getStatusOrder(childIssues[i])
			sj := m.getStatusOrder(childIssues[j])
			if si != sj {
				return si < sj
			}
			return childIssues[i].Priority < childIssues[j].Priority
		})

		newParentPath := append(parentPath, isLast)
		for i, child := range childIssues {
			childIsLast := i == len(childIssues)-1
			childNode := m.buildCenteredTreeNode(child, relDepth+1, maxDepth, seen, childIsLast, newParentPath, depthPrimaryIDs)
			if childNode != nil {
				node.Children = append(node.Children, childNode)
			}
		}
	}

	return node
}

// isIssueBlockedByDeps checks if an issue is blocked by dependencies
func (m *LensDashboardModel) isIssueBlockedByDeps(issueID string) bool {
	return m.blockedByMap[issueID] != ""
}
// buildTreePrefix builds the tree line prefix for a node
// Uses refined minimal connectors: ├─ └─ │
func (m *LensDashboardModel) buildTreePrefix(node *LensTreeNode) string {
	if node.Depth == 0 {
		return ""
	}

	var prefix strings.Builder

	// Build prefix from parent path with refined spacing
	for i := 0; i < len(node.ParentPath); i++ {
		if node.ParentPath[i] {
			prefix.WriteString("  ") // parent was last child, no line
		} else {
			prefix.WriteString("│ ") // parent has siblings, continue line
		}
	}

	// Refined minimal connectors
	if node.IsLastChild {
		prefix.WriteString("└─")
	} else {
		prefix.WriteString("├─")
	}

	return prefix.String()
}
// End of tree building functions

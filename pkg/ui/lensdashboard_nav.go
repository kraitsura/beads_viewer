package ui

// ══════════════════════════════════════════════════════════════════════════════
// NAVIGATION - Cursor movement and scroll management
// ══════════════════════════════════════════════════════════════════════════════

func (m *LensDashboardModel) MoveUp() {
	if m.viewType == ViewTypeGrouped && len(m.groupedSections) > 0 {
		m.moveUpGrouped()
		m.updateDetailContent()
		return
	}

	if m.viewType == ViewTypeWorkstream && len(m.workstreams) > 1 {
		m.moveUpWS()
		m.updateDetailContent()
		return
	}

	// Handle centered mode navigation (epic/bead modes)
	if (m.viewMode == "epic" || m.viewMode == "bead") && m.egoNode != nil {
		if m.cursor > 0 {
			m.cursor--
			m.selectedIssueID = m.getSelectedIDForCenteredMode()
			m.ensureCenteredVisible()
			m.updateDetailContent()
		}
		return
	}

	// Standard flat view navigation
	if m.cursor > 0 {
		m.cursor--
		m.selectedIssueID = m.flatNodes[m.cursor].Node.Issue.ID
		m.ensureVisible()
		m.updateDetailContent()
	}
}

// MoveDown moves cursor down
func (m *LensDashboardModel) MoveDown() {
	if m.viewType == ViewTypeGrouped && len(m.groupedSections) > 0 {
		m.moveDownGrouped()
		m.updateDetailContent()
		return
	}

	if m.viewType == ViewTypeWorkstream && len(m.workstreams) > 1 {
		m.moveDownWS()
		m.updateDetailContent()
		return
	}

	// Handle centered mode navigation (epic/bead modes)
	if (m.viewMode == "epic" || m.viewMode == "bead") && m.egoNode != nil {
		totalNodes := m.getTotalCenteredNodeCount()
		if m.cursor < totalNodes-1 {
			m.cursor++
			m.selectedIssueID = m.getSelectedIDForCenteredMode()
			m.ensureCenteredVisible()
			m.updateDetailContent()
		}
		return
	}

	// Standard flat view navigation
	if m.cursor < len(m.flatNodes)-1 {
		m.cursor++
		m.selectedIssueID = m.flatNodes[m.cursor].Node.Issue.ID
		m.ensureVisible()
		m.updateDetailContent()
	}
}

// moveUpWS moves cursor up in workstream view
func (m *LensDashboardModel) moveUpWS() {
	if len(m.workstreams) == 0 {
		return
	}

	if m.wsIssueCursor > 0 {
		// Move up within current workstream's issues
		m.wsIssueCursor--
	} else if m.wsIssueCursor == 0 {
		// At first issue, go to header
		m.wsIssueCursor = -1
	} else if m.wsCursor > 0 {
		// At header, go to previous workstream's last issue
		m.wsCursor--
		issueCount := m.getVisibleIssueCount(m.wsCursor)
		if issueCount > 0 {
			m.wsIssueCursor = issueCount - 1
		} else {
			m.wsIssueCursor = -1
		}
	}

	m.updateSelectedIssueFromWS()
}

// getVisibleIssueCount returns the number of visible issues for a workstream
func (m *LensDashboardModel) getVisibleIssueCount(wsIdx int) int {
	if wsIdx >= len(m.workstreams) {
		return 0
	}
	ws := m.workstreams[wsIdx]
	isExpanded := m.wsExpanded[wsIdx]

	// In tree view with expansion, count tree nodes
	if m.wsTreeView && isExpanded {
		wsCopy := ws
		treeRoots := m.buildWorkstreamTree(&wsCopy)
		flatNodes := m.flattenWSTree(treeRoots)
		return len(flatNodes)
	}

	// Flat view
	issueCount := len(ws.Issues)
	if !isExpanded && issueCount > 3 {
		return 3 // Collapsed: show max 3
	}
	return issueCount // Expanded: show all
}

// moveDownWS moves cursor down in workstream view
func (m *LensDashboardModel) moveDownWS() {
	if len(m.workstreams) == 0 {
		return
	}

	maxIssues := m.getVisibleIssueCount(m.wsCursor)

	if m.wsIssueCursor < 0 {
		// At header, move to first issue (if any)
		if maxIssues > 0 {
			m.wsIssueCursor = 0
		} else if m.wsCursor < len(m.workstreams)-1 {
			// No issues, go to next workstream
			m.wsCursor++
			m.wsIssueCursor = -1
		}
	} else if m.wsIssueCursor < maxIssues-1 {
		// Move down within issues
		m.wsIssueCursor++
	} else if m.wsCursor < len(m.workstreams)-1 {
		// At last issue, go to next workstream header
		m.wsCursor++
		m.wsIssueCursor = -1
	}

	m.updateSelectedIssueFromWS()
}

// moveUpGrouped moves cursor up in grouped view
func (m *LensDashboardModel) moveUpGrouped() {
	if len(m.groupedSections) == 0 {
		return
	}

	group := m.groupedSections[m.groupedCursor]
	_ = len(group.SubWorkstreams) > 0 && m.groupedExpanded[m.groupedCursor] // hasSubGroups used below

	if m.groupedIssueCursor > 0 {
		// Move up within current issues
		m.groupedIssueCursor--
	} else if m.groupedIssueCursor == 0 {
		// At first issue, go to sub-group header (if in sub-group) or group header
		m.groupedIssueCursor = -1
	} else if m.groupedSubCursor >= 0 && len(group.SubWorkstreams) > 0 {
		// At sub-group header, check previous sub-group or go to group header
		if m.groupedSubCursor > 0 && m.groupedSubCursor <= len(group.SubWorkstreams) {
			// Go to previous sub-group's last issue or header
			m.groupedSubCursor--
			subGroup := group.SubWorkstreams[m.groupedSubCursor]
			if m.groupedSubExpanded[m.groupedCursor] != nil && m.groupedSubExpanded[m.groupedCursor][m.groupedSubCursor] && subGroup != nil {
				m.groupedIssueCursor = len(subGroup.Issues) - 1
				if m.groupedIssueCursor < 0 {
					m.groupedIssueCursor = -1
				}
			} else {
				m.groupedIssueCursor = -1
			}
		} else {
			// At first sub-group, go to group header
			m.groupedSubCursor = -1
			m.groupedIssueCursor = -1
		}
	} else if m.groupedSubCursor >= 0 {
		// Invalid state: sub-cursor set but no sub-groups, reset
		m.groupedSubCursor = -1
		m.groupedIssueCursor = -1
	} else if m.groupedCursor > 0 {
		// At group header, go to previous group
		m.groupedCursor--
		prevGroup := m.groupedSections[m.groupedCursor]
		prevHasSubGroups := len(prevGroup.SubWorkstreams) > 0 && m.groupedExpanded[m.groupedCursor]

		if prevHasSubGroups {
			// Go to last sub-group
			m.groupedSubCursor = len(prevGroup.SubWorkstreams) - 1
			subGroup := prevGroup.SubWorkstreams[m.groupedSubCursor]
			if m.groupedSubExpanded[m.groupedCursor] != nil && m.groupedSubExpanded[m.groupedCursor][m.groupedSubCursor] && subGroup != nil {
				m.groupedIssueCursor = len(subGroup.Issues) - 1
				if m.groupedIssueCursor < 0 {
					m.groupedIssueCursor = -1
				}
			} else {
				m.groupedIssueCursor = -1
			}
		} else if m.groupedExpanded[m.groupedCursor] && len(prevGroup.Issues) > 0 {
			// Go to last issue in previous group
			m.groupedSubCursor = -1
			m.groupedIssueCursor = len(prevGroup.Issues) - 1
		} else {
			// Go to previous group header
			m.groupedSubCursor = -1
			m.groupedIssueCursor = -1
		}
	}

	m.updateSelectedIssueFromGrouped()
	m.ensureGroupedVisible()
}

// moveDownGrouped moves cursor down in grouped view
func (m *LensDashboardModel) moveDownGrouped() {
	if len(m.groupedSections) == 0 {
		return
	}

	group := m.groupedSections[m.groupedCursor]
	hasSubGroups := len(group.SubWorkstreams) > 0
	isGroupExpanded := m.groupedExpanded[m.groupedCursor]

	if m.groupedSubCursor >= 0 && len(group.SubWorkstreams) > m.groupedSubCursor {
		// We're in a sub-group
		subGroup := group.SubWorkstreams[m.groupedSubCursor]
		isSubExpanded := m.groupedSubExpanded[m.groupedCursor] != nil && m.groupedSubExpanded[m.groupedCursor][m.groupedSubCursor]
		subIssueCount := 0
		if subGroup != nil {
			subIssueCount = len(subGroup.Issues)
		}

		if m.groupedIssueCursor < 0 {
			// At sub-group header
			if isSubExpanded && subIssueCount > 0 {
				m.groupedIssueCursor = 0
			} else if m.groupedSubCursor < len(group.SubWorkstreams)-1 {
				// Go to next sub-group
				m.groupedSubCursor++
				m.groupedIssueCursor = -1
			} else if m.groupedCursor < len(m.groupedSections)-1 {
				// Go to next group
				m.groupedCursor++
				m.groupedSubCursor = -1
				m.groupedIssueCursor = -1
			}
		} else if m.groupedIssueCursor < subIssueCount-1 {
			// Move down within sub-group issues
			m.groupedIssueCursor++
		} else if m.groupedSubCursor < len(group.SubWorkstreams)-1 {
			// Go to next sub-group
			m.groupedSubCursor++
			m.groupedIssueCursor = -1
		} else if m.groupedCursor < len(m.groupedSections)-1 {
			// Go to next group
			m.groupedCursor++
			m.groupedSubCursor = -1
			m.groupedIssueCursor = -1
		}
	} else if m.groupedSubCursor >= 0 {
		// Invalid state: sub-cursor set but no sub-groups, reset and go to next group
		m.groupedSubCursor = -1
		m.groupedIssueCursor = -1
		if m.groupedCursor < len(m.groupedSections)-1 {
			m.groupedCursor++
		}
	} else if m.groupedIssueCursor >= 0 {
		// We're in group issues (no sub-groups)
		if m.groupedIssueCursor < len(group.Issues)-1 {
			m.groupedIssueCursor++
		} else if m.groupedCursor < len(m.groupedSections)-1 {
			// Go to next group
			m.groupedCursor++
			m.groupedSubCursor = -1
			m.groupedIssueCursor = -1
		}
	} else {
		// At group header
		if isGroupExpanded && hasSubGroups {
			// Go to first sub-group
			m.groupedSubCursor = 0
			m.groupedIssueCursor = -1
		} else if isGroupExpanded && len(group.Issues) > 0 {
			// Go to first issue
			m.groupedIssueCursor = 0
		} else if m.groupedCursor < len(m.groupedSections)-1 {
			// Go to next group
			m.groupedCursor++
			m.groupedSubCursor = -1
			m.groupedIssueCursor = -1
		}
	}

	m.updateSelectedIssueFromGrouped()
	m.ensureGroupedVisible()
}

// getVisibleGroupedIssueCount returns the number of visible issues for a grouped section
func (m *LensDashboardModel) getVisibleGroupedIssueCount(gIdx int) int {
	if gIdx >= len(m.groupedSections) {
		return 0
	}
	group := m.groupedSections[gIdx]
	isExpanded := m.groupedExpanded[gIdx]

	if !isExpanded {
		return 0 // Collapsed: no issues visible
	}

	// If there are sub-groups, issues are shown within sub-groups
	if len(group.SubWorkstreams) > 0 {
		return 0 // Issues in sub-groups, not directly navigable at group level
	}

	return len(group.Issues)
}

// getTotalGroupedLines calculates total lines in grouped view
func (m *LensDashboardModel) getTotalGroupedLines() int {
	totalLines := 0
	for i, group := range m.groupedSections {
		totalLines++ // Header line
		if m.groupedExpanded[i] {
			if len(group.SubWorkstreams) == 0 {
				totalLines += len(group.Issues)
			} else {
				for j, sub := range group.SubWorkstreams {
					if sub == nil {
						continue
					}
					totalLines++ // Sub-group header
					if m.groupedSubExpanded[i] != nil && m.groupedSubExpanded[i][j] {
						totalLines += len(sub.Issues)
					}
				}
			}
		}
		totalLines++ // Empty line between groups
	}
	return totalLines
}

// ensureGroupedVisible ensures the current cursor position is visible
func (m *LensDashboardModel) ensureGroupedVisible() {
	// Calculate line position for current cursor
	// This must match the rendering logic in renderGroupedView()
	linePos := 0

	for i := 0; i < m.groupedCursor; i++ {
		linePos++ // Header line
		if m.groupedExpanded[i] {
			group := m.groupedSections[i]
			if len(group.SubWorkstreams) == 0 {
				// No sub-groups: just add issue count
				linePos += len(group.Issues)
			} else {
				// Has sub-groups: add each sub-group header + expanded issues
				for j, sub := range group.SubWorkstreams {
					if sub == nil {
						continue
					}
					linePos++ // Sub-group header
					if m.groupedSubExpanded[i] != nil && m.groupedSubExpanded[i][j] {
						linePos += len(sub.Issues)
					}
				}
			}
		}
		linePos++ // Empty line between groups
	}

	// Add current group header
	linePos++

	// Handle position within current group
	if m.groupedCursor >= 0 && m.groupedCursor < len(m.groupedSections) {
		group := m.groupedSections[m.groupedCursor]

		if m.groupedExpanded[m.groupedCursor] {
			if len(group.SubWorkstreams) == 0 {
				// No sub-groups: issue cursor directly under group
				if m.groupedIssueCursor >= 0 {
					linePos += m.groupedIssueCursor + 1
				}
			} else {
				// Has sub-groups
				if m.groupedSubCursor >= 0 {
					// Add lines for sub-groups before current sub-cursor
					for j := 0; j < m.groupedSubCursor && j < len(group.SubWorkstreams); j++ {
						sub := group.SubWorkstreams[j]
						if sub == nil {
							continue
						}
						linePos++ // Sub-group header
						if m.groupedSubExpanded[m.groupedCursor] != nil && m.groupedSubExpanded[m.groupedCursor][j] {
							linePos += len(sub.Issues)
						}
					}
					// Add current sub-group header
					linePos++
					// Add issue position within sub-group
					if m.groupedIssueCursor >= 0 {
						linePos += m.groupedIssueCursor + 1
					}
				}
			}
		}
	}

	// Calculate visible lines using viewport config
	// renderGroupedView adds 2 lines for header, rest is content
	vp := m.calculateViewport()
	contentLines := vp.ContentHeight - 2
	if contentLines < 5 {
		contentLines = 5
	}

	// Center cursor in viewport with reduced scrolloff (1/4 viewport instead of 1/2)
	scrolloff := contentLines / 4
	targetScroll := linePos - scrolloff
	if targetScroll < 0 {
		targetScroll = 0
	}

	// Clamp to max scroll to keep cursor visible within viewport
	totalLines := m.getTotalGroupedLines()
	maxScroll := totalLines - contentLines + scrolloff
	if maxScroll > 0 && targetScroll > maxScroll {
		targetScroll = maxScroll
	}

	m.groupedScroll = targetScroll
}

// updateSelectedIssueFromWS updates selectedIssueID based on workstream cursor
func (m *LensDashboardModel) updateSelectedIssueFromWS() {
	if len(m.workstreams) == 0 {
		m.selectedIssueID = ""
		return
	}

	ws := m.workstreams[m.wsCursor]
	isExpanded := m.wsExpanded[m.wsCursor]

	if m.wsIssueCursor >= 0 {
		// Get the issue at cursor position
		if m.wsTreeView && isExpanded {
			// Tree view - get from flattened tree
			wsCopy := ws
			treeRoots := m.buildWorkstreamTree(&wsCopy)
			flatNodes := m.flattenWSTree(treeRoots)
			if m.wsIssueCursor < len(flatNodes) {
				m.selectedIssueID = flatNodes[m.wsIssueCursor].Node.Issue.ID
			} else if len(flatNodes) > 0 {
				m.selectedIssueID = flatNodes[len(flatNodes)-1].Node.Issue.ID
			} else {
				m.selectedIssueID = ""
			}
		} else {
			// Flat view
			visibleCount := m.getVisibleIssueCount(m.wsCursor)
			if m.wsIssueCursor < visibleCount && m.wsIssueCursor < len(ws.Issues) {
				m.selectedIssueID = ws.Issues[m.wsIssueCursor].ID
			} else if len(ws.Issues) > 0 {
				m.selectedIssueID = ws.Issues[0].ID
			} else {
				m.selectedIssueID = ""
			}
		}
	} else if len(ws.Issues) > 0 {
		// Header selected, use first issue ID
		m.selectedIssueID = ws.Issues[0].ID
	} else {
		m.selectedIssueID = ""
	}

	// Ensure current position is visible
	m.ensureVisibleWS()
}

// ensureVisibleWS adjusts wsScroll to keep cursor centered in viewport
func (m *LensDashboardModel) ensureVisibleWS() {
	// Calculate the line number of the current cursor position
	cursorLine := m.getWSCursorLine()

	// Calculate visible lines using viewport config
	// renderWorkstreamView adds 2 lines for header, rest is content
	vp := m.calculateViewport()
	contentLines := vp.ContentHeight - 2
	if contentLines < 3 {
		contentLines = 3
	}

	// Center cursor in viewport with reduced scrolloff (1/4 viewport instead of 1/2)
	scrolloff := contentLines / 4
	targetScroll := cursorLine - scrolloff
	if targetScroll < 0 {
		targetScroll = 0
	}

	// Clamp to max scroll to keep cursor visible within viewport
	totalLines := m.getTotalWSLines()
	maxScroll := totalLines - contentLines + scrolloff
	if maxScroll > 0 && targetScroll > maxScroll {
		targetScroll = maxScroll
	}

	m.wsScroll = targetScroll
}

// getWSCursorLine calculates the line number of the current cursor in workstream view
func (m *LensDashboardModel) getWSCursorLine() int {
	line := 0
	for wsIdx := 0; wsIdx < len(m.workstreams); wsIdx++ {
		ws := m.workstreams[wsIdx]
		isExpanded := m.wsExpanded[wsIdx]

		// Header line
		if wsIdx == m.wsCursor && m.wsIssueCursor < 0 {
			return line
		}
		line++

		// Calculate issue lines based on view mode
		var issueLineCount int
		if m.wsTreeView && isExpanded {
			// Tree view - count tree nodes
			wsCopy := ws
			treeRoots := m.buildWorkstreamTree(&wsCopy)
			flatNodes := m.flattenWSTree(treeRoots)
			issueLineCount = len(flatNodes)
		} else {
			// Flat view
			issueLineCount = m.getVisibleIssueCount(wsIdx)
		}

		if wsIdx == m.wsCursor && m.wsIssueCursor >= 0 {
			// Clamp cursor to valid range
			if m.wsIssueCursor >= issueLineCount {
				return line + issueLineCount - 1
			}
			return line + m.wsIssueCursor
		}
		line += issueLineCount

		// "+N more" line if collapsed with hidden issues (only in flat view)
		if !isExpanded && !m.wsTreeView && len(ws.Issues) > 3 {
			line++
		}

		// Empty line between workstreams
		line++
	}
	return line
}

// getTotalWSLines calculates total lines in workstream view
func (m *LensDashboardModel) getTotalWSLines() int {
	line := 0
	for wsIdx := range m.workstreams {
		ws := m.workstreams[wsIdx]
		isExpanded := m.wsExpanded[wsIdx]

		line++ // header

		if m.wsTreeView && isExpanded {
			wsCopy := ws
			treeRoots := m.buildWorkstreamTree(&wsCopy)
			flatNodes := m.flattenWSTree(treeRoots)
			line += len(flatNodes)
		} else {
			line += m.getVisibleIssueCount(wsIdx)
			if !isExpanded && len(ws.Issues) > 3 {
				line++ // "+N more" line
			}
		}

		line++ // empty line
	}
	return line
}

// getFlatLinePosition returns the line position for a given flatNodes index
// accounting for status headers that appear when status changes
func (m *LensDashboardModel) getFlatLinePosition(nodeIdx int) int {
	if nodeIdx < 0 || len(m.flatNodes) == 0 {
		return 0
	}
	if nodeIdx >= len(m.flatNodes) {
		nodeIdx = len(m.flatNodes) - 1
	}

	linePos := 0
	lastStatus := ""
	for i := 0; i <= nodeIdx; i++ {
		if m.flatNodes[i].Status != lastStatus {
			linePos++ // status header
			lastStatus = m.flatNodes[i].Status
		}
		if i < nodeIdx {
			linePos++ // node line (don't count target node itself)
		}
	}
	return linePos
}

// getTotalFlatLines returns total lines for flat view including status headers
func (m *LensDashboardModel) getTotalFlatLines() int {
	if len(m.flatNodes) == 0 {
		return 0
	}
	lines := 0
	lastStatus := ""
	for _, fn := range m.flatNodes {
		if fn.Status != lastStatus {
			lines++ // status header
			lastStatus = fn.Status
		}
		lines++ // node line
	}
	return lines
}

// ensureVisible adjusts scroll to keep cursor centered in viewport
// NOTE: For flat view, m.scroll stores LINE position (not node index)
func (m *LensDashboardModel) ensureVisible() {
	if len(m.flatNodes) == 0 {
		m.scroll = 0
		return
	}

	vp := m.calculateViewport()
	// Use contentLines to match renderFlatView offset (subtracts 2 for header)
	contentLines := vp.ContentHeight - 2
	if contentLines < 5 {
		contentLines = 5
	}

	// Get line position of cursor (accounting for status headers)
	cursorLine := m.getFlatLinePosition(m.cursor)

	// Center cursor in viewport with reduced scrolloff (1/4 viewport instead of 1/2)
	scrolloff := contentLines / 4
	targetScrollLine := cursorLine - scrolloff
	if targetScrollLine < 0 {
		targetScrollLine = 0
	}

	// Clamp to max scroll to keep cursor visible within viewport
	totalLines := m.getTotalFlatLines()
	maxScroll := totalLines - contentLines + scrolloff
	if maxScroll > 0 && targetScrollLine > maxScroll {
		targetScrollLine = maxScroll
	}

	m.scroll = targetScrollLine
}

// findNodeForLine finds the flatNodes index for a given line position
func (m *LensDashboardModel) findNodeForLine(targetLine int) int {
	if len(m.flatNodes) == 0 || targetLine <= 0 {
		return 0
	}

	linePos := 0
	lastStatus := ""
	for i, fn := range m.flatNodes {
		if fn.Status != lastStatus {
			linePos++ // status header
			lastStatus = fn.Status
		}
		if linePos >= targetLine {
			return i
		}
		linePos++ // node line
	}
	return len(m.flatNodes) - 1
}

// ensureCenteredVisible adjusts scroll to keep cursor visible in centered mode
func (m *LensDashboardModel) ensureCenteredVisible() {
	if m.egoNode == nil {
		return
	}

	vp := m.calculateViewport()
	// Use contentLines to match renderCenteredView offset (subtracts 2)
	contentLines := vp.ContentHeight - 2
	if contentLines < 5 {
		contentLines = 5
	}

	// Center cursor in viewport with reduced scrolloff (1/4 viewport instead of 1/2)
	scrolloff := contentLines / 4
	targetScroll := m.cursor - scrolloff
	if targetScroll < 0 {
		targetScroll = 0
	}

	// Clamp to max scroll to keep cursor visible within viewport
	// Total navigable items: upstream nodes + ego node + downstream nodes
	totalItems := len(m.upstreamNodes) + 1 + len(m.flatNodes)
	maxScroll := totalItems - contentLines + scrolloff
	if maxScroll > 0 && targetScroll > maxScroll {
		targetScroll = maxScroll
	}

	m.scroll = targetScroll
}

// NextSection jumps to next status group
func (m *LensDashboardModel) NextSection() {
	if len(m.flatNodes) == 0 {
		return
	}

	currentStatus := m.flatNodes[m.cursor].Status
	for i := m.cursor + 1; i < len(m.flatNodes); i++ {
		if m.flatNodes[i].Status != currentStatus {
			m.cursor = i
			m.selectedIssueID = m.flatNodes[m.cursor].Node.Issue.ID
			m.ensureVisible()
			return
		}
	}
}

// PrevSection jumps to previous status group
func (m *LensDashboardModel) PrevSection() {
	if len(m.flatNodes) == 0 {
		return
	}

	currentStatus := m.flatNodes[m.cursor].Status

	// Find start of current section
	sectionStart := m.cursor
	for sectionStart > 0 && m.flatNodes[sectionStart-1].Status == currentStatus {
		sectionStart--
	}

	// If at start of section, go to previous section
	if m.cursor == sectionStart && sectionStart > 0 {
		m.cursor = sectionStart - 1
		m.selectedIssueID = m.flatNodes[m.cursor].Node.Issue.ID
		m.ensureVisible()
	} else {
		// Go to start of current section
		m.cursor = sectionStart
		m.selectedIssueID = m.flatNodes[m.cursor].Node.Issue.ID
		m.ensureVisible()
	}
}

// SelectedIssueID returns the ID of the currently selected issue
func (m *LensDashboardModel) SelectedIssueID() string {
	return m.selectedIssueID
}

// LabelName returns the current label name
func (m *LensDashboardModel) LabelName() string {
	return m.labelName
}

// IssueCount returns the total number of issues
func (m *LensDashboardModel) IssueCount() int {
	return m.totalCount
}


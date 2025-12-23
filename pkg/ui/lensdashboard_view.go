package ui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

// ══════════════════════════════════════════════════════════════════════════════
// VIEW & RENDERING - Main view function and all render helpers
// Extracted from lensdashboard.go for maintainability
// ══════════════════════════════════════════════════════════════════════════════

// View renders the dashboard
func (m *LensDashboardModel) View() string {
	// Use split view for wide terminals
	if m.splitViewMode {
		return m.renderSplitView()
	}

	t := m.theme

	var lines []string

	contentWidth := m.width - 4
	if contentWidth < 40 {
		contentWidth = 40
	}

	// Header
	headerStyle := t.Renderer.NewStyle().
		Foreground(t.Primary).
		Bold(true)

	modeIcon := "🔭 " // Default lens icon for label mode
	switch m.viewMode {
	case "epic":
		modeIcon = "◈ " // Epic icon
	case "bead":
		modeIcon = "◇ " // Bead icon
	}

	// Progress bar
	progress := 0.0
	if m.totalCount > 0 {
		progress = float64(m.closedCount) / float64(m.totalCount)
	}
	progressBar := m.renderProgressBar(progress, 10)
	progressText := fmt.Sprintf("%d/%d done", m.closedCount, m.totalCount)

	header := fmt.Sprintf("%s%s", modeIcon, m.labelName)
	headerLine := headerStyle.Render(header) + "  " + progressBar + " " + progressText
	lines = append(lines, headerLine)

	// Stats line
	statsStyle := t.Renderer.NewStyle().Foreground(t.Subtext)
	depthStyle := t.Renderer.NewStyle().Foreground(t.InProgress).Bold(true)

	statsLine := fmt.Sprintf("%d in lens  %d context  Depth: [%s]",
		m.primaryCount, m.contextCount,
		depthStyle.Render(m.dependencyDepth.String()))
	lines = append(lines, statsStyle.Render(statsLine))

	// Status summary
	readyStyle := t.Renderer.NewStyle().Foreground(t.Open)
	blockedStyle := t.Renderer.NewStyle().Foreground(t.Blocked)
	inProgStyle := t.Renderer.NewStyle().Foreground(t.InProgress)
	summaryLine := fmt.Sprintf("%s ready  %s blocked  %s in progress",
		readyStyle.Render(fmt.Sprintf("%d", m.readyCount)),
		blockedStyle.Render(fmt.Sprintf("%d", m.blockedCount)),
		inProgStyle.Render(fmt.Sprintf("%d", m.totalCount-m.readyCount-m.blockedCount-m.closedCount)))
	lines = append(lines, statsStyle.Render(summaryLine))

	// Scope indicator (if scope is active)
	if len(m.scopeLabels) > 0 {
		scopeStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)
		modeStyle := t.Renderer.NewStyle().Foreground(t.InProgress)
		tagStyle := t.Renderer.NewStyle().Foreground(t.Secondary)

		// Build scope tags
		var tags []string
		for _, label := range m.scopeLabels {
			tags = append(tags, tagStyle.Render("["+label+"]"))
		}

		// Mode indicator
		modeIndicator := m.scopeMode.ShortString()

		scopeLine := scopeStyle.Render("Scope: ") + strings.Join(tags, " ") + "  " + modeStyle.Render(modeIndicator)
		lines = append(lines, scopeLine)
	}

	// Scope input field (inline, appears when adding scope)
	if m.showScopeInput {
		inputStyle := t.Renderer.NewStyle().Foreground(t.Primary)
		promptStyle := t.Renderer.NewStyle().Foreground(t.Secondary)
		hintStyle := t.Renderer.NewStyle().Faint(true)

		inputLine := promptStyle.Render("+ Scope: ") + inputStyle.Render(m.scopeInput) + inputStyle.Render("█")
		lines = append(lines, inputLine)

		// Show matching labels on separate line to avoid breaking layout
		if m.scopeInput != "" {
			query := strings.ToLower(m.scopeInput)
			var matches []string
			for _, label := range m.GetAvailableScopeLabels() {
				if strings.Contains(strings.ToLower(label), query) {
					matches = append(matches, label)
					if len(matches) >= 5 {
						break
					}
				}
			}
			if len(matches) > 0 {
				// Truncate matches to fit width
				matchText := strings.Join(matches, ", ")
				maxLen := contentWidth - 6
				if maxLen > 0 && len(matchText) > maxLen {
					matchText = matchText[:maxLen-3] + "..."
				}
				lines = append(lines, hintStyle.Render("  → "+matchText))
			}
		}
	}

	lines = append(lines, "")

	// Calculate visible area using viewport config
	vp := m.calculateViewport()
	visibleLines := vp.ContentHeight

	// Render based on view type
	var contentLines []string
	if m.viewType == ViewTypeGrouped && len(m.groupedSections) > 0 {
		// Render grouped view
		contentLines = m.renderGroupedView(contentWidth, visibleLines, statsStyle)
	} else if m.viewType == ViewTypeWorkstream && len(m.workstreams) > 1 {
		// Render workstream view
		contentLines = m.renderWorkstreamView(contentWidth, visibleLines, statsStyle)
	} else if (m.viewMode == "epic" || m.viewMode == "bead") && m.egoNode != nil {
		// Render ego-centered view for epic/bead modes
		contentLines = m.renderCenteredView(contentWidth, visibleLines, statsStyle)
	} else {
		// Render flat tree view
		contentLines = m.renderFlatView(contentWidth, visibleLines, statsStyle)
	}

	// Truncate content to exactly ContentHeight for fixed footer positioning
	if len(contentLines) > visibleLines {
		contentLines = contentLines[:visibleLines]
	}
	// Pad if needed
	for len(contentLines) < visibleLines {
		contentLines = append(contentLines, "")
	}
	lines = append(lines, contentLines...)

	// Footer
	lines = append(lines, "")
	footerStyle := t.Renderer.NewStyle().Foreground(t.Subtext).Italic(true)

	// Show view type indicator and toggle hint
	viewIndicator := "[flat view]"
	toggleHint := "w: streams"
	navHint := "n/N: section"
	enterHint := "enter: focus"
	treeHint := ""
	if m.viewMode == "epic" || m.viewMode == "bead" {
		viewIndicator = "[centered]"
	}
	if m.viewType == ViewTypeWorkstream && len(m.workstreams) > 1 {
		viewIndicator = fmt.Sprintf("[%d streams]", m.workstreamCount)
		toggleHint = "w: flat"
		navHint = "n/N: stream"
		enterHint = "enter: expand"
		if m.wsTreeView {
			treeHint = " • T: list • d: depth"
		} else {
			treeHint = " • T: tree • d: depth"
		}
	}

	lines = append(lines, footerStyle.Render(fmt.Sprintf("%s • j/k: nav • g/G: top/bottom • %s • %s • %s%s • esc: back", viewIndicator, toggleHint, navHint, enterHint, treeHint)))

	return strings.Join(lines, "\n")
}

// renderFlatView renders the flat tree view using line-based scrolling
func (m *LensDashboardModel) renderFlatView(contentWidth, visibleLines int, statsStyle lipgloss.Style) []string {
	t := m.theme

	if len(m.flatNodes) == 0 {
		emptyStyle := t.Renderer.NewStyle().Foreground(t.Subtext).Italic(true)
		return []string{emptyStyle.Render("  No issues found")}
	}

	// Build ALL lines first (including status headers)
	var allLines []string
	lastStatus := ""
	for i, fn := range m.flatNodes {
		// Add status header when status changes
		if fn.Status != lastStatus {
			statusHeader := m.renderStatusHeader(fn.Status)
			allLines = append(allLines, statusHeader)
			lastStatus = fn.Status
		}

		isSelected := i == m.cursor
		line := m.renderTreeNode(fn, isSelected, contentWidth)
		allLines = append(allLines, line)
	}

	// Add header lines (matching workstream/grouped views pattern)
	var lines []string
	viewModeStr := fmt.Sprintf("tree [depth:%s]", m.dependencyDepth.String())
	lines = append(lines, statsStyle.Render(fmt.Sprintf("  %d issues (%s):", len(m.flatNodes), viewModeStr)))
	lines = append(lines, "")

	// Calculate visible window (2 lines for header above, rest for content)
	contentLines := visibleLines - 2
	if contentLines < 1 {
		contentLines = 1
	}

	// m.scroll is already a LINE position (set by ensureVisible)
	scrollLine := m.scroll
	if scrollLine < 0 {
		scrollLine = 0
	}

	// Add visible content lines
	if scrollLine < len(allLines) {
		endLine := scrollLine + contentLines
		if endLine > len(allLines) {
			endLine = len(allLines)
		}
		for i := scrollLine; i < endLine; i++ {
			lines = append(lines, allLines[i])
		}
	}

	// Pad to exactly visibleLines
	for len(lines) < visibleLines {
		lines = append(lines, "")
	}

	return lines
}

// renderCenteredView renders the ego-centered layout for epic/bead modes:
// Upstream blockers → Entry point (center) → Downstream descendants
func (m *LensDashboardModel) renderCenteredView(contentWidth, visibleLines int, statsStyle lipgloss.Style) []string {
	t := m.theme

	upstreamLen := len(m.upstreamNodes)

	// Elegant section header styles with gradient colors
	upstreamIconStyle := t.Renderer.NewStyle().Foreground(t.Blocked).Bold(true)
	downstreamIconStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)
	sectionLabelStyle := t.Renderer.NewStyle().Foreground(t.Subtext).Bold(true)
	separatorStyle := t.Renderer.NewStyle().Foreground(t.Subtext)
	boxStyle := t.Renderer.NewStyle().Foreground(t.Primary)

	// Helper to render elegant header with decorative line
	renderSectionHeader := func(icon, iconStyled, label string, lineWidth int) string {
		lineLen := lineWidth - len(icon) - len(label) - 2
		if lineLen < 5 {
			lineLen = 5
		}
		line := strings.Repeat("─", lineLen)
		return iconStyled + " " + sectionLabelStyle.Render(label) + " " + separatorStyle.Render(line)
	}

	// Build ALL lines first (including headers, decorations, etc.)
	var allLines []string
	nodeIdx := 0

	// === UPSTREAM SECTION (blockers) ===
	if len(m.upstreamNodes) > 0 {
		header := renderSectionHeader("◇", upstreamIconStyle.Render("◇"), "BLOCKERS", min(contentWidth, 50))
		allLines = append(allLines, header)

		for _, fn := range m.upstreamNodes {
			isSelected := nodeIdx == m.cursor
			line := m.renderCenteredNode(fn, isSelected, contentWidth, -1)
			allLines = append(allLines, line)
			nodeIdx++
		}
		allLines = append(allLines, "")
	}

	// === CENTER SECTION (entry point/ego) with elegant top/bottom lines ===
	if m.egoNode != nil {
		lineWidth := min(contentWidth-4, 50)
		topLine := boxStyle.Render("═" + strings.Repeat("═", lineWidth) + "═")
		bottomLine := boxStyle.Render("─" + strings.Repeat("─", lineWidth) + "─")

		allLines = append(allLines, topLine)

		egoNodeIdx := upstreamLen
		isSelected := m.cursor == egoNodeIdx
		line := m.renderEgoNodeLine(*m.egoNode, isSelected, contentWidth)
		allLines = append(allLines, line)

		allLines = append(allLines, bottomLine)
		allLines = append(allLines, "")
		nodeIdx = upstreamLen + 1
	}

	// === DOWNSTREAM SECTION (children/dependents) ===
	if len(m.flatNodes) > 0 {
		header := renderSectionHeader("◆", downstreamIconStyle.Render("◆"), "DESCENDANTS", min(contentWidth, 50))
		allLines = append(allLines, header)

		lastStatus := ""
		for i, fn := range m.flatNodes {
			cursorPos := upstreamLen + 1 + i

			// Show status header when status changes
			if fn.Status != lastStatus {
				statusHeader := m.renderStatusHeader(fn.Status)
				allLines = append(allLines, statusHeader)
				lastStatus = fn.Status
			}

			isSelected := cursorPos == m.cursor
			line := m.renderCenteredNode(fn, isSelected, contentWidth, fn.Node.RelativeDepth)
			allLines = append(allLines, line)
		}
	} else if len(m.upstreamNodes) == 0 {
		emptyStyle := t.Renderer.NewStyle().Foreground(t.Subtext).Italic(true)
		allLines = append(allLines, emptyStyle.Render("  No descendants found"))
	}

	// Now apply scroll and buffer (matching flat view pattern)
	var lines []string
	contentLines := visibleLines - 2
	if contentLines < 1 {
		contentLines = 1
	}

	scrollLine := m.scroll
	if scrollLine < 0 {
		scrollLine = 0
	}

	// Add visible content lines
	if scrollLine < len(allLines) {
		endLine := scrollLine + contentLines
		if endLine > len(allLines) {
			endLine = len(allLines)
		}
		for i := scrollLine; i < endLine; i++ {
			lines = append(lines, allLines[i])
		}
	}

	// Pad to exactly visibleLines
	for len(lines) < visibleLines {
		lines = append(lines, "")
	}

	return lines
}

// renderEgoNodeLine renders the center/ego node with prominent styling
// No [CENTER] badge - the framed box already indicates this is the center
func (m *LensDashboardModel) renderEgoNodeLine(fn LensFlatNode, isSelected bool, maxWidth int) string {
	t := m.theme
	node := fn.Node

	// Selection indicator
	selectPrefix := "  "
	if isSelected {
		selectPrefix = "▸ "
	}

	// Issue ID and title with prominent styling
	idStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)
	titleStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)

	// Calculate max title length
	prefixLen := len(selectPrefix) + len(node.Issue.ID) + 2
	maxTitleLen := maxWidth - prefixLen
	if maxTitleLen < 15 {
		maxTitleLen = 15
	}
	title := truncateRunesHelper(node.Issue.Title, maxTitleLen, "…")

	// Status indicator (only show if blocker not already visible in tree)
	statusSuffix := ""
	if fn.Status == "blocked" && len(fn.BlockedBy) > 0 && !fn.BlockerInTree {
		blockerStyle := t.Renderer.NewStyle().Foreground(t.Blocked)
		blockerText := fn.BlockedBy[0]
		if len(fn.BlockedBy) > 1 {
			blockerText += fmt.Sprintf(" +%d", len(fn.BlockedBy)-1)
		}
		statusSuffix = blockerStyle.Render(" ◄ " + blockerText)
	}

	return fmt.Sprintf("%s%s %s%s",
		selectPrefix,
		idStyle.Render(node.Issue.ID),
		titleStyle.Render(title),
		statusSuffix)
}

// renderCenteredNode renders a node with elegant depth-based styling
func (m *LensDashboardModel) renderCenteredNode(fn LensFlatNode, isSelected bool, maxWidth int, relDepth int) string {
	t := m.theme
	node := fn.Node

	// Selection indicator
	selectPrefix := "  "
	if isSelected {
		selectPrefix = "▸ "
	}

	// Tree prefix (styled dimmer)
	treePrefix := ""
	if fn.TreePrefix != "" {
		treePrefix = t.Renderer.NewStyle().Foreground(t.Subtext).Render(fn.TreePrefix) + " "
	}

	// Gradient depth coloring: deeper nodes fade toward subtext
	// Depth 1 = full color, Depth 2+ = progressively dimmer
	idStyle := t.Renderer.NewStyle()
	titleStyle := t.Renderer.NewStyle()

	absDepth := relDepth
	if absDepth < 0 {
		absDepth = -absDepth
	}

	if isSelected {
		idStyle = idStyle.Foreground(t.Primary).Bold(true)
		titleStyle = titleStyle.Foreground(t.Primary).Bold(true)
	} else if node.IsUpstream {
		idStyle = idStyle.Foreground(t.Blocked)
		titleStyle = titleStyle.Foreground(t.Blocked)
	} else if absDepth >= 3 {
		// Deep nodes (depth 3+): dimmer subtext color
		idStyle = idStyle.Foreground(t.Subtext)
		titleStyle = titleStyle.Foreground(t.Subtext)
	} else if absDepth == 2 {
		// Medium depth: secondary/subdued color
		if node.IsPrimary {
			idStyle = idStyle.Foreground(t.Secondary)
			titleStyle = titleStyle.Foreground(t.Secondary)
		} else {
			idStyle = idStyle.Foreground(t.Subtext)
			titleStyle = titleStyle.Foreground(t.Subtext)
		}
	} else if !node.IsPrimary {
		// Depth 1, context node
		idStyle = idStyle.Foreground(t.Subtext)
		titleStyle = titleStyle.Foreground(t.Subtext)
	} else {
		// Depth 1, primary node: full brightness
		idStyle = idStyle.Foreground(t.Base.GetForeground())
		titleStyle = titleStyle.Foreground(t.Base.GetForeground())
	}

	// Calculate max title length
	prefixLen := len(selectPrefix) + len(fn.TreePrefix) + len(node.Issue.ID) + 2
	maxTitleLen := maxWidth - prefixLen
	if maxTitleLen < 15 {
		maxTitleLen = 15
	}
	title := truncateRunesHelper(node.Issue.Title, maxTitleLen, "…")

	// Status indicator for blocked items (only show if blocker not already visible in tree)
	statusSuffix := ""
	if fn.Status == "blocked" && len(fn.BlockedBy) > 0 && !fn.BlockerInTree {
		blockerStyle := t.Renderer.NewStyle().Foreground(t.Blocked)
		blockerText := fn.BlockedBy[0]
		if len(fn.BlockedBy) > 1 {
			blockerText += fmt.Sprintf(" +%d", len(fn.BlockedBy)-1)
		}
		statusSuffix = blockerStyle.Render(" ◄ " + blockerText)
	}

	return fmt.Sprintf("%s%s%s %s%s",
		selectPrefix,
		treePrefix,
		idStyle.Render(node.Issue.ID),
		titleStyle.Render(title),
		statusSuffix)
}

// renderWorkstreamView renders issues grouped by workstream
func (m *LensDashboardModel) renderWorkstreamView(contentWidth, visibleLines int, statsStyle lipgloss.Style) []string {
	t := m.theme
	var allLines []string

	wsHeaderStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)
	wsHeaderSelectedStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true).Background(t.Highlight)
	wsSubStyle := t.Renderer.NewStyle().Foreground(t.Subtext)
	issueStyle := t.Renderer.NewStyle()
	issueSelectedStyle := t.Renderer.NewStyle().Bold(true)
	readyStyle := t.Renderer.NewStyle().Foreground(t.Open)
	blockedStyle := t.Renderer.NewStyle().Foreground(t.Blocked)
	closedStyle := t.Renderer.NewStyle().Foreground(t.Closed)
	inProgStyle := t.Renderer.NewStyle().Foreground(t.InProgress)

	// Build all lines first, then apply scroll
	for wsIdx, ws := range m.workstreams {
		// Check if this workstream header is selected
		isHeaderSelected := wsIdx == m.wsCursor && m.wsIssueCursor < 0
		isExpanded := m.wsExpanded[wsIdx]

		// Workstream header with progress
		progressPct := int(ws.Progress * 100)
		progressBar := m.renderMiniProgressBar(ws.Progress, 8)

		// Status counts
		statusCounts := fmt.Sprintf("○%d ●%d ◈%d ✓%d",
			ws.ReadyCount, ws.InProgressCount, ws.BlockedCount, ws.ClosedCount)

		// Expand/collapse indicator
		expandIcon := "▶"
		if isExpanded {
			expandIcon = "▼"
		}

		// Selection indicator
		selectPrefix := "  "
		headerStyle := wsHeaderStyle
		if isHeaderSelected {
			selectPrefix = "▸ "
			headerStyle = wsHeaderSelectedStyle
		}

		// Show sub-workstream indicator if present
		subWsIndicator := ""
		if m.wsSubdivided && wsIdx < len(m.workstreamPtrs) && m.workstreamPtrs[wsIdx] != nil {
			subCount := len(m.workstreamPtrs[wsIdx].SubWorkstreams)
			if subCount > 0 {
				subWsIndicator = fmt.Sprintf(" [%d sub]", subCount)
			}
		}

		wsLine := fmt.Sprintf("%s%s %s %s %d%% %s%s",
			selectPrefix,
			expandIcon,
			headerStyle.Render(ws.Name),
			progressBar,
			progressPct,
			wsSubStyle.Render(statusCounts),
			wsSubStyle.Render(subWsIndicator))
		allLines = append(allLines, wsLine)

		// Render sub-workstreams when subdivision is active and expanded
		if m.wsSubdivided && isExpanded && wsIdx < len(m.workstreamPtrs) && m.workstreamPtrs[wsIdx] != nil {
			for subIdx, subWs := range m.workstreamPtrs[wsIdx].SubWorkstreams {
				if subWs == nil {
					continue
				}
				subProgress := int(subWs.Progress * 100)
				subStatusCounts := fmt.Sprintf("○%d ●%d ◈%d ✓%d",
					subWs.ReadyCount, subWs.InProgressCount, subWs.BlockedCount, subWs.ClosedCount)
				subLine := fmt.Sprintf("     %s (%d%%) %s",
					wsSubStyle.Render("├─ "+subWs.Name),
					subProgress,
					wsSubStyle.Render(subStatusCounts))
				_ = subIdx // Will be used for sub-workstream selection in future
				allLines = append(allLines, subLine)
			}
		}

		// Render issues - either as tree or flat list
		if m.wsTreeView && isExpanded {
			// Tree view for expanded workstreams
			wsCopy := ws
			treeRoots := m.buildWorkstreamTree(&wsCopy)
			flatNodes := m.flattenWSTree(treeRoots)

			for i, fn := range flatNodes {
				// Check if this issue is selected
				isIssueSelected := wsIdx == m.wsCursor && i == m.wsIssueCursor
				isEpicEntry := m.isEntryEpic(fn.Node.Issue.ID)

				// Format issue line with tree prefix
				var statusIcon string
				var style lipgloss.Style
				if isEpicEntry {
					// Entry epic gets distinct diamond icon
					statusIcon = "◆"
					style = t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)
				} else {
					switch fn.Node.Issue.Status {
					case model.StatusClosed:
						statusIcon = "✓"
						style = closedStyle
					case model.StatusBlocked:
						statusIcon = "◈"
						style = blockedStyle
					case model.StatusInProgress:
						statusIcon = "●"
						style = inProgStyle
					default:
						statusIcon = "○"
						style = readyStyle
					}
				}

				// Selection indicator
				issuePrefix := "    "
				idStyle := issueStyle
				titleStyle := issueStyle
				if isEpicEntry {
					// Entry epic: always bold primary
					idStyle = issueSelectedStyle.Foreground(t.Primary)
					titleStyle = issueSelectedStyle.Foreground(t.Primary)
				}
				if isIssueSelected {
					issuePrefix = "  ▸ "
					idStyle = issueSelectedStyle.Foreground(t.Primary)
					titleStyle = issueSelectedStyle
				}

				// Tree prefix
				treePrefix := ""
				if fn.TreePrefix != "" {
					treePrefix = wsSubStyle.Render(fn.TreePrefix) + " "
				}

				title := truncateRunesHelper(fn.Node.Issue.Title, contentWidth-25-len(fn.TreePrefix), "…")
				epicBadge := ""
				if isEpicEntry {
					epicBadge = wsSubStyle.Render(" [EPIC]")
				}
				issueLine := fmt.Sprintf("%s%s %s%s %s%s",
					issuePrefix,
					style.Render(statusIcon),
					treePrefix,
					idStyle.Render(fn.Node.Issue.ID),
					titleStyle.Render(title),
					epicBadge)
				allLines = append(allLines, issueLine)
			}
		} else {
			// Flat list view
			maxIssues := m.getVisibleIssueCount(wsIdx)

			for i, issue := range ws.Issues {
				if i >= maxIssues {
					break
				}

				// Check if this issue is selected
				isIssueSelected := wsIdx == m.wsCursor && i == m.wsIssueCursor
				isEpicEntry := m.isEntryEpic(issue.ID)

				// Format issue line
				var statusIcon string
				var style lipgloss.Style
				if isEpicEntry {
					// Entry epic gets distinct diamond icon
					statusIcon = "◆"
					style = t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)
				} else {
					switch issue.Status {
					case model.StatusClosed:
						statusIcon = "✓"
						style = closedStyle
					case model.StatusBlocked:
						statusIcon = "◈"
						style = blockedStyle
					case model.StatusInProgress:
						statusIcon = "●"
						style = inProgStyle
					default:
						statusIcon = "○"
						style = readyStyle
					}
				}

				// Selection indicator
				issuePrefix := "    "
				idStyle := issueStyle
				titleStyle := issueStyle
				if isEpicEntry {
					// Entry epic: always bold primary
					idStyle = issueSelectedStyle.Foreground(t.Primary)
					titleStyle = issueSelectedStyle.Foreground(t.Primary)
				}
				if isIssueSelected {
					issuePrefix = "  ▸ "
					idStyle = issueSelectedStyle.Foreground(t.Primary)
					titleStyle = issueSelectedStyle
				}

				title := truncateRunesHelper(issue.Title, contentWidth-20, "…")
				epicBadge := ""
				if isEpicEntry {
					epicBadge = wsSubStyle.Render(" [EPIC]")
				}
				issueLine := fmt.Sprintf("%s%s %s %s%s",
					issuePrefix,
					style.Render(statusIcon),
					idStyle.Render(issue.ID),
					titleStyle.Render(title),
					epicBadge)
				allLines = append(allLines, issueLine)
			}

			// Show "+N more" hint for collapsed workstreams
			if !isExpanded && len(ws.Issues) > 3 {
				remaining := len(ws.Issues) - 3
				allLines = append(allLines, wsSubStyle.Render(fmt.Sprintf("        ... +%d more (enter to expand)", remaining)))
			}
		}

		allLines = append(allLines, "") // Empty line between workstreams
	}

	// Apply scroll offset
	var lines []string
	viewModeStr := "list"
	if m.wsTreeView {
		viewModeStr = fmt.Sprintf("tree [depth:%s]", m.dependencyDepth.String())
	}
	if m.wsSubdivided {
		viewModeStr += " [subdivided]"
	}
	lines = append(lines, wsSubStyle.Render(fmt.Sprintf("  %d workstreams (%s):", len(m.workstreams), viewModeStr)))
	lines = append(lines, "")

	// Calculate visible window (2 lines for header above, rest for content)
	totalLines := len(allLines)
	contentLines := visibleLines - 2

	startIdx := m.wsScroll
	if startIdx < 0 {
		startIdx = 0
	}

	// Add visible content lines
	if startIdx < totalLines {
		endIdx := startIdx + contentLines
		if endIdx > totalLines {
			endIdx = totalLines
		}
		for i := startIdx; i < endIdx; i++ {
			lines = append(lines, allLines[i])
		}
	}

	// Pad with empty lines to allow empty space after list end
	// This ensures the last items can be centered with empty space below
	for len(lines) < visibleLines {
		lines = append(lines, "")
	}

	return lines
}

// renderMiniProgressBar renders a small progress bar
func (m *LensDashboardModel) renderMiniProgressBar(progress float64, width int) string {
	t := m.theme

	filled := int(progress * float64(width))
	if filled > width {
		filled = width
	}

	var barColor lipgloss.AdaptiveColor
	if progress >= 1.0 {
		barColor = t.Closed
	} else if progress >= 0.5 {
		barColor = t.InProgress
	} else {
		barColor = t.Open
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	return t.Renderer.NewStyle().Foreground(barColor).Render("[" + bar + "]")
}

// renderGroupedView renders the grouped view with workstream-like styling
func (m *LensDashboardModel) renderGroupedView(contentWidth, visibleLines int, statsStyle lipgloss.Style) []string {
	t := m.theme
	var allLines []string

	// Same styles as workstream view for consistency
	groupHeaderStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)
	groupHeaderSelectedStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true).Background(t.Highlight)
	subStyle := t.Renderer.NewStyle().Foreground(t.Subtext)
	issueStyle := t.Renderer.NewStyle()
	issueSelectedStyle := t.Renderer.NewStyle().Bold(true)
	readyStyle := t.Renderer.NewStyle().Foreground(t.Open)
	blockedStyle := t.Renderer.NewStyle().Foreground(t.Blocked)
	closedStyle := t.Renderer.NewStyle().Foreground(t.Closed)
	inProgStyle := t.Renderer.NewStyle().Foreground(t.InProgress)

	// Build all lines first, then apply scroll
	for gIdx, group := range m.groupedSections {
		// Check if this group header is selected (on group header, not in sub-group)
		isHeaderSelected := gIdx == m.groupedCursor && m.groupedSubCursor < 0 && m.groupedIssueCursor < 0
		isExpanded := m.groupedExpanded[gIdx]

		// Group header with progress
		progressPct := int(group.Progress * 100)
		progressBar := m.renderMiniProgressBar(group.Progress, 8)

		// Status counts
		statusCounts := fmt.Sprintf("○%d ●%d ◈%d ✓%d",
			group.ReadyCount, group.InProgressCount, group.BlockedCount, group.ClosedCount)

		// Expand/collapse indicator
		expandIcon := "▶"
		if isExpanded {
			expandIcon = "▼"
		}

		// Selection indicator
		selectPrefix := "  "
		headerStyle := groupHeaderStyle
		if isHeaderSelected {
			selectPrefix = "▸ "
			headerStyle = groupHeaderSelectedStyle
		}

		// Sub-group indicator
		subGroupIndicator := ""
		if len(group.SubWorkstreams) > 0 {
			subGroupIndicator = fmt.Sprintf(" [%d sub]", len(group.SubWorkstreams))
		}

		groupLine := fmt.Sprintf("%s%s %s %s %d%% %s (%d)%s",
			selectPrefix,
			expandIcon,
			headerStyle.Render(group.Name),
			progressBar,
			progressPct,
			subStyle.Render(statusCounts),
			len(group.Issues),
			subStyle.Render(subGroupIndicator))
		allLines = append(allLines, groupLine)

		// Render sub-groups if expanded and present
		if isExpanded && len(group.SubWorkstreams) > 0 {
			for subIdx, subGroup := range group.SubWorkstreams {
				if subGroup == nil {
					continue
				}
				subProgress := int(subGroup.Progress * 100)
				subStatusCounts := fmt.Sprintf("○%d ●%d ◈%d ✓%d",
					subGroup.ReadyCount, subGroup.InProgressCount, subGroup.BlockedCount, subGroup.ClosedCount)

				// Check sub-group expansion
				subExpanded := m.groupedSubExpanded[gIdx] != nil && m.groupedSubExpanded[gIdx][subIdx]
				subExpandIcon := "▶"
				if subExpanded {
					subExpandIcon = "▼"
				}

				// Check if this sub-group header is selected
				isSubHeaderSelected := gIdx == m.groupedCursor && subIdx == m.groupedSubCursor && m.groupedIssueCursor < 0
				subSelectPrefix := "     "
				subHeaderStyle := subStyle
				if isSubHeaderSelected {
					subSelectPrefix = "   ▸ "
					subHeaderStyle = groupHeaderSelectedStyle
				}

				subLine := fmt.Sprintf("%s%s %s (%d%%) %s (%d)",
					subSelectPrefix,
					subExpandIcon,
					subHeaderStyle.Render(subGroup.Name),
					subProgress,
					subStyle.Render(subStatusCounts),
					len(subGroup.Issues))
				allLines = append(allLines, subLine)

				// Render sub-group issues if expanded
				if subExpanded {
					if m.groupedTreeView {
						// Tree view for sub-group issues
						subGroupCopy := *subGroup
						treeRoots := m.buildWorkstreamTree(&subGroupCopy)
						flatNodes := m.flattenWSTree(treeRoots)
						for i, fn := range flatNodes {
							isIssueSelected := gIdx == m.groupedCursor && subIdx == m.groupedSubCursor && i == m.groupedIssueCursor
							allLines = append(allLines, m.renderGroupedTreeIssue(fn, isIssueSelected, contentWidth, "        ", issueStyle, issueSelectedStyle, readyStyle, blockedStyle, closedStyle, inProgStyle, subStyle))
						}
					} else {
						for i, issue := range subGroup.Issues {
							isIssueSelected := gIdx == m.groupedCursor && subIdx == m.groupedSubCursor && i == m.groupedIssueCursor
							allLines = append(allLines, m.renderGroupedIssue(issue, isIssueSelected, contentWidth, "        ", issueStyle, issueSelectedStyle, readyStyle, blockedStyle, closedStyle, inProgStyle, subStyle))
						}
					}
				}
			}
		}

		// Render issues when expanded (only if no sub-groups, otherwise they're shown in sub-groups)
		if isExpanded && len(group.SubWorkstreams) == 0 {
			if m.groupedTreeView {
				// Tree view for group issues
				groupCopy := group
				treeRoots := m.buildWorkstreamTree(&groupCopy)
				flatNodes := m.flattenWSTree(treeRoots)
				for i, fn := range flatNodes {
					isIssueSelected := gIdx == m.groupedCursor && m.groupedSubCursor < 0 && i == m.groupedIssueCursor
					allLines = append(allLines, m.renderGroupedTreeIssue(fn, isIssueSelected, contentWidth, "    ", issueStyle, issueSelectedStyle, readyStyle, blockedStyle, closedStyle, inProgStyle, subStyle))
				}
			} else {
				maxIssues := len(group.Issues)
				for i := 0; i < maxIssues && i < len(group.Issues); i++ {
					issue := group.Issues[i]
					isIssueSelected := gIdx == m.groupedCursor && m.groupedSubCursor < 0 && i == m.groupedIssueCursor
					allLines = append(allLines, m.renderGroupedIssue(issue, isIssueSelected, contentWidth, "    ", issueStyle, issueSelectedStyle, readyStyle, blockedStyle, closedStyle, inProgStyle, subStyle))
				}
			}
		}

		allLines = append(allLines, "") // Empty line between groups
	}

	// Apply scroll offset
	var lines []string
	viewModeStr := "list"
	if m.groupedTreeView {
		viewModeStr = fmt.Sprintf("tree [depth:%s]", m.dependencyDepth.String())
	}
	lines = append(lines, subStyle.Render(fmt.Sprintf("  Grouped by %s (%d groups, %s):", m.groupByMode.String(), len(m.groupedSections), viewModeStr)))
	lines = append(lines, "")

	// Calculate visible window (2 lines for header above, rest for content)
	totalLines := len(allLines)
	contentLines := visibleLines - 2

	startIdx := m.groupedScroll
	if startIdx < 0 {
		startIdx = 0
	}

	// Add visible content lines
	if startIdx < totalLines {
		endIdx := startIdx + contentLines
		if endIdx > totalLines {
			endIdx = totalLines
		}
		for i := startIdx; i < endIdx; i++ {
			lines = append(lines, allLines[i])
		}
	}

	// Pad with empty lines to allow empty space after list end
	// This ensures the last items can be centered with empty space below
	for len(lines) < visibleLines {
		lines = append(lines, "")
	}

	return lines
}

// renderGroupedIssue renders a single issue in grouped view
func (m *LensDashboardModel) renderGroupedIssue(issue model.Issue, isSelected bool, contentWidth int, indent string, issueStyle, issueSelectedStyle, readyStyle, blockedStyle, closedStyle, inProgStyle, subStyle lipgloss.Style) string {
	t := m.theme

	// Determine status icon and style
	var statusIcon string
	var style lipgloss.Style
	switch issue.Status {
	case model.StatusClosed:
		statusIcon = "✓"
		style = closedStyle
	case model.StatusBlocked:
		statusIcon = "◈"
		style = blockedStyle
	case model.StatusInProgress:
		statusIcon = "●"
		style = inProgStyle
	default:
		// Check if blocked by dependencies
		if m.isIssueBlockedByDeps(issue.ID) {
			statusIcon = "◈"
			style = blockedStyle
		} else {
			statusIcon = "○"
			style = readyStyle
		}
	}

	// Selection indicator
	issuePrefix := indent
	idStyle := issueStyle
	titleStyle := issueStyle
	if isSelected {
		issuePrefix = indent[:len(indent)-2] + "▸ "
		idStyle = issueSelectedStyle.Foreground(t.Primary)
		titleStyle = issueSelectedStyle
	}

	title := truncateRunesHelper(issue.Title, contentWidth-20-len(indent), "…")
	return fmt.Sprintf("%s%s %s %s",
		issuePrefix,
		style.Render(statusIcon),
		idStyle.Render(issue.ID),
		titleStyle.Render(title))
}

// renderGroupedTreeIssue renders a single issue with tree prefix in grouped view
func (m *LensDashboardModel) renderGroupedTreeIssue(fn LensFlatNode, isSelected bool, contentWidth int, indent string, issueStyle, issueSelectedStyle, readyStyle, blockedStyle, closedStyle, inProgStyle, subStyle lipgloss.Style) string {
	t := m.theme
	issue := fn.Node.Issue

	// Determine status icon and style
	var statusIcon string
	var style lipgloss.Style
	switch issue.Status {
	case model.StatusClosed:
		statusIcon = "✓"
		style = closedStyle
	case model.StatusBlocked:
		statusIcon = "◈"
		style = blockedStyle
	case model.StatusInProgress:
		statusIcon = "●"
		style = inProgStyle
	default:
		// Check if blocked by dependencies
		if m.isIssueBlockedByDeps(issue.ID) {
			statusIcon = "◈"
			style = blockedStyle
		} else {
			statusIcon = "○"
			style = readyStyle
		}
	}

	// Build tree prefix
	treePrefix := fn.TreePrefix

	// Selection indicator
	issuePrefix := indent + treePrefix
	idStyle := issueStyle
	titleStyle := issueStyle
	if isSelected {
		// For tree view, highlight the whole line
		idStyle = issueSelectedStyle.Foreground(t.Primary)
		titleStyle = issueSelectedStyle
		// Add selection marker at the beginning
		issuePrefix = indent[:len(indent)-2] + "▸ " + treePrefix
	}

	title := truncateRunesHelper(issue.Title, contentWidth-20-len(indent)-len(treePrefix), "…")
	return fmt.Sprintf("%s%s %s %s",
		issuePrefix,
		style.Render(statusIcon),
		idStyle.Render(issue.ID),
		titleStyle.Render(title))
}

// renderStatusHeader renders a status section header with elegant dotted dividers
func (m *LensDashboardModel) renderStatusHeader(status string) string {
	t := m.theme

	var color lipgloss.AdaptiveColor
	var label string

	switch status {
	case "ready":
		color = t.Open
		label = "READY"
	case "blocked":
		color = t.Blocked
		label = "BLOCKED"
	case "in_progress":
		color = t.InProgress
		label = "IN PROGRESS"
	case "closed":
		color = t.Closed
		label = "CLOSED"
	default:
		color = t.Subtext
		label = strings.ToUpper(status)
	}

	// Elegant dotted divider: ┄ LABEL ┄┄┄┄┄┄
	labelStyle := t.Renderer.NewStyle().Foreground(color).Bold(true)
	dividerStyle := t.Renderer.NewStyle().Foreground(t.Subtext)

	dotCount := 20 - len(label)
	if dotCount < 3 {
		dotCount = 3
	}

	return dividerStyle.Render("┄ ") + labelStyle.Render(label) + " " + dividerStyle.Render(strings.Repeat("┄", dotCount))
}

// renderTreeNode renders a single tree node
func (m *LensDashboardModel) renderTreeNode(fn LensFlatNode, isSelected bool, maxWidth int) string {
	t := m.theme
	node := fn.Node

	// Selection indicator
	selectPrefix := "  "
	if isSelected {
		selectPrefix = "▸ "
	}

	// Tree prefix (styled dimmer)
	treePrefix := ""
	if fn.TreePrefix != "" {
		treePrefix = t.Renderer.NewStyle().Foreground(t.Subtext).Render(fn.TreePrefix) + " "
	}

	// Issue ID and title
	idStyle := t.Renderer.NewStyle()
	titleStyle := t.Renderer.NewStyle()

	if node.IsEntryEpic {
		// Entry epic: bold with primary color, stands out
		idStyle = idStyle.Foreground(t.Primary).Bold(true)
		titleStyle = titleStyle.Foreground(t.Primary).Bold(true)
	} else if isSelected {
		idStyle = idStyle.Foreground(t.Primary).Bold(true)
		titleStyle = titleStyle.Foreground(t.Primary).Bold(true)
	} else if !node.IsPrimary {
		idStyle = idStyle.Foreground(t.Subtext)
		titleStyle = titleStyle.Foreground(t.Subtext)
	} else {
		idStyle = idStyle.Foreground(t.Base.GetForeground())
		titleStyle = titleStyle.Foreground(t.Base.GetForeground())
	}

	// Calculate max title length (removed bullet indicator, so less prefix)
	prefixLen := len(selectPrefix) + len(fn.TreePrefix) + len(node.Issue.ID) + 2
	maxTitleLen := maxWidth - prefixLen
	if maxTitleLen < 15 {
		maxTitleLen = 15
	}
	title := truncateRunesHelper(node.Issue.Title, maxTitleLen, "…")

	// Entry epic badge
	epicBadge := ""
	if node.IsEntryEpic {
		epicBadge = t.Renderer.NewStyle().Foreground(t.Subtext).Render(" [EPIC]")
	}

	// Status indicator for blocked items (only show if blocker not already visible in tree)
	statusSuffix := ""
	if fn.Status == "blocked" && len(fn.BlockedBy) > 0 && !fn.BlockerInTree {
		blockerStyle := t.Renderer.NewStyle().Foreground(t.Blocked)
		blockerText := fn.BlockedBy[0]
		if len(fn.BlockedBy) > 1 {
			blockerText += fmt.Sprintf(" +%d", len(fn.BlockedBy)-1)
		}
		statusSuffix = blockerStyle.Render(" ◄ " + blockerText)
	}

	return fmt.Sprintf("%s%s%s %s%s%s",
		selectPrefix,
		treePrefix,
		idStyle.Render(node.Issue.ID),
		titleStyle.Render(title),
		epicBadge,
		statusSuffix)
}

func (m *LensDashboardModel) renderProgressBar(progress float64, width int) string {
	t := m.theme

	filled := int(progress * float64(width))
	if filled > width {
		filled = width
	}

	var barColor lipgloss.AdaptiveColor
	if progress >= 1.0 {
		barColor = t.Closed
	} else if progress >= 0.5 {
		barColor = t.InProgress
	} else {
		barColor = t.Open
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	return t.Renderer.NewStyle().Foreground(barColor).Render("[" + bar + "]")
}

// DumpToFile writes workstream information to a text file
func (m *LensDashboardModel) DumpToFile() (string, error) {
	filename := fmt.Sprintf("%s-dump.txt", m.labelName)

	var buf strings.Builder

	// Header
	buf.WriteString(fmt.Sprintf("Label Dashboard Dump: %s\n", m.labelName))
	buf.WriteString(fmt.Sprintf("Generated: %s\n", time.Now().Format(time.RFC3339)))
	buf.WriteString(strings.Repeat("=", 60) + "\n\n")

	// Summary stats
	buf.WriteString("SUMMARY\n")
	buf.WriteString(strings.Repeat("-", 40) + "\n")
	buf.WriteString(fmt.Sprintf("  Total: %d issues (%d primary, %d context)\n",
		m.totalCount, m.primaryCount, m.contextCount))
	buf.WriteString(fmt.Sprintf("  Ready: %d, Blocked: %d, In Progress: %d, Closed: %d\n",
		m.readyCount, m.blockedCount,
		m.totalCount-m.readyCount-m.blockedCount-m.closedCount, m.closedCount))
	progress := 0.0
	if m.totalCount > 0 {
		progress = float64(m.closedCount) / float64(m.totalCount)
	}
	buf.WriteString(fmt.Sprintf("  Progress: %d%%\n", int(progress*100)))
	buf.WriteString(fmt.Sprintf("  Dependency Depth: %s\n\n", m.dependencyDepth.String()))

	// Workstream hierarchy (if workstreams exist)
	if len(m.workstreamPtrs) > 0 {
		buf.WriteString("WORKSTREAMS (Hierarchical)\n")
		buf.WriteString(strings.Repeat("-", 40) + "\n")
		for _, ws := range m.workstreamPtrs {
			if ws != nil {
				buf.WriteString(m.dumpWorkstreamTree(ws, 0))
			}
		}
		buf.WriteString("\n")
	}

	// Flat output by depth
	buf.WriteString("ISSUES BY DEPTH\n")
	buf.WriteString(strings.Repeat("-", 40) + "\n")
	buf.WriteString(m.dumpFlatByDepth())

	return filename, os.WriteFile(filename, []byte(buf.String()), 0644)
}

// dumpWorkstreamTree recursively dumps a workstream and its sub-workstreams
func (m *LensDashboardModel) dumpWorkstreamTree(ws *analysis.Workstream, indent int) string {
	var buf strings.Builder
	prefix := strings.Repeat("  ", indent)

	// Workstream header
	buf.WriteString(fmt.Sprintf("%s[%s] %s (%d issues, %d%% done)\n",
		prefix, ws.ID, ws.Name, len(ws.Issues), int(ws.Progress*100)))
	buf.WriteString(fmt.Sprintf("%s  Ready: %d, Blocked: %d, In Progress: %d, Closed: %d\n",
		prefix, ws.ReadyCount, ws.BlockedCount, ws.InProgressCount, ws.ClosedCount))

	if ws.GroupedBy != "" {
		buf.WriteString(fmt.Sprintf("%s  Grouped by: %s\n", prefix, ws.GroupedBy))
	}

	// Issues in this workstream
	if len(ws.Issues) > 0 {
		buf.WriteString(fmt.Sprintf("%s  Issues:\n", prefix))
		for _, issue := range ws.Issues {
			buf.WriteString(fmt.Sprintf("%s    - [%s] %s (%s)\n",
				prefix, issue.ID, issue.Title, issue.Status))
		}
	}

	// Recurse into sub-workstreams
	if len(ws.SubWorkstreams) > 0 {
		buf.WriteString(fmt.Sprintf("%s  Sub-workstreams (%d):\n", prefix, len(ws.SubWorkstreams)))
		for _, subWs := range ws.SubWorkstreams {
			buf.WriteString(m.dumpWorkstreamTree(subWs, indent+1))
		}
	}

	buf.WriteString("\n")
	return buf.String()
}

// dumpFlatByDepth groups issues by their depth in the tree
func (m *LensDashboardModel) dumpFlatByDepth() string {
	var buf strings.Builder

	// Group all issues by their depth
	depthMap := make(map[int][]model.Issue)
	maxDepth := 0

	for _, fn := range m.flatNodes {
		depth := fn.Node.Depth
		depthMap[depth] = append(depthMap[depth], fn.Node.Issue)
		if depth > maxDepth {
			maxDepth = depth
		}
	}

	for depth := 0; depth <= maxDepth; depth++ {
		issues := depthMap[depth]
		if len(issues) > 0 {
			buf.WriteString(fmt.Sprintf("\nDepth %d (%d issues):\n", depth, len(issues)))
			for _, issue := range issues {
				statusStr := issue.Status
				if statusStr == "" {
					statusStr = "open"
				}
				buf.WriteString(fmt.Sprintf("  [%s] %s (%s)\n", issue.ID, issue.Title, statusStr))
			}
		}
	}

	if len(m.flatNodes) == 0 {
		buf.WriteString("\n  No issues in current view\n")
	}

	return buf.String()
}

// ══════════════════════════════════════════════════════════════════════════════
// SPLIT VIEW - Bead detail panel on the right
// ══════════════════════════════════════════════════════════════════════════════

const LensSplitViewThreshold = 120 // Minimum width for split view

// initDetailViewport initializes the detail viewport for split view
func (m *LensDashboardModel) initDetailViewport() {
	m.detailViewport = viewport.New(40, 20)
	m.detailViewport.Style = lipgloss.NewStyle()
	m.updateDetailContent()
}

// IsSplitView returns true if split view is active
func (m *LensDashboardModel) IsSplitView() bool {
	return m.splitViewMode
}

// IsDetailFocused returns true if the detail panel has focus
func (m *LensDashboardModel) IsDetailFocused() bool {
	return m.detailFocus
}

// ToggleDetailFocus switches focus between tree and detail panels
func (m *LensDashboardModel) ToggleDetailFocus() {
	if m.splitViewMode {
		m.detailFocus = !m.detailFocus
	}
}

// SetDetailFocus sets the detail panel focus state
func (m *LensDashboardModel) SetDetailFocus(focused bool) {
	m.detailFocus = focused
}

// ScrollDetailUp scrolls the detail viewport up
func (m *LensDashboardModel) ScrollDetailUp() {
	if m.detailFocus {
		m.detailViewport.LineUp(1)
	}
}

// ScrollDetailDown scrolls the detail viewport down
func (m *LensDashboardModel) ScrollDetailDown() {
	if m.detailFocus {
		m.detailViewport.LineDown(1)
	}
}

// ScrollDetailPageUp scrolls the detail viewport up by a page
func (m *LensDashboardModel) ScrollDetailPageUp() {
	if m.detailFocus {
		m.detailViewport.HalfViewUp()
	}
}

// ScrollDetailPageDown scrolls the detail viewport down by a page
func (m *LensDashboardModel) ScrollDetailPageDown() {
	if m.detailFocus {
		m.detailViewport.HalfViewDown()
	}
}

// updateDetailContent updates the detail viewport content based on selected issue
func (m *LensDashboardModel) updateDetailContent() {
	if m.selectedIssueID == "" {
		m.detailViewport.SetContent("No issue selected")
		return
	}

	issue, exists := m.issueMap[m.selectedIssueID]
	if !exists {
		m.detailViewport.SetContent("Issue not found: " + m.selectedIssueID)
		return
	}

	content := m.renderIssueDetail(issue)
	m.detailViewport.SetContent(content)
	m.detailViewport.GotoTop()
}

// renderIssueDetail renders the detailed view of an issue for the viewport
func (m *LensDashboardModel) renderIssueDetail(issue *model.Issue) string {
	t := m.theme
	var sb strings.Builder

	// Title with type icon
	titleStyle := t.Renderer.NewStyle().Bold(true).Foreground(t.Primary)
	typeIcon, typeColor := t.GetTypeIcon(string(issue.IssueType))
	typeStyle := t.Renderer.NewStyle().Foreground(typeColor)

	sb.WriteString(typeStyle.Render(typeIcon) + " ")
	sb.WriteString(titleStyle.Render(issue.Title))
	sb.WriteString("\n\n")

	// ID and metadata
	labelStyle := t.Renderer.NewStyle().Foreground(t.Subtext)
	valueStyle := t.Renderer.NewStyle().Foreground(t.Base.GetForeground())

	sb.WriteString(labelStyle.Render("ID:       "))
	sb.WriteString(valueStyle.Render(issue.ID))
	sb.WriteString("\n")

	sb.WriteString(labelStyle.Render("Status:   "))
	sb.WriteString(RenderStatusBadge(string(issue.Status)))
	sb.WriteString("\n")

	sb.WriteString(labelStyle.Render("Priority: "))
	sb.WriteString(RenderPriorityBadge(issue.Priority))
	sb.WriteString("\n")

	sb.WriteString(labelStyle.Render("Type:     "))
	sb.WriteString(typeStyle.Render(string(issue.IssueType)))
	sb.WriteString("\n")

	if issue.Assignee != "" {
		sb.WriteString(labelStyle.Render("Assignee: "))
		sb.WriteString(valueStyle.Render("@"+issue.Assignee))
		sb.WriteString("\n")
	}

	sb.WriteString(labelStyle.Render("Created:  "))
	sb.WriteString(valueStyle.Render(issue.CreatedAt.Format("2006-01-02 15:04")))
	sb.WriteString("\n")

	if !issue.UpdatedAt.IsZero() && issue.UpdatedAt != issue.CreatedAt {
		sb.WriteString(labelStyle.Render("Updated:  "))
		sb.WriteString(valueStyle.Render(issue.UpdatedAt.Format("2006-01-02 15:04")))
		sb.WriteString("\n")
	}

	// Labels
	if len(issue.Labels) > 0 {
		sb.WriteString("\n")
		sectionStyle := t.Renderer.NewStyle().Bold(true).Foreground(t.Secondary)
		sb.WriteString(sectionStyle.Render("🏷 Labels"))
		sb.WriteString("\n")

		chipStyle := t.Renderer.NewStyle().Foreground(t.Primary)
		for _, label := range issue.Labels {
			sb.WriteString("  ")
			sb.WriteString(chipStyle.Render(label))
			sb.WriteString("\n")
		}
	}

	// Dependencies
	blockers := m.upstream[issue.ID]
	dependents := m.downstream[issue.ID]

	if len(blockers) > 0 || len(dependents) > 0 {
		sb.WriteString("\n")
		sectionStyle := t.Renderer.NewStyle().Bold(true).Foreground(t.Secondary)
		sb.WriteString(sectionStyle.Render("🔗 Dependencies"))
		sb.WriteString("\n")

		if len(blockers) > 0 {
			blockerStyle := t.Renderer.NewStyle().Foreground(t.Blocked)
			sb.WriteString(blockerStyle.Render(fmt.Sprintf("  ↓ Blocked by (%d):", len(blockers))))
			sb.WriteString("\n")
			for _, blockerID := range blockers {
				if blocker, ok := m.issueMap[blockerID]; ok {
					title := blocker.Title
					if len(title) > 30 {
						title = title[:27] + "..."
					}
					sb.WriteString(fmt.Sprintf("    %s %s\n", blockerID, labelStyle.Render(title)))
				}
			}
		}

		if len(dependents) > 0 {
			dependentStyle := t.Renderer.NewStyle().Foreground(t.Open)
			sb.WriteString(dependentStyle.Render(fmt.Sprintf("  ↑ Blocks (%d):", len(dependents))))
			sb.WriteString("\n")
			for _, depID := range dependents {
				if dep, ok := m.issueMap[depID]; ok {
					title := dep.Title
					if len(title) > 30 {
						title = title[:27] + "..."
					}
					sb.WriteString(fmt.Sprintf("    %s %s\n", depID, labelStyle.Render(title)))
				}
			}
		}
	}

	// Description
	if issue.Description != "" {
		sb.WriteString("\n")
		sectionStyle := t.Renderer.NewStyle().Bold(true).Foreground(t.Secondary)
		sb.WriteString(sectionStyle.Render("📝 Description"))
		sb.WriteString("\n\n")
		sb.WriteString(issue.Description)
		sb.WriteString("\n")
	}

	// Design
	if issue.Design != "" {
		sb.WriteString("\n")
		sectionStyle := t.Renderer.NewStyle().Bold(true).Foreground(t.Secondary)
		sb.WriteString(sectionStyle.Render("🎨 Design"))
		sb.WriteString("\n\n")
		sb.WriteString(issue.Design)
		sb.WriteString("\n")
	}

	// Acceptance Criteria
	if issue.AcceptanceCriteria != "" {
		sb.WriteString("\n")
		sectionStyle := t.Renderer.NewStyle().Bold(true).Foreground(t.Secondary)
		sb.WriteString(sectionStyle.Render("✅ Acceptance Criteria"))
		sb.WriteString("\n\n")
		sb.WriteString(issue.AcceptanceCriteria)
		sb.WriteString("\n")
	}

	// Notes
	if issue.Notes != "" {
		sb.WriteString("\n")
		sectionStyle := t.Renderer.NewStyle().Bold(true).Foreground(t.Secondary)
		sb.WriteString(sectionStyle.Render("📋 Notes"))
		sb.WriteString("\n\n")
		sb.WriteString(issue.Notes)
		sb.WriteString("\n")
	}

	return sb.String()
}

// renderSplitView renders the split layout with tree on left and detail on right
func (m *LensDashboardModel) renderSplitView() string {
	t := m.theme

	// Calculate panel widths (45% tree, 55% detail)
	leftWidth := (m.width * 45) / 100
	rightWidth := m.width - leftWidth - 1 // 1 for separator

	if leftWidth < 40 {
		leftWidth = 40
	}
	if rightWidth < 30 {
		rightWidth = 30
	}

	// Panel styles based on focus
	var leftStyle, rightStyle lipgloss.Style
	borderColor := t.Border
	focusBorderColor := t.Primary

	// Use full height given to us - the parent wraps everything with Height/MaxHeight
	panelHeight := m.height
	if m.detailFocus {
		leftStyle = t.Renderer.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Width(leftWidth - 2).
			Height(panelHeight).
			MaxHeight(panelHeight)
		rightStyle = t.Renderer.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(focusBorderColor).
			Width(rightWidth - 2).
			Height(panelHeight).
			MaxHeight(panelHeight)
	} else {
		leftStyle = t.Renderer.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(focusBorderColor).
			Width(leftWidth - 2).
			Height(panelHeight).
			MaxHeight(panelHeight)
		rightStyle = t.Renderer.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Width(rightWidth - 2).
			Height(panelHeight).
			MaxHeight(panelHeight)
	}

	// Render left panel (tree content)
	leftContent := m.renderTreeContent(leftWidth - 4)

	// Render right panel (detail viewport)
	// Viewport height = panelHeight - 1 (for header line)
	m.detailViewport.Width = rightWidth - 4
	m.detailViewport.Height = panelHeight - 1
	rightContent := m.detailViewport.View()

	// Add panel headers
	leftHeader := t.Renderer.NewStyle().Bold(true).Foreground(t.Primary).Render("◆ " + m.labelName)
	rightHeader := t.Renderer.NewStyle().Bold(true).Foreground(t.Secondary).Render("📋 Details")

	if m.detailFocus {
		rightHeader = t.Renderer.NewStyle().Bold(true).Foreground(t.Primary).Render("📋 Details")
	}

	leftPanel := lipgloss.JoinVertical(lipgloss.Left, leftHeader, leftContent)
	rightPanel := lipgloss.JoinVertical(lipgloss.Left, rightHeader, rightContent)

	// Apply styles
	leftView := leftStyle.Render(leftPanel)
	rightView := rightStyle.Render(rightPanel)

	// Join horizontally
	return lipgloss.JoinHorizontal(lipgloss.Top, leftView, rightView)
}

// renderTreeContent renders just the tree portion for split view
func (m *LensDashboardModel) renderTreeContent(contentWidth int) string {
	t := m.theme
	var lines []string

	statsStyle := t.Renderer.NewStyle().Foreground(t.Subtext)

	// Stats line
	depthStyle := t.Renderer.NewStyle().Foreground(t.InProgress).Bold(true)

	statsLine := fmt.Sprintf("%d in lens  %d context  [%s]",
		m.primaryCount, m.contextCount,
		depthStyle.Render(m.dependencyDepth.String()))
	lines = append(lines, statsStyle.Render(statsLine))

	// Status summary
	readyStyle := t.Renderer.NewStyle().Foreground(t.Open)
	blockedStyle := t.Renderer.NewStyle().Foreground(t.Blocked)
	summaryLine := fmt.Sprintf("%s ready  %s blocked",
		readyStyle.Render(fmt.Sprintf("%d", m.readyCount)),
		blockedStyle.Render(fmt.Sprintf("%d", m.blockedCount)))
	lines = append(lines, statsStyle.Render(summaryLine))
	lines = append(lines, "")

	// Calculate visible area
	visibleLines := m.height - 10
	if visibleLines < 5 {
		visibleLines = 5
	}

	// Render based on view type
	if m.viewType == ViewTypeGrouped && len(m.groupedSections) > 0 {
		lines = append(lines, m.renderGroupedView(contentWidth, visibleLines, statsStyle)...)
	} else if m.viewType == ViewTypeWorkstream && len(m.workstreams) > 1 {
		lines = append(lines, m.renderWorkstreamView(contentWidth, visibleLines, statsStyle)...)
	} else if (m.viewMode == "epic" || m.viewMode == "bead") && m.egoNode != nil {
		lines = append(lines, m.renderCenteredView(contentWidth, visibleLines, statsStyle)...)
	} else {
		// Render flat tree view (reuse the main render function)
		flatLines := m.renderFlatView(contentWidth, visibleLines, statsStyle)
		lines = append(lines, flatLines...)
	}

	return strings.Join(lines, "\n")
}

package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

// LensItem represents a selectable entry in the lens picker (label, epic, or bead)
type LensItem struct {
	Type         string  // "label", "epic", or "bead"
	Value        string  // label name, epic ID, or issue ID
	Title        string  // display text (same as Value for labels, title for epics/beads)
	IssueCount   int     // total issues in this lens
	ClosedCount  int     // closed issues
	Progress     float64 // completion percentage
	IsPinned     bool    // is this item pinned
	OverlapCount int     // issues overlapping with scope (when scope filter is active)
}

// LensSelectorModel represents the lens picker overlay for exploring workstreams
type LensSelectorModel struct {
	// Data - separated by type for mode filtering
	allLabels     []LensItem    // All label items
	allEpics      []LensItem    // All epic items
	allBeads      []LensItem    // All bead/issue items
	filteredItems []LensItem    // Filtered by search and mode
	issues        []model.Issue // Reference to issues for scope filtering

	// Stats panel data
	issueMap   map[string]*model.Issue // Fast lookup by ID for stats panel
	graphStats *analysis.GraphStats    // Graph metrics for centrality display

	// UI State
	searchInput    textinput.Model
	selectedIndex  int
	currentSection int // 0=pinned, 1=recent, 2=epics, 3=labels (or search results)
	hasNavigated   bool // True after user navigates (hides welcome panel)

	// Search mode state
	searchMode string // "merged", "epic", "label", "bead"

	// Scope state (multi-scope filtering)
	scopeLabels    []string  // Currently set scope labels (empty = no scope)
	scopeMode      bool      // True when in scope mode
	scopeMatchMode ScopeMode // Union (ANY) or Intersection (ALL) for multi-label scoping

	// Mode state (vim-style)
	insertMode      bool // True when in insert mode (typing into search)
	scopeAddMode    bool // True when insert mode was triggered by 'l' (adding to scope)
	reviewRequested bool // True when 'r' pressed (opens review mode vs normal selection)

	// Dimensions
	width  int
	height int
	theme  Theme

	// Selection result
	confirmed    bool
	cancelled    bool      // True when user explicitly cancelled (esc/q)
	selectedItem *LensItem
	scopedLabels []string // When scope is set and item selected, both labels returned
}

// NewLensSelectorModel creates a new lens selector for exploring workstreams
func NewLensSelectorModel(issues []model.Issue, theme Theme, graphStats *analysis.GraphStats) LensSelectorModel {
	// Create search input with explorative placeholder
	ti := textinput.New()
	ti.Placeholder = "Explore lenses..."
	ti.Focus()
	ti.CharLimit = 64
	ti.Width = 40

	// Build issue map for O(1) lookups in stats panel
	issueMap := make(map[string]*model.Issue, len(issues))
	for i := range issues {
		issueMap[issues[i].ID] = &issues[i]
	}

	// Collect unique label names and epics
	labelSet := make(map[string]bool)
	var epics []LensItem
	var beads []LensItem

	// Pre-build maps once for efficient epic child counting (O(n) instead of O(e*n))
	childrenMap := BuildChildrenMap(issues)
	statusMap := BuildStatusMap(issues)

	for _, issue := range issues {
		// Collect epics
		if issue.IssueType == model.TypeEpic && issue.Status != model.StatusClosed {
			// Count children for epic progress using pre-built maps
			childTotal, childClosed := countEpicChildrenWithMaps(issue.ID, childrenMap, statusMap)
			progress := 0.0
			if childTotal > 0 {
				progress = float64(childClosed) / float64(childTotal)
			}
			epics = append(epics, LensItem{
				Type:        "epic",
				Value:       issue.ID,
				Title:       issue.Title,
				IssueCount:  childTotal,
				ClosedCount: childClosed,
				Progress:    progress,
			})
		}

		// Collect all issues as beads
		beads = append(beads, LensItem{
			Type:       "bead",
			Value:      issue.ID,
			Title:      issue.Title,
			IssueCount: 1,
		})

		// Collect unique label names
		for _, label := range issue.Labels {
			labelSet[label] = true
		}
	}

	// Build label items with direct counts only (no descendants)
	labelCounts := make(map[string]struct{ total, closed int })
	for _, issue := range issues {
		for _, label := range issue.Labels {
			counts := labelCounts[label]
			counts.total++
			if issue.Status == model.StatusClosed {
				counts.closed++
			}
			labelCounts[label] = counts
		}
	}

	var labels []LensItem
	for name := range labelSet {
		counts := labelCounts[name]
		progress := 0.0
		if counts.total > 0 {
			progress = float64(counts.closed) / float64(counts.total)
		}
		labels = append(labels, LensItem{
			Type:        "label",
			Value:       name,
			Title:       name,
			IssueCount:  counts.total,
			ClosedCount: counts.closed,
			Progress:    progress,
		})
	}

	// Sort labels alphabetically
	sort.Slice(labels, func(i, j int) bool {
		return labels[i].Value < labels[j].Value
	})

	// Sort epics by progress (incomplete first)
	sort.Slice(epics, func(i, j int) bool {
		if epics[i].Progress == epics[j].Progress {
			return epics[i].Title < epics[j].Title
		}
		return epics[i].Progress < epics[j].Progress
	})

	// Sort beads by ID
	sort.Slice(beads, func(i, j int) bool {
		return beads[i].Value < beads[j].Value
	})

	// Default filtered items: epics + labels (merged mode, no search yet)
	filteredItems := append([]LensItem{}, epics...)
	filteredItems = append(filteredItems, labels...)

	return LensSelectorModel{
		allLabels:     labels,
		allEpics:      epics,
		allBeads:      beads,
		filteredItems: filteredItems,
		issues:        issues,
		issueMap:      issueMap,
		graphStats:    graphStats,
		searchInput:   ti,
		searchMode:    "merged",
		selectedIndex: 0,
		hasNavigated:  false,
		theme:         theme,
		width:         120, // Wider default for dual-panel layout
		height:        20,
	}
}

// countEpicChildrenWithMaps counts total and closed descendants for an epic using pre-built maps.
// This is O(d) where d = number of descendants, much better than the old O(n) approach
// when called for multiple epics.
func countEpicChildrenWithMaps(epicID string, children map[string][]string, issueStatus map[string]model.Status) (total, closed int) {
	// BFS to count all descendants
	visited := make(map[string]bool)
	queue := []string{epicID}
	visited[epicID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, childID := range children[current] {
			if !visited[childID] {
				visited[childID] = true
				total++
				if issueStatus[childID] == model.StatusClosed {
					closed++
				}
				queue = append(queue, childID)
			}
		}
	}
	return
}

// SetSize updates the selector dimensions
func (m *LensSelectorModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	// Update text input width
	inputWidth := width - 20
	if inputWidth < 20 {
		inputWidth = 20
	}
	// No upper cap - let search bar fill available width
	m.searchInput.Width = inputWidth
}

// Update handles input and returns whether the model changed
func (m *LensSelectorModel) Update(key string) (handled bool) {
	// Handle insert mode (all keys go to search except esc/enter)
	if m.insertMode {
		return m.updateInsertMode(key)
	}
	return m.updateNormalMode(key)
}

// updateInsertMode handles keys when in insert/search mode
func (m *LensSelectorModel) updateInsertMode(key string) bool {
	switch key {
	case "esc":
		// Exit insert mode, return to normal
		m.insertMode = false
		m.scopeAddMode = false
		// Keep search text and filtered items so user can resume where they left off
		// User can press backspace in normal mode to clear search if desired
		return true
	case "enter":
		if len(m.filteredItems) > 0 && m.selectedIndex < len(m.filteredItems) {
			item := m.filteredItems[m.selectedIndex]

			if m.scopeAddMode && item.Type == "label" {
				// Adding to scope - add label and stay in selector
				m.addToScope(item.Value)
				m.insertMode = false
				m.scopeAddMode = false
				m.searchInput.SetValue("")
				// Don't confirm - stay in selector
				return true
			}

			// Normal selection - confirm and close
			m.selectedItem = &item
			// Build scoped labels: all scope labels + selected label
			if m.scopeMode && len(m.scopeLabels) > 0 && item.Type == "label" {
				m.scopedLabels = make([]string, 0, len(m.scopeLabels)+1)
				m.scopedLabels = append(m.scopedLabels, m.scopeLabels...)
				m.scopedLabels = append(m.scopedLabels, item.Value)
			}
			m.confirmed = true
		}
		return true
	case "backspace":
		if len(m.searchInput.Value()) > 0 {
			m.searchInput.SetValue(m.searchInput.Value()[:len(m.searchInput.Value())-1])
			m.filterItems()
		}
		return true
	case "tab":
		// Tab completion - complete with first matching label
		if m.scopeAddMode {
			query := strings.ToLower(m.searchInput.Value())
			if query != "" {
				for _, item := range m.allLabels {
					if strings.HasPrefix(strings.ToLower(item.Value), query) {
						m.searchInput.SetValue(item.Value)
						m.filterItems()
						return true
					}
				}
				// If no prefix match, try contains match
				for _, item := range m.allLabels {
					if strings.Contains(strings.ToLower(item.Value), query) {
						m.searchInput.SetValue(item.Value)
						m.filterItems()
						return true
					}
				}
			}
		}
		return true
	case "up":
		m.moveUp()
		return true
	case "down":
		m.moveDown()
		return true
	default:
		// All single characters go to search input (including j, k, s, q)
		if len(key) == 1 {
			m.searchInput.SetValue(m.searchInput.Value() + key)
			m.filterItems()
			return true
		}
	}
	return false
}

// updateNormalMode handles keys when in normal/navigation mode
func (m *LensSelectorModel) updateNormalMode(key string) bool {
	switch key {
	case "up", "k":
		m.moveUp()
		return true
	case "down", "j":
		m.moveDown()
		return true
	case "u":
		m.moveUpJump(5)
		return true
	case "d":
		m.moveDownJump(5)
		return true
	case "i", "/":
		// Enter insert mode (for searching)
		m.insertMode = true
		m.scopeAddMode = false
		return true
	case "s":
		// Enter insert mode for scope adding
		m.insertMode = true
		m.scopeAddMode = true
		return true
	case "S":
		// Toggle scope match mode between union (ANY) and intersection (ALL)
		// Only meaningful with 2+ scope labels
		if m.scopeMode && len(m.scopeLabels) >= 2 {
			if m.scopeMatchMode == ScopeModeUnion {
				m.scopeMatchMode = ScopeModeIntersection
			} else {
				m.scopeMatchMode = ScopeModeUnion
			}
			m.filterByScope()
		}
		return true
	case "m":
		// Cycle search mode: merged -> epic -> label -> bead -> merged
		m.cycleSearchMode()
		return true
	case "r":
		// Open review mode for selected item
		if len(m.filteredItems) > 0 && m.selectedIndex < len(m.filteredItems) {
			item := m.filteredItems[m.selectedIndex]
			m.selectedItem = &item
			m.reviewRequested = true
			m.confirmed = true
		}
		return true
	case "enter":
		if len(m.filteredItems) > 0 && m.selectedIndex < len(m.filteredItems) {
			item := m.filteredItems[m.selectedIndex]
			m.selectedItem = &item
			// Build scoped labels: all scope labels + selected label
			if m.scopeMode && len(m.scopeLabels) > 0 && item.Type == "label" {
				m.scopedLabels = make([]string, 0, len(m.scopeLabels)+1)
				m.scopedLabels = append(m.scopedLabels, m.scopeLabels...)
				m.scopedLabels = append(m.scopedLabels, item.Value)
			}
			m.confirmed = true
		}
		return true
	case "esc", "q":
		// If in scope mode, clear scope first (esc only)
		if key == "esc" && m.scopeMode {
			m.clearScope()
			return true
		}
		m.cancelled = true
		m.confirmed = false
		m.selectedItem = nil
		return true
	case "backspace":
		// Clear search in normal mode, or remove last scope if search empty
		if len(m.searchInput.Value()) > 0 {
			m.searchInput.SetValue("")
			m.filterItems()
		} else if len(m.scopeLabels) > 0 {
			// Remove last scope label
			m.scopeLabels = m.scopeLabels[:len(m.scopeLabels)-1]
			if len(m.scopeLabels) == 0 {
				m.scopeMode = false
				m.rebuildFilteredItems()
			} else {
				m.filterByScope()
			}
			m.selectedIndex = 0
		}
		return true
	}
	return false
}

// cycleSearchMode cycles through search modes: merged -> epic -> label -> bead -> merged
func (m *LensSelectorModel) cycleSearchMode() {
	switch m.searchMode {
	case "merged":
		m.searchMode = "epic"
	case "epic":
		m.searchMode = "label"
	case "label":
		m.searchMode = "bead"
	default:
		m.searchMode = "merged"
	}
	// Preserve search text and re-filter with new mode
	m.filterItems()
	m.selectedIndex = 0
}

// rebuildFilteredItems rebuilds the filtered items based on current search mode
func (m *LensSelectorModel) rebuildFilteredItems() {
	switch m.searchMode {
	case "epic":
		m.filteredItems = append([]LensItem{}, m.allEpics...)
	case "label":
		m.filteredItems = append([]LensItem{}, m.allLabels...)
	case "bead":
		m.filteredItems = append([]LensItem{}, m.allBeads...)
	default: // merged
		// In merged mode without search: show epics + labels (no beads)
		m.filteredItems = append([]LensItem{}, m.allEpics...)
		m.filteredItems = append(m.filteredItems, m.allLabels...)
	}
}

// HandleTextInput processes a text input message
func (m *LensSelectorModel) HandleTextInput(value string) {
	m.searchInput.SetValue(value)
	m.filterItems()
}

func (m *LensSelectorModel) moveUp() {
	if m.selectedIndex > 0 {
		m.selectedIndex--
		m.hasNavigated = true
	}
}

func (m *LensSelectorModel) moveDown() {
	if m.selectedIndex < len(m.filteredItems)-1 {
		m.selectedIndex++
		m.hasNavigated = true
	}
}

func (m *LensSelectorModel) moveUpJump(n int) {
	m.selectedIndex -= n
	if m.selectedIndex < 0 {
		m.selectedIndex = 0
	}
	m.hasNavigated = true
}

func (m *LensSelectorModel) moveDownJump(n int) {
	m.selectedIndex += n
	if m.selectedIndex >= len(m.filteredItems) {
		m.selectedIndex = len(m.filteredItems) - 1
	}
	if m.selectedIndex < 0 {
		m.selectedIndex = 0
	}
	m.hasNavigated = true
}

func (m *LensSelectorModel) filterItems() {
	query := strings.TrimSpace(m.searchInput.Value())

	// Special case: scopeAddMode always searches labels (for adding to scope)
	if m.scopeAddMode {
		if query == "" {
			m.filteredItems = append([]LensItem{}, m.allLabels...)
			m.selectedIndex = 0
			return
		}
		// Search all labels
		searchStrings := make([]string, len(m.allLabels))
		for i, item := range m.allLabels {
			searchStrings[i] = item.Title + " " + item.Value
		}
		matches := fuzzy.Find(query, searchStrings)
		m.filteredItems = make([]LensItem, 0, len(matches))
		for _, match := range matches {
			m.filteredItems = append(m.filteredItems, m.allLabels[match.Index])
		}
		m.selectedIndex = 0
		return
	}

	if query == "" {
		// No search query - rebuild based on mode, respecting scope
		if m.scopeMode && len(m.scopeLabels) > 0 {
			m.filterByScope()
		} else {
			m.rebuildFilteredItems()
		}
		m.selectedIndex = 0
		return
	}

	// Get source items based on search mode, respecting scope
	var sourceItems []LensItem

	if m.scopeMode && len(m.scopeLabels) > 0 {
		// When in scope mode, search only within scoped items
		// First compute the scoped items, then we'll search within them
		sourceItems = m.getScopedSourceItems()
	} else {
		// Normal mode: get items based on search mode
		switch m.searchMode {
		case "epic":
			sourceItems = m.allEpics
		case "label":
			sourceItems = m.allLabels
		case "bead":
			sourceItems = m.allBeads
		default: // merged
			// In merged mode with search: include beads too
			sourceItems = append([]LensItem{}, m.allEpics...)
			sourceItems = append(sourceItems, m.allLabels...)
			sourceItems = append(sourceItems, m.allBeads...)
		}
	}

	// Build searchable strings
	searchStrings := make([]string, len(sourceItems))
	for i, item := range sourceItems {
		searchStrings[i] = item.Title + " " + item.Value
	}

	// Fuzzy search
	matches := fuzzy.Find(query, searchStrings)

	m.filteredItems = make([]LensItem, 0, len(matches))
	for _, match := range matches {
		m.filteredItems = append(m.filteredItems, sourceItems[match.Index])
	}

	// Reset selection to top
	m.selectedIndex = 0
}

// getScopedSourceItems returns items filtered by the current scope labels.
// This is used when searching within scope mode to ensure search respects scope.
// Uses scopeMatchMode to determine if issues need ALL (intersection) or ANY (union) scope labels.
func (m *LensSelectorModel) getScopedSourceItems() []LensItem {
	if len(m.scopeLabels) == 0 {
		// No scope, return based on search mode
		switch m.searchMode {
		case "epic":
			return m.allEpics
		case "label":
			return m.allLabels
		case "bead":
			return m.allBeads
		default:
			result := append([]LensItem{}, m.allEpics...)
			result = append(result, m.allLabels...)
			return result
		}
	}

	// Build set of scope labels for quick lookup
	scopeSet := make(map[string]bool)
	for _, l := range m.scopeLabels {
		scopeSet[l] = true
	}

	// Find all issues that match the scope criteria (using scopeMatchMode)
	scopeIssues := make(map[string]bool)
	for _, issue := range m.issues {
		if m.issueMatchesScope(issue) {
			scopeIssues[issue.ID] = true
		}
	}

	// Count co-occurring labels (excluding scope labels)
	labelOverlap := make(map[string]int)
	for _, issue := range m.issues {
		if !scopeIssues[issue.ID] {
			continue
		}
		for _, label := range issue.Labels {
			if !scopeSet[label] {
				labelOverlap[label]++
			}
		}
	}

	// Build items with overlap counts based on search mode
	var result []LensItem

	switch m.searchMode {
	case "label":
		// Return labels that co-occur with scope
		for _, item := range m.allLabels {
			if !scopeSet[item.Value] {
				if overlap, ok := labelOverlap[item.Value]; ok && overlap > 0 {
					itemCopy := item
					itemCopy.OverlapCount = overlap
					result = append(result, itemCopy)
				}
			}
		}
	case "epic":
		// Return epics that have descendants matching scope
		childrenMap := BuildChildrenMap(m.issues)
		for _, item := range m.allEpics {
			overlapCount := countScopedDescendants(item.Value, childrenMap, scopeIssues)
			if overlapCount > 0 {
				itemCopy := item
				itemCopy.OverlapCount = overlapCount
				result = append(result, itemCopy)
			}
		}
	case "bead":
		// Return beads that match scope criteria
		for _, item := range m.allBeads {
			if scopeIssues[item.Value] {
				result = append(result, item)
			}
		}
	default: // "merged" - return ALL types: beads, epics, and labels
		// Build children map for epic descendant counting
		childrenMap := BuildChildrenMap(m.issues)

		// 1. Add matching beads
		for _, item := range m.allBeads {
			if scopeIssues[item.Value] {
				itemCopy := item
				itemCopy.OverlapCount = 1
				result = append(result, itemCopy)
			}
		}

		// 2. Add matching epics (with scoped descendant counts)
		for _, item := range m.allEpics {
			overlapCount := countScopedDescendants(item.Value, childrenMap, scopeIssues)
			if overlapCount > 0 {
				itemCopy := item
				itemCopy.OverlapCount = overlapCount
				result = append(result, itemCopy)
			}
		}

		// 3. Add co-occurring labels with overlap counts
		for _, item := range m.allLabels {
			if !scopeSet[item.Value] {
				if overlap, ok := labelOverlap[item.Value]; ok && overlap > 0 {
					itemCopy := item
					itemCopy.OverlapCount = overlap
					result = append(result, itemCopy)
				}
			}
		}
	}

	return result
}

// IsConfirmed returns true if user confirmed a selection
func (m *LensSelectorModel) IsConfirmed() bool {
	return m.confirmed
}

// IsCancelled returns true if user cancelled the selector
func (m *LensSelectorModel) IsCancelled() bool {
	return m.cancelled
}

// SelectedItem returns the selected lens item, or nil if none
func (m *LensSelectorModel) SelectedItem() *LensItem {
	return m.selectedItem
}

// ScopedLabels returns the scoped labels when scope mode is active
func (m *LensSelectorModel) ScopedLabels() []string {
	return m.scopedLabels
}

// ScopeLabels returns all current scope labels
func (m *LensSelectorModel) ScopeLabels() []string {
	return m.scopeLabels
}

// IsScopeMode returns true if scope mode is active
func (m *LensSelectorModel) IsScopeMode() bool {
	return m.scopeMode
}

// addToScope adds a label to the scope set (no toggle, just add if not present)
func (m *LensSelectorModel) addToScope(label string) {
	// Check if already in scope
	for _, l := range m.scopeLabels {
		if l == label {
			// Already in scope, just refilter
			m.filterByScope()
			return
		}
	}

	// Add to scope
	m.scopeLabels = append(m.scopeLabels, label)
	m.scopeMode = true
	m.filterByScope()
}

// toggleScope toggles a label in/out of the scope set
func (m *LensSelectorModel) toggleScope(label string) {
	// Check if already in scope
	idx := -1
	for i, l := range m.scopeLabels {
		if l == label {
			idx = i
			break
		}
	}

	if idx >= 0 {
		// Remove from scope
		m.scopeLabels = append(m.scopeLabels[:idx], m.scopeLabels[idx+1:]...)
		if len(m.scopeLabels) == 0 {
			m.scopeMode = false
		}
	} else {
		// Add to scope
		m.scopeLabels = append(m.scopeLabels, label)
		m.scopeMode = true
	}

	m.searchInput.SetValue("")
	if m.scopeMode {
		m.filterByScope()
	} else {
		m.rebuildFilteredItems()
		m.selectedIndex = 0
	}
}

// clearScope clears all scopes and resets to full list
func (m *LensSelectorModel) clearScope() {
	m.scopeLabels = nil
	m.scopeMode = false
	m.scopeMatchMode = ScopeModeIntersection // Reset to default (ALL)
	m.scopedLabels = nil
	m.searchInput.SetValue("")
	m.rebuildFilteredItems()
	m.selectedIndex = 0
}

// ScopeMatchMode returns the current scope match mode (union or intersection)
func (m *LensSelectorModel) ScopeMatchMode() ScopeMode {
	return m.scopeMatchMode
}

// issueMatchesScope returns true if the issue matches the current scope criteria.
// In Union mode, returns true if the issue has ANY of the scope labels.
// In Intersection mode (default), returns true if the issue has ALL scope labels.
func (m *LensSelectorModel) issueMatchesScope(issue model.Issue) bool {
	if len(m.scopeLabels) == 0 {
		return true
	}

	// Build set of issue's labels for quick lookup
	issueLabels := make(map[string]bool)
	for _, label := range issue.Labels {
		issueLabels[label] = true
	}

	if m.scopeMatchMode == ScopeModeUnion {
		// Union: issue has ANY of the scope labels
		for _, scopeLabel := range m.scopeLabels {
			if issueLabels[scopeLabel] {
				return true
			}
		}
		return false
	}

	// Intersection (default): issue has ALL of the scope labels
	for _, scopeLabel := range m.scopeLabels {
		if !issueLabels[scopeLabel] {
			return false
		}
	}
	return true
}

// countScopedDescendants counts how many descendants of an epic match the scope
func countScopedDescendants(epicID string, children map[string][]string, scopeIssues map[string]bool) int {
	count := 0
	var visit func(id string)
	visit = func(id string) {
		for _, childID := range children[id] {
			if scopeIssues[childID] {
				count++
			}
			visit(childID)
		}
	}
	visit(epicID)
	return count
}

// filterByScope filters items to respect current scope labels and search mode.
// Uses scopeMatchMode to determine if issues need ALL (intersection) or ANY (union) scope labels.
// For merged mode: shows beads, epics, and labels sorted by overlap count.
// For label mode: shows only co-occurring labels with overlap counts.
// For epic/bead modes: shows items that match the scope criteria.
func (m *LensSelectorModel) filterByScope() {
	if len(m.scopeLabels) == 0 {
		m.rebuildFilteredItems()
		return
	}

	// Build set of scope labels for quick lookup
	scopeSet := make(map[string]bool)
	for _, l := range m.scopeLabels {
		scopeSet[l] = true
	}

	// Find all issues that match the scope criteria (using scopeMatchMode)
	scopeIssues := make(map[string]bool)
	for _, issue := range m.issues {
		if m.issueMatchesScope(issue) {
			scopeIssues[issue.ID] = true
		}
	}

	var filtered []LensItem

	switch m.searchMode {
	case "epic":
		// Show epics that have descendants matching scope
		childrenMap := BuildChildrenMap(m.issues)
		for _, item := range m.allEpics {
			overlapCount := countScopedDescendants(item.Value, childrenMap, scopeIssues)
			if overlapCount > 0 {
				itemCopy := item
				itemCopy.OverlapCount = overlapCount
				filtered = append(filtered, itemCopy)
			}
		}
	case "bead":
		// Show beads that match scope criteria
		for _, item := range m.allBeads {
			if scopeIssues[item.Value] {
				filtered = append(filtered, item)
			}
		}
	case "label":
		// Count co-occurring labels (excluding scope labels)
		labelOverlap := make(map[string]int)
		for _, issue := range m.issues {
			if !scopeIssues[issue.ID] {
				continue
			}
			for _, label := range issue.Labels {
				if !scopeSet[label] {
					labelOverlap[label]++
				}
			}
		}

		// Build filtered items with overlap counts (labels only)
		for _, item := range m.allLabels {
			if !scopeSet[item.Value] {
				if overlap, ok := labelOverlap[item.Value]; ok && overlap > 0 {
					itemCopy := item
					itemCopy.OverlapCount = overlap
					filtered = append(filtered, itemCopy)
				}
			}
		}

		// Sort by overlap count (descending)
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].OverlapCount > filtered[j].OverlapCount
		})
	default: // "merged" - show ALL types: beads, epics, and labels
		// Count co-occurring labels (excluding scope labels)
		labelOverlap := make(map[string]int)
		for _, issue := range m.issues {
			if !scopeIssues[issue.ID] {
				continue
			}
			for _, label := range issue.Labels {
				if !scopeSet[label] {
					labelOverlap[label]++
				}
			}
		}

		// Build children map for epic descendant counting
		childrenMap := BuildChildrenMap(m.issues)

		// 1. Add matching beads
		for _, item := range m.allBeads {
			if scopeIssues[item.Value] {
				itemCopy := item
				itemCopy.OverlapCount = 1
				filtered = append(filtered, itemCopy)
			}
		}

		// 2. Add matching epics (with scoped descendant counts)
		for _, item := range m.allEpics {
			overlapCount := countScopedDescendants(item.Value, childrenMap, scopeIssues)
			if overlapCount > 0 {
				itemCopy := item
				itemCopy.OverlapCount = overlapCount
				filtered = append(filtered, itemCopy)
			}
		}

		// 3. Add co-occurring labels with overlap counts
		for _, item := range m.allLabels {
			if !scopeSet[item.Value] {
				if overlap, ok := labelOverlap[item.Value]; ok && overlap > 0 {
					itemCopy := item
					itemCopy.OverlapCount = overlap
					filtered = append(filtered, itemCopy)
				}
			}
		}

		// Sort ALL items by overlap count (highest first), regardless of type
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].OverlapCount > filtered[j].OverlapCount
		})
	}

	m.filteredItems = filtered
	m.selectedIndex = 0
}

// Reset clears the selection state for reuse
func (m *LensSelectorModel) Reset() {
	m.confirmed = false
	m.selectedItem = nil
	m.scopedLabels = nil
	m.scopeLabels = nil
	m.scopeMode = false
	m.scopeMatchMode = ScopeModeIntersection // Reset to default (ALL)
	m.searchInput.SetValue("")
	m.searchMode = "merged"
	m.rebuildFilteredItems()
	m.selectedIndex = 0
	m.insertMode = false
	m.scopeAddMode = false
	m.reviewRequested = false
	m.hasNavigated = false // Show welcome panel on reset
}

// ══════════════════════════════════════════════════════════════════════════════
// STATS PANEL HELPERS - Data access for rich item statistics
// ══════════════════════════════════════════════════════════════════════════════

// getBlockers returns IDs of issues that block the given issue
func (m *LensSelectorModel) getBlockers(issueID string) []string {
	issue := m.issueMap[issueID]
	if issue == nil {
		return nil
	}
	var blockers []string
	for _, dep := range issue.Dependencies {
		if dep.Type == model.DepBlocks {
			blockers = append(blockers, dep.DependsOnID)
		}
	}
	return blockers
}

// getDependents returns IDs of issues that depend on (are blocked by) the given issue
func (m *LensSelectorModel) getDependents(issueID string) []string {
	var dependents []string
	for _, issue := range m.issues {
		for _, dep := range issue.Dependencies {
			if dep.DependsOnID == issueID && dep.Type == model.DepBlocks {
				dependents = append(dependents, issue.ID)
				break
			}
		}
	}
	return dependents
}

// getIssuesWithLabel returns all issues that have the specified label
func (m *LensSelectorModel) getIssuesWithLabel(label string) []model.Issue {
	var result []model.Issue
	for _, issue := range m.issues {
		for _, l := range issue.Labels {
			if l == label {
				result = append(result, issue)
				break
			}
		}
	}
	return result
}

// getRelatedLabels finds labels that co-occur with the given label, sorted by count
func (m *LensSelectorModel) getRelatedLabels(label string, limit int) []LabelCount {
	// Count co-occurring labels
	cooccurrence := make(map[string]int)
	for _, issue := range m.issues {
		hasLabel := false
		for _, l := range issue.Labels {
			if l == label {
				hasLabel = true
				break
			}
		}
		if hasLabel {
			for _, l := range issue.Labels {
				if l != label {
					cooccurrence[l]++
				}
			}
		}
	}

	// Convert to slice and sort by count
	var related []LabelCount
	for l, count := range cooccurrence {
		related = append(related, LabelCount{Label: l, Count: count})
	}
	sort.Slice(related, func(i, j int) bool {
		return related[i].Count > related[j].Count
	})

	// Limit results
	if limit > 0 && len(related) > limit {
		related = related[:limit]
	}
	return related
}

// countStatuses returns a map of status -> count for the given issues
func (m *LensSelectorModel) countStatuses(issues []model.Issue) map[model.Status]int {
	counts := make(map[model.Status]int)
	for _, issue := range issues {
		counts[issue.Status]++
	}
	return counts
}

// countTypes returns a map of issue type -> count for the given issues
func (m *LensSelectorModel) countTypes(issues []model.Issue) map[model.IssueType]int {
	counts := make(map[model.IssueType]int)
	for _, issue := range issues {
		counts[issue.IssueType]++
	}
	return counts
}

// getEpicChildrenIssues returns all descendant issues for an epic
func (m *LensSelectorModel) getEpicChildrenIssues(epicID string) []model.Issue {
	children := BuildChildrenMap(m.issues)

	// BFS to collect all descendants
	visited := make(map[string]bool)
	queue := []string{epicID}
	visited[epicID] = true

	var result []model.Issue
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, childID := range children[current] {
			if !visited[childID] {
				visited[childID] = true
				if issue := m.issueMap[childID]; issue != nil {
					result = append(result, *issue)
				}
				queue = append(queue, childID)
			}
		}
	}
	return result
}

// getCentralityRank returns the rank and score for an issue's centrality metric
// Returns (rank, score, total) where rank is 1-indexed position
func (m *LensSelectorModel) getCentralityRank(issueID string) (pageRank int, prScore float64, betweenness int, btScore float64, total int) {
	if m.graphStats == nil {
		return 0, 0, 0, 0, 0
	}

	total = len(m.issues)

	// Get PageRank
	prScores := m.graphStats.PageRank()
	if prScores != nil {
		prScore = prScores[issueID]
		// Calculate rank
		rank := 1
		for id, score := range prScores {
			if score > prScore && id != issueID {
				rank++
			}
		}
		pageRank = rank
	}

	// Get Betweenness
	btScores := m.graphStats.Betweenness()
	if btScores != nil {
		btScore = btScores[issueID]
		// Calculate rank
		rank := 1
		for id, score := range btScores {
			if score > btScore && id != issueID {
				rank++
			}
		}
		betweenness = rank
	}

	return
}

// IsReviewRequested returns true if 'r' was pressed (review mode requested)
func (m *LensSelectorModel) IsReviewRequested() bool {
	return m.reviewRequested
}

// SearchMode returns the current search mode
func (m *LensSelectorModel) SearchMode() string {
	return m.searchMode
}

// IsInsertMode returns true if in insert/search mode
func (m *LensSelectorModel) IsInsertMode() bool {
	return m.insertMode
}

// IsScopeAddMode returns true if in scope-adding insert mode
func (m *LensSelectorModel) IsScopeAddMode() bool {
	return m.scopeAddMode
}

// View renders the lens selector overlay with dual-panel layout
func (m *LensSelectorModel) View() string {
	t := m.theme

	// Check for very narrow terminal - use minimal layout (list only)
	if m.width < BreakpointNarrow {
		return m.renderMinimalLayout()
	}

	// Check for narrow terminal - use stacked layout
	if m.width < BreakpointMedium {
		return m.renderStackedLayout()
	}

	// Calculate total width for dual-panel layout
	totalWidth := 106
	if m.width < 120 {
		totalWidth = m.width - 14
	}

	// Panel dimensions - each panel gets half the width minus separator
	panelWidth := (totalWidth - 3) / 2 // 3 chars for separator " │ "
	contentHeight := m.height - 10     // Account for header, footer, borders

	// Render header
	header := m.renderLensHeader(totalWidth)

	// Render left panel (list selector) with fixed width
	leftContent := m.renderLeftPanel(panelWidth, contentHeight)

	// Render right panel (welcome or stats) with fixed width
	rightContent := m.renderRightPanel(panelWidth, contentHeight)

	// Ensure both panels have consistent height by counting lines
	leftLines := strings.Split(leftContent, "\n")
	rightLines := strings.Split(rightContent, "\n")

	// Pad shorter panel to match height
	maxLines := len(leftLines)
	if len(rightLines) > maxLines {
		maxLines = len(rightLines)
	}

	for len(leftLines) < maxLines {
		leftLines = append(leftLines, strings.Repeat(" ", panelWidth))
	}
	for len(rightLines) < maxLines {
		rightLines = append(rightLines, strings.Repeat(" ", panelWidth))
	}

	// Build panels line by line with separator
	sepStyle := t.Renderer.NewStyle().Foreground(ColorBgHighlight)
	var panelLines []string
	for i := 0; i < maxLines; i++ {
		// Pad each line to panel width
		leftLine := leftLines[i]
		rightLine := rightLines[i]

		// Pad left line
		leftVisualWidth := lipgloss.Width(leftLine)
		if leftVisualWidth < panelWidth {
			leftLine = leftLine + strings.Repeat(" ", panelWidth-leftVisualWidth)
		}

		// Pad right line
		rightVisualWidth := lipgloss.Width(rightLine)
		if rightVisualWidth < panelWidth {
			rightLine = rightLine + strings.Repeat(" ", panelWidth-rightVisualWidth)
		}

		panelLines = append(panelLines, leftLine+" "+sepStyle.Render("│")+" "+rightLine)
	}

	panels := strings.Join(panelLines, "\n")

	// Render footer
	footer := m.renderKeybindFooter(totalWidth)

	// Combine all sections
	content := lipgloss.JoinVertical(lipgloss.Left,
		header,
		"",
		panels,
		"",
		footer,
	)

	// Box style
	boxStyle := t.Renderer.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Primary).
		Padding(1, 2)

	box := boxStyle.Render(content)

	// Center in viewport
	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		box,
	)
}

func (m *LensSelectorModel) renderItem(item LensItem, isSelected bool, maxWidth int) string {
	t := m.theme

	// Selection prefix
	prefix := "  "
	if isSelected {
		prefix = "▸ "
	}

	// Type indicator: colored E/L/B
	var typeIndicator string
	switch item.Type {
	case "epic":
		typeStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)
		typeIndicator = typeStyle.Render("E") + " "
	case "bead":
		typeStyle := t.Renderer.NewStyle().Foreground(t.InProgress).Bold(true)
		typeIndicator = typeStyle.Render("B") + " "
	default: // label
		typeStyle := t.Renderer.NewStyle().Foreground(t.Secondary).Bold(true)
		typeIndicator = typeStyle.Render("L") + " "
	}

	// Name/title style
	nameStyle := t.Renderer.NewStyle()
	if isSelected {
		nameStyle = nameStyle.Foreground(t.Primary).Bold(true)
	} else {
		nameStyle = nameStyle.Foreground(t.Base.GetForeground())
	}

	// Build display text
	var displayText string
	if item.Type == "bead" {
		// Show ID followed by title
		idPart := item.Value
		titlePart := item.Title
		maxTitleLen := maxWidth - 28 - len(idPart) // Leave room for ID and padding
		if len(titlePart) > maxTitleLen && maxTitleLen > 5 {
			titlePart = titlePart[:maxTitleLen-1] + "…"
		}
		displayText = idPart + " " + titlePart
	} else {
		// Truncate title if needed
		title := item.Title
		maxTitleLen := maxWidth - 23 // Leave room for progress bar or overlap
		if len(title) > maxTitleLen {
			title = title[:maxTitleLen-1] + "…"
		}
		displayText = title
	}

	// Build the line with type indicator
	name := prefix + typeIndicator + nameStyle.Render(displayText)

	// Show overlap count when in scope mode, otherwise progress bar
	var suffix string
	if m.scopeMode && item.OverlapCount > 0 {
		overlapStyle := t.Renderer.NewStyle().Foreground(t.InProgress)
		suffix = overlapStyle.Render("(" + strconv.Itoa(item.OverlapCount) + ")")
	} else if item.IssueCount > 0 {
		suffix = m.renderProgressBar(item.Progress, item.ClosedCount, item.IssueCount)
	}

	// Pad to align using visual width (handles ANSI escape codes correctly)
	nameWidth := lipgloss.Width(name)
	suffixWidth := lipgloss.Width(suffix)
	padding := maxWidth - nameWidth - suffixWidth
	if padding < 1 {
		padding = 1
	}

	return name + strings.Repeat(" ", padding) + suffix
}

func (m *LensSelectorModel) renderProgressBar(progress float64, closed, total int) string {
	t := m.theme

	if total == 0 {
		return t.Renderer.NewStyle().Foreground(t.Subtext).Render("(0)")
	}

	// Progress bar: 8 characters wide
	barWidth := 8
	filled := int(progress * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}

	var barColor lipgloss.AdaptiveColor
	if progress >= 1.0 {
		barColor = t.Closed // Completed
	} else if progress >= 0.5 {
		barColor = t.InProgress
	} else {
		barColor = t.Open
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	barStyled := t.Renderer.NewStyle().Foreground(barColor).Render("[" + bar + "]")

	// Count
	countStr := t.Renderer.NewStyle().Foreground(t.Subtext).Render(
		" " + strconv.Itoa(closed) + "/" + strconv.Itoa(total))

	// Checkmark if complete
	if progress >= 1.0 {
		countStr += " ✓"
	}

	return barStyled + countStr
}

// SearchValue returns the current search input value
func (m *LensSelectorModel) SearchValue() string {
	return m.searchInput.Value()
}

// ItemCount returns the number of filtered items
func (m *LensSelectorModel) ItemCount() int {
	return len(m.filteredItems)
}

// ══════════════════════════════════════════════════════════════════════════════
// DUAL-PANEL RENDERING - Header, Footer, Welcome, Stats Panels
// ══════════════════════════════════════════════════════════════════════════════

// padToHeight pads or truncates content to exactly the specified height
func padToHeight(content string, height int, width int) string {
	lines := strings.Split(content, "\n")

	// Truncate if too tall
	if len(lines) > height {
		lines = lines[:height]
	}

	// Pad if too short
	emptyLine := strings.Repeat(" ", width)
	for len(lines) < height {
		lines = append(lines, emptyLine)
	}

	return strings.Join(lines, "\n")
}

// renderLensHeader creates an eye-catching gradient LENS header
func (m *LensSelectorModel) renderLensHeader(width int) string {
	t := m.theme

	// Gradient colors for L-E-N-S letters
	colors := []lipgloss.Color{
		GradientHigh, // L - Purple
		GradientPeak, // E - Pink
		GradientPeak, // N - Pink
		GradientHigh, // S - Purple
	}

	letters := []string{"L", "E", "N", "S"}
	var headerParts []string

	// Build spaced gradient letters
	for i, letter := range letters {
		letterStyle := t.Renderer.NewStyle().
			Foreground(colors[i]).
			Bold(true)
		headerParts = append(headerParts, letterStyle.Render(letter))
	}

	// Join with spaces for elegant spacing
	lensText := strings.Join(headerParts, " ")

	// Add decorative elements
	iconStyle := t.Renderer.NewStyle().Foreground(GradientPeak)
	icon := iconStyle.Render("◈")

	header := icon + "  " + lensText + "  " + icon

	// Center the header
	headerStyle := t.Renderer.NewStyle().
		Width(width).
		Align(lipgloss.Center)

	return headerStyle.Render(header)
}

// renderKeybindFooter creates a refined keybind section with proper grouping
func (m *LensSelectorModel) renderKeybindFooter(width int) string {
	t := m.theme

	// Style definitions
	keyStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)
	descStyle := t.Renderer.NewStyle().Foreground(t.Subtext)
	sepStyle := t.Renderer.NewStyle().Foreground(ColorBgHighlight)
	modeStyle := t.Renderer.NewStyle().
		Foreground(t.Primary).
		Bold(true).
		Background(ColorBgSubtle).
		Padding(0, 1)

	sep := sepStyle.Render(" │ ")

	var line string

	if m.insertMode {
		if m.scopeAddMode {
			mode := modeStyle.Render("FILTER+")
			line = mode + "  " +
				descStyle.Render("type to filter") + sep +
				keyStyle.Render("↑↓") + descStyle.Render(" nav") + sep +
				keyStyle.Render("⏎") + descStyle.Render(" add") + sep +
				keyStyle.Render("esc") + descStyle.Render(" back")
		} else {
			mode := modeStyle.Render("SEARCH")
			line = mode + "  " +
				descStyle.Render("type to search") + sep +
				keyStyle.Render("↑↓") + descStyle.Render(" nav") + sep +
				keyStyle.Render("⏎") + descStyle.Render(" select") + sep +
				keyStyle.Render("esc") + descStyle.Render(" back")
		}
	} else if m.scopeMode {
		mode := modeStyle.Render("FILTERED")

		// Only show scope match mode indicator when 2+ labels (meaningless with 1)
		var matchModeIndicator string
		var toggleHint string
		if len(m.scopeLabels) >= 2 {
			matchModeStyle := t.Renderer.NewStyle().Foreground(t.Secondary).Bold(true)
			matchModeIndicator = " " + matchModeStyle.Render(m.scopeMatchMode.ShortString())
			// Show what mode Shift+S will toggle to
			if m.scopeMatchMode == ScopeModeUnion {
				toggleHint = keyStyle.Render("S") + descStyle.Render(" →all") + sep
			} else {
				toggleHint = keyStyle.Render("S") + descStyle.Render(" →any") + sep
			}
		}

		line = mode + matchModeIndicator + "  " +
			keyStyle.Render("j/k") + descStyle.Render(" nav") + sep +
			toggleHint +
			keyStyle.Render("s") + descStyle.Render(" +scope") + sep +
			keyStyle.Render("m") + descStyle.Render(" mode") + sep +
			keyStyle.Render("⌫") + descStyle.Render(" clear") + sep +
			keyStyle.Render("⏎") + descStyle.Render(" select") + sep +
			keyStyle.Render("q") + descStyle.Render(" exit")
	} else {
		mode := modeStyle.Render("BROWSE")
		line = mode + "  " +
			keyStyle.Render("j/k") + descStyle.Render(" nav") + sep +
			keyStyle.Render("i") + descStyle.Render(" insert") + sep +
			keyStyle.Render("m") + descStyle.Render(" mode") + sep +
			keyStyle.Render("s") + descStyle.Render(" scope") + sep +
			keyStyle.Render("r") + descStyle.Render(" review") + sep +
			keyStyle.Render("q") + descStyle.Render(" exit")
	}

	// Center the footer
	footerStyle := t.Renderer.NewStyle().
		Width(width).
		Align(lipgloss.Center)

	return footerStyle.Render(line)
}

// renderWelcomePanel creates a decorative welcome UI when first entering
func (m *LensSelectorModel) renderWelcomePanel(width, height int) string {
	t := m.theme

	// Fixed content width for consistent layout
	contentWidth := width - 4
	if contentWidth < 30 {
		contentWidth = 30
	}

	var lines []string

	// Decorative header box
	headerBoxStyle := t.Renderer.NewStyle().
		Foreground(GradientPeak).
		Bold(true)
	boxWidth := 26 // Fixed width for "Welcome to Lens View"

	topBorder := "╔" + strings.Repeat("═", boxWidth-2) + "╗"
	bottomBorder := "╚" + strings.Repeat("═", boxWidth-2) + "╝"
	titleText := "Welcome to Lens View"
	padding := (boxWidth - 2 - len(titleText)) / 2
	titleLine := "║" + strings.Repeat(" ", padding) + titleText + strings.Repeat(" ", boxWidth-2-padding-len(titleText)) + "║"

	lines = append(lines, headerBoxStyle.Render(topBorder))
	lines = append(lines, headerBoxStyle.Render(titleLine))
	lines = append(lines, headerBoxStyle.Render(bottomBorder))
	lines = append(lines, "")

	// Icon and tagline
	iconStyle := t.Renderer.NewStyle().Foreground(GradientHigh).Bold(true)
	lines = append(lines, iconStyle.Render("🔮 Explore Your Work"))
	lines = append(lines, "")

	// Feature descriptions
	labelStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)
	descStyle := t.Renderer.NewStyle().Foreground(t.Subtext)

	lines = append(lines, descStyle.Render("Navigate to see stats for:"))
	lines = append(lines, "")
	lines = append(lines, labelStyle.Render("► Epics")+"   "+descStyle.Render("Progress & children"))
	lines = append(lines, labelStyle.Render("► Labels")+"  "+descStyle.Render("Distribution"))
	lines = append(lines, labelStyle.Render("► Beads")+"   "+descStyle.Render("Details & deps"))
	lines = append(lines, "")

	// Tip
	tipStyle := t.Renderer.NewStyle().
		Foreground(ColorInfo).
		Italic(true)
	lines = append(lines, tipStyle.Render("💡 Press j/k to navigate"))

	// Join content
	content := strings.Join(lines, "\n")

	// Create a container with fixed height and centered content
	containerStyle := t.Renderer.NewStyle().
		Width(contentWidth).
		Height(height).
		Align(lipgloss.Center, lipgloss.Center)

	return containerStyle.Render(content)
}

// renderLeftPanel renders the list selector (extracted from View logic)
func (m *LensSelectorModel) renderLeftPanel(width, height int) string {
	t := m.theme
	contentWidth := width - 4

	var lines []string

	// Search input first (highlighted when in insert mode)
	inputBorderColor := t.Secondary
	if m.insertMode {
		inputBorderColor = t.Primary
	}
	inputStyle := t.Renderer.NewStyle().
		Foreground(t.Base.GetForeground()).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(inputBorderColor).
		Padding(0, 1).
		Width(contentWidth - 2)

	searchValue := m.searchInput.Value()
	if m.insertMode {
		// Show cursor in insert mode
		cursorStyle := t.Renderer.NewStyle().Background(t.Primary).Foreground(t.Base.GetBackground())
		searchValue = searchValue + cursorStyle.Render(" ")
	} else if searchValue == "" {
		searchValue = t.Renderer.NewStyle().Foreground(t.Subtext).Render(m.searchInput.Placeholder)
	}
	lines = append(lines, inputStyle.Render(searchValue))

	// Mode and count info below search bar
	modeStyle := t.Renderer.NewStyle().Foreground(t.Subtext)
	var modeLabel string
	switch m.searchMode {
	case "epic":
		modeLabel = "EPIC"
	case "label":
		modeLabel = "LABEL"
	case "bead":
		modeLabel = "BEAD"
	default:
		modeLabel = "ALL"
	}
	countInfo := fmt.Sprintf("%s · %d items", modeLabel, len(m.filteredItems))
	lines = append(lines, modeStyle.Render(countInfo))

	// Scope indicator and inline input
	if m.scopeMode && len(m.scopeLabels) > 0 {
		scopeStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)
		tagStyle := t.Renderer.NewStyle().Foreground(t.Secondary)

		// Build scope tags
		var tags []string
		for _, label := range m.scopeLabels {
			tags = append(tags, tagStyle.Render("["+label+"]"))
		}
		scopeLine := scopeStyle.Render("Scope: ") + strings.Join(tags, " ")
		lines = append(lines, scopeLine)
	}

	// Show inline scope input when in scopeAddMode
	if m.scopeAddMode && m.insertMode {
		inputStyle := t.Renderer.NewStyle().Foreground(t.Primary)
		promptStyle := t.Renderer.NewStyle().Foreground(t.Secondary)
		hintStyle := t.Renderer.NewStyle().Faint(true)

		inputLine := promptStyle.Render("+ Filter: ") + inputStyle.Render(m.searchInput.Value()) + inputStyle.Render("█")
		lines = append(lines, inputLine)

		// Get matching labels for hint (show on separate line to avoid breaking layout)
		query := strings.ToLower(m.searchInput.Value())
		if query != "" {
			var matches []string
			for _, item := range m.allLabels {
				if strings.Contains(strings.ToLower(item.Value), query) {
					matches = append(matches, item.Value)
					if len(matches) >= 5 {
						break
					}
				}
			}
			if len(matches) > 0 {
				// Truncate matches to fit width
				matchText := strings.Join(matches, ", ")
				maxLen := contentWidth - 4
				if len(matchText) > maxLen {
					matchText = matchText[:maxLen-3] + "..."
				}
				lines = append(lines, hintStyle.Render("  → "+matchText))
			}
		}
	}

	lines = append(lines, "")

	// Calculate max visible items - fill available height
	maxVisible := height - 8
	if maxVisible < 5 {
		maxVisible = 5
	}
	// No upper cap - items span full available height

	// Render items as unified list
	if len(m.filteredItems) == 0 {
		emptyStyle := t.Renderer.NewStyle().Foreground(t.Subtext).Italic(true)
		lines = append(lines, emptyStyle.Render("  No matching items found"))
	} else {
		// Calculate scroll window
		startIdx := 0
		if m.selectedIndex >= maxVisible {
			startIdx = m.selectedIndex - maxVisible + 1
		}
		endIdx := startIdx + maxVisible
		if endIdx > len(m.filteredItems) {
			endIdx = len(m.filteredItems)
		}

		// Render visible items
		for i := startIdx; i < endIdx; i++ {
			item := m.filteredItems[i]
			line := m.renderItem(item, i == m.selectedIndex, contentWidth)
			lines = append(lines, line)
		}

		// Show "more" indicator if truncated
		if len(m.filteredItems) > maxVisible {
			moreStyle := t.Renderer.NewStyle().Foreground(t.Subtext).Italic(true)
			remaining := len(m.filteredItems) - endIdx
			if remaining > 0 {
				lines = append(lines, moreStyle.Render(
					strings.Repeat(" ", 2)+"↓ "+strconv.Itoa(remaining)+" more"))
			}
		}
	}

	return strings.Join(lines, "\n")
}

// renderRightPanel routes to the appropriate stats panel or welcome
func (m *LensSelectorModel) renderRightPanel(width, height int) string {
	// Show welcome if no navigation yet
	if !m.hasNavigated || len(m.filteredItems) == 0 {
		return m.renderWelcomePanel(width, height)
	}

	// Get selected item
	if m.selectedIndex >= len(m.filteredItems) {
		return m.renderWelcomePanel(width, height)
	}

	item := m.filteredItems[m.selectedIndex]

	// Route to appropriate stats renderer
	switch item.Type {
	case "epic":
		return m.renderEpicStats(item, width, height)
	case "label":
		return m.renderLabelStats(item, width, height)
	case "bead":
		return m.renderBeadStats(item, width, height)
	default:
		return m.renderWelcomePanel(width, height)
	}
}

// renderEpicStats renders statistics for an epic item
func (m *LensSelectorModel) renderEpicStats(item LensItem, width, height int) string {
	t := m.theme
	var lines []string

	// Header box - dynamic width
	headerStyle := t.Renderer.NewStyle().
		Foreground(t.Epic).
		Bold(true)
	boxWidth := width - 4
	if boxWidth < MinBoxWidth {
		boxWidth = MinBoxWidth
	}
	topBorder := "╔" + strings.Repeat("═", boxWidth-2) + "╗"
	bottomBorder := "╚" + strings.Repeat("═", boxWidth-2) + "╝"
	lines = append(lines, headerStyle.Render(topBorder))

	// Truncate title
	title := item.Title
	maxTitleLen := boxWidth - 10
	if maxTitleLen < 5 {
		maxTitleLen = 5
	}
	if len(title) > maxTitleLen {
		title = title[:maxTitleLen-1] + "…"
	}
	titleLine := fmt.Sprintf("║ EPIC: %-*s║", boxWidth-9, title)
	lines = append(lines, headerStyle.Render(titleLine))
	lines = append(lines, headerStyle.Render(bottomBorder))
	lines = append(lines, "")

	// Get children data
	children := m.getEpicChildrenIssues(item.Value)
	statusCounts := m.countStatuses(children)

	// Overview section
	sectionStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)
	labelStyle := t.Renderer.NewStyle().Foreground(t.Subtext)
	valueStyle := t.Renderer.NewStyle().Foreground(t.Base.GetForeground())

	lines = append(lines, sectionStyle.Render("📊 Overview"))
	lines = append(lines, fmt.Sprintf("   %s %s  │  %s %s",
		labelStyle.Render("Children:"),
		valueStyle.Render(strconv.Itoa(item.IssueCount)),
		labelStyle.Render("Closed:"),
		valueStyle.Render(fmt.Sprintf("%d (%.0f%%)", item.ClosedCount, item.Progress*100))))

	// Progress bar
	progressBar := RenderMiniBar(item.Progress, 20, t)
	lines = append(lines, fmt.Sprintf("   %s %s %.0f%%",
		labelStyle.Render("Progress:"),
		progressBar,
		item.Progress*100))
	lines = append(lines, "")

	// Status breakdown
	lines = append(lines, sectionStyle.Render("📈 Status Breakdown"))

	openCount := statusCounts[model.StatusOpen]
	inProgCount := statusCounts[model.StatusInProgress]
	blockedCount := statusCounts[model.StatusBlocked]
	closedCount := statusCounts[model.StatusClosed]
	total := len(children)
	if total == 0 {
		total = 1
	}

	openBar := RenderMiniBar(float64(openCount)/float64(total), 10, t)
	inProgBar := RenderMiniBar(float64(inProgCount)/float64(total), 10, t)
	blockedBar := RenderMiniBar(float64(blockedCount)/float64(total), 10, t)
	closedBar := RenderMiniBar(float64(closedCount)/float64(total), 10, t)

	openStyle := t.Renderer.NewStyle().Foreground(t.Open)
	inProgStyle := t.Renderer.NewStyle().Foreground(t.InProgress)
	blockedStyle := t.Renderer.NewStyle().Foreground(t.Blocked)
	closedStyle := t.Renderer.NewStyle().Foreground(t.Closed)

	lines = append(lines, fmt.Sprintf("   %s %-12s %2d %s",
		openStyle.Render("●"), "Open:", openCount, openBar))
	lines = append(lines, fmt.Sprintf("   %s %-12s %2d %s",
		inProgStyle.Render("●"), "In Progress:", inProgCount, inProgBar))
	lines = append(lines, fmt.Sprintf("   %s %-12s %2d %s",
		blockedStyle.Render("●"), "Blocked:", blockedCount, blockedBar))
	lines = append(lines, fmt.Sprintf("   %s %-12s %2d %s",
		closedStyle.Render("●"), "Closed:", closedCount, closedBar))
	lines = append(lines, "")

	// Dependencies
	blockers := m.getBlockers(item.Value)
	dependents := m.getDependents(item.Value)
	lines = append(lines, sectionStyle.Render("🔗 Dependencies"))
	lines = append(lines, fmt.Sprintf("   %s %s  │  %s %s",
		labelStyle.Render("Blocked by:"),
		valueStyle.Render(strconv.Itoa(len(blockers))),
		labelStyle.Render("Blocks:"),
		valueStyle.Render(strconv.Itoa(len(dependents)))))
	lines = append(lines, "")

	// Centrality metrics (if available)
	prRank, prScore, btRank, btScore, total := m.getCentralityRank(item.Value)
	if prRank > 0 || btRank > 0 {
		lines = append(lines, sectionStyle.Render("📊 Centrality"))
		if prRank > 0 {
			rankBadge := RenderRankBadge(prRank, total)
			lines = append(lines, fmt.Sprintf("   %s %s (%.3f)",
				labelStyle.Render("PageRank:"),
				rankBadge,
				prScore))
		}
		if btRank > 0 {
			rankBadge := RenderRankBadge(btRank, total)
			lines = append(lines, fmt.Sprintf("   %s %s (%.3f)",
				labelStyle.Render("Betweenness:"),
				rankBadge,
				btScore))
		}
	}

	// Pad to fixed height for consistent layout
	return padToHeight(strings.Join(lines, "\n"), height, width)
}

// renderLabelStats renders statistics for a label item
func (m *LensSelectorModel) renderLabelStats(item LensItem, width, height int) string {
	t := m.theme
	var lines []string

	// Header box - dynamic width
	headerStyle := t.Renderer.NewStyle().
		Foreground(t.Secondary).
		Bold(true)
	boxWidth := width - 4
	if boxWidth < MinBoxWidth {
		boxWidth = MinBoxWidth
	}
	topBorder := "╔" + strings.Repeat("═", boxWidth-2) + "╗"
	bottomBorder := "╚" + strings.Repeat("═", boxWidth-2) + "╝"
	lines = append(lines, headerStyle.Render(topBorder))

	// Truncate title
	title := item.Title
	maxTitleLen := boxWidth - 11
	if maxTitleLen < 5 {
		maxTitleLen = 5
	}
	if len(title) > maxTitleLen {
		title = title[:maxTitleLen-1] + "…"
	}
	titleLine := fmt.Sprintf("║ LABEL: %-*s║", boxWidth-10, title)
	lines = append(lines, headerStyle.Render(titleLine))
	lines = append(lines, headerStyle.Render(bottomBorder))
	lines = append(lines, "")

	// Get issues with this label
	issues := m.getIssuesWithLabel(item.Value)
	statusCounts := m.countStatuses(issues)
	typeCounts := m.countTypes(issues)

	// Overview section
	sectionStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)
	labelStyle := t.Renderer.NewStyle().Foreground(t.Subtext)
	valueStyle := t.Renderer.NewStyle().Foreground(t.Base.GetForeground())

	lines = append(lines, sectionStyle.Render("📊 Overview"))
	lines = append(lines, fmt.Sprintf("   %s %s  │  %s %s",
		labelStyle.Render("Issues:"),
		valueStyle.Render(strconv.Itoa(item.IssueCount)),
		labelStyle.Render("Closed:"),
		valueStyle.Render(fmt.Sprintf("%d (%.0f%%)", item.ClosedCount, item.Progress*100))))

	// Progress bar
	progressBar := RenderMiniBar(item.Progress, 20, t)
	lines = append(lines, fmt.Sprintf("   %s %s %.0f%%",
		labelStyle.Render("Progress:"),
		progressBar,
		item.Progress*100))
	lines = append(lines, "")

	// Status distribution
	lines = append(lines, sectionStyle.Render("📈 Status Distribution"))

	openCount := statusCounts[model.StatusOpen]
	inProgCount := statusCounts[model.StatusInProgress]
	blockedCount := statusCounts[model.StatusBlocked]
	closedCount := statusCounts[model.StatusClosed]
	total := len(issues)
	if total == 0 {
		total = 1
	}

	openBar := RenderMiniBar(float64(openCount)/float64(total), 10, t)
	inProgBar := RenderMiniBar(float64(inProgCount)/float64(total), 10, t)
	blockedBar := RenderMiniBar(float64(blockedCount)/float64(total), 10, t)
	closedBar := RenderMiniBar(float64(closedCount)/float64(total), 10, t)

	openStyle := t.Renderer.NewStyle().Foreground(t.Open)
	inProgStyle := t.Renderer.NewStyle().Foreground(t.InProgress)
	blockedStyle := t.Renderer.NewStyle().Foreground(t.Blocked)
	closedStyle := t.Renderer.NewStyle().Foreground(t.Closed)

	lines = append(lines, fmt.Sprintf("   %s %-12s %2d %s",
		openStyle.Render("●"), "Open:", openCount, openBar))
	lines = append(lines, fmt.Sprintf("   %s %-12s %2d %s",
		inProgStyle.Render("●"), "In Progress:", inProgCount, inProgBar))
	lines = append(lines, fmt.Sprintf("   %s %-12s %2d %s",
		blockedStyle.Render("●"), "Blocked:", blockedCount, blockedBar))
	lines = append(lines, fmt.Sprintf("   %s %-12s %2d %s",
		closedStyle.Render("●"), "Closed:", closedCount, closedBar))
	lines = append(lines, "")

	// Related labels
	related := m.getRelatedLabels(item.Value, 3)
	if len(related) > 0 {
		lines = append(lines, sectionStyle.Render("🏷 Related Labels"))
		for _, r := range related {
			lines = append(lines, fmt.Sprintf("   %s %-15s %s",
				labelStyle.Render("●"),
				r.Label+":",
				valueStyle.Render(strconv.Itoa(r.Count)+" issues")))
		}
		lines = append(lines, "")
	}

	// Type breakdown
	lines = append(lines, sectionStyle.Render("📦 Types"))
	var typeParts []string
	if c := typeCounts[model.TypeBug]; c > 0 {
		typeParts = append(typeParts, fmt.Sprintf("Bug: %d", c))
	}
	if c := typeCounts[model.TypeFeature]; c > 0 {
		typeParts = append(typeParts, fmt.Sprintf("Feature: %d", c))
	}
	if c := typeCounts[model.TypeTask]; c > 0 {
		typeParts = append(typeParts, fmt.Sprintf("Task: %d", c))
	}
	if c := typeCounts[model.TypeChore]; c > 0 {
		typeParts = append(typeParts, fmt.Sprintf("Chore: %d", c))
	}
	if len(typeParts) > 0 {
		lines = append(lines, "   "+valueStyle.Render(strings.Join(typeParts, "  ")))
	}

	// Pad to fixed height for consistent layout
	return padToHeight(strings.Join(lines, "\n"), height, width)
}

// renderBeadStats renders statistics for a bead/issue item
func (m *LensSelectorModel) renderBeadStats(item LensItem, width, height int) string {
	t := m.theme
	var lines []string

	issue := m.issueMap[item.Value]
	if issue == nil {
		return t.Renderer.NewStyle().Foreground(t.Subtext).Render("Issue not found")
	}

	// Header box - dynamic width
	headerStyle := t.Renderer.NewStyle().
		Foreground(t.InProgress).
		Bold(true)
	boxWidth := width - 4
	if boxWidth < MinBoxWidth {
		boxWidth = MinBoxWidth
	}
	topBorder := "╔" + strings.Repeat("═", boxWidth-2) + "╗"
	bottomBorder := "╚" + strings.Repeat("═", boxWidth-2) + "╝"
	lines = append(lines, headerStyle.Render(topBorder))

	// Truncate bead ID if needed
	beadID := item.Value
	maxIDLen := boxWidth - 10
	if maxIDLen < 5 {
		maxIDLen = 5
	}
	if len(beadID) > maxIDLen {
		beadID = beadID[:maxIDLen-1] + "…"
	}
	titleLine := fmt.Sprintf("║ BEAD: %-*s║", boxWidth-9, beadID)
	lines = append(lines, headerStyle.Render(titleLine))
	lines = append(lines, headerStyle.Render(bottomBorder))
	lines = append(lines, "")

	// Issue title
	titleStyle := t.Renderer.NewStyle().Foreground(t.Base.GetForeground()).Italic(true)
	title := issue.Title
	maxTitleLen := width - 4
	if len(title) > maxTitleLen {
		title = title[:maxTitleLen-1] + "…"
	}
	lines = append(lines, titleStyle.Render(title))
	lines = append(lines, "")

	// Details section
	sectionStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)
	labelStyle := t.Renderer.NewStyle().Foreground(t.Subtext)

	lines = append(lines, sectionStyle.Render("📋 Details"))

	// Status badge
	statusBadge := RenderStatusBadge(string(issue.Status))
	lines = append(lines, fmt.Sprintf("   %s   %s",
		labelStyle.Render("Status:"),
		statusBadge))

	// Priority badge
	priorityBadge := RenderPriorityBadge(issue.Priority)
	lines = append(lines, fmt.Sprintf("   %s %s",
		labelStyle.Render("Priority:"),
		priorityBadge))

	// Type
	typeIcon, typeColor := t.GetTypeIcon(string(issue.IssueType))
	typeStyle := t.Renderer.NewStyle().Foreground(typeColor)
	lines = append(lines, fmt.Sprintf("   %s     %s %s",
		labelStyle.Render("Type:"),
		typeIcon,
		typeStyle.Render(string(issue.IssueType))))
	lines = append(lines, "")

	// Labels - with wrapping and overflow indicator
	if len(issue.Labels) > 0 {
		lines = append(lines, sectionStyle.Render("🏷 Labels"))
		chipStyle := t.Renderer.NewStyle().Foreground(t.Primary)
		maxLabelWidth := width - 8 // Leave margin for indentation

		// Build label rows with width constraint
		var currentRow []string
		currentRowWidth := 0
		var labelRows []string

		for _, l := range issue.Labels {
			chipText := l
			chipWidth := len(l) + 3 // label + " • " separator

			// If adding this chip exceeds width and we have items, start new row
			if currentRowWidth+chipWidth > maxLabelWidth && len(currentRow) > 0 {
				labelRows = append(labelRows, "   "+strings.Join(currentRow, " • "))
				currentRow = nil
				currentRowWidth = 0
			}

			currentRow = append(currentRow, chipStyle.Render(chipText))
			currentRowWidth += chipWidth
		}
		// Add remaining row
		if len(currentRow) > 0 {
			labelRows = append(labelRows, "   "+strings.Join(currentRow, " • "))
		}

		// Limit to 2 rows, show overflow indicator
		maxRows := 2
		for i, row := range labelRows {
			if i >= maxRows {
				remaining := len(labelRows) - maxRows
				overflowStyle := t.Renderer.NewStyle().Foreground(t.Subtext).Italic(true)
				lines = append(lines, "   "+overflowStyle.Render(fmt.Sprintf("+%d more rows", remaining)))
				break
			}
			lines = append(lines, row)
		}
		lines = append(lines, "")
	}

	// Dependencies
	blockers := m.getBlockers(item.Value)
	dependents := m.getDependents(item.Value)

	lines = append(lines, sectionStyle.Render("🔗 Dependencies"))
	if len(blockers) > 0 {
		blockerStyle := t.Renderer.NewStyle().Foreground(t.Blocked)
		lines = append(lines, fmt.Sprintf("   %s (%d):",
			blockerStyle.Render("↓ Blocked by"),
			len(blockers)))
		for _, b := range blockers {
			if len(b) > 0 {
				bIssue := m.issueMap[b]
				if bIssue != nil {
					bTitle := bIssue.Title
					if len(bTitle) > 25 {
						bTitle = bTitle[:24] + "…"
					}
					lines = append(lines, fmt.Sprintf("     %s %s",
						labelStyle.Render(b),
						t.Renderer.NewStyle().Foreground(t.Subtext).Render(bTitle)))
				} else {
					lines = append(lines, fmt.Sprintf("     %s", labelStyle.Render(b)))
				}
			}
		}
	}

	if len(dependents) > 0 {
		dependentStyle := t.Renderer.NewStyle().Foreground(t.Open)
		lines = append(lines, fmt.Sprintf("   %s (%d):",
			dependentStyle.Render("↑ Blocks"),
			len(dependents)))
		// Show up to 3 dependents
		shown := dependents
		if len(shown) > 3 {
			shown = shown[:3]
		}
		lines = append(lines, "     "+strings.Join(shown, ", "))
		if len(dependents) > 3 {
			lines = append(lines, fmt.Sprintf("     ... and %d more", len(dependents)-3))
		}
	}

	if len(blockers) == 0 && len(dependents) == 0 {
		lines = append(lines, "   "+labelStyle.Render("No dependencies"))
	}
	lines = append(lines, "")

	// Centrality metrics
	prRank, prScore, btRank, btScore, total := m.getCentralityRank(item.Value)
	if prRank > 0 || btRank > 0 {
		lines = append(lines, sectionStyle.Render("📊 Centrality"))
		if prRank > 0 {
			rankBadge := RenderRankBadge(prRank, total)
			lines = append(lines, fmt.Sprintf("   %s %s (%.3f)",
				labelStyle.Render("PageRank:"),
				rankBadge,
				prScore))
		}
		if btRank > 0 {
			rankBadge := RenderRankBadge(btRank, total)
			lines = append(lines, fmt.Sprintf("   %s %s (%.3f)",
				labelStyle.Render("Betweenness:"),
				rankBadge,
				btScore))
		}
	}

	// Pad to fixed height for consistent layout
	return padToHeight(strings.Join(lines, "\n"), height, width)
}

// renderStackedLayout renders the selector in stacked mode for narrow terminals
func (m *LensSelectorModel) renderStackedLayout() string {
	t := m.theme

	// Calculate dimensions
	totalWidth := m.width - 6
	if totalWidth < 50 {
		totalWidth = 50
	}

	listHeight := (m.height * 55) / 100 // 55% for list
	statsHeight := (m.height * 35) / 100 // 35% for stats

	// Render header
	header := m.renderLensHeader(totalWidth)

	// Render left panel (list)
	leftContent := m.renderLeftPanel(totalWidth, listHeight)

	// Render right panel (stats)
	rightContent := m.renderRightPanel(totalWidth, statsHeight)

	// Divider between panels
	divider := t.Renderer.NewStyle().
		Foreground(ColorBgHighlight).
		Render(strings.Repeat("─", totalWidth-4))

	// Render footer
	footer := m.renderKeybindFooter(totalWidth)

	// Combine vertically
	content := lipgloss.JoinVertical(lipgloss.Left,
		header,
		"",
		leftContent,
		"",
		divider,
		"",
		rightContent,
		"",
		footer,
	)

	// Box style
	boxStyle := t.Renderer.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Primary).
		Padding(1, 2)

	box := boxStyle.Render(content)

	// Center in viewport
	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		box,
	)
}

// renderMinimalLayout renders a minimal list-only view for very narrow terminals
func (m *LensSelectorModel) renderMinimalLayout() string {
	t := m.theme

	// Calculate dimensions - use nearly full width
	totalWidth := m.width - 4
	if totalWidth < 30 {
		totalWidth = 30
	}
	contentWidth := totalWidth - 2 // Account for box padding

	var lines []string

	// Minimal header - just LENS
	headerStyle := t.Renderer.NewStyle().
		Foreground(t.Primary).
		Bold(true)
	lines = append(lines, headerStyle.Render("LENS"))
	lines = append(lines, "")

	// Search input (simplified, no border to save space)
	searchStyle := t.Renderer.NewStyle().
		Foreground(t.Base.GetForeground())
	promptStyle := t.Renderer.NewStyle().
		Foreground(t.Primary).
		Bold(true)

	searchValue := m.searchInput.Value()
	if m.insertMode {
		cursorStyle := t.Renderer.NewStyle().
			Background(t.Primary).
			Foreground(t.Base.GetBackground())
		lines = append(lines, promptStyle.Render("> ")+searchStyle.Render(searchValue)+cursorStyle.Render(" "))
	} else if searchValue == "" {
		lines = append(lines, promptStyle.Render("> ")+t.Renderer.NewStyle().Foreground(t.Subtext).Render("search..."))
	} else {
		lines = append(lines, promptStyle.Render("> ")+searchStyle.Render(searchValue))
	}
	lines = append(lines, "")

	// Calculate max visible - account for header(2) + search(2) + footer(2) + box border(2)
	maxVisible := m.height - 8
	if maxVisible < 3 {
		maxVisible = 3
	}

	// Item list
	if len(m.filteredItems) == 0 {
		emptyStyle := t.Renderer.NewStyle().Foreground(t.Subtext).Italic(true)
		lines = append(lines, emptyStyle.Render("  No items"))
	} else {
		// Calculate scroll window to keep selected in view
		startIdx := 0
		if m.selectedIndex >= maxVisible {
			startIdx = m.selectedIndex - maxVisible + 1
		}
		endIdx := startIdx + maxVisible
		if endIdx > len(m.filteredItems) {
			endIdx = len(m.filteredItems)
		}

		for i := startIdx; i < endIdx; i++ {
			item := m.filteredItems[i]
			lines = append(lines, m.renderMinimalItem(item, i == m.selectedIndex, contentWidth))
		}

		// Scroll indicator if more items
		if endIdx < len(m.filteredItems) {
			moreStyle := t.Renderer.NewStyle().Foreground(t.Subtext)
			lines = append(lines, moreStyle.Render(fmt.Sprintf("  ... %d more", len(m.filteredItems)-endIdx)))
		}
	}

	// Pad to fill available height before footer
	targetLines := m.height - 6 // header(2) + search(2) + footer(2)
	for len(lines) < targetLines {
		lines = append(lines, "")
	}

	// Responsive footer based on width
	lines = append(lines, "")
	lines = append(lines, m.renderMinimalFooter(contentWidth))

	content := strings.Join(lines, "\n")

	// Thin box style
	boxStyle := t.Renderer.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Primary).
		Padding(0, 1)

	box := boxStyle.Render(content)

	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		box,
	)
}

// renderMinimalFooter creates a width-responsive footer for minimal layout
func (m *LensSelectorModel) renderMinimalFooter(width int) string {
	t := m.theme
	keyStyle := t.Renderer.NewStyle().Foreground(t.Primary).Bold(true)
	descStyle := t.Renderer.NewStyle().Foreground(t.Subtext)

	// Build keybinds based on available width
	if width >= 60 {
		// Full descriptions
		if m.insertMode {
			return keyStyle.Render("j/k") + descStyle.Render(" navigate") + "  " +
				keyStyle.Render("enter") + descStyle.Render(" select") + "  " +
				keyStyle.Render("esc") + descStyle.Render(" back")
		}
		return keyStyle.Render("j/k") + descStyle.Render(" navigate") + "  " +
			keyStyle.Render("i") + descStyle.Render(" insert") + "  " +
			keyStyle.Render("enter") + descStyle.Render(" select") + "  " +
			keyStyle.Render("q") + descStyle.Render(" quit")
	} else if width >= 45 {
		// Abbreviated
		if m.insertMode {
			return keyStyle.Render("j/k") + descStyle.Render(" nav") + "  " +
				keyStyle.Render("⏎") + descStyle.Render(" sel") + "  " +
				keyStyle.Render("esc")
		}
		return keyStyle.Render("j/k") + descStyle.Render(" nav") + "  " +
			keyStyle.Render("i") + descStyle.Render(" insert") + "  " +
			keyStyle.Render("q") + descStyle.Render(" quit")
	}
	// Ultra-compact for very narrow
	if m.insertMode {
		return keyStyle.Render("j/k") + " " + keyStyle.Render("⏎") + " " + keyStyle.Render("esc")
	}
	return keyStyle.Render("j/k") + " " + keyStyle.Render("i") + " " + keyStyle.Render("q")
}

// renderMinimalItem renders a compact item for minimal layout
func (m *LensSelectorModel) renderMinimalItem(item LensItem, isSelected bool, maxWidth int) string {
	t := m.theme

	prefix := "  "
	if isSelected {
		prefix = "▸ "
	}

	// Type indicator
	var typeChar string
	switch item.Type {
	case "epic":
		typeChar = t.Renderer.NewStyle().Foreground(t.Primary).Bold(true).Render("E")
	case "bead":
		typeChar = t.Renderer.NewStyle().Foreground(t.InProgress).Bold(true).Render("B")
	default:
		typeChar = t.Renderer.NewStyle().Foreground(t.Secondary).Bold(true).Render("L")
	}

	// Title with truncation
	title := item.Title
	maxTitleLen := maxWidth - 8
	if len(title) > maxTitleLen && maxTitleLen > 5 {
		title = title[:maxTitleLen-1] + "…"
	}

	nameStyle := t.Renderer.NewStyle()
	if isSelected {
		nameStyle = nameStyle.Foreground(t.Primary).Bold(true)
	}

	return prefix + typeChar + " " + nameStyle.Render(title)
}

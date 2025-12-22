package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

// ══════════════════════════════════════════════════════════════════════════════

// GetScopeLabels returns the currently selected scope labels
func (m *LensDashboardModel) GetScopeLabels() []string {
	return m.scopeLabels
}

// GetScopeMode returns the current scope mode (union or intersection)
func (m *LensDashboardModel) GetScopeMode() ScopeMode {
	return m.scopeMode
}

// HasScope returns true if scope filtering is active
func (m *LensDashboardModel) HasScope() bool {
	return len(m.scopeLabels) > 0
}

// AddScopeLabel adds a label to the scope (if not already present)
func (m *LensDashboardModel) AddScopeLabel(label string) {
	// Check if already in scope
	for _, l := range m.scopeLabels {
		if l == label {
			return // Already present
		}
	}
	m.scopeLabels = append(m.scopeLabels, label)
	m.rebuildWithScope()
}

// RemoveScopeLabel removes a specific label from the scope
func (m *LensDashboardModel) RemoveScopeLabel(label string) {
	for i, l := range m.scopeLabels {
		if l == label {
			m.scopeLabels = append(m.scopeLabels[:i], m.scopeLabels[i+1:]...)
			m.rebuildWithScope()
			return
		}
	}
}

// RemoveLastScopeLabel removes the most recently added scope label
func (m *LensDashboardModel) RemoveLastScopeLabel() bool {
	if len(m.scopeLabels) == 0 {
		return false
	}
	m.scopeLabels = m.scopeLabels[:len(m.scopeLabels)-1]
	m.rebuildWithScope()
	return true
}

// ClearScope clears all scope labels
func (m *LensDashboardModel) ClearScope() {
	m.scopeLabels = nil
	m.rebuildWithScope()
}

// ToggleScopeMode toggles between union (ANY) and intersection (ALL) mode
func (m *LensDashboardModel) ToggleScopeMode() {
	if m.scopeMode == ScopeModeUnion {
		m.scopeMode = ScopeModeIntersection
	} else {
		m.scopeMode = ScopeModeUnion
	}
	// Rebuild only if scope is active
	if len(m.scopeLabels) > 0 {
		m.rebuildWithScope()
	}
}

// rebuildWithScope rebuilds primaryIDs based on scope and rebuilds tree
func (m *LensDashboardModel) rebuildWithScope() {
	// If no scope, reset to original behavior
	if len(m.scopeLabels) == 0 {
		// Reset primaryIDs based on view mode
		if m.viewMode == "label" {
			// Rebuild directPrimaryIDs from labelName
			m.directPrimaryIDs = make(map[string]bool)
			for _, issue := range m.allIssues {
				for _, label := range issue.Labels {
					if label == m.labelName {
						m.directPrimaryIDs[issue.ID] = true
						break
					}
				}
			}
			m.primaryIDs = expandToDescendants(m.directPrimaryIDs, m.allIssues)
		}
		// For epic/bead modes, scope doesn't change primary logic (epic children stay)
	} else {
		// Apply scope filtering
		m.applyScopeFilter()
	}

	m.buildGraphs()
	m.buildTree()
	m.recomputeWorkstreams()
}

// applyScopeFilter filters primaryIDs to only include issues matching scope criteria
func (m *LensDashboardModel) applyScopeFilter() {
	if len(m.scopeLabels) == 0 {
		return
	}

	// Build set of issues that match scope criteria
	scopeMatchingIDs := make(map[string]bool)

	for _, issue := range m.allIssues {
		matches := m.issueMatchesScope(issue)
		if matches {
			scopeMatchingIDs[issue.ID] = true
		}
	}

	// For label mode: intersection of original primaryIDs and scope-matching IDs
	if m.viewMode == "label" {
		// First get the original primary IDs (issues with the labelName)
		originalPrimaryIDs := make(map[string]bool)
		for _, issue := range m.allIssues {
			for _, label := range issue.Labels {
				if label == m.labelName {
					originalPrimaryIDs[issue.ID] = true
					break
				}
			}
		}

		// Intersect with scope matching
		m.directPrimaryIDs = make(map[string]bool)
		for id := range originalPrimaryIDs {
			if scopeMatchingIDs[id] {
				m.directPrimaryIDs[id] = true
			}
		}
		m.primaryIDs = expandToDescendants(m.directPrimaryIDs, m.allIssues)
	} else {
		// For epic/bead modes, filter the existing primaryIDs
		filteredPrimary := make(map[string]bool)
		for id := range m.primaryIDs {
			if scopeMatchingIDs[id] {
				filteredPrimary[id] = true
			}
		}
		// Always keep the entry point visible
		if m.epicID != "" {
			filteredPrimary[m.epicID] = true
		}
		m.primaryIDs = filteredPrimary
	}
}

// issueMatchesScope checks if an issue matches the current scope criteria
func (m *LensDashboardModel) issueMatchesScope(issue model.Issue) bool {
	if len(m.scopeLabels) == 0 {
		return true
	}

	// Build set of issue's labels for quick lookup
	issueLabels := make(map[string]bool)
	for _, label := range issue.Labels {
		issueLabels[label] = true
	}

	if m.scopeMode == ScopeModeUnion {
		// Union: issue has ANY of the scope labels
		for _, scopeLabel := range m.scopeLabels {
			if issueLabels[scopeLabel] {
				return true
			}
		}
		return false
	}

	// Intersection: issue has ALL of the scope labels
	for _, scopeLabel := range m.scopeLabels {
		if !issueLabels[scopeLabel] {
			return false
		}
	}
	return true
}

// GetAvailableScopeLabels returns labels that co-occur with current scope
// Useful for suggesting additional scope labels
func (m *LensDashboardModel) GetAvailableScopeLabels() []string {
	if len(m.scopeLabels) == 0 {
		// Return all unique labels
		labelSet := make(map[string]bool)
		for _, issue := range m.allIssues {
			for _, label := range issue.Labels {
				labelSet[label] = true
			}
		}
		var labels []string
		for label := range labelSet {
			labels = append(labels, label)
		}
		sort.Strings(labels)
		return labels
	}

	// Return labels that co-occur with current scope (excluding already selected)
	scopeSet := make(map[string]bool)
	for _, l := range m.scopeLabels {
		scopeSet[l] = true
	}

	cooccurring := make(map[string]int)
	for _, issue := range m.allIssues {
		if m.issueMatchesScope(issue) {
			for _, label := range issue.Labels {
				if !scopeSet[label] {
					cooccurring[label]++
				}
			}
		}
	}

	// Sort by count (descending)
	var sorted []LabelCount
	for label, count := range cooccurring {
		sorted = append(sorted, LabelCount{Label: label, Count: count})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Count > sorted[j].Count
	})

	var result []string
	for _, lc := range sorted {
		result = append(result, lc.Label)
	}
	return result
}

// ══════════════════════════════════════════════════════════════════════════════
// SCOPE INPUT MODAL - Inline label input for scope filtering
// ══════════════════════════════════════════════════════════════════════════════

// ShowScopeInput returns true if the scope input modal is visible
func (m *LensDashboardModel) ShowScopeInput() bool {
	return m.showScopeInput
}

// OpenScopeInput opens the scope input modal
func (m *LensDashboardModel) OpenScopeInput() {
	m.showScopeInput = true
	m.scopeInput = ""
}

// CloseScopeInput closes the scope input modal
func (m *LensDashboardModel) CloseScopeInput() {
	m.showScopeInput = false
	m.scopeInput = ""
}

// GetScopeInput returns the current scope input text
func (m *LensDashboardModel) GetScopeInput() string {
	return m.scopeInput
}

// HandleScopeInputKey handles a key press when the scope input modal is open
// Returns true if the key was handled, false if modal should close
func (m *LensDashboardModel) HandleScopeInputKey(key string) (handled bool, statusMsg string) {
	switch key {
	case "esc":
		m.CloseScopeInput()
		return true, "Scope input cancelled"
	case "enter":
		// Add the label to scope if it's a valid label
		if m.scopeInput != "" {
			label := strings.TrimSpace(m.scopeInput)
			// Check if it's a valid label (exists in the data)
			isValid := false
			for _, issue := range m.allIssues {
				for _, l := range issue.Labels {
					if strings.EqualFold(l, label) {
						label = l // Use exact case from data
						isValid = true
						break
					}
				}
				if isValid {
					break
				}
			}
			if isValid {
				// Check if already in scope
				alreadyInScope := false
				for _, l := range m.scopeLabels {
					if l == label {
						alreadyInScope = true
						break
					}
				}
				if !alreadyInScope {
					m.AddScopeLabel(label)
					m.CloseScopeInput()
					return true, fmt.Sprintf("Added '%s' to scope (%s)", label, m.scopeMode.ShortString())
				}
				m.CloseScopeInput()
				return true, fmt.Sprintf("'%s' already in scope", label)
			}
			m.scopeInput = ""
			return true, fmt.Sprintf("Label '%s' not found", label)
		}
		m.CloseScopeInput()
		return true, ""
	case "backspace", "ctrl+h":
		if len(m.scopeInput) > 0 {
			m.scopeInput = m.scopeInput[:len(m.scopeInput)-1]
		}
		return true, ""
	case "tab":
		// Auto-complete with first matching label
		if m.scopeInput != "" {
			query := strings.ToLower(m.scopeInput)
			for _, label := range m.GetAvailableScopeLabels() {
				if strings.HasPrefix(strings.ToLower(label), query) {
					m.scopeInput = label
					return true, ""
				}
			}
		}
		return true, ""
	default:
		// Add printable characters to input
		if len(key) == 1 && key[0] >= 32 && key[0] < 127 {
			m.scopeInput += key
			return true, ""
		}
	}
	return false, ""
}

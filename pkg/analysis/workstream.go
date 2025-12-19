package analysis

import (
	"regexp"
	"sort"
	"strings"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

// === CROSS-WORKSTREAM DEPENDENCIES ===

// CrossWorkstreamBlocker represents a blocking relationship across workstreams
type CrossWorkstreamBlocker struct {
	BlockerID         string // The issue doing the blocking
	BlockerWorkstream string // Name of the workstream containing the blocker
	BlockedID         string // The issue being blocked
	BlockedWorkstream string // Name of the workstream containing the blocked issue
}

// === WORKSTREAM ===

// Workstream represents a connected component of issues within a label view.
type Workstream struct {
	ID           string        // Representative issue ID or label-based ID
	Name         string        // Auto-detected name or "standalone"
	Issues       []model.Issue // All issues in this component
	IssueIDs     []string      // Issue IDs for quick lookup
	PrimaryCount int           // Issues with the selected label
	ContextCount int           // Issues pulled in via deps
	Progress     float64       // Closed / Total (primary only)
	IsBlocked    bool          // All actionable issues blocked?

	// Detailed counts
	ReadyCount      int
	BlockedCount    int
	InProgressCount int
	ClosedCount     int

	// Related labels (excluding the selected one)
	RelatedLabels []string

	// Cross-workstream dependencies
	CrossBlockedBy []CrossWorkstreamBlocker
	CrossBlocks    []CrossWorkstreamBlocker

	// Ordering for sequential families
	Order int

	// Sub-grouping support
	SubWorkstreams []*Workstream // Child workstreams (populated by SubdivideWorkstream)
	Depth          int           // Nesting depth (0 = top level)
	GroupedBy      string        // What family/method was used to create this group
}

// === LABEL FAMILY DETECTION ===

// LabelFamily groups related labels (e.g., phase1/phase2/phase3)
type LabelFamily struct {
	Type       string   // "sequential", "prefixed", "generic"
	Prefix     string   // e.g., "phase", "feat:"
	Labels     []string // All labels in this family
	Sequential bool     // True if labels have numeric ordering
}

// LabelStats holds statistics for a single label
type WorkstreamLabelStats struct {
	Label    string
	Count    int
	Coverage float64 // count / total issues
	Family   *LabelFamily
	Order    int // For sequential: extracted number
}

// FamilyScore evaluates how well a family partitions issues
type FamilyScore struct {
	Family      *LabelFamily
	Score       float64
	Coverage    float64 // % of issues covered by family
	Exclusivity float64 // % of covered issues with exactly one label from family
	Balance     float64 // How evenly distributed across labels
}

// Minimum score threshold for label-based grouping
const MinFamilyScore = 0.15

// ViewContext provides context for workstream detection
type ViewContext struct {
	Type          string // "label" or "epic"
	SelectedLabel string // The label/epic being viewed
}

// GroupingOptions controls sub-grouping behavior
type GroupingOptions struct {
	MaxDepth        int      // Maximum recursion depth (1-3 typically)
	CurrentDepth    int      // Current depth (0 = top level)
	ExcludeLabels   []string // Labels to exclude from grouping (already used at parent levels)
	ExcludeFamilies []string // Family prefixes to exclude
	MinGroupSize    int      // Minimum issues for a workstream (default: 2)
	MinScoreAtDepth float64  // Minimum family score, can be lower at deeper levels
}

// DefaultGroupingOptions returns sensible defaults
func DefaultGroupingOptions() GroupingOptions {
	return GroupingOptions{
		MaxDepth:        3,
		CurrentDepth:    0,
		ExcludeLabels:   nil,
		ExcludeFamilies: nil,
		MinGroupSize:    2,
		MinScoreAtDepth: MinFamilyScore,
	}
}

// OptionsForSubdivision creates options for the next depth level
func (opts GroupingOptions) OptionsForSubdivision(usedFamilyPrefix string, usedLabels []string) GroupingOptions {
	newOpts := GroupingOptions{
		MaxDepth:        opts.MaxDepth,
		CurrentDepth:    opts.CurrentDepth + 1,
		ExcludeLabels:   append(append([]string{}, opts.ExcludeLabels...), usedLabels...),
		ExcludeFamilies: opts.ExcludeFamilies,
		MinGroupSize:    opts.MinGroupSize,
		// Lower threshold at deeper levels to allow finer grouping
		MinScoreAtDepth: opts.MinScoreAtDepth * 0.7,
	}

	// Don't go below a minimum threshold
	if newOpts.MinScoreAtDepth < 0.08 {
		newOpts.MinScoreAtDepth = 0.08
	}

	if usedFamilyPrefix != "" {
		newOpts.ExcludeFamilies = append(newOpts.ExcludeFamilies, usedFamilyPrefix)
	}

	return newOpts
}

// Patterns for detecting sequential labels
var sequentialPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^(.+?)(\d+)$`),          // phase1, sprint2, v3
	regexp.MustCompile(`^(.+?-)(\d+)$`),         // sprint-1, phase-2
	regexp.MustCompile(`^(.+?)\s+(\d+)$`),       // "phase 1", "sprint 2"
	regexp.MustCompile(`^(q)(\d)$`),             // q1, q2, q3, q4
	regexp.MustCompile(`^(v)(\d+(?:\.\d+)?)$`),  // v1, v2, v1.0
}

// Pattern for detecting prefixed labels (colon style)
var colonPrefixPattern = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9]*):(.+)$`) // feat:auth, area:payments

// Patterns for detecting hyphen/underscore prefixed labels
var separatorPrefixPattern = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9]*)[-_](.+)$`) // auth-login, pay_form

// Patterns for detecting suffix-based labels
var separatorSuffixPattern = regexp.MustCompile(`^(.+)[-_]([a-zA-Z][a-zA-Z0-9]*)$`) // task-backend, feature-ui

// looksSequential checks if a string looks like a sequence number
func looksSequential(s string) bool {
	if len(s) == 0 {
		return false
	}
	// Pure digits
	allDigits := true
	for _, c := range s {
		if c < '0' || c > '9' {
			allDigits = false
			break
		}
	}
	if allDigits {
		return true
	}
	// Common sequential patterns
	lower := strings.ToLower(s)
	return lower == "1" || lower == "2" || lower == "3" ||
		strings.HasPrefix(lower, "v") && len(lower) <= 4 ||
		lower == "q1" || lower == "q2" || lower == "q3" || lower == "q4"
}

// DetectLabelFamilies groups labels into families based on naming patterns
func DetectLabelFamilies(labels []string) []*LabelFamily {
	families := make(map[string]*LabelFamily)
	assigned := make(map[string]bool)

	// Pass 1: Find sequential families (phase1, phase2, etc.)
	sequentialGroups := make(map[string][]string)
	for _, label := range labels {
		for _, pattern := range sequentialPatterns {
			if matches := pattern.FindStringSubmatch(strings.ToLower(label)); matches != nil {
				prefix := matches[1]
				sequentialGroups[prefix] = append(sequentialGroups[prefix], label)
				break
			}
		}
	}

	// Only consider it a sequential family if 2+ labels share the prefix
	for prefix, members := range sequentialGroups {
		if len(members) >= 2 {
			familyKey := "seq:" + prefix
			families[familyKey] = &LabelFamily{
				Type:       "sequential",
				Prefix:     prefix,
				Labels:     members,
				Sequential: true,
			}
			for _, label := range members {
				assigned[label] = true
			}
		}
	}

	// Pass 2: Find colon-prefixed families (feat:*, area:*, etc.)
	colonPrefixedGroups := make(map[string][]string)
	for _, label := range labels {
		if assigned[label] {
			continue
		}
		if matches := colonPrefixPattern.FindStringSubmatch(label); matches != nil {
			prefix := matches[1] + ":"
			colonPrefixedGroups[prefix] = append(colonPrefixedGroups[prefix], label)
		}
	}

	for prefix, members := range colonPrefixedGroups {
		if len(members) >= 2 {
			familyKey := "pre:" + prefix
			families[familyKey] = &LabelFamily{
				Type:       "prefixed",
				Prefix:     prefix,
				Labels:     members,
				Sequential: false,
			}
			for _, label := range members {
				assigned[label] = true
			}
		}
	}

	// Pass 3: Find separator-prefixed families (auth-*, pay_*, etc.)
	// Only if not already assigned and the suffix part varies
	separatorPrefixGroups := make(map[string][]string)
	for _, label := range labels {
		if assigned[label] {
			continue
		}
		if matches := separatorPrefixPattern.FindStringSubmatch(label); matches != nil {
			prefix := matches[1]
			// Check it's not a sequential pattern we missed
			if !looksSequential(matches[2]) {
				separatorPrefixGroups[prefix] = append(separatorPrefixGroups[prefix], label)
			}
		}
	}

	for prefix, members := range separatorPrefixGroups {
		if len(members) >= 2 {
			familyKey := "sep-pre:" + prefix
			families[familyKey] = &LabelFamily{
				Type:       "prefixed",
				Prefix:     prefix + "-", // or could detect the actual separator used
				Labels:     members,
				Sequential: false,
			}
			for _, label := range members {
				assigned[label] = true
			}
		}
	}

	// Pass 4: Find suffix-based families (*-backend, *-ui, etc.)
	// Merge all suffix-identified labels into ONE family, with suffix values as sub-groups
	suffixGroups := make(map[string][]string)
	for _, label := range labels {
		if assigned[label] {
			continue
		}
		if matches := separatorSuffixPattern.FindStringSubmatch(label); matches != nil {
			suffix := matches[2]
			// Ignore numeric suffixes (those should be sequential)
			if !looksSequential(suffix) {
				suffixGroups[suffix] = append(suffixGroups[suffix], label)
			}
		}
	}

	// Collect all valid suffix groups (2+ members each)
	validSuffixLabels := make([]string, 0)
	validSuffixCount := 0
	for _, members := range suffixGroups {
		if len(members) >= 2 {
			validSuffixLabels = append(validSuffixLabels, members...)
			validSuffixCount++
		}
	}

	// Only create a suffix family if we have 2+ distinct suffix values
	// This represents "group by suffix" as a strategy
	if validSuffixCount >= 2 {
		families["suf:_combined_"] = &LabelFamily{
			Type:       "suffixed",
			Prefix:     "_by_suffix_", // Special marker for combined suffix family
			Labels:     validSuffixLabels,
			Sequential: false,
		}
		for _, label := range validSuffixLabels {
			assigned[label] = true
		}
	} else {
		// If only one suffix group, create it as a single family
		for suffix, members := range suffixGroups {
			if len(members) >= 2 {
				familyKey := "suf:" + suffix
				families[familyKey] = &LabelFamily{
					Type:       "suffixed",
					Prefix:     "-" + suffix,
					Labels:     members,
					Sequential: false,
				}
				for _, label := range members {
					assigned[label] = true
				}
			}
		}
	}

	// Pass 5: Remaining labels are singletons (generic)
	for _, label := range labels {
		if !assigned[label] {
			families["gen:"+label] = &LabelFamily{
				Type:       "generic",
				Prefix:     "",
				Labels:     []string{label},
				Sequential: false,
			}
		}
	}

	result := make([]*LabelFamily, 0, len(families))
	for _, f := range families {
		result = append(result, f)
	}
	return result
}

// AnalyzeLabels computes statistics for all labels in the issue set
func AnalyzeLabels(issues []model.Issue, contextLabel string) map[string]*WorkstreamLabelStats {
	stats := make(map[string]*WorkstreamLabelStats)
	total := len(issues)
	if total == 0 {
		return stats
	}

	// Count occurrences
	for _, issue := range issues {
		for _, label := range issue.Labels {
			if label == contextLabel {
				continue
			}
			if stats[label] == nil {
				stats[label] = &WorkstreamLabelStats{Label: label}
			}
			stats[label].Count++
		}
	}

	// Compute coverage
	for _, s := range stats {
		s.Coverage = float64(s.Count) / float64(total)
	}

	// Detect families and assign
	allLabels := make([]string, 0, len(stats))
	for label := range stats {
		allLabels = append(allLabels, label)
	}
	families := DetectLabelFamilies(allLabels)

	for _, family := range families {
		for i, label := range family.Labels {
			if stats[label] != nil {
				stats[label].Family = family
				if family.Sequential {
					stats[label].Order = extractSequenceNumber(label)
				} else {
					stats[label].Order = i
				}
			}
		}
	}

	return stats
}

// extractSequenceNumber pulls the numeric part from a sequential label
func extractSequenceNumber(label string) int {
	for _, pattern := range sequentialPatterns {
		if matches := pattern.FindStringSubmatch(strings.ToLower(label)); matches != nil {
			numStr := strings.Split(matches[2], ".")[0]
			var num int
			for _, c := range numStr {
				if c >= '0' && c <= '9' {
					num = num*10 + int(c-'0')
				}
			}
			return num
		}
	}
	return 0
}

// === FAMILY SCORING ===

// ScoreFamily evaluates how well a label family partitions the issues
func ScoreFamily(family *LabelFamily, issues []model.Issue, contextLabel string) *FamilyScore {
	if len(family.Labels) < 2 && family.Type != "generic" {
		return &FamilyScore{Family: family, Score: 0}
	}

	// Count issues per label in family
	labelCounts := make(map[string]int)
	issuesWithFamily := make(map[string]bool)
	issuesWithMultiple := 0

	for _, issue := range issues {
		labelsFromFamily := 0
		for _, label := range issue.Labels {
			if label == contextLabel {
				continue
			}
			for _, familyLabel := range family.Labels {
				if label == familyLabel {
					labelCounts[label]++
					issuesWithFamily[issue.ID] = true
					labelsFromFamily++
				}
			}
		}
		if labelsFromFamily > 1 {
			issuesWithMultiple++
		}
	}

	coveredCount := len(issuesWithFamily)
	total := len(issues)

	if coveredCount == 0 {
		return &FamilyScore{Family: family, Score: 0}
	}

	// Coverage: what % of issues have at least one label from this family
	coverage := float64(coveredCount) / float64(total)

	// Exclusivity: what % of covered issues have exactly ONE label from family
	exclusivity := 1.0
	if coveredCount > 0 {
		exclusivity = float64(coveredCount-issuesWithMultiple) / float64(coveredCount)
	}

	// Balance: how evenly distributed (using coefficient of variation)
	balance := computeBalance(labelCounts)

	// Base score
	score := coverage * exclusivity * balance

	// Boost for sequential families (natural ordering)
	if family.Sequential {
		score *= 1.4
	}

	// Boost for prefixed families (explicit structure)
	if family.Type == "prefixed" {
		score *= 1.2
	}

	// Slight boost for suffixed families (also explicit structure, but less common)
	if family.Type == "suffixed" {
		score *= 1.1
	}

	// Penalty for very low coverage (need meaningful groups)
	if coverage < 0.3 {
		score *= coverage / 0.3
	}

	// Penalty for very high coverage single labels (probably cross-cutting)
	if family.Type == "generic" && coverage > 0.6 {
		score *= 0.3
	}

	return &FamilyScore{
		Family:      family,
		Score:       score,
		Coverage:    coverage,
		Exclusivity: exclusivity,
		Balance:     balance,
	}
}

func computeBalance(counts map[string]int) float64 {
	if len(counts) < 2 {
		return 1.0
	}

	values := make([]float64, 0, len(counts))
	var sum float64
	for _, c := range counts {
		values = append(values, float64(c))
		sum += float64(c)
	}

	mean := sum / float64(len(values))
	if mean == 0 {
		return 0
	}

	var variance float64
	for _, v := range values {
		diff := v - mean
		variance += diff * diff
	}
	variance /= float64(len(values))

	// Coefficient of variation (lower is more balanced)
	cv := 0.0
	if mean > 0 {
		cv = variance / (mean * mean)
		if cv > 1 {
			cv = 1
		}
	}

	return 1 - cv
}

// === DEPENDENCY GRAPH ===

type dependencyGraph struct {
	issues    map[string]*model.Issue
	blockedBy map[string][]string // issueID -> issues that block it
	blocks    map[string][]string // issueID -> issues it blocks
	children  map[string][]string // parentID -> child issue IDs
	parents   map[string]string   // issueID -> parent ID
}

func buildDependencyGraph(issues []model.Issue) *dependencyGraph {
	g := &dependencyGraph{
		issues:    make(map[string]*model.Issue),
		blockedBy: make(map[string][]string),
		blocks:    make(map[string][]string),
		children:  make(map[string][]string),
		parents:   make(map[string]string),
	}

	for i := range issues {
		g.issues[issues[i].ID] = &issues[i]
	}

	for i := range issues {
		issue := &issues[i]
		for _, dep := range issue.Dependencies {
			switch dep.Type {
			case model.DepBlocks, "":
				// dep.DependsOnID blocks this issue
				g.blockedBy[issue.ID] = append(g.blockedBy[issue.ID], dep.DependsOnID)
				g.blocks[dep.DependsOnID] = append(g.blocks[dep.DependsOnID], issue.ID)

			case model.DepParentChild:
				// This issue is child of dep.DependsOnID
				g.parents[issue.ID] = dep.DependsOnID
				g.children[dep.DependsOnID] = append(g.children[dep.DependsOnID], issue.ID)
			}
		}
	}

	return g
}

// inheritLabels propagates labels through parent-child relationships only.
// This prevents pollution from blocking deps crossing domain boundaries.
// Example: E-Commerce issues should NOT inherit feat:auth just because
// auth endpoints block shared API documentation.
func (g *dependencyGraph) inheritLabels(family *LabelFamily) {
	if family == nil {
		return
	}

	familyLabels := make(map[string]bool)
	for _, label := range family.Labels {
		familyLabels[label] = true
	}

	// Find issues with family labels (roots for inheritance)
	type root struct {
		issue *model.Issue
		label string
	}
	roots := make([]root, 0)

	for _, issue := range g.issues {
		for _, label := range issue.Labels {
			if familyLabels[label] {
				roots = append(roots, root{issue: issue, label: label})
				break
			}
		}
	}

	// BFS from each root, propagating labels to children (parent-child only)
	for _, r := range roots {
		visited := make(map[string]bool)
		queue := []string{r.issue.ID}
		visited[r.issue.ID] = true

		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]

			// Propagate to children only (NOT blocking deps)
			for _, childID := range g.children[current] {
				if visited[childID] {
					continue
				}
				visited[childID] = true

				child := g.issues[childID]
				if child == nil {
					continue
				}

				// Check if already has a family label
				hasLabel := false
				for _, label := range child.Labels {
					if familyLabels[label] {
						hasLabel = true
						break
					}
				}

				// Inherit if missing
				if !hasLabel {
					child.Labels = append(child.Labels, r.label)
				}

				queue = append(queue, childID)
			}
		}
	}
}

// === MAIN ALGORITHM ===

// DetectWorkstreams finds connected components in a set of issues.
// Uses family-based label analysis for intelligent partitioning.
//
// Parameters:
//   - issues: The slice of issues to analyze (typically filtered by label)
//   - primaryIDs: Map of issue IDs that are "primary" (have the selected label)
//   - selectedLabel: The label being viewed (excluded from grouping)
//
// Returns workstreams sorted appropriately (sequential order or by size).
func DetectWorkstreams(issues []model.Issue, primaryIDs map[string]bool, selectedLabel string) []Workstream {
	if len(issues) == 0 {
		return nil
	}

	// Separate primary and context issues
	primary := make([]model.Issue, 0)
	context := make([]model.Issue, 0)
	for _, issue := range issues {
		if primaryIDs[issue.ID] {
			primary = append(primary, issue)
		} else {
			context = append(context, issue)
		}
	}

	// If no primary issues marked, treat all as primary
	if len(primary) == 0 {
		primary = issues
		context = nil
	}

	// Build dependency graph
	graph := buildDependencyGraph(issues)

	// Build global issue map for blocking checks (needed for cross-workstream blockers)
	globalIssueMap := make(map[string]model.Issue)
	for _, issue := range issues {
		globalIssueMap[issue.ID] = issue
	}

	// Analyze labels on primary issues only
	labelStats := AnalyzeLabels(primary, selectedLabel)

	// Collect unique families
	familyMap := make(map[string]*LabelFamily)
	for _, stat := range labelStats {
		if stat.Family != nil {
			key := stat.Family.Type + ":" + stat.Family.Prefix
			familyMap[key] = stat.Family
		}
	}

	// Score each family
	scores := make([]*FamilyScore, 0, len(familyMap))
	for _, family := range familyMap {
		score := ScoreFamily(family, primary, selectedLabel)
		if score.Score > 0.1 { // Minimum threshold
			scores = append(scores, score)
		}
	}

	// Sort by score descending
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].Score > scores[j].Score
	})

	// Select winning family
	var winningFamily *LabelFamily
	if len(scores) > 0 {
		winningFamily = scores[0].Family
	}

	// Inherit labels through dependencies
	if winningFamily != nil {
		graph.inheritLabels(winningFamily)
	}

	// Partition issues into workstreams
	workstreams := partitionByFamily(primary, winningFamily, labelStats, selectedLabel, primaryIDs)

	// Assign context issues to workstreams
	assignContextIssues(workstreams, context, graph)

	// Compute stats for each workstream (using global issue map for cross-workstream blockers)
	for i := range workstreams {
		computeWorkstreamStats(&workstreams[i], primaryIDs, globalIssueMap)
	}

	// Detect cross-workstream dependencies
	detectCrossWorkstreamDeps(workstreams, graph)

	// Sort workstreams
	sortWorkstreams(workstreams, winningFamily)

	return workstreams
}

func partitionByFamily(issues []model.Issue, family *LabelFamily, stats map[string]*WorkstreamLabelStats, selectedLabel string, primaryIDs map[string]bool) []Workstream {
	workstreams := make(map[string]*Workstream)
	standalone := &Workstream{
		ID:       "standalone",
		Name:     "Standalone",
		Issues:   make([]model.Issue, 0),
		IssueIDs: make([]string, 0),
		Order:    9999,
	}

	if family == nil {
		// No good family found - put everything in standalone
		for _, issue := range issues {
			standalone.Issues = append(standalone.Issues, issue)
			standalone.IssueIDs = append(standalone.IssueIDs, issue.ID)
		}
		return []Workstream{*standalone}
	}

	familyLabels := make(map[string]bool)
	for _, label := range family.Labels {
		familyLabels[label] = true
	}

	// For combined suffix families, extract suffix from each label
	// For single suffix families, use the stored suffix
	// For other families, use the full label
	getWorkstreamKey := func(label string) string {
		if family.Type == "suffixed" {
			if family.Prefix == "_by_suffix_" {
				// Combined suffix family - extract suffix from label
				if matches := separatorSuffixPattern.FindStringSubmatch(label); matches != nil {
					return "ws:" + matches[2]
				}
			} else {
				// Single suffix family - use stored suffix
				suffix := strings.TrimPrefix(family.Prefix, "-")
				return "ws:" + suffix
			}
		}
		return "ws:" + label
	}

	getWorkstreamName := func(label string) string {
		if family.Type == "suffixed" {
			if family.Prefix == "_by_suffix_" {
				// Combined suffix family - extract and format suffix
				if matches := separatorSuffixPattern.FindStringSubmatch(label); matches != nil {
					return formatWorkstreamName(matches[2])
				}
			} else {
				// Single suffix family - use stored suffix
				suffix := strings.TrimPrefix(family.Prefix, "-")
				return formatWorkstreamName(suffix)
			}
		}
		return formatWorkstreamName(label)
	}

	for _, issue := range issues {
		assigned := false
		for _, label := range issue.Labels {
			if familyLabels[label] {
				wsKey := getWorkstreamKey(label)
				if workstreams[wsKey] == nil {
					stat := stats[label]
					order := 0
					if stat != nil {
						order = stat.Order
					}
					workstreams[wsKey] = &Workstream{
						ID:       wsKey,
						Name:     getWorkstreamName(label),
						Issues:   make([]model.Issue, 0),
						IssueIDs: make([]string, 0),
						Order:    order,
					}
				}
				workstreams[wsKey].Issues = append(workstreams[wsKey].Issues, issue)
				workstreams[wsKey].IssueIDs = append(workstreams[wsKey].IssueIDs, issue.ID)
				assigned = true
				break // Only assign to first matching label
			}
		}
		if !assigned {
			standalone.Issues = append(standalone.Issues, issue)
			standalone.IssueIDs = append(standalone.IssueIDs, issue.ID)
		}
	}

	result := make([]Workstream, 0, len(workstreams)+1)
	for _, ws := range workstreams {
		result = append(result, *ws)
	}

	// Only include standalone if it has issues
	if len(standalone.Issues) > 0 {
		result = append(result, *standalone)
	}

	return result
}

func formatWorkstreamName(label string) string {
	// Remove common prefixes for cleaner display
	if idx := strings.Index(label, ":"); idx != -1 {
		label = label[idx+1:]
	}

	// Capitalize first letter
	if len(label) > 0 {
		return strings.ToUpper(label[:1]) + label[1:]
	}
	return label
}

// assignContextIssues assigns context issues to workstreams based on parent-child
// relationships ONLY. Blocking deps are NOT used because they cause cross-domain
// pollution (e.g., E-Commerce issues assigned to Auth workstream just because
// auth endpoints block shared documentation).
//
// Context issues that block/are-blocked-by primary issues will still APPEAR in
// the view (via addUpstreamContextBlockers in labeldashboard.go), but they won't
// be assigned to any workstream unless they have a parent-child relationship.
func assignContextIssues(workstreams []Workstream, context []model.Issue, graph *dependencyGraph) {
	if len(context) == 0 {
		return
	}

	// Build reverse lookup: issueID -> workstream index
	issueToWS := make(map[string]int)
	for i, ws := range workstreams {
		for _, id := range ws.IssueIDs {
			issueToWS[id] = i
		}
	}

	// Iterate multiple times to handle chains: if A→B→C and only A is connected to a primary,
	// first pass assigns A, second pass assigns B (connected to A), third assigns C
	unassigned := context
	for iteration := 0; iteration < 5 && len(unassigned) > 0; iteration++ {
		var stillUnassigned []model.Issue

		for _, issue := range unassigned {
			// Skip if already assigned in a previous iteration
			if _, ok := issueToWS[issue.ID]; ok {
				continue
			}

			// Find which workstream(s) this issue connects to via PARENT-CHILD ONLY
			// Blocking deps are NOT used to prevent cross-domain pollution
			wsCounts := make(map[int]int)

			// Check parent-child: if this issue is a child of an assigned issue
			if parentID := graph.parents[issue.ID]; parentID != "" {
				if wsIdx, ok := issueToWS[parentID]; ok {
					wsCounts[wsIdx]++
				}
			}

			// Check parent-child: if this issue is a parent of assigned issues
			for _, childID := range graph.children[issue.ID] {
				if wsIdx, ok := issueToWS[childID]; ok {
					wsCounts[wsIdx]++
				}
			}

			// Assign to workstream with most connections (parent-child only)
			bestIdx := -1
			bestCount := 0
			for idx, count := range wsCounts {
				if count > bestCount {
					bestCount = count
					bestIdx = idx
				}
			}

			if bestIdx >= 0 {
				workstreams[bestIdx].Issues = append(workstreams[bestIdx].Issues, issue)
				workstreams[bestIdx].IssueIDs = append(workstreams[bestIdx].IssueIDs, issue.ID)
				issueToWS[issue.ID] = bestIdx
			} else {
				stillUnassigned = append(stillUnassigned, issue)
			}
		}

		// Stop if no progress was made
		if len(stillUnassigned) == len(unassigned) {
			break
		}
		unassigned = stillUnassigned
	}
	// Note: Context issues with no parent-child connections to any workstream
	// are intentionally NOT added. They appear in the view as blockers but
	// don't belong to any specific workstream.
}

func computeWorkstreamStats(ws *Workstream, primaryIDs map[string]bool, globalIssueMap map[string]model.Issue) {
	total := len(ws.Issues)
	if total == 0 {
		return
	}

	ws.PrimaryCount = 0
	ws.ContextCount = 0
	ws.ReadyCount = 0
	ws.BlockedCount = 0
	ws.InProgressCount = 0
	ws.ClosedCount = 0

	for _, issue := range ws.Issues {
		// Count primary vs context
		if primaryIDs[issue.ID] {
			ws.PrimaryCount++
		} else {
			ws.ContextCount++
		}

		// Count by status
		switch issue.Status {
		case model.StatusClosed:
			ws.ClosedCount++
		case model.StatusBlocked:
			ws.BlockedCount++
		case model.StatusInProgress:
			ws.InProgressCount++
		case model.StatusOpen:
			// Check if actually blocked by dependencies (using global map to catch cross-workstream blockers)
			if isBlockedByDeps(issue, globalIssueMap) {
				ws.BlockedCount++
			} else {
				ws.ReadyCount++
			}
		default:
			ws.ReadyCount++
		}
	}

	// Compute progress (based on primary issues only)
	if ws.PrimaryCount > 0 {
		primaryClosed := 0
		for _, issue := range ws.Issues {
			if primaryIDs[issue.ID] && issue.Status == model.StatusClosed {
				primaryClosed++
			}
		}
		ws.Progress = float64(primaryClosed) / float64(ws.PrimaryCount)
	}

	// Determine if fully blocked
	ws.IsBlocked = ws.ReadyCount == 0 && ws.InProgressCount == 0 && ws.ClosedCount < len(ws.Issues)

	// Collect related labels
	labelCounts := make(map[string]int)
	for _, issue := range ws.Issues {
		for _, label := range issue.Labels {
			labelCounts[label]++
		}
	}
	ws.RelatedLabels = topLabels(labelCounts, 3)
}

func isBlockedByDeps(issue model.Issue, issueMap map[string]model.Issue) bool {
	for _, dep := range issue.Dependencies {
		if dep.Type == model.DepBlocks || dep.Type == "" {
			if blocker, exists := issueMap[dep.DependsOnID]; exists {
				if blocker.Status != model.StatusClosed {
					return true
				}
			}
		}
	}
	return false
}

func detectCrossWorkstreamDeps(workstreams []Workstream, graph *dependencyGraph) {
	if len(workstreams) < 2 {
		return
	}

	// Build issueID -> workstream index lookup
	issueToWS := make(map[string]int)
	for i, ws := range workstreams {
		for _, id := range ws.IssueIDs {
			issueToWS[id] = i
		}
	}

	// Find cross-workstream blocking relationships
	for i := range workstreams {
		ws := &workstreams[i]
		blockedBySet := make(map[string]bool)
		blocksSet := make(map[string]bool)

		for _, issue := range ws.Issues {
			// Skip closed issues
			if issue.Status == model.StatusClosed {
				continue
			}

			// Issues that block this one
			for _, blockerID := range graph.blockedBy[issue.ID] {
				blockerWSIdx, ok := issueToWS[blockerID]
				if !ok || blockerWSIdx == i {
					continue
				}
				// Check blocker is open
				if blocker := graph.issues[blockerID]; blocker != nil && blocker.Status != model.StatusClosed {
					blockedBySet[workstreams[blockerWSIdx].Name] = true
					ws.CrossBlockedBy = append(ws.CrossBlockedBy, CrossWorkstreamBlocker{
						BlockerID:         blockerID,
						BlockerWorkstream: workstreams[blockerWSIdx].Name,
						BlockedID:         issue.ID,
						BlockedWorkstream: ws.Name,
					})
				}
			}

			// Issues this one blocks
			for _, blockedID := range graph.blocks[issue.ID] {
				blockedWSIdx, ok := issueToWS[blockedID]
				if !ok || blockedWSIdx == i {
					continue
				}
				blocksSet[workstreams[blockedWSIdx].Name] = true
				workstreams[blockedWSIdx].CrossBlocks = append(workstreams[blockedWSIdx].CrossBlocks, CrossWorkstreamBlocker{
					BlockerID:         issue.ID,
					BlockerWorkstream: ws.Name,
					BlockedID:         blockedID,
					BlockedWorkstream: workstreams[blockedWSIdx].Name,
				})
			}
		}
	}
}

func sortWorkstreams(workstreams []Workstream, family *LabelFamily) {
	sort.Slice(workstreams, func(i, j int) bool {
		// Standalone always last
		if workstreams[i].ID == "standalone" {
			return false
		}
		if workstreams[j].ID == "standalone" {
			return true
		}

		// Sequential families: sort by order
		if family != nil && family.Sequential {
			return workstreams[i].Order < workstreams[j].Order
		}

		// Otherwise: sort by size (largest first) then alphabetically
		if len(workstreams[i].Issues) != len(workstreams[j].Issues) {
			return len(workstreams[i].Issues) > len(workstreams[j].Issues)
		}
		return workstreams[i].Name < workstreams[j].Name
	})
}

func topLabels(labelCounts map[string]int, n int) []string {
	type labelCount struct {
		label string
		count int
	}

	counts := make([]labelCount, 0, len(labelCounts))
	for label, count := range labelCounts {
		counts = append(counts, labelCount{label, count})
	}

	sort.Slice(counts, func(i, j int) bool {
		if counts[i].count != counts[j].count {
			return counts[i].count > counts[j].count
		}
		return counts[i].label < counts[j].label
	})

	result := make([]string, 0, n)
	for i := 0; i < n && i < len(counts); i++ {
		result = append(result, counts[i].label)
	}

	return result
}

// === LEGACY COMPATIBILITY ===

// looksLikeSequenceLabel returns true if a label appears to be a sequence marker.
// Exported for testing.
func looksLikeSequenceLabel(label string) bool {
	lower := strings.ToLower(label)
	for _, pattern := range sequentialPatterns {
		if pattern.MatchString(lower) {
			return true
		}
	}
	return false
}

// propagateLabelsThroughDeps is a legacy compatibility function.
// The new algorithm uses inheritLabels on the dependency graph instead.
func propagateLabelsThroughDeps(assigned map[string]string, issueToLabels map[string][]string, memberIDs []string, issueMap map[string]model.Issue) {
	// Build downstream graph (blocker -> issues it unblocks)
	downstream := make(map[string][]string)
	for _, id := range memberIDs {
		issue, ok := issueMap[id]
		if !ok {
			continue
		}
		for _, dep := range issue.Dependencies {
			if dep.Type == model.DepBlocks || dep.Type == "" {
				downstream[dep.DependsOnID] = append(downstream[dep.DependsOnID], id)
			}
		}
	}

	// BFS from each anchor to propagate labels
	visited := make(map[string]bool)

	var propagate func(fromID string, label string)
	propagate = func(fromID string, label string) {
		for _, toID := range downstream[fromID] {
			if visited[toID] {
				continue
			}

			// Check if this issue is already an anchor (has its own label)
			if len(issueToLabels[toID]) > 0 {
				continue
			}

			// Propagate the label
			if assigned[toID] == "" {
				assigned[toID] = label
				visited[toID] = true
				propagate(toID, label)
			}
		}
	}

	// Start propagation from each anchor
	for _, id := range memberIDs {
		if len(issueToLabels[id]) > 0 && assigned[id] != "" {
			visited[id] = true
			propagate(id, assigned[id])
		}
	}
}

// DistinguishingLabel is kept for backward compatibility
type DistinguishingLabel struct {
	Label          string
	Score          float64
	IssueCount     int
	PartitionRatio float64
}

// FindDistinguishingLabels returns labels that effectively partition issues.
// This is a compatibility wrapper around the new family-based algorithm.
func FindDistinguishingLabels(issues []model.Issue, selectedLabel string, minGroupSize int) []DistinguishingLabel {
	stats := AnalyzeLabels(issues, selectedLabel)

	result := make([]DistinguishingLabel, 0)
	for _, stat := range stats {
		if stat.Count >= minGroupSize && stat.Count < len(issues) {
			result = append(result, DistinguishingLabel{
				Label:          stat.Label,
				Score:          stat.Coverage * 100, // Scale to 0-100 for compatibility
				IssueCount:     stat.Count,
				PartitionRatio: stat.Coverage,
			})
		}
	}

	// Sort by score descending
	sort.Slice(result, func(i, j int) bool {
		return result[i].Score > result[j].Score
	})

	return result
}

// === SUB-GROUPING API ===

// DetectWorkstreamsWithOptions allows fine-grained control over grouping.
// This is the new API that supports subdivision and depth control.
func DetectWorkstreamsWithOptions(issues []model.Issue, primaryIDs map[string]bool, ctx ViewContext, opts GroupingOptions) []Workstream {
	if len(issues) == 0 {
		return nil
	}

	// Separate primary and context issues
	primary := make([]model.Issue, 0)
	context := make([]model.Issue, 0)
	for _, issue := range issues {
		if primaryIDs[issue.ID] {
			primary = append(primary, issue)
		} else {
			context = append(context, issue)
		}
	}

	// If no primary issues marked, treat all as primary
	if len(primary) == 0 {
		primary = issues
		context = nil
	}

	// Build dependency graph
	graph := buildDependencyGraph(issues)

	// Build global issue map for blocking checks (needed for cross-workstream blockers)
	globalIssueMap := make(map[string]model.Issue)
	for _, issue := range issues {
		globalIssueMap[issue.ID] = issue
	}

	// Combine context label with excluded labels
	excludeLabels := make(map[string]bool)
	excludeLabels[ctx.SelectedLabel] = true
	for _, label := range opts.ExcludeLabels {
		excludeLabels[label] = true
	}

	// Analyze labels on primary issues only
	labelStats := AnalyzeLabels(primary, ctx.SelectedLabel)

	// Collect unique families, excluding already-used ones
	familyMap := make(map[string]*LabelFamily)
	for _, stat := range labelStats {
		if stat.Family != nil {
			// Check if this family is excluded
			excluded := false
			for _, excludePrefix := range opts.ExcludeFamilies {
				if stat.Family.Prefix == excludePrefix ||
					strings.HasPrefix(stat.Family.Prefix, excludePrefix) {
					excluded = true
					break
				}
			}
			if !excluded {
				key := stat.Family.Type + ":" + stat.Family.Prefix
				familyMap[key] = stat.Family
			}
		}
	}

	// Score each family
	scores := make([]*FamilyScore, 0, len(familyMap))
	for _, family := range familyMap {
		score := ScoreFamily(family, primary, ctx.SelectedLabel)
		if score.Score > 0.05 { // Low initial threshold, filter by opts later
			scores = append(scores, score)
		}
	}

	// Sort by score descending
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].Score > scores[j].Score
	})

	// Decide grouping strategy
	var workstreams []Workstream
	var winningFamily *LabelFamily
	var usedFamilyPrefix string

	if len(scores) > 0 && scores[0].Score >= opts.MinScoreAtDepth {
		// Label-based grouping
		winningFamily = scores[0].Family
		usedFamilyPrefix = winningFamily.Prefix

		// Inherit labels through dependencies
		graph.inheritLabels(winningFamily)

		// Partition by labels
		workstreams = partitionByFamily(primary, winningFamily, labelStats, ctx.SelectedLabel, primaryIDs)
	} else {
		// Fallback to standalone grouping
		workstreams = partitionByFamily(primary, nil, labelStats, ctx.SelectedLabel, primaryIDs)
	}

	// Set depth and grouping method on all workstreams
	for i := range workstreams {
		workstreams[i].Depth = opts.CurrentDepth
		if winningFamily != nil {
			workstreams[i].GroupedBy = winningFamily.Type + ":" + winningFamily.Prefix
		} else {
			workstreams[i].GroupedBy = "dependencies"
		}
	}

	// Assign context issues to workstreams
	assignContextIssues(workstreams, context, graph)

	// Compute stats for each workstream (using global issue map for cross-workstream blockers)
	for i := range workstreams {
		computeWorkstreamStats(&workstreams[i], primaryIDs, globalIssueMap)
	}

	// Detect cross-workstream dependencies
	detectCrossWorkstreamDeps(workstreams, graph)

	// Sort workstreams
	sortWorkstreams(workstreams, winningFamily)

	// Store info for potential subdivision
	for i := range workstreams {
		if usedFamilyPrefix != "" {
			workstreams[i].GroupedBy = usedFamilyPrefix
		}
	}

	return workstreams
}

// SubdivideWorkstream attempts to further divide a workstream into smaller groups.
// Returns the sub-workstreams, or nil if no meaningful subdivision found.
func SubdivideWorkstream(ws *Workstream, primaryIDs map[string]bool, opts GroupingOptions) []*Workstream {
	if opts.CurrentDepth >= opts.MaxDepth {
		return nil // Max depth reached
	}

	minSize := opts.MinGroupSize
	if minSize < 2 {
		minSize = 2
	}

	if len(ws.Issues) < minSize*2 {
		return nil // Not enough issues to subdivide meaningfully
	}

	// Create context that excludes the labels used to create this workstream
	usedLabels := extractWorkstreamLabels(ws)
	subOpts := opts.OptionsForSubdivision(ws.GroupedBy, usedLabels)

	// Build primaryIDs for this workstream's issues
	subPrimaryIDs := make(map[string]bool)
	for _, issue := range ws.Issues {
		if primaryIDs[issue.ID] {
			subPrimaryIDs[issue.ID] = true
		}
	}

	// If no primary IDs, treat all as primary
	if len(subPrimaryIDs) == 0 {
		for _, issue := range ws.Issues {
			subPrimaryIDs[issue.ID] = true
		}
	}

	// Run detection on just this workstream's issues
	ctx := ViewContext{
		Type:          "subdivision",
		SelectedLabel: "", // No single context label at subdivision level
	}

	subWorkstreams := DetectWorkstreamsWithOptions(ws.Issues, subPrimaryIDs, ctx, subOpts)

	// Only accept subdivision if it creates 2+ non-standalone groups
	meaningfulGroups := 0
	for _, sub := range subWorkstreams {
		if sub.ID != "standalone" && len(sub.Issues) >= minSize {
			meaningfulGroups++
		}
	}

	if meaningfulGroups < 2 {
		return nil // Subdivision not meaningful
	}

	// Convert to pointers and update parent reference
	result := make([]*Workstream, len(subWorkstreams))
	for i := range subWorkstreams {
		subWorkstreams[i].Depth = opts.CurrentDepth + 1
		result[i] = &subWorkstreams[i]
	}

	ws.SubWorkstreams = result
	return result
}

// SubdivideAll recursively subdivides all workstreams up to MaxDepth.
func SubdivideAll(workstreams []*Workstream, primaryIDs map[string]bool, opts GroupingOptions) {
	minSize := opts.MinGroupSize
	if minSize < 2 {
		minSize = 2
	}

	for _, ws := range workstreams {
		if ws.ID == "standalone" {
			// Try to group standalone issues too
			subWs := SubdivideWorkstream(ws, primaryIDs, opts)
			if subWs != nil {
				// Standalone got subdivided - recurse
				SubdivideAll(subWs, primaryIDs, opts.OptionsForSubdivision(ws.GroupedBy, nil))
			}
		} else if len(ws.Issues) >= minSize*2 {
			// Regular workstream - try to subdivide
			subWs := SubdivideWorkstream(ws, primaryIDs, opts)
			if subWs != nil {
				SubdivideAll(subWs, primaryIDs, opts.OptionsForSubdivision(ws.GroupedBy, nil))
			}
		}
	}
}

// extractWorkstreamLabels gets the labels that define this workstream
func extractWorkstreamLabels(ws *Workstream) []string {
	// Find labels common to most issues in this workstream
	labelCounts := make(map[string]int)
	for _, issue := range ws.Issues {
		for _, label := range issue.Labels {
			labelCounts[label]++
		}
	}

	threshold := len(ws.Issues) / 2 // Label must appear in >50% of issues
	if threshold < 1 {
		threshold = 1
	}

	var commonLabels []string
	for label, count := range labelCounts {
		if count >= threshold {
			commonLabels = append(commonLabels, label)
		}
	}

	return commonLabels
}

// WorkstreamPointers converts a slice of Workstreams to pointers for mutation.
func WorkstreamPointers(workstreams []Workstream) []*Workstream {
	result := make([]*Workstream, len(workstreams))
	for i := range workstreams {
		result[i] = &workstreams[i]
	}
	return result
}

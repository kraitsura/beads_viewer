package ui

// Layout breakpoints for responsive design.
// These values determine when to switch between different layout modes.
const (
	// BreakpointNarrow is the width below which the UI uses minimal/compact layout.
	// Used in: lens selector, review dashboard, label tree review, graph, model.
	BreakpointNarrow = 80

	// BreakpointMedium is the width above which the UI can use dual-panel layouts.
	// Used in: lens selector, review dashboard, board.
	BreakpointMedium = 100

	// BreakpointWide is the width for expanded layouts (currently unused but reserved).
	BreakpointWide = 140
)

// Box and panel dimension constraints.
const (
	// MinBoxWidth is the minimum width for bordered content boxes.
	// Used in: lens selector stats panels, stats header boxes.
	MinBoxWidth = 20

	// MinContentHeight is the minimum height for scrollable content areas.
	MinContentHeight = 5

	// StatsPanelPadding is the padding subtracted from width for stats panels.
	StatsPanelPadding = 4
)

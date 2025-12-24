package review

import "time"

// ReviewAction represents a single review action to be persisted
type ReviewAction struct {
	IssueID    string
	Status     string // "approved", "needs_revision", "deferred"
	Reviewer   string
	Notes      string
	ReviewType string // "plan", "implementation", "security"
	Timestamp  time.Time
}

// ReviewSaver defines the interface for persisting review actions
type ReviewSaver interface {
	// Save persists all review actions from a session
	// Returns the number of successfully saved actions and any errors
	Save(actions []ReviewAction) (saved int, errors []error)

	// Close releases any resources
	Close() error
}

// ReviewSaveResult contains the outcome of a save operation
type ReviewSaveResult struct {
	Saved  int
	Failed int
	Errors []error
}

// NewReviewSaver creates a saver that persists reviews as comments
func NewReviewSaver(workspaceRoot string) ReviewSaver {
	return NewCommentReviewSaver(workspaceRoot)
}

package model

import "time"

// Review represents a single review action on an issue
type Review struct {
	ID         int64     `json:"id"`
	IssueID    string    `json:"issue_id"`
	ReviewType string    `json:"review_type"` // plan, implementation, security
	Outcome    string    `json:"outcome"`     // approved, needs_revision, deferred, note
	Reviewer   string    `json:"reviewer"`
	Notes      string    `json:"notes"`
	CreatedAt  time.Time `json:"created_at"`
}

// ReviewSession groups reviews done together in one session
type ReviewSession struct {
	ID                 int64      `json:"id"`
	RootIssueID        string     `json:"root_issue_id"`
	Reviewer           string     `json:"reviewer"`
	StartedAt          time.Time  `json:"started_at"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
	Summary            string     `json:"summary"`
	ItemsReviewed      int        `json:"items_reviewed"`
	ItemsApproved      int        `json:"items_approved"`
	ItemsNeedsRevision int        `json:"items_needs_revision"`
	ItemsDeferred      int        `json:"items_deferred"`
}

// Review status constants
const (
	ReviewStatusUnreviewed    = "unreviewed"
	ReviewStatusApproved      = "approved"
	ReviewStatusNeedsRevision = "needs_revision"
	ReviewStatusDeferred      = "deferred"
)

// Review type constants
const (
	ReviewTypePlan           = "plan"
	ReviewTypeImplementation = "implementation"
	ReviewTypeSecurity       = "security"
)

// Review outcome constants (includes status values plus "note")
const (
	ReviewOutcomeApproved      = "approved"
	ReviewOutcomeNeedsRevision = "needs_revision"
	ReviewOutcomeDeferred      = "deferred"
	ReviewOutcomeNote          = "note"
)

// IsValidReviewStatus checks if a review status is valid
func IsValidReviewStatus(status string) bool {
	switch status {
	case ReviewStatusUnreviewed, ReviewStatusApproved, ReviewStatusNeedsRevision, ReviewStatusDeferred, "":
		return true
	}
	return false
}

// IsValidReviewType checks if a review type is valid
func IsValidReviewType(t string) bool {
	switch t {
	case ReviewTypePlan, ReviewTypeImplementation, ReviewTypeSecurity:
		return true
	}
	return false
}

// IsValidReviewOutcome checks if a review outcome is valid
func IsValidReviewOutcome(outcome string) bool {
	switch outcome {
	case ReviewOutcomeApproved, ReviewOutcomeNeedsRevision, ReviewOutcomeDeferred, ReviewOutcomeNote:
		return true
	}
	return false
}

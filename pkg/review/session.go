package review

import (
	"log"
	"path/filepath"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

// SessionManager handles review session lifecycle
type SessionManager struct {
	db      *DB
	session *model.ReviewSession
	dbPath  string
}

// NewSessionManager creates a new session manager
// dbPath should be like ".bv/review.db"
func NewSessionManager(dbPath string) (*SessionManager, error) {
	db, err := OpenDB(dbPath)
	if err != nil {
		return nil, err
	}

	return &SessionManager{
		db:     db,
		dbPath: dbPath,
	}, nil
}

// StartSession creates a new review session for the given root issue
func (sm *SessionManager) StartSession(rootIssueID, reviewer string) error {
	session, err := sm.db.StartSession(rootIssueID, reviewer)
	if err != nil {
		return err
	}
	sm.session = session
	return nil
}

// CurrentSession returns the current active session
func (sm *SessionManager) CurrentSession() *model.ReviewSession {
	return sm.session
}

// RecordReview records a review action and updates counters
func (sm *SessionManager) RecordReview(issueID, reviewType, outcome, reviewer, notes string) error {
	review := &model.Review{
		IssueID:    issueID,
		ReviewType: reviewType,
		Outcome:    outcome,
		Reviewer:   reviewer,
		Notes:      notes,
		CreatedAt:  time.Now(),
	}

	if err := sm.db.CreateReview(review); err != nil {
		return err
	}

	// Update session counters if we have an active session
	if sm.session != nil {
		sm.session.ItemsReviewed++
		switch outcome {
		case model.ReviewOutcomeApproved:
			sm.session.ItemsApproved++
		case model.ReviewOutcomeNeedsRevision:
			sm.session.ItemsNeedsRevision++
		case model.ReviewOutcomeDeferred:
			sm.session.ItemsDeferred++
		}
		// Note: "note" outcome doesn't affect counters

		if err := sm.db.UpdateSessionCounters(sm.session); err != nil {
			log.Printf("Warning: failed to update session counters: %v", err)
		}
	}

	return nil
}

// GetReviewHistory returns review history for an issue
func (sm *SessionManager) GetReviewHistory(issueID string) ([]model.Review, error) {
	return sm.db.GetReviewsForIssue(issueID)
}

// CompleteSession marks the current session as complete
func (sm *SessionManager) CompleteSession(summary string) error {
	if sm.session == nil {
		return nil
	}

	sm.session.Summary = summary
	return sm.db.CompleteSession(sm.session)
}

// Close closes the database connection
func (sm *SessionManager) Close() error {
	return sm.db.Close()
}

// GetDefaultDBPath returns the default review database path
func GetDefaultDBPath() string {
	return filepath.Join(".bv", "review.db")
}

// TryStartSession attempts to start a session, logging errors but not failing
func TryStartSession(rootIssueID, reviewer string) *SessionManager {
	sm, err := NewSessionManager(GetDefaultDBPath())
	if err != nil {
		log.Printf("Warning: could not open review database: %v", err)
		return nil
	}

	if err := sm.StartSession(rootIssueID, reviewer); err != nil {
		log.Printf("Warning: could not start review session: %v", err)
		sm.Close()
		return nil
	}

	return sm
}

package review

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

// DB handles review data persistence
type DB struct {
	db *sql.DB
}

// OpenDB opens or creates the review database at the given path
func OpenDB(dbPath string) (*DB, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	rdb := &DB{db: db}
	if err := rdb.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return rdb, nil
}

// Close closes the database connection
func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS reviews (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		issue_id TEXT NOT NULL,
		review_type TEXT NOT NULL,
		outcome TEXT NOT NULL,
		reviewer TEXT NOT NULL,
		notes TEXT DEFAULT '',
		created_at DATETIME NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_reviews_issue_id ON reviews(issue_id);

	CREATE TABLE IF NOT EXISTS review_sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		root_issue_id TEXT NOT NULL,
		reviewer TEXT NOT NULL,
		started_at DATETIME NOT NULL,
		completed_at DATETIME,
		summary TEXT DEFAULT '',
		items_reviewed INTEGER DEFAULT 0,
		items_approved INTEGER DEFAULT 0,
		items_needs_revision INTEGER DEFAULT 0,
		items_deferred INTEGER DEFAULT 0
	);

	CREATE INDEX IF NOT EXISTS idx_sessions_root ON review_sessions(root_issue_id);
	`

	_, err := d.db.Exec(schema)
	return err
}

// CreateReview inserts a new review record
func (d *DB) CreateReview(r *model.Review) error {
	result, err := d.db.Exec(`
		INSERT INTO reviews (issue_id, review_type, outcome, reviewer, notes, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, r.IssueID, r.ReviewType, r.Outcome, r.Reviewer, r.Notes, r.CreatedAt)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	r.ID = id
	return nil
}

// GetReviewsForIssue returns all reviews for a given issue ID
func (d *DB) GetReviewsForIssue(issueID string) ([]model.Review, error) {
	rows, err := d.db.Query(`
		SELECT id, issue_id, review_type, outcome, reviewer, notes, created_at
		FROM reviews
		WHERE issue_id = ?
		ORDER BY created_at DESC
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reviews []model.Review
	for rows.Next() {
		var r model.Review
		if err := rows.Scan(&r.ID, &r.IssueID, &r.ReviewType, &r.Outcome, &r.Reviewer, &r.Notes, &r.CreatedAt); err != nil {
			return nil, err
		}
		reviews = append(reviews, r)
	}
	return reviews, rows.Err()
}

// StartSession creates a new review session
func (d *DB) StartSession(rootIssueID, reviewer string) (*model.ReviewSession, error) {
	now := time.Now()
	result, err := d.db.Exec(`
		INSERT INTO review_sessions (root_issue_id, reviewer, started_at)
		VALUES (?, ?, ?)
	`, rootIssueID, reviewer, now)
	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &model.ReviewSession{
		ID:          id,
		RootIssueID: rootIssueID,
		Reviewer:    reviewer,
		StartedAt:   now,
	}, nil
}

// UpdateSessionCounters updates the review counters for a session
func (d *DB) UpdateSessionCounters(session *model.ReviewSession) error {
	_, err := d.db.Exec(`
		UPDATE review_sessions
		SET items_reviewed = ?, items_approved = ?, items_needs_revision = ?, items_deferred = ?
		WHERE id = ?
	`, session.ItemsReviewed, session.ItemsApproved, session.ItemsNeedsRevision, session.ItemsDeferred, session.ID)
	return err
}

// CompleteSession marks a session as complete
func (d *DB) CompleteSession(session *model.ReviewSession) error {
	now := time.Now()
	session.CompletedAt = &now
	_, err := d.db.Exec(`
		UPDATE review_sessions
		SET completed_at = ?, summary = ?, items_reviewed = ?, items_approved = ?, items_needs_revision = ?, items_deferred = ?
		WHERE id = ?
	`, now, session.Summary, session.ItemsReviewed, session.ItemsApproved, session.ItemsNeedsRevision, session.ItemsDeferred, session.ID)
	return err
}

// GetSession retrieves a session by ID
func (d *DB) GetSession(id int64) (*model.ReviewSession, error) {
	var s model.ReviewSession
	var completedAt sql.NullTime
	err := d.db.QueryRow(`
		SELECT id, root_issue_id, reviewer, started_at, completed_at, summary, items_reviewed, items_approved, items_needs_revision, items_deferred
		FROM review_sessions
		WHERE id = ?
	`, id).Scan(&s.ID, &s.RootIssueID, &s.Reviewer, &s.StartedAt, &completedAt, &s.Summary, &s.ItemsReviewed, &s.ItemsApproved, &s.ItemsNeedsRevision, &s.ItemsDeferred)
	if err != nil {
		return nil, err
	}
	if completedAt.Valid {
		s.CompletedAt = &completedAt.Time
	}
	return &s, nil
}

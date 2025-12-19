package review

import (
	"database/sql"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

// BeadsVersion represents the detected beads installation type
type BeadsVersion int

const (
	BeadsVersionUnknown BeadsVersion = iota
	BeadsVersionLegacy               // No reviews table - use comments
	BeadsVersionModern               // Has reviews table - use bd review
)

// DetectBeadsVersion checks the beads installation to determine capabilities
// Returns BeadsVersionModern if .beads/beads.db exists and has a reviews table
// Returns BeadsVersionLegacy otherwise (will use comment-based persistence)
func DetectBeadsVersion(workspaceRoot string) BeadsVersion {
	beadsDir := filepath.Join(workspaceRoot, ".beads")

	// Check for SQLite database
	dbPath := filepath.Join(beadsDir, "beads.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return BeadsVersionLegacy
	}

	// Open database and check for reviews table
	db, err := sql.Open("sqlite3", dbPath+"?mode=ro")
	if err != nil {
		return BeadsVersionLegacy
	}
	defer db.Close()

	// Query to check if reviews table exists
	var tableName string
	err = db.QueryRow(`
		SELECT name FROM sqlite_master
		WHERE type='table' AND name='reviews'
	`).Scan(&tableName)

	if err != nil || tableName == "" {
		return BeadsVersionLegacy
	}

	return BeadsVersionModern
}

// String returns a human-readable version name
func (v BeadsVersion) String() string {
	switch v {
	case BeadsVersionModern:
		return "modern (native review support)"
	case BeadsVersionLegacy:
		return "legacy (comment-based reviews)"
	default:
		return "unknown"
	}
}

// IsModern returns true if this is a modern beads version with native review support
func (v BeadsVersion) IsModern() bool {
	return v == BeadsVersionModern
}

// IsLegacy returns true if this is a legacy beads version using comment-based reviews
func (v BeadsVersion) IsLegacy() bool {
	return v == BeadsVersionLegacy || v == BeadsVersionUnknown
}

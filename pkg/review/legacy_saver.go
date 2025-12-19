package review

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// LegacyReviewSaver persists reviews as structured comments
type LegacyReviewSaver struct {
	workspaceRoot string
}

// NewLegacyReviewSaver creates a saver that uses bd comment
func NewLegacyReviewSaver(workspaceRoot string) *LegacyReviewSaver {
	return &LegacyReviewSaver{
		workspaceRoot: workspaceRoot,
	}
}

// Save implements ReviewSaver using bd comment command with structured format
func (s *LegacyReviewSaver) Save(actions []ReviewAction) (int, []error) {
	var errors []error
	saved := 0

	for _, action := range actions {
		err := s.saveOne(action)
		if err != nil {
			errors = append(errors, fmt.Errorf("%s: %w", action.IssueID, err))
		} else {
			saved++
		}
	}

	return saved, errors
}

func (s *LegacyReviewSaver) saveOne(action ReviewAction) error {
	// Build structured review comment
	commentText := s.formatReviewComment(action)

	// bd comment add <id> "<text>" [--author <reviewer>]
	args := []string{"comment", "add", action.IssueID, commentText}
	if action.Reviewer != "" {
		args = append(args, "--author", action.Reviewer)
	}

	cmd := exec.Command("bd", args...)
	cmd.Dir = s.workspaceRoot

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("bd comment failed: %v, output: %s", err, strings.TrimSpace(string(output)))
	}

	return nil
}

// formatReviewComment creates the structured comment format
func (s *LegacyReviewSaver) formatReviewComment(action ReviewAction) string {
	var sb strings.Builder

	sb.WriteString("---REVIEW---\n")
	sb.WriteString(fmt.Sprintf("Status: %s\n", action.Status))
	sb.WriteString(fmt.Sprintf("Reviewer: %s\n", action.Reviewer))
	sb.WriteString(fmt.Sprintf("Date: %s\n", action.Timestamp.Format(time.RFC3339)))
	if action.ReviewType != "" {
		sb.WriteString(fmt.Sprintf("Type: %s\n", action.ReviewType))
	}
	if action.Notes != "" {
		sb.WriteString(fmt.Sprintf("Notes: %s\n", action.Notes))
	}
	sb.WriteString("---")

	return sb.String()
}

// Close implements ReviewSaver
func (s *LegacyReviewSaver) Close() error {
	return nil
}

// ReviewCommentMarker is the marker that identifies review comments
const ReviewCommentMarker = "---REVIEW---"

// ParseReviewFromComment extracts review status from a legacy comment
// Returns the status or empty string if not a review comment
func ParseReviewFromComment(commentText string) (status, reviewer string, reviewedAt time.Time, notes string, ok bool) {
	if !strings.Contains(commentText, ReviewCommentMarker) {
		return "", "", time.Time{}, "", false
	}

	lines := strings.Split(commentText, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "Status:") {
			status = strings.TrimSpace(strings.TrimPrefix(line, "Status:"))
		} else if strings.HasPrefix(line, "Reviewer:") {
			reviewer = strings.TrimSpace(strings.TrimPrefix(line, "Reviewer:"))
		} else if strings.HasPrefix(line, "Date:") {
			dateStr := strings.TrimSpace(strings.TrimPrefix(line, "Date:"))
			if t, err := time.Parse(time.RFC3339, dateStr); err == nil {
				reviewedAt = t
			}
		} else if strings.HasPrefix(line, "Notes:") {
			notes = strings.TrimSpace(strings.TrimPrefix(line, "Notes:"))
		}
	}

	ok = status != ""
	return
}

// GetLatestReviewFromComments scans comments and returns the latest review status
// This is useful for displaying review status when loading issues in legacy mode
func GetLatestReviewFromComments(comments []string) (status, reviewer string, reviewedAt time.Time, found bool) {
	var latestTime time.Time

	for _, comment := range comments {
		s, r, t, _, ok := ParseReviewFromComment(comment)
		if ok && (t.After(latestTime) || latestTime.IsZero()) {
			status = s
			reviewer = r
			reviewedAt = t
			latestTime = t
			found = true
		}
	}

	return
}

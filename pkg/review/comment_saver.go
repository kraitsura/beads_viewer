package review

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// CommentReviewSaver persists reviews as structured comments via bd comment
type CommentReviewSaver struct {
	workspaceRoot string
}

// NewCommentReviewSaver creates a saver that uses bd comment
func NewCommentReviewSaver(workspaceRoot string) *CommentReviewSaver {
	return &CommentReviewSaver{
		workspaceRoot: workspaceRoot,
	}
}

// Save implements ReviewSaver using bd comment command with structured format
func (s *CommentReviewSaver) Save(actions []ReviewAction) (int, []error) {
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

func (s *CommentReviewSaver) saveOne(action ReviewAction) error {
	// Build structured review comment
	commentText := s.formatReviewComment(action)

	// bd comment <id> "<text>" [--author <reviewer>]
	args := []string{"comment", action.IssueID, commentText}
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
func (s *CommentReviewSaver) formatReviewComment(action ReviewAction) string {
	var sb strings.Builder

	sb.WriteString("[REVIEW]\n")
	sb.WriteString(fmt.Sprintf("status: %s\n", action.Status))
	sb.WriteString(fmt.Sprintf("reviewer: %s\n", action.Reviewer))
	sb.WriteString(fmt.Sprintf("date: %s\n", action.Timestamp.Format(time.RFC3339)))
	if action.ReviewType != "" {
		sb.WriteString(fmt.Sprintf("type: %s\n", action.ReviewType))
	}
	if action.Notes != "" {
		sb.WriteString(fmt.Sprintf("notes: %s\n", action.Notes))
	}
	sb.WriteString("[/REVIEW]")

	return sb.String()
}

// Close implements ReviewSaver
func (s *CommentReviewSaver) Close() error {
	return nil
}

// ReviewCommentMarker is the marker that identifies review comments
const ReviewCommentMarker = "[REVIEW]"

// LegacyReviewCommentMarker is the old marker for backward compatibility
const LegacyReviewCommentMarker = "---REVIEW---"

// ParseReviewFromComment extracts review status from a review comment
// Supports both new [REVIEW] format and legacy ---REVIEW--- format
func ParseReviewFromComment(commentText string) (status, reviewer string, reviewedAt time.Time, notes string, ok bool) {
	// Check for either marker format
	if !strings.Contains(commentText, ReviewCommentMarker) && !strings.Contains(commentText, LegacyReviewCommentMarker) {
		return "", "", time.Time{}, "", false
	}

	lines := strings.Split(commentText, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Handle both lowercase (new) and titlecase (legacy) field names
		lineLower := strings.ToLower(line)
		if strings.HasPrefix(lineLower, "status:") {
			status = strings.TrimSpace(line[7:])
		} else if strings.HasPrefix(lineLower, "reviewer:") {
			reviewer = strings.TrimSpace(line[9:])
		} else if strings.HasPrefix(lineLower, "date:") {
			dateStr := strings.TrimSpace(line[5:])
			if t, err := time.Parse(time.RFC3339, dateStr); err == nil {
				reviewedAt = t
			}
		} else if strings.HasPrefix(lineLower, "notes:") {
			notes = strings.TrimSpace(line[6:])
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

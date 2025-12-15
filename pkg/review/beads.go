package review

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

// UpdateReviewStatus updates an issue's review status via the bd CLI
// Returns nil if successful, error otherwise
func UpdateReviewStatus(issueID, status, reviewer string) error {
	if !model.IsValidReviewStatus(status) {
		return fmt.Errorf("invalid review status: %s", status)
	}

	// Build bd update command
	// bd update <id> --review-status <status> --reviewed-by <reviewer>
	args := []string{
		"update", issueID,
	}

	// Note: These flags may not exist in bd yet - this is forward-looking
	// For now, we'll try the command and handle failure gracefully
	if status != "" {
		args = append(args, "--review-status", status)
	}
	if reviewer != "" {
		args = append(args, "--reviewed-by", reviewer)
	}

	cmd := exec.Command("bd", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Log but don't fail - bd might not support these flags yet
		return fmt.Errorf("bd update failed (may not support review flags yet): %v, output: %s", err, string(output))
	}

	return nil
}

// TryUpdateReviewStatus attempts to update via bd but doesn't fail if bd is unavailable
// Returns true if successful, false otherwise (with optional error for logging)
func TryUpdateReviewStatus(issueID, status, reviewer string) (bool, error) {
	err := UpdateReviewStatus(issueID, status, reviewer)
	if err != nil {
		return false, err
	}
	return true, nil
}

// GetReviewFields extracts review fields from an issue
func GetReviewFields(issue *model.Issue) (status, reviewer string, reviewedAt time.Time) {
	return issue.ReviewStatus, issue.ReviewedBy, issue.ReviewedAt
}

// IsReviewed returns true if the issue has been reviewed (not unreviewed or empty)
func IsReviewed(issue *model.Issue) bool {
	return issue.ReviewStatus != "" && issue.ReviewStatus != model.ReviewStatusUnreviewed
}

// IsApproved returns true if the issue is approved
func IsApproved(issue *model.Issue) bool {
	return issue.ReviewStatus == model.ReviewStatusApproved
}

// NeedsRevision returns true if the issue needs revision
func NeedsRevision(issue *model.Issue) bool {
	return issue.ReviewStatus == model.ReviewStatusNeedsRevision
}

// IsDeferred returns true if the issue is deferred
func IsDeferred(issue *model.Issue) bool {
	return issue.ReviewStatus == model.ReviewStatusDeferred
}

// ReviewSummary contains aggregate review statistics
type ReviewSummary struct {
	Total          int
	Unreviewed     int
	Approved       int
	NeedsRevision  int
	Deferred       int
}

// CalculateReviewSummary calculates review statistics for a set of issues
func CalculateReviewSummary(issues []*model.Issue) ReviewSummary {
	summary := ReviewSummary{
		Total: len(issues),
	}

	for _, issue := range issues {
		switch issue.ReviewStatus {
		case model.ReviewStatusApproved:
			summary.Approved++
		case model.ReviewStatusNeedsRevision:
			summary.NeedsRevision++
		case model.ReviewStatusDeferred:
			summary.Deferred++
		default:
			summary.Unreviewed++
		}
	}

	return summary
}

// ApprovalRate returns the percentage of reviewed items that are approved
func (s ReviewSummary) ApprovalRate() float64 {
	reviewed := s.Total - s.Unreviewed
	if reviewed == 0 {
		return 0
	}
	return float64(s.Approved) / float64(reviewed) * 100
}

// ReviewedCount returns the number of items that have been reviewed
func (s ReviewSummary) ReviewedCount() int {
	return s.Total - s.Unreviewed
}

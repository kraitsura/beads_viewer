package review

import (
	"fmt"
	"os/exec"
	"strings"
)

// ModernReviewSaver persists reviews using the bd review command
type ModernReviewSaver struct {
	workspaceRoot string
}

// NewModernReviewSaver creates a saver that uses bd review
func NewModernReviewSaver(workspaceRoot string) *ModernReviewSaver {
	return &ModernReviewSaver{
		workspaceRoot: workspaceRoot,
	}
}

// Save implements ReviewSaver using bd review command
func (s *ModernReviewSaver) Save(actions []ReviewAction) (int, []error) {
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

func (s *ModernReviewSaver) saveOne(action ReviewAction) error {
	// Build bd review command
	// bd review <id> --approve|--revise|--defer --reviewer <name> [--notes <text>] [--type <type>]
	args := []string{"review", action.IssueID}

	// Add status flag
	switch action.Status {
	case "approved":
		args = append(args, "--approve")
	case "needs_revision":
		args = append(args, "--revise")
	case "deferred":
		args = append(args, "--defer")
	default:
		return fmt.Errorf("invalid review status: %s", action.Status)
	}

	// Add reviewer (required)
	if action.Reviewer != "" {
		args = append(args, "--reviewer", action.Reviewer)
	}

	// Add notes if present
	if action.Notes != "" {
		args = append(args, "--notes", action.Notes)
	}

	// Add review type if specified
	if action.ReviewType != "" {
		args = append(args, "--type", action.ReviewType)
	}

	cmd := exec.Command("bd", args...)
	cmd.Dir = s.workspaceRoot

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("bd review failed: %v, output: %s", err, strings.TrimSpace(string(output)))
	}

	return nil
}

// Close implements ReviewSaver
func (s *ModernReviewSaver) Close() error {
	return nil
}

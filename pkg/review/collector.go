package review

import (
	"sync"
	"time"
)

// ReviewActionCollector accumulates review actions during a session
type ReviewActionCollector struct {
	mu         sync.Mutex
	actions    []ReviewAction
	issueSet   map[string]int // Maps issueID to index in actions (for deduplication)
	reviewer   string
	reviewType string
}

// NewReviewActionCollector creates a new collector
func NewReviewActionCollector(reviewer, reviewType string) *ReviewActionCollector {
	return &ReviewActionCollector{
		actions:    make([]ReviewAction, 0),
		issueSet:   make(map[string]int),
		reviewer:   reviewer,
		reviewType: reviewType,
	}
}

// Record adds or updates a review action
// If the same issue is reviewed multiple times, only the last action is kept
func (c *ReviewActionCollector) Record(issueID, status, notes string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	action := ReviewAction{
		IssueID:    issueID,
		Status:     status,
		Reviewer:   c.reviewer,
		Notes:      notes,
		ReviewType: c.reviewType,
		Timestamp:  time.Now(),
	}

	if idx, exists := c.issueSet[issueID]; exists {
		// Update existing action
		c.actions[idx] = action
	} else {
		// Add new action
		c.issueSet[issueID] = len(c.actions)
		c.actions = append(c.actions, action)
	}
}

// Actions returns all collected actions
func (c *ReviewActionCollector) Actions() []ReviewAction {
	c.mu.Lock()
	defer c.mu.Unlock()

	result := make([]ReviewAction, len(c.actions))
	copy(result, c.actions)
	return result
}

// Count returns the number of recorded actions
func (c *ReviewActionCollector) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.actions)
}

// Clear removes all recorded actions
func (c *ReviewActionCollector) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.actions = make([]ReviewAction, 0)
	c.issueSet = make(map[string]int)
}

// SetReviewer updates the reviewer name
func (c *ReviewActionCollector) SetReviewer(reviewer string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reviewer = reviewer
}

// SetReviewType updates the review type
func (c *ReviewActionCollector) SetReviewType(reviewType string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reviewType = reviewType
}

// Reviewer returns the current reviewer name
func (c *ReviewActionCollector) Reviewer() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reviewer
}

// ReviewType returns the current review type
func (c *ReviewActionCollector) ReviewType() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reviewType
}

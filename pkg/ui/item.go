package ui

import (
	"fmt"
	"beads_viewer/pkg/model"
)

// IssueItem wraps model.Issue to implement list.Item
type IssueItem struct {
	Issue model.Issue
}

func (i IssueItem) Title() string {
	return i.Issue.Title
}

func (i IssueItem) Description() string {
	// Preview description or metadata
	return fmt.Sprintf("%s %s • %s", i.Issue.ID, i.Issue.Status, i.Issue.Assignee)
}

func (i IssueItem) FilterValue() string {
	return i.Issue.Title + " " + i.Issue.ID + " " + string(i.Issue.Status) + " " + string(i.Issue.IssueType)
}

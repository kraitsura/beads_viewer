package model

import (
	"fmt"
	"time"
)

// Issue represents a trackable work item
type Issue struct {
	ID                 string        `json:"id"`
	ContentHash        string        `json:"-"`
	Title              string        `json:"title"`
	Description        string        `json:"description"`
	Design             string        `json:"design,omitempty"`
	AcceptanceCriteria string        `json:"acceptance_criteria,omitempty"`
	Notes              string        `json:"notes,omitempty"`
	Status             Status        `json:"status"`
	Priority           int           `json:"priority"`
	IssueType          IssueType     `json:"issue_type"`
	Assignee           string        `json:"assignee,omitempty"`
	EstimatedMinutes   *int          `json:"estimated_minutes,omitempty"`
	CreatedAt          time.Time     `json:"created_at"`
	UpdatedAt          time.Time     `json:"updated_at"`
	ClosedAt           *time.Time    `json:"closed_at,omitempty"`
	ExternalRef        *string       `json:"external_ref,omitempty"`
	CompactionLevel    int           `json:"compaction_level,omitempty"`
	CompactedAt        *time.Time    `json:"compacted_at,omitempty"`
	CompactedAtCommit  *string       `json:"compacted_at_commit,omitempty"`
	OriginalSize       int           `json:"original_size,omitempty"`
	Labels             []string      `json:"labels,omitempty"`
	Dependencies       []*Dependency `json:"dependencies,omitempty"`
	Comments           []*Comment    `json:"comments,omitempty"`
	SourceRepo         string        `json:"source_repo,omitempty"`
}

// Clone creates a deep copy of the issue
func (i Issue) Clone() Issue {
	clone := i

	if i.EstimatedMinutes != nil {
		v := *i.EstimatedMinutes
		clone.EstimatedMinutes = &v
	}
	if i.ClosedAt != nil {
		v := *i.ClosedAt
		clone.ClosedAt = &v
	}
	if i.ExternalRef != nil {
		v := *i.ExternalRef
		clone.ExternalRef = &v
	}
	if i.CompactedAt != nil {
		v := *i.CompactedAt
		clone.CompactedAt = &v
	}
	if i.CompactedAtCommit != nil {
		v := *i.CompactedAtCommit
		clone.CompactedAtCommit = &v
	}

	if i.Labels != nil {
		clone.Labels = make([]string, len(i.Labels))
		copy(clone.Labels, i.Labels)
	}

	if i.Dependencies != nil {
		clone.Dependencies = make([]*Dependency, len(i.Dependencies))
		for idx, dep := range i.Dependencies {
			if dep != nil {
				v := *dep
				clone.Dependencies[idx] = &v
			}
		}
	}

	if i.Comments != nil {
		clone.Comments = make([]*Comment, len(i.Comments))
		for idx, comment := range i.Comments {
			if comment != nil {
				v := *comment
				clone.Comments[idx] = &v
			}
		}
	}

	return clone
}

// Validate checks if the issue data is logically valid
func (i *Issue) Validate() error {
	if i.ID == "" {
		return fmt.Errorf("issue ID cannot be empty")
	}
	if i.Title == "" {
		return fmt.Errorf("issue title cannot be empty")
	}
	if !i.Status.IsValid() {
		return fmt.Errorf("invalid status: %s", i.Status)
	}
	if !i.IssueType.IsValid() {
		return fmt.Errorf("invalid issue type: %s", i.IssueType)
	}
	if !i.UpdatedAt.IsZero() && !i.CreatedAt.IsZero() && i.UpdatedAt.Before(i.CreatedAt) {
		return fmt.Errorf("updated_at (%v) cannot be before created_at (%v)", i.UpdatedAt, i.CreatedAt)
	}
	return nil
}

// Status represents the current state of an issue
type Status string

const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in_progress"
	StatusBlocked    Status = "blocked"
	StatusClosed     Status = "closed"
)

// IsValid returns true if the status is a recognized value
func (s Status) IsValid() bool {
	switch s {
	case StatusOpen, StatusInProgress, StatusBlocked, StatusClosed:
		return true
	}
	return false
}

// IsClosed returns true if the status represents a closed state
func (s Status) IsClosed() bool {
	return s == StatusClosed
}

// IsOpen returns true if the status represents an active (open or in_progress) state
func (s Status) IsOpen() bool {
	return s == StatusOpen || s == StatusInProgress
}

// IssueType categorizes the kind of work
type IssueType string

const (
	TypeBug     IssueType = "bug"
	TypeFeature IssueType = "feature"
	TypeTask    IssueType = "task"
	TypeEpic    IssueType = "epic"
	TypeChore   IssueType = "chore"
)

// IsValid returns true if the issue type is a recognized value
func (t IssueType) IsValid() bool {
	switch t {
	case TypeBug, TypeFeature, TypeTask, TypeEpic, TypeChore:
		return true
	}
	return false
}

// Dependency represents a relationship between issues
type Dependency struct {
	IssueID     string         `json:"issue_id"`
	DependsOnID string         `json:"depends_on_id"`
	Type        DependencyType `json:"type"`
	CreatedAt   time.Time      `json:"created_at"`
	CreatedBy   string         `json:"created_by"`
}

// IssueMetrics holds computed metrics for export/robot consumers.
type IssueMetrics struct {
	PageRank          float64 `json:"pagerank,omitempty"`
	Betweenness       float64 `json:"betweenness,omitempty"`
	CriticalPathDepth int     `json:"critical_path_depth,omitempty"`
	TriageScore       float64 `json:"triage_score,omitempty"`
	BlocksCount       int     `json:"blocks_count,omitempty"`
	BlockedByCount    int     `json:"blocked_by_count,omitempty"`
}

// DependencyType categorizes the relationship
type DependencyType string

const (
	DepBlocks         DependencyType = "blocks"
	DepRelated        DependencyType = "related"
	DepParentChild    DependencyType = "parent-child"
	DepDiscoveredFrom DependencyType = "discovered-from"
)

// IsValid returns true if the dependency type is a recognized value
func (d DependencyType) IsValid() bool {
	switch d {
	case DepBlocks, DepRelated, DepParentChild, DepDiscoveredFrom:
		return true
	}
	return false
}

// IsBlocking returns true if this dependency type represents a blocking relationship
func (d DependencyType) IsBlocking() bool {
	return d == "" || d == DepBlocks
}

// Comment represents a comment on an issue
type Comment struct {
	ID        int64     `json:"id"`
	IssueID   string    `json:"issue_id"`
	Author    string    `json:"author"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

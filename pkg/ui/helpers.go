package ui

import (
	"fmt"
	"strings"
	"time"
	
	"beads_viewer/pkg/model"
)

// FormatTimeRel returns a relative time string (e.g., "2h ago", "3d ago")
func FormatTimeRel(t time.Time) string {
	d := time.Since(t)
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

// DependencyNode represents a visual node in the dependency tree
type DependencyNode struct {
	ID       string
	Title    string
	Status   string
	Type     string
	Children []*DependencyNode
}

// BuildDependencyTree constructs a tree from dependencies for visualization
func BuildDependencyTree(rootID string, issueMap map[string]*model.Issue) *DependencyNode {
	root, exists := issueMap[rootID]
	if !exists {
		return nil
	}
	
	node := &DependencyNode{
		ID:     root.ID,
		Title:  root.Title,
		Status: string(root.Status),
		Type:   "root",
	}
	
	// This is a simplified tree builder - ideally it would be recursive but handled carefully to avoid cycles
	// For now, we just show direct dependencies + blockers
	for _, dep := range root.Dependencies {
		childIssue, exists := issueMap[dep.DependsOnID]
		title := "???"
		status := "?"
		if exists {
			title = childIssue.Title
			status = string(childIssue.Status)
		}
		
		child := &DependencyNode{
			ID:     dep.DependsOnID,
			Title:  title,
			Status: status,
			Type:   string(dep.Type),
		}
		node.Children = append(node.Children, child)
	}
	
	return node
}

func RenderDependencyTree(node *DependencyNode) string {
	if node == nil {
		return "No dependency data."
	}
	
	var sb strings.Builder
	sb.WriteString("Dependency Graph:\n")
	
	// Root
	sb.WriteString(fmt.Sprintf("%s %s (%s)\n", GetStatusIcon(node.Status), node.ID, node.Title))
	
	for _, child := range node.Children {
		icon := "🔗"
		if child.Type == "blocks" {
			icon = "⛔"
		}
		sb.WriteString(fmt.Sprintf("  └─ %s %s %s (%s) [%s]\n", icon, child.Type, child.ID, child.Title, child.Status))
	}
	
	return sb.String()
}

func GetStatusIcon(s string) string {
	switch s {
	case "open": return "🟢"
	case "in_progress": return "🔵"
	case "blocked": return "🔴"
	case "closed": return "⚫"
	default: return "⚪"
	}
}

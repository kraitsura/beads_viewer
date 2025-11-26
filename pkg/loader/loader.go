package loader

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"beads_viewer/pkg/model"
)

// LoadIssues reads issues from the .beads/issues.jsonl file in the given repository path.
func LoadIssues(repoPath string) ([]model.Issue, error) {
	if repoPath == "" {
		var err error
		repoPath, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get current working directory: %w", err)
		}
	}

	jsonlPath := filepath.Join(repoPath, ".beads", "issues.jsonl")
	return LoadIssuesFromFile(jsonlPath)
}

// LoadIssuesFromFile reads issues directly from a specific JSONL file path.
func LoadIssuesFromFile(path string) ([]model.Issue, error) {
	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("no beads issues found at %s", path)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open issues file: %w", err)
	}
	defer file.Close()

	var issues []model.Issue
	scanner := bufio.NewScanner(file)
	// Increase buffer size for large lines (issues can be large)
	const maxCapacity = 1024 * 1024 * 10 // 10MB
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var issue model.Issue
		if err := json.Unmarshal(line, &issue); err != nil {
			// Skip malformed lines but continue loading the rest
			// In a real app we might want to log this to a debug log
			continue
		}
		issues = append(issues, issue)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading issues file: %w", err)
	}

	return issues, nil
}

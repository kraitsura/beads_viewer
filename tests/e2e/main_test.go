package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestEndToEndBuildAndRun(t *testing.T) {
	// 1. Build the binary
	tempDir := t.TempDir()
	binPath := filepath.Join(tempDir, "bv")
	
	// Go up to root
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/bv/main.go")
	cmd.Dir = "../../" // Run from project root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Build failed: %v\n%s", err, out)
	}

	// 2. Prepare a fake environment with .beads/issues.jsonl
	envDir := filepath.Join(tempDir, "env")
	if err := os.MkdirAll(filepath.Join(envDir, ".beads"), 0755); err != nil {
		t.Fatal(err)
	}

	jsonlContent := `{"id": "bd-1", "title": "E2E Test Issue", "status": "open", "priority": 0, "issue_type": "bug"}`
	if err := os.WriteFile(filepath.Join(envDir, ".beads", "issues.jsonl"), []byte(jsonlContent), 0644); err != nil {
		t.Fatal(err)
	}

	// 3. Run bv --version to verify it runs
	runCmd := exec.Command(binPath, "--version")
	runCmd.Dir = envDir
	if out, err := runCmd.CombinedOutput(); err != nil {
		t.Fatalf("Execution failed: %v\n%s", err, out)
	}
}

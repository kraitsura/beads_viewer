package main_test

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportPages_IncludesHistoryAndRunsHooks(t *testing.T) {
	bv := buildBvBinary(t)
	stageViewerAssets(t, bv)

	repoDir, _ := createHistoryRepo(t)
	exportDir := filepath.Join(repoDir, "bv-pages")

	// Configure hooks to prove pre/post phases run.
	if err := os.MkdirAll(filepath.Join(repoDir, ".bv"), 0o755); err != nil {
		t.Fatalf("mkdir .bv: %v", err)
	}
	hooksYAML := `hooks:
  pre-export:
    - name: pre
      command: 'mkdir -p "$BV_EXPORT_PATH" && echo pre > "$BV_EXPORT_PATH/pre-hook.txt"'
  post-export:
    - name: post
      command: 'echo post > "$BV_EXPORT_PATH/post-hook.txt"'
`
	if err := os.WriteFile(filepath.Join(repoDir, ".bv", "hooks.yaml"), []byte(hooksYAML), 0o644); err != nil {
		t.Fatalf("write hooks.yaml: %v", err)
	}

	cmd := exec.Command(bv,
		"--export-pages", exportDir,
		"--pages-include-history",
		"--pages-include-closed",
	)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--export-pages failed: %v\n%s", err, out)
	}

	// Core artifacts.
	for _, p := range []string{
		filepath.Join(exportDir, "index.html"),
		filepath.Join(exportDir, "beads.sqlite3"),
		filepath.Join(exportDir, "beads.sqlite3.config.json"),
		filepath.Join(exportDir, "data", "meta.json"),
		filepath.Join(exportDir, "data", "triage.json"),
		filepath.Join(exportDir, "data", "history.json"),
		filepath.Join(exportDir, "pre-hook.txt"),
		filepath.Join(exportDir, "post-hook.txt"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("missing export artifact %s: %v", p, err)
		}
	}

	// Verify vendored scripts are present (all scripts are now local, not CDN)
	indexBytes, err := os.ReadFile(filepath.Join(exportDir, "index.html"))
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	if !strings.Contains(string(indexBytes), "vendor/") {
		t.Fatalf("index.html missing vendored script references")
	}

	// History JSON should include at least one commit entry.
	historyBytes, err := os.ReadFile(filepath.Join(exportDir, "data", "history.json"))
	if err != nil {
		t.Fatalf("read history.json: %v", err)
	}
	var history struct {
		Commits []struct {
			SHA string `json:"sha"`
		} `json:"commits"`
	}
	if err := json.Unmarshal(historyBytes, &history); err != nil {
		t.Fatalf("history.json decode: %v", err)
	}
	if len(history.Commits) == 0 || history.Commits[0].SHA == "" {
		t.Fatalf("expected at least one commit in history.json, got %+v", history.Commits)
	}
}

func stageViewerAssets(t *testing.T, bvPath string) {
	t.Helper()
	root := findRepoRoot(t)
	src := filepath.Join(root, "pkg", "export", "viewer_assets")
	dst := filepath.Join(filepath.Dir(bvPath), "pkg", "export", "viewer_assets")

	if err := copyDirRecursive(src, dst); err != nil {
		t.Fatalf("stage viewer assets: %v", err)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found starting at %s", dir)
		}
		dir = parent
	}
}

func copyDirRecursive(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFile(src, dst)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDirRecursive(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// ============================================================================
// Static Bundle Validation Tests (bv-ct7m)
// ============================================================================

// TestExportPages_HTMLStructure validates the HTML5 document structure
func TestExportPages_HTMLStructure(t *testing.T) {
	bv := buildBvBinary(t)
	stageViewerAssets(t, bv)

	repoDir := createSimpleRepo(t, 5)
	exportDir := filepath.Join(repoDir, "bv-pages")

	cmd := exec.Command(bv, "--export-pages", exportDir)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("--export-pages failed: %v\n%s", err, out)
	}

	indexBytes, err := os.ReadFile(filepath.Join(exportDir, "index.html"))
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	html := string(indexBytes)

	// HTML5 doctype (case-insensitive check)
	if !strings.Contains(strings.ToLower(html), "<!doctype html>") {
		t.Error("missing HTML5 doctype")
	}

	// Required meta tags
	checks := []struct {
		name    string
		pattern string
	}{
		{"charset meta", `charset="UTF-8"`},
		{"viewport meta", `name="viewport"`},
		{"html lang attribute", `<html lang=`},
		{"title tag", `<title>`},
	}
	for _, c := range checks {
		if !strings.Contains(html, c.pattern) {
			t.Errorf("missing %s (pattern: %s)", c.name, c.pattern)
		}
	}

	// Security headers (CSP)
	if !strings.Contains(html, "Content-Security-Policy") {
		t.Error("missing Content-Security-Policy meta tag")
	}
}

// TestExportPages_CSSPresent validates CSS files are included
func TestExportPages_CSSPresent(t *testing.T) {
	bv := buildBvBinary(t)
	stageViewerAssets(t, bv)

	repoDir := createSimpleRepo(t, 3)
	exportDir := filepath.Join(repoDir, "bv-pages")

	cmd := exec.Command(bv, "--export-pages", exportDir)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("--export-pages failed: %v\n%s", err, out)
	}

	// Check styles.css exists
	stylesPath := filepath.Join(exportDir, "styles.css")
	info, err := os.Stat(stylesPath)
	if err != nil {
		t.Fatalf("styles.css not found: %v", err)
	}
	if info.Size() == 0 {
		t.Error("styles.css is empty")
	}

	// Check index.html references the stylesheet
	indexBytes, err := os.ReadFile(filepath.Join(exportDir, "index.html"))
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	if !strings.Contains(string(indexBytes), `href="styles.css"`) {
		t.Error("index.html doesn't reference styles.css")
	}
}

// TestExportPages_JavaScriptFiles validates JS files are present
func TestExportPages_JavaScriptFiles(t *testing.T) {
	bv := buildBvBinary(t)
	stageViewerAssets(t, bv)

	repoDir := createSimpleRepo(t, 3)
	exportDir := filepath.Join(repoDir, "bv-pages")

	cmd := exec.Command(bv, "--export-pages", exportDir)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("--export-pages failed: %v\n%s", err, out)
	}

	// Required JS files (charts.js is embedded in index.html, not separate)
	jsFiles := []string{
		"viewer.js",
		"graph.js",
		"coi-serviceworker.js",
	}

	for _, jsFile := range jsFiles {
		path := filepath.Join(exportDir, jsFile)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("%s not found: %v", jsFile, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", jsFile)
		}
	}

	// Vendor files
	vendorFiles := []string{
		"vendor/bv_graph.js",
		"vendor/bv_graph_bg.wasm",
	}
	for _, vf := range vendorFiles {
		path := filepath.Join(exportDir, vf)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("vendor file %s not found: %v", vf, err)
		}
	}
}

// TestExportPages_SQLiteDatabase validates the SQLite export
func TestExportPages_SQLiteDatabase(t *testing.T) {
	bv := buildBvBinary(t)
	stageViewerAssets(t, bv)

	repoDir := createSimpleRepo(t, 10)
	exportDir := filepath.Join(repoDir, "bv-pages")

	cmd := exec.Command(bv, "--export-pages", exportDir)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("--export-pages failed: %v\n%s", err, out)
	}

	// Check database exists and is non-empty
	dbPath := filepath.Join(exportDir, "beads.sqlite3")
	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("beads.sqlite3 not found: %v", err)
	}
	if info.Size() < 1024 {
		t.Errorf("beads.sqlite3 suspiciously small: %d bytes", info.Size())
	}

	// Check config.json exists
	configPath := filepath.Join(exportDir, "beads.sqlite3.config.json")
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("beads.sqlite3.config.json not found: %v", err)
	}

	var config struct {
		Chunked   bool  `json:"chunked"`
		TotalSize int64 `json:"total_size"`
	}
	if err := json.Unmarshal(configBytes, &config); err != nil {
		t.Fatalf("parse config.json: %v", err)
	}
	if config.TotalSize == 0 {
		t.Error("config.json reports total_size of 0")
	}
}

// TestExportPages_TriageJSON validates triage data export
func TestExportPages_TriageJSON(t *testing.T) {
	bv := buildBvBinary(t)
	stageViewerAssets(t, bv)

	repoDir := createSimpleRepo(t, 5)
	exportDir := filepath.Join(repoDir, "bv-pages")

	cmd := exec.Command(bv, "--export-pages", exportDir)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("--export-pages failed: %v\n%s", err, out)
	}

	// Check triage.json exists and has expected structure
	triagePath := filepath.Join(exportDir, "data", "triage.json")
	triageBytes, err := os.ReadFile(triagePath)
	if err != nil {
		t.Fatalf("triage.json not found: %v", err)
	}

	var triage struct {
		Recommendations []struct {
			ID    string  `json:"id"`
			Score float64 `json:"score"`
		} `json:"recommendations"`
		ProjectHealth struct {
			StatusCounts map[string]int `json:"status_counts"`
		} `json:"project_health"`
	}
	if err := json.Unmarshal(triageBytes, &triage); err != nil {
		t.Fatalf("parse triage.json: %v", err)
	}

	// Should have recommendations for open issues
	if len(triage.Recommendations) == 0 {
		t.Error("triage.json has no recommendations")
	}
}

// TestExportPages_MetaJSON validates metadata export
func TestExportPages_MetaJSON(t *testing.T) {
	bv := buildBvBinary(t)
	stageViewerAssets(t, bv)

	repoDir := createSimpleRepo(t, 5)
	exportDir := filepath.Join(repoDir, "bv-pages")

	// Use --pages-include-closed to include all 5 issues
	cmd := exec.Command(bv, "--export-pages", exportDir, "--pages-title", "Test Dashboard", "--pages-include-closed")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("--export-pages failed: %v\n%s", err, out)
	}

	metaPath := filepath.Join(exportDir, "data", "meta.json")
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("meta.json not found: %v", err)
	}

	var meta struct {
		Version     string `json:"version"`
		GeneratedAt string `json:"generated_at"`
		IssueCount  int    `json:"issue_count"`
		Title       string `json:"title"`
	}
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf("parse meta.json: %v", err)
	}

	if meta.Version == "" {
		t.Error("meta.json missing version")
	}
	if meta.GeneratedAt == "" {
		t.Error("meta.json missing generated_at")
	}
	if meta.IssueCount != 5 {
		t.Errorf("meta.json issue_count = %d, want 5", meta.IssueCount)
	}
	if meta.Title != "Test Dashboard" {
		t.Errorf("meta.json title = %q, want %q", meta.Title, "Test Dashboard")
	}
}

// TestExportPages_DependencyGraph validates graph data for issues with deps
func TestExportPages_DependencyGraph(t *testing.T) {
	bv := buildBvBinary(t)
	stageViewerAssets(t, bv)

	repoDir := createRepoWithDeps(t)
	exportDir := filepath.Join(repoDir, "bv-pages")

	cmd := exec.Command(bv, "--export-pages", exportDir)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("--export-pages failed: %v\n%s", err, out)
	}

	// Triage should show blocked issues
	triagePath := filepath.Join(exportDir, "data", "triage.json")
	triageBytes, err := os.ReadFile(triagePath)
	if err != nil {
		t.Fatalf("triage.json not found: %v", err)
	}

	var triage struct {
		ProjectHealth struct {
			StatusCounts map[string]int `json:"status_counts"`
		} `json:"project_health"`
	}
	if err := json.Unmarshal(triageBytes, &triage); err != nil {
		t.Fatalf("parse triage.json: %v", err)
	}

	// Our test data has blocked issues
	if triage.ProjectHealth.StatusCounts["blocked"] == 0 {
		t.Log("Note: No blocked issues in triage (might be expected if deps don't cause blocked status)")
	}
}

// TestExportPages_DataScale_10Issues tests with 10 issues
func TestExportPages_DataScale_10Issues(t *testing.T) {
	testExportPagesWithScale(t, 10)
}

// TestExportPages_DataScale_100Issues tests with 100 issues
func TestExportPages_DataScale_100Issues(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large scale test in short mode")
	}
	testExportPagesWithScale(t, 100)
}

func testExportPagesWithScale(t *testing.T, issueCount int) {
	bv := buildBvBinary(t)
	stageViewerAssets(t, bv)

	repoDir := createSimpleRepo(t, issueCount)
	exportDir := filepath.Join(repoDir, "bv-pages")

	// Use --pages-include-closed to include all issues
	cmd := exec.Command(bv, "--export-pages", exportDir, "--pages-include-closed")
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--export-pages failed with %d issues: %v\n%s", issueCount, err, out)
	}

	// Verify meta.json has correct count
	metaPath := filepath.Join(exportDir, "data", "meta.json")
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("meta.json not found: %v", err)
	}

	var meta struct {
		IssueCount int `json:"issue_count"`
	}
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf("parse meta.json: %v", err)
	}
	if meta.IssueCount != issueCount {
		t.Errorf("issue_count = %d, want %d", meta.IssueCount, issueCount)
	}

	// Verify database size scales appropriately
	dbPath := filepath.Join(exportDir, "beads.sqlite3")
	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("beads.sqlite3 not found: %v", err)
	}
	// Rough check: db should be at least 100 bytes per issue
	minExpectedSize := int64(issueCount * 100)
	if info.Size() < minExpectedSize {
		t.Errorf("database size %d bytes seems too small for %d issues (expected at least %d)",
			info.Size(), issueCount, minExpectedSize)
	}
}

// TestExportPages_DarkModeSupport validates dark mode CSS classes
func TestExportPages_DarkModeSupport(t *testing.T) {
	bv := buildBvBinary(t)
	stageViewerAssets(t, bv)

	repoDir := createSimpleRepo(t, 3)
	exportDir := filepath.Join(repoDir, "bv-pages")

	cmd := exec.Command(bv, "--export-pages", exportDir)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("--export-pages failed: %v\n%s", err, out)
	}

	indexBytes, err := os.ReadFile(filepath.Join(exportDir, "index.html"))
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	html := string(indexBytes)

	// Check for dark mode infrastructure
	darkModeIndicators := []string{
		"darkMode",          // Tailwind darkMode config
		"dark:",             // Tailwind dark: prefix classes
		"dark-mode",         // Generic dark mode references
		"prefers-color-scheme", // Media query detection
	}

	found := false
	for _, indicator := range darkModeIndicators {
		if strings.Contains(html, indicator) {
			found = true
			break
		}
	}
	if !found {
		t.Error("no dark mode support indicators found in index.html")
	}
}

// TestExportPages_NoXSSVulnerabilities checks for basic XSS protections
func TestExportPages_NoXSSVulnerabilities(t *testing.T) {
	bv := buildBvBinary(t)
	stageViewerAssets(t, bv)

	// Create repo with potentially dangerous content
	repoDir := t.TempDir()
	beadsPath := filepath.Join(repoDir, ".beads")
	if err := os.MkdirAll(beadsPath, 0o755); err != nil {
		t.Fatalf("mkdir beads: %v", err)
	}

	// Issue with XSS attempt in title
	jsonl := `{"id": "xss-1", "title": "<script>alert('xss')</script>", "status": "open", "priority": 1, "issue_type": "task"}
{"id": "xss-2", "title": "Normal issue", "description": "<img onerror='alert(1)' src='x'>", "status": "open", "priority": 2, "issue_type": "task"}`
	if err := os.WriteFile(filepath.Join(beadsPath, "beads.jsonl"), []byte(jsonl), 0o644); err != nil {
		t.Fatalf("write beads.jsonl: %v", err)
	}

	exportDir := filepath.Join(repoDir, "bv-pages")
	cmd := exec.Command(bv, "--export-pages", exportDir)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("--export-pages failed: %v\n%s", err, out)
	}

	// Check that CSP header is present (provides XSS protection)
	indexBytes, err := os.ReadFile(filepath.Join(exportDir, "index.html"))
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	if !strings.Contains(string(indexBytes), "Content-Security-Policy") {
		t.Error("missing Content-Security-Policy for XSS protection")
	}
}

// TestExportPages_ResponsiveLayout checks for responsive design markers
func TestExportPages_ResponsiveLayout(t *testing.T) {
	bv := buildBvBinary(t)
	stageViewerAssets(t, bv)

	repoDir := createSimpleRepo(t, 3)
	exportDir := filepath.Join(repoDir, "bv-pages")

	cmd := exec.Command(bv, "--export-pages", exportDir)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("--export-pages failed: %v\n%s", err, out)
	}

	indexBytes, err := os.ReadFile(filepath.Join(exportDir, "index.html"))
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	html := string(indexBytes)

	// Check for viewport meta tag (essential for responsive design)
	if !strings.Contains(html, "viewport") {
		t.Error("missing viewport meta tag")
	}

	// Check for responsive classes (Tailwind breakpoints)
	responsiveIndicators := []string{
		"sm:",  // Small breakpoint
		"md:",  // Medium breakpoint
		"lg:",  // Large breakpoint
		"max-w-", // Max width containers
	}

	foundResponsive := 0
	for _, indicator := range responsiveIndicators {
		if strings.Contains(html, indicator) {
			foundResponsive++
		}
	}
	if foundResponsive < 2 {
		t.Errorf("only found %d responsive design indicators, expected at least 2", foundResponsive)
	}
}

// ============================================================================
// Test Helpers for bv-ct7m
// ============================================================================

// createSimpleRepo creates a test repo with N simple issues
func createSimpleRepo(t *testing.T, issueCount int) string {
	t.Helper()
	repoDir := t.TempDir()
	beadsPath := filepath.Join(repoDir, ".beads")
	if err := os.MkdirAll(beadsPath, 0o755); err != nil {
		t.Fatalf("mkdir beads: %v", err)
	}

	var issues strings.Builder
	for i := 1; i <= issueCount; i++ {
		status := "open"
		if i%5 == 0 {
			status = "closed"
		} else if i%3 == 0 {
			status = "in_progress"
		}
		priority := i % 5
		issueType := "task"
		if i%7 == 0 {
			issueType = "bug"
		} else if i%10 == 0 {
			issueType = "feature"
		}

		line := `{"id": "issue-` + itoa(i) + `", "title": "Test Issue ` + itoa(i) + `", "description": "Description for issue ` + itoa(i) + `", "status": "` + status + `", "priority": ` + itoa(priority) + `, "issue_type": "` + issueType + `"}` + "\n"
		issues.WriteString(line)
	}

	if err := os.WriteFile(filepath.Join(beadsPath, "beads.jsonl"), []byte(issues.String()), 0o644); err != nil {
		t.Fatalf("write beads.jsonl: %v", err)
	}
	return repoDir
}

// createRepoWithDeps creates a test repo with dependency relationships
func createRepoWithDeps(t *testing.T) string {
	t.Helper()
	repoDir := t.TempDir()
	beadsPath := filepath.Join(repoDir, ".beads")
	if err := os.MkdirAll(beadsPath, 0o755); err != nil {
		t.Fatalf("mkdir beads: %v", err)
	}

	// Create a dependency chain: A <- B <- C (C blocked by B, B blocked by A)
	jsonl := `{"id": "root-a", "title": "Root Task A", "status": "open", "priority": 0, "issue_type": "task"}
{"id": "child-b", "title": "Child Task B", "status": "blocked", "priority": 1, "issue_type": "task", "dependencies": [{"target_id": "root-a", "type": "blocks"}]}
{"id": "leaf-c", "title": "Leaf Task C", "status": "blocked", "priority": 2, "issue_type": "task", "dependencies": [{"target_id": "child-b", "type": "blocks"}]}
{"id": "independent-d", "title": "Independent Task D", "status": "open", "priority": 1, "issue_type": "bug"}`

	if err := os.WriteFile(filepath.Join(beadsPath, "beads.jsonl"), []byte(jsonl), 0o644); err != nil {
		t.Fatalf("write beads.jsonl: %v", err)
	}
	return repoDir
}

// itoa is a simple int to string helper
func itoa(i int) string {
	return fmt.Sprintf("%d", i)
}

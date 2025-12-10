# Beads Viewer (bv)

A high-performance TUI for browsing and analyzing Beads issue tracking projects. Built with Go and Bubble Tea.

## Task Tracking with Beads (bd)

Use beads as the primary task tracker for this project. Do NOT use TodoWrite tool.

### Core Workflow

**Find work:**
```bash
# IMPORTANT: Use bash `bd ready` command, NOT the MCP plugin for ready queries
bd ready                              # Show issues ready to work (no blockers)
bd ready --label=<scope>              # Filter by label (e.g., --label=ui, --label=api)
bd list --status=open --label=<scope> # Scoped open issues (prefer over unfiltered)
```

**Get condensed overviews (prefer bv for insights):**
```bash
bv --robot-plan                       # Fast actionable items, execution plan
bv --robot-priority                   # Priority recommendations
bv --robot-insights                   # Full graph metrics (slower)
```

**View specific issue details:**
```bash
bd show <id>                          # ALWAYS specify an ID - never run bare `bd show`
```
> **Context Warning:** Never use `bd show` without an issue ID. It dumps all issues and eats context. Always filter first with `bd list --label=` or `bv --robot-*`, then `bd show <specific-id>`.

**Claim and complete work:**
```bash
bd update <id> --status=in_progress   # Claim work
bd close <id>                         # Mark complete
bd close <id1> <id2> ...              # Close multiple at once
```

**Create issues:**
```bash
bd create --title="..." --type=task   # Types: bug, feature, task, epic, chore
bd dep add <issue> <depends-on>       # Add dependency
```

---

## Git Workflow

**Branches:**
- `main` — mirrors upstream, never commit directly
- `fix/*`, `feat/*` — clean PR branches off main
- `local` — daily driver, personal customizations, never PR'd

**Daily work (on `local`):**
```bash
go build -o bv ./cmd/bv               # Rebuild after changes
git commit -m "LOCAL: description"    # Personal changes
git commit -m "PR: description"       # PR-worthy changes (mark for later)
```

**Submitting a PR:**
```bash
git checkout -b fix/my-fix main       # Clean branch off main
git cherry-pick <commit>              # Pick PR: commits
git commit --amend --no-verify        # Skip beads hook for PR commits
git push origin fix/my-fix            # Push, open PR to upstream
```

**After PR merged:**
```bash
git checkout main && git pull upstream main
git checkout local && git rebase main # Rebase local onto updated main
```

---

## Using BV for Analysis

bv pre-computes graph metrics so you don't need to parse JSONL or risk hallucinated traversals.

### Robot Protocol Commands

```bash
bv --robot-insights                   # JSON graph metrics (PageRank, betweenness, HITS, cycles)
bv --robot-plan                       # JSON execution plan with parallel tracks
bv --robot-priority                   # Priority recommendations with reasoning
bv --robot-diff --diff-since <ref>    # JSON diff of issue changes
bv --robot-recipes                    # List available view recipes
bv --robot-help                       # All AI-facing commands
```

### Performance Notes

- **Phase 1 (instant):** Degree, topo sort, basic stats
- **Phase 2 (async):** PageRank, betweenness, HITS, cycles
- Use `--robot-plan` for fast actionable items (Phase 1 only)
- Use `--robot-insights` when full metrics needed (waits for Phase 2)
- Large graphs (>500 nodes): some metrics may be skipped automatically

---

## Project Structure

```
cmd/bv/main.go          # Entry point, CLI flags
pkg/ui/                 # TUI components (Bubble Tea models)
pkg/loader/             # Data loading, git integration
pkg/analysis/           # Graph algorithms (PageRank, Betweenness, HITS)
pkg/export/             # Markdown/Mermaid export
pkg/recipe/             # View configuration (YAML-based filters)
pkg/model/              # Data types (Issue, Dependency, etc.)
pkg/watcher/            # Live reload on file changes
pkg/baseline/           # Drift detection, snapshots
```

---

## Development Guidelines

### Code Style

- Run `gofmt` on all code
- `CamelCase` for exports, `camelCase` for private
- Wrap errors with `fmt.Errorf("context: %w", err)`
- Don't panic except for unrecoverable init errors

### TUI Development

- Follow Elm Architecture: Model → Update → View
- Use Lipgloss for styling (central `styles.go`)
- Break complex UIs into smaller `tea.Model` components
- State in main model, delegate update logic to sub-models

### Testing

```bash
go test ./...           # Run all tests
go build ./cmd/bv       # Build binary
```

Use table-driven tests for parsers and validators.

---

## Search Tool Guidance

| Scenario | Tool |
|----------|------|
| Project graph analysis | `bv --robot-*` |
| Exploratory "how does X work?" | `warp_grep` (if available) |
| Known pattern search | `ripgrep` via Grep tool |
| Structural refactor | `ast-grep` |

### ast-grep vs ripgrep

- **ast-grep:** Structural matches, safe rewrites, ignores comments/strings
- **ripgrep:** Fast text search, known patterns

```bash
# ast-grep for codemods
ast-grep run -l Go -p 'func $NAME($$$)' -r 'func $NAME(ctx context.Context, $$$)'

# ripgrep for text hunt
rg -n 'TODO' -t go
```

---

## Key Files Reference

| File | Purpose |
|------|---------|
| `cmd/bv/main.go` | CLI entry, flag parsing (~1350 lines) |
| `pkg/ui/model.go` | Main Bubble Tea model |
| `pkg/ui/graph.go` | ASCII/Unicode dependency graph renderer |
| `pkg/ui/insights.go` | 6-panel metrics dashboard |
| `pkg/analysis/analyzer.go` | Graph algorithm orchestration |
| `pkg/loader/loader.go` | JSONL parsing, workspace discovery |

---

## Related Documentation

- `AGENTS.md` - Detailed AI agent integration guide
- `GOLANG_BEST_PRACTICES.md` - Go coding conventions
- `docs/performance.md` - Performance tuning guide
- `README.md` - Full feature documentation

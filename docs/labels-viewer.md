# Labels Viewer Feature Specification

> A dashboard for long-horizon task coordination in Beads Viewer

## Overview

The Labels Viewer (`l` key) provides a focused view of all issues associated with a selected **label or epic**, expanded to include their dependency chains. It solves the **session continuity problem** - enabling users to quickly pick up where they left off on complex, multi-phase projects.

Labels and epics act as a "net" that captures primary issues, with the viewer automatically expanding to show all connected dependencies for comprehensive visibility.

### Entry Point Modes

The viewer supports two entry point modes with identical UI:

1. **Label Mode:** Select a label → shows all issues with that label + dependencies
2. **Epic Mode:** Select an epic → shows the epic + all child tasks + dependencies

Both modes use the same dashboard UI, workstream detection, and scoped tools.

---

## Core Concepts

### The "Net" Model

```
Label = entry point (the net)
     │
     ▼
┌─────────────────────────────────────────────────────┐
│  LABELED ISSUES (primary catches)                   │
│  ● BV-12 Setup email ingestion pipeline             │
│  ● BV-15 Parse MIME attachments                     │
│  ● BV-18 Store parsed emails                        │
└─────────────────────────────────────────────────────┘
     │
     ▼ expand dependencies (configurable depth)
┌─────────────────────────────────────────────────────┐
│  UNLABELED BUT CONNECTED (context)                  │
│  ○ BV-09 [infra] Add Redis queue          (blocks BV-12)
│  ○ BV-11 Core attachment parser           (blocks BV-15)
│  ○ BV-14 [db] Email schema migration      (blocks BV-18)
│  ○ BV-07 Shared validation utils          (blocks BV-11)
└─────────────────────────────────────────────────────┘
```

### Visual Language

| Symbol | Meaning |
|--------|---------|
| `●` Filled | Caught by label (primary issue) |
| `○` Hollow | Pulled in via dependency graph (context) |
| `[label]` | Different label tag on context issues |

### Workstream Detection

When a label captures unrelated work (e.g., generic `infra` label), the viewer automatically groups issues by **graph connectivity**:

- Two issues are in the same workstream if they're connected through dependencies
- Unconnected clusters become separate workstreams
- Each workstream gets its own progress tracking and status

### Epic Mode

When an epic is selected instead of a label:

```
Epic = entry point (the anchor)
     │
     ▼
┌─────────────────────────────────────────────────────┐
│  EPIC HEADER (the anchor issue)                     │
│  📋 BV-6i8 Labels Viewer Epic                       │
└─────────────────────────────────────────────────────┘
     │
     ▼ parent-child relationships
┌─────────────────────────────────────────────────────┐
│  CHILD TASKS (primary)                              │
│  ● BV-jpk Label selector UI                         │
│  ● BV-ljz Basic dashboard view                      │
│  ● BV-37c Dependency expansion                      │
│  ...                                                │
└─────────────────────────────────────────────────────┘
     │
     ▼ expand dependencies (same as label mode)
┌─────────────────────────────────────────────────────┐
│  BLOCKING DEPENDENCIES (context)                    │
│  ○ BV-xxx [other-epic] Shared component             │
│  ○ BV-yyy [infra] Database setup                    │
└─────────────────────────────────────────────────────┘
```

**Epic Mode Behavior:**
- Epic itself shown as header/anchor (not in the issue list)
- All child tasks (parent-child dependencies) are "primary" issues
- Dependency expansion works the same as label mode
- Workstream detection groups children by connectivity
- Progress bar shows child task completion (not including epic itself)

**Visual Language in Epic Mode:**
| Symbol | Meaning |
|--------|---------|
| `📋` Header | The epic itself (anchor) |
| `●` Filled | Direct child of epic (primary) |
| `○` Hollow | Pulled in via dependency graph (context) |

---

## Features

### 1. Label & Epic Selection Interface

**Trigger:** Press `l` from any view

**UI Components:**
- Fuzzy search input (searches both labels AND epics)
- Pinned items section at top (labels and epics, with status indicators)
- Recent items section
- Epics section (all open epics)
- Labels section (grouped by prefix)

```
┌─────────────────────────────────────────────────────┐
│ Select Label or Epic: [_____________]               │
├─────────────────────────────────────────────────────┤
│ PINNED                                              │
│   ★ feat:inbound-emails        [██████░░] 6/10     │
│   ★ 📋 Labels Viewer Epic      [████░░░░] 4/20     │
├─────────────────────────────────────────────────────┤
│ RECENT                                              │
│   frontend/inbox               [████████] 8/8 ✓    │
│   📋 Q4 Infrastructure         [██░░░░░░] 2/12     │
├─────────────────────────────────────────────────────┤
│ EPICS                                               │
│   📋 Labels Viewer Epic        [████░░░░] 4/20     │
│   📋 Q4 Infrastructure         [██░░░░░░] 2/12     │
│   📋 Auth Overhaul             [██████░░] 6/10     │
├─────────────────────────────────────────────────────┤
│ LABELS                                              │
│   ▼ feat:                                           │
│       feat:inbound-emails                           │
│       feat:outbound-sync                            │
│   ▼ Phase:                                          │
│       Phase1-intake                                 │
│       Phase2-parsing                                │
└─────────────────────────────────────────────────────┘
```

**Epic vs Label Visual Distinction:**
- Epics show 📋 icon prefix
- Labels show no icon (or optional tag icon)
- Both show progress bars when pinned or in recent

### 2. Prefix-Based Label Grouping

**Auto-Detection Algorithm:**
1. Scan all labels for common delimiters: `:`, `/`, `-`, `_`
2. For each delimiter, extract prefix (text before first delimiter)
3. Count frequency of each prefix
4. If prefix appears 2+ times, treat as a group

**Examples:**
- `feat:inbound-emails`, `feat:outbound` → Group: `feat:`
- `Phase1-intake`, `Phase1-parsing` → Group: `Phase1-`
- `frontend/inbox`, `frontend/settings` → Group: `frontend/`

**User Override:**
Store custom groupings in `.beads/viewer.json`:
```json
{
  "labelGroups": {
    "feat:": { "name": "Features", "collapsed": false },
    "Phase": { "name": "Phases", "collapsed": false }
  }
}
```

### 3. Main Dashboard View

**Single Workstream Layout:**
```
┌─────────────────────────────────────────────────────────────────────┐
│ feat:inbound-emails                    [██████░░░░] 6/10            │
│ ● Labeled: 4  ○ Dependencies: 6        Mode: [Union ▾] Depth: [3]   │
├────────────────────────────────────────────┬────────────────────────┤
│ READY (2)                                  │                        │
│ ● BV-15 Parse MIME attachments             │  BV-15                 │
│ ○ BV-07 Shared validation utils            │  ──────                │
│────────────────────────────────────────────│  Parse MIME...         │
│ BLOCKED (2)                                │                        │
│ ● BV-12 Setup email pipeline               │  Blocked by:           │
│   └─ blocked by: ○ BV-09 Add Redis queue   │   • BV-09 Redis queue  │
│ ○ BV-14 Email schema migration             │                        │
│   └─ blocked by: ○ BV-03 DB credentials    │  Labels: feat:inbound  │
│────────────────────────────────────────────│                        │
│ IN PROGRESS (1)                            │  Dependencies:         │
│ ● BV-18 Store parsed emails                │   • BV-14 schema       │
│────────────────────────────────────────────│   • BV-11 parser       │
│ CLOSED (5)                                 │                        │
│ ● BV-10 ✓  ● BV-13 ✓  ○ BV-06 ✓ ...       │                        │
└────────────────────────────────────────────┴────────────────────────┘
  g=graph  i=insights  p=pin  d=depth  m=mode  a=apply-label  /=search
```

**Multi-Workstream Layout (Generic Labels):**
```
infra                                    12 issues · 3 workstreams
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

▼ inbound-emails context                          [██░░░░░░] 2/8
  Related: feat:inbound-emails, Phase1-intake
  ┌────────────────────────────────────────────────────────────┐
  │ BLOCKED (1)                                                │
  │ ● BV-09 Add Redis queue                                    │
  │   └─ waiting: ○ BV-03 [devops] AWS credentials            │
  │   └─ blocks: ○ BV-12 [feat:inbound] Setup pipeline        │
  │                                                            │
  │ READY (1)                                                  │
  │ ● BV-14 Email schema migration                             │
  │   └─ blocks: ○ BV-18 [feat:inbound] Store parsed emails   │
  └────────────────────────────────────────────────────────────┘

▼ devops-q4 context                               [████░░░░] 4/8
  Related: epic:devops-q4
  ┌────────────────────────────────────────────────────────────┐
  │ IN PROGRESS (1)                                            │
  │ ● BV-31 Migration runner framework                         │
  │                                                            │
  │ READY (2)                                                  │
  │ ● BV-33 Rollback automation                                │
  │ ● BV-35 Blue-green deploy scripts                          │
  └────────────────────────────────────────────────────────────┘

▾ standalone (completed)                          [████████] 3/3 ✓
  ● BV-22 ✓  ● BV-23 ✓  ● BV-24 ✓

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
g=graph  i=insights  p=pin  1/2/3=focus workstream  e=expand all
```

### 4. Workstream Auto-Detection

**Algorithm:**
1. Get all issues with selected label
2. Expand to include their dependencies (up to configured depth)
3. Find connected components in this subgraph
4. Each connected component = one workstream

**Workstream Naming:**
- Look at other labels present in the workstream
- Pick the most specific/frequent label as context name
- If connected to an epic, use epic name
- If isolated, label as "standalone"

**Per-Workstream Features:**
- Individual progress bar
- Ready/blocked/in-progress counts
- Collapse/expand toggle
- Auto-collapse completed workstreams

### 5. Fully Blocked Workstream Handling

When all issues in a workstream are blocked:

```
▼ Phase2 work                                     [░░░░░░░░] 0/4 ⚠️
  ⚠️ FULLY BLOCKED - waiting on Phase1-intake (3 remaining)
  ┌────────────────────────────────────────────────────────────┐
  │ BLOCKING THIS WORKSTREAM:                                  │
  │ ○ BV-50 [Phase1] Complete intake parser        IN PROGRESS │
  │ ○ BV-51 [Phase1] Validate intake schema        READY       │
  │ ○ BV-52 [Phase1] Intake error handling         READY       │
  │ ─────────────────────────────────────────────────────────  │
  │ WAITING:                                                   │
  │ ● BV-60 Phase2 transform setup                 BLOCKED     │
  │ ● BV-61 Phase2 output formatting               BLOCKED     │
  └────────────────────────────────────────────────────────────┘
```

**UI Indicators:**
- Warning icon on workstream header
- "FULLY BLOCKED" status message
- Summary of what's blocking (label and count)
- Blockers shown at top of workstream panel

### 6. Dependency Expansion

**Default Depth:** 3 levels

**Direction:** Upstream (blockers) by default
- Shows what's preventing labeled work from starting
- Toggle available for downstream (dependents)

**Depth Control:**
- Press `d` to cycle: 1 → 2 → 3 → All → 1
- Or access via mode menu

**Expand All:**
- Press `e` to expand all transitive dependencies
- Warning shown if graph is very large (50+ nodes)

### 7. Multi-Label Selection

**Union Mode:** Show issues with ANY of the selected labels
**Intersection Mode:** Show issues with ALL of the selected labels

**UI:**
- Press `m` to toggle mode
- Or select multiple labels in selector (shift+enter to add)

```
Labels: feat:inbound-emails + Phase1    Mode: [Intersection ▾]
```

### 8. Pinned Labels

**Persistence:** Stored in `.beads/viewer.json`

**Features:**
- Press `p` to pin/unpin current label view
- Pinned labels appear at top of selector with status
- Status indicators update in real-time

**Config Format:**
```json
{
  "pinnedLabels": [
    "feat:inbound-emails",
    "Phase2-parsing",
    "epic:q4-launch"
  ]
}
```

### 9. Inline Label Creation & Application

**Create New Label:**
- Press `n` in label selector
- Enter label name
- Optionally apply to current issue

**Apply Label to Issues:**
When applying a label, choose scope:

```
┌─────────────────────────────────────────────┐
│ Apply label: feat:inbound-emails            │
├─────────────────────────────────────────────┤
│ ○ This issue only                           │
│ ○ This + direct dependencies (3 issues)     │
│ ● This + all upstream deps (7 issues)       │
│ ○ This + all downstream dependents (2)      │
│ ○ Entire connected subgraph (12 issues)     │
└─────────────────────────────────────────────┘
```

Shows affected issue count before confirming.

### 10. Scoped Tool Integration

When in label view, other tools scope to the current context:

| Tool | Behavior |
|------|----------|
| `g` Graph | Shows dependency graph filtered to label (or focused workstream) |
| `i` Insights | Shows metrics for labeled issues only |
| `/` Search | Searches within labeled issues |
| Side panel | Works as usual, shows full issue details |

**Focus Mode:**
- Press `1`, `2`, `3` etc. to focus a specific workstream
- Focused workstream expands full screen
- Scoped tools operate only on focused workstream
- Press `Esc` to unfocus

### 11. Session Restoration

**Last Viewed Label:**
- Remember which label view was active when user quit
- Offer to restore on next launch (optional)

**Config:**
```json
{
  "lastLabel": "feat:inbound-emails",
  "restoreOnLaunch": false
}
```

---

## Keybindings

| Key | Action |
|-----|--------|
| `l` | Open label selector (from any view) |
| `Enter` | Select label / Confirm action |
| `Esc` | Back / Unfocus workstream |
| `p` | Pin/unpin current label |
| `d` | Cycle dependency depth (1/2/3/All) |
| `m` | Toggle union/intersection mode |
| `e` | Expand all dependencies |
| `a` | Apply label to issue(s) |
| `n` | Create new label |
| `g` | Open scoped graph view |
| `i` | Open scoped insights view |
| `/` | Search within label |
| `1-9` | Focus workstream by number |
| `Tab` | Cycle between sections |
| `j/k` | Navigate issues |

---

## Configuration

**File:** `.beads/viewer.json`

```json
{
  "labels": {
    "pinnedLabels": [
      "feat:inbound-emails",
      "Phase2-parsing"
    ],
    "labelGroups": {
      "feat:": { "name": "Features", "collapsed": false },
      "Phase": { "name": "Phases", "collapsed": true },
      "frontend/": { "name": "Frontend", "collapsed": false }
    },
    "defaultDepth": 3,
    "defaultMode": "union",
    "autoCollapseCompleted": true,
    "lastLabel": "feat:inbound-emails",
    "restoreOnLaunch": false
  }
}
```

---

## Edge Cases

### Single Connected Workstream
If all labeled issues are connected, show single workstream view (no grouping UI).

### Workstream with Only Context Issues
If a workstream has no directly-labeled issues (all pulled in via dependencies):
```
▼ indirect context                               (no direct matches)
  These issues connect to your labeled work but don't have the label
```

### Large Workstreams
If workstream has 50+ issues:
- Show top 10 by priority by default
- "Show all (N)" expand option
- Pagination for very large sets

### Empty Label
If no issues have the selected label:
- Show empty state with suggestion to apply label to issues
- Quick action to search for issues to label

---

## Implementation Phases

### Phase 1: Core Label View
- [ ] Label selector with fuzzy search
- [ ] Basic dashboard view (single label, no workstreams)
- [ ] Dependency expansion (fixed depth)
- [ ] Visual distinction (● vs ○)
- [ ] Basic keybindings
- [ ] Epic support in selector (list epics alongside labels)
- [ ] Epic mode dashboard (epic as anchor, children as primary)

### Phase 2: Workstream Detection
- [ ] Graph connectivity algorithm
- [ ] Auto-grouping into workstreams
- [ ] Workstream naming heuristic
- [ ] Per-workstream progress tracking
- [ ] Collapse/expand workstreams

### Phase 3: Advanced Features
- [ ] Pinned labels with persistence
- [ ] Multi-label selection (union/intersection)
- [ ] Inline label creation
- [ ] Apply label to downstream/upstream
- [ ] Scoped tool integration (graph, insights)

### Phase 4: Polish
- [ ] Prefix-based label grouping
- [ ] Session restoration
- [ ] Fully blocked workstream warnings
- [ ] Focus mode for workstreams
- [ ] Performance optimization for large graphs

---

## Success Metrics

1. **Session continuity:** Users can return to a project and immediately see where they left off
2. **Context clarity:** Blocked work shows clear blockers, even across labels
3. **Navigation speed:** < 3 keystrokes to get from launch to working context
4. **Comprehensive view:** No surprises from hidden dependencies outside the label

# Beads Viewer (bv)

A polished, high-performance TUI for managing and exploring [Beads](https://github.com/steveyegge/beads) issue trackers. Designed for developers who live in the terminal.

## Features

### 🖥️ Slick Dashboard
*   **Adaptive Layout**: Automatically transitions from a compact list to a master-detail dashboard on wide screens (>100 cols).
*   **Columnar List**: At-a-glance details including Type (🐛/✨), Priority (🔥/⚡), Status, Assignee, Age, and Comment counts.
*   **Live Stats**: Always-visible counters for Open, Ready, Blocked, and Closed issues.

### 🎨 Rich Visualization
*   **Markdown Rendering**: Beautiful rendering of issue descriptions and comments with syntax highlighting (via `glamour`).
*   **Dracula Theme**: A vibrant, high-contrast color scheme optimized for long coding sessions.
*   **Dependency Graph**: Visualizes blocking (⛔) and related (🔗) issues directly in the context of the work.

### ⚡ Workflow
*   **Instant Filtering**: 
    *   `o`: **Open** only
    *   `r`: **Ready** work (Open & Unblocked)
    *   `c`: **Closed**
    *   `a`: **All**
*   **Keyboard Driven**: `vim` style navigation (`j`/`k`), Tab focus switching, and rapid filtering.

### 🛠️ Robust & Reliable
*   **Self-Updating**: Automatically checks for new releases and notifies you.
*   **Resilient Loader**: Handles large or partially malformed JSONL databases gracefully.

## Installation

### One-line Install
```bash
curl -fsSL https://raw.githubusercontent.com/Dicklesworthstone/beads_viewer/main/install.sh | bash
```

### From Source
```bash
go install github.com/Dicklesworthstone/beads_viewer/cmd/bv@latest
```

## Usage

Navigate to any project initialized with `bd init` and run:

```bash
bv
```

### Keybindings

| Key | Context | Action |
| :--- | :--- | :--- |
| `Tab` | Split View | Switch focus between List and Details |
| `j` / `k` | Global | Navigate list or scroll details |
| `Enter` | List | Open details (Mobile) or Focus details (Split) |
| `o` | Global | Filter: **Open** |
| `r` | Global | Filter: **Ready** |
| `c` | Global | Filter: **Closed** |
| `a` | Global | Filter: **All** |
| `q` | Global | Quit |

## CI/CD

This project uses GitHub Actions for:
*   **Tests**: Runs full unit and integration suite on every push.
*   **Releases**: Automatically builds and attaches optimized binaries for Linux, macOS, and Windows to every release tag.

## License

MIT
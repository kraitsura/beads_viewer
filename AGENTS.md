# Agents Guide to Beads Viewer (bv)

`bv` is a high-performance TUI for the [Beads](https://github.com/steveyegge/beads) issue tracker.

## Features

- **Split View Dashboard**: On wide terminals (>100 cols), shows a list on the left and rich details on the right.
- **Markdown Rendering**: Renders issue descriptions, notes, and comments with syntax highlighting.
- **Live Filtering**: Filter by Open (`o`), Closed (`c`), Ready (`r`), or All (`a`).
- **Dependency Graph**: Visualizes blockers and dependencies.

## Navigation

### Global
- `q` / `Ctrl+C`: Quit
- `Tab`: Switch focus between List and Details pane (Split View only)

### List View
- `j` / `↓`: Next issue
- `k` / `↑`: Previous issue
- `Enter`: Open details (Mobile view) or Focus details (Split view)
- `o`: Filter Open
- `c`: Filter Closed
- `r`: Filter Ready (Open + Unblocked)
- `a`: Show All

### Details View
- `j` / `k` / Arrows: Scroll content
- `Esc`: Back to list (Mobile view)

## Installation

```bash
./install.sh
```

## Development

Built with Go + Charmbracelet (Bubble Tea, Lipgloss, Glamour).
Follows `GOLANG_BEST_PRACTICES.md`.
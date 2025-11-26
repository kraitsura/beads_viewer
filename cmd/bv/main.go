package main

import (
	"flag"
	"fmt"
	"os"

	"beads_viewer/pkg/loader"
	"beads_viewer/pkg/ui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	help := flag.Bool("help", false, "Show help")
	version := flag.Bool("version", false, "Show version")
	flag.Parse()

	if *help {
		fmt.Println("Usage: bv [options]")
		fmt.Println("\nA TUI viewer for beads issue tracker.")
		flag.PrintDefaults()
		os.Exit(0)
	}

	if *version {
		fmt.Println("bv version 0.1.0")
		os.Exit(0)
	}

	// Load issues from current directory
	issues, err := loader.LoadIssues("")
	if err != nil {
		fmt.Printf("Error loading beads: %v\n", err)
		fmt.Println("Make sure you are in a project initialized with 'bd init'.")
		os.Exit(1)
	}

	if len(issues) == 0 {
		fmt.Println("No issues found. Create some with 'bd create'!")
		os.Exit(0)
	}

	// Initial Model
	m := ui.NewModel(issues)

	// Run Program
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running beads viewer: %v\n", err)
		os.Exit(1)
	}
}

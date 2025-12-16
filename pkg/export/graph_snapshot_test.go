package export

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

func TestSaveGraphSnapshot_SVGAndPNG(t *testing.T) {
	issues := []model.Issue{
		{ID: "A", Title: "Root task", Status: model.StatusOpen},
		{ID: "B", Title: "Depends on A", Status: model.StatusBlocked, Dependencies: []*model.Dependency{{DependsOnID: "A", Type: model.DepBlocks}}},
		{ID: "C", Title: "Independent", Status: model.StatusOpen},
	}
	analyzer := analysis.NewAnalyzer(issues)
	stats := analyzer.Analyze()

	tmp := t.TempDir()
	cases := []struct {
		name   string
		format string
	}{
		{"svg", "graph.svg"},
		{"png", "graph.png"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(tmp, tc.format)
			err := SaveGraphSnapshot(GraphSnapshotOptions{
				Path:     out,
				Issues:   issues,
				Stats:    &stats,
				DataHash: analysis.ComputeDataHash(issues),
			})
			if err != nil {
				t.Fatalf("SaveGraphSnapshot error: %v", err)
			}
			info, err := os.Stat(out)
			if err != nil {
				t.Fatalf("output not created: %v", err)
			}
			if info.Size() == 0 {
				t.Fatalf("output file is empty")
			}
		})
	}
}

func TestSaveGraphSnapshot_InvalidFormat(t *testing.T) {
	issues := []model.Issue{{ID: "A", Title: "Root", Status: model.StatusOpen}}
	analyzer := analysis.NewAnalyzer(issues)
	stats := analyzer.Analyze()

	err := SaveGraphSnapshot(GraphSnapshotOptions{
		Path:     "graph.txt",
		Format:   "txt",
		Issues:   issues,
		Stats:    &stats,
		DataHash: "hash",
	})
	if err == nil {
		t.Fatalf("expected error for invalid format")
	}
}

package ui

import (
	"testing"
)

func TestCompareHierarchicalIDs(t *testing.T) {
	tests := []struct {
		name     string
		id1      string
		id2      string
		expected int
	}{
		// Equal IDs
		{
			name:     "identical IDs",
			id1:      "bv-abc",
			id2:      "bv-abc",
			expected: 0,
		},
		{
			name:     "identical IDs with suffix",
			id1:      "bv-abc.1",
			id2:      "bv-abc.1",
			expected: 0,
		},

		// Different base IDs
		{
			name:     "different base IDs - first less",
			id1:      "bv-abc",
			id2:      "bv-xyz",
			expected: -1,
		},
		{
			name:     "different base IDs - first greater",
			id1:      "bv-xyz",
			id2:      "bv-abc",
			expected: 1,
		},

		// Same base ID, different first-level suffix
		{
			name:     "same base, suffix 1 < 2",
			id1:      "bv-xyz.1",
			id2:      "bv-xyz.2",
			expected: -1,
		},
		{
			name:     "same base, suffix 2 > 1",
			id1:      "bv-xyz.2",
			id2:      "bv-xyz.1",
			expected: 1,
		},
		{
			name:     "same base, suffix 1 < 10 (numeric)",
			id1:      "bv-xyz.1",
			id2:      "bv-xyz.10",
			expected: -1,
		},
		{
			name:     "same base, suffix 9 < 10 (numeric)",
			id1:      "bv-xyz.9",
			id2:      "bv-xyz.10",
			expected: -1,
		},

		// Parent before children (shorter ID first)
		{
			name:     "parent before child",
			id1:      "bv-xyz.1",
			id2:      "bv-xyz.1.1",
			expected: -1,
		},
		{
			name:     "child after parent",
			id1:      "bv-xyz.1.1",
			id2:      "bv-xyz.1",
			expected: 1,
		},

		// Same base ID, different second-level suffix
		{
			name:     "same base and first suffix, second suffix 1 < 2",
			id1:      "bv-xyz.1.1",
			id2:      "bv-xyz.1.2",
			expected: -1,
		},
		{
			name:     "same base and first suffix, second suffix 2 > 1",
			id1:      "bv-xyz.1.2",
			id2:      "bv-xyz.1.1",
			expected: 1,
		},

		// Three levels deep
		{
			name:     "three levels - parent before grandchild",
			id1:      "bv-xyz.1.1",
			id2:      "bv-xyz.1.1.1",
			expected: -1,
		},
		{
			name:     "three levels - sorting at third level",
			id1:      "bv-xyz.1.1.1",
			id2:      "bv-xyz.1.1.2",
			expected: -1,
		},

		// Mixed with base ID only
		{
			name:     "base ID before any suffix",
			id1:      "bv-xyz",
			id2:      "bv-xyz.1",
			expected: -1,
		},
		{
			name:     "suffix after base ID",
			id1:      "bv-xyz.1",
			id2:      "bv-xyz",
			expected: 1,
		},

		// Different bases with suffixes
		{
			name:     "different bases with suffixes",
			id1:      "bv-aaa.5",
			id2:      "bv-bbb.1",
			expected: -1,
		},

		// Non-numeric suffixes (alphabetical comparison)
		{
			name:     "non-numeric suffix - alphabetical",
			id1:      "bv-xyz.a",
			id2:      "bv-xyz.b",
			expected: -1,
		},

		// Mixed numeric and non-numeric
		{
			name:     "numeric vs non-numeric suffix",
			id1:      "bv-xyz.1",
			id2:      "bv-xyz.a",
			expected: -1, // "1" < "a" in string comparison
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CompareHierarchicalIDs(tt.id1, tt.id2)
			if result != tt.expected {
				t.Errorf("CompareHierarchicalIDs(%q, %q) = %d, want %d",
					tt.id1, tt.id2, result, tt.expected)
			}
		})
	}
}

package core

import (
	"testing"
)

func TestFilterExcludedDatabases(t *testing.T) {
	tests := []struct {
		name     string
		dbnames  []string
		exclude  []string
		expected []string
	}{
		{
			name:     "no exclusions",
			dbnames:  []string{"db1", "db2", "db3"},
			exclude:  nil,
			expected: []string{"db1", "db2", "db3"},
		},
		{
			name:     "empty exclusions",
			dbnames:  []string{"db1", "db2", "db3"},
			exclude:  []string{},
			expected: []string{"db1", "db2", "db3"},
		},
		{
			name:     "exclude one",
			dbnames:  []string{"db1", "db2", "db3"},
			exclude:  []string{"db2"},
			expected: []string{"db1", "db3"},
		},
		{
			name:     "exclude multiple",
			dbnames:  []string{"db1", "db2", "db3", "db4"},
			exclude:  []string{"db2", "db4"},
			expected: []string{"db1", "db3"},
		},
		{
			name:     "exclude all",
			dbnames:  []string{"db1", "db2"},
			exclude:  []string{"db1", "db2"},
			expected: []string{},
		},
		{
			name:     "exclude nonexistent",
			dbnames:  []string{"db1", "db2"},
			exclude:  []string{"db99"},
			expected: []string{"db1", "db2"},
		},
		{
			name:     "exclude with mixed existing and nonexistent",
			dbnames:  []string{"db1", "db2", "db3"},
			exclude:  []string{"db2", "db99"},
			expected: []string{"db1", "db3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterExcludedDatabases(tt.dbnames, tt.exclude)
			if len(result) != len(tt.expected) {
				t.Fatalf("expected %v, got %v", tt.expected, result)
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Fatalf("expected %v, got %v", tt.expected, result)
				}
			}
		})
	}
}

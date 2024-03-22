package cmd

import (
	"net/url"
	"testing"

	"github.com/databacker/mysql-backup/pkg/core"
	"github.com/databacker/mysql-backup/pkg/storage"
	"github.com/databacker/mysql-backup/pkg/storage/file"
	"github.com/go-test/deep"
	"github.com/stretchr/testify/mock"
)

func TestPruneCmd(t *testing.T) {
	t.Parallel()
	fileTarget := "file:///foo/bar"
	fileTargetURL, _ := url.Parse(fileTarget)

	tests := []struct {
		name                 string
		args                 []string // "dump" will be prepended automatically
		config               string
		wantErr              bool
		expectedPruneOptions core.PruneOptions
		expectedTimerOptions core.TimerOptions
	}{
		{"invalid target URL", []string{"--target", "def"}, "", true, core.PruneOptions{}, core.TimerOptions{}},
		{"file URL", []string{"--target", fileTarget, "--retention", "1h"}, "", false, core.PruneOptions{Targets: []storage.Storage{file.New(*fileTargetURL)}, Retention: "1h"}, core.TimerOptions{Frequency: defaultFrequency, Begin: defaultBegin}},
		{"config file", []string{"--config-file", "testdata/config.yml"}, "", false, core.PruneOptions{Targets: []storage.Storage{file.New(*fileTargetURL)}, Retention: "1h"}, core.TimerOptions{Frequency: defaultFrequency, Begin: defaultBegin}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMockExecs()
			m.On("prune", mock.MatchedBy(func(pruneOpts core.PruneOptions) bool {
				diff := deep.Equal(pruneOpts, tt.expectedPruneOptions)
				if diff == nil {
					return true
				}
				t.Errorf("pruneOpts compare failed: %v", diff)
				return false
			})).Return(nil)
			m.On("timer", tt.expectedTimerOptions).Return(nil)
			cmd, err := rootCmd(m)
			if err != nil {
				t.Fatal(err)
			}
			cmd.SetArgs(append([]string{"prune"}, tt.args...))
			err = cmd.Execute()
			switch {
			case err == nil && tt.wantErr:
				t.Fatal("missing error")
			case err != nil && !tt.wantErr:
				t.Fatal(err)
			case err == nil:
				m.AssertExpectations(t)
			}
		})
	}
}

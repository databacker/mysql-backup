package cmd

import (
	"io"
	"net/url"
	"testing"

	"github.com/databacker/mysql-backup/pkg/compression"
	"github.com/databacker/mysql-backup/pkg/core"
	"github.com/databacker/mysql-backup/pkg/database"
	"github.com/databacker/mysql-backup/pkg/storage/file"
	"github.com/stretchr/testify/mock"
)

func TestRestoreCmd(t *testing.T) {
	t.Parallel()

	fileTarget := "file:///foo/bar"
	fileTargetURL, _ := url.Parse(fileTarget)

	tests := []struct {
		name                   string
		args                   []string // "restore" will be prepended automatically
		config                 string
		wantErr                bool
		expectedRestoreOptions core.RestoreOptions
		//expectedTarget       storage.Storage
		//expectedTargetFile   string
		//expectedDbconn       database.Connection
		//expectedDatabasesMap map[string]string
		//expectedCompressor   compression.Compressor
	}{
		{"missing server and target options", []string{""}, "", true, core.RestoreOptions{}},
		{"invalid target URL", []string{"--server", "abc", "--target", "def"}, "", true, core.RestoreOptions{}},
		{"valid URL missing dump filename", []string{"--server", "abc", "--target", "file:///foo/bar"}, "", true, core.RestoreOptions{}},
		{"valid file URL", []string{"--server", "abc", "--target", fileTarget, "filename.tgz", "--verbose", "2"}, "", false, core.RestoreOptions{Target: file.New(*fileTargetURL), TargetFile: "filename.tgz", DBConn: database.Connection{Host: "abc", Port: defaultPort}, DatabasesMap: map[string]string{}, Compressor: &compression.GzipCompressor{}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMockExecs()
			m.On("Restore", mock.MatchedBy(func(restoreOpts core.RestoreOptions) bool {
				if equalIgnoreFields(restoreOpts, tt.expectedRestoreOptions, []string{"Run"}) {
					return true
				}
				t.Errorf("restoreOpts compare failed: %#v %#v", restoreOpts, tt.expectedRestoreOptions)
				return false
			})).Return(nil)
			cmd, err := rootCmd(m)
			if err != nil {
				t.Fatal(err)
			}
			cmd.SetOutput(io.Discard)
			cmd.SetArgs(append([]string{"restore"}, tt.args...))
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

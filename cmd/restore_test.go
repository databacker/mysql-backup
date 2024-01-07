package cmd

import (
	"net/url"
	"testing"

	"github.com/databacker/mysql-backup/pkg/compression"
	"github.com/databacker/mysql-backup/pkg/database"
	"github.com/databacker/mysql-backup/pkg/storage"
	"github.com/databacker/mysql-backup/pkg/storage/file"
)

func TestRestoreCmd(t *testing.T) {
	t.Parallel()

	fileTarget := "file:///foo/bar"
	fileTargetURL, _ := url.Parse(fileTarget)

	tests := []struct {
		name                 string
		args                 []string // "restore" will be prepended automatically
		config               string
		wantErr              bool
		expectedTarget       storage.Storage
		expectedTargetFile   string
		expectedDbconn       database.Connection
		expectedDatabasesMap map[string]string
		expectedCompressor   compression.Compressor
	}{
		{"missing server and target options", []string{""}, "", true, nil, "", database.Connection{}, nil, &compression.GzipCompressor{}},
		{"invalid target URL", []string{"--server", "abc", "--target", "def"}, "", true, nil, "", database.Connection{Host: "abc"}, nil, &compression.GzipCompressor{}},
		{"valid URL missing dump filename", []string{"--server", "abc", "--target", "file:///foo/bar"}, "", true, nil, "", database.Connection{Host: "abc"}, nil, &compression.GzipCompressor{}},
		{"valid file URL", []string{"--server", "abc", "--target", fileTarget, "filename.tgz"}, "", false, file.New(*fileTargetURL), "filename.tgz", database.Connection{Host: "abc"}, map[string]string{}, &compression.GzipCompressor{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMockExecs()
			m.On("restore", tt.expectedTarget, tt.expectedTargetFile, tt.expectedDbconn, tt.expectedDatabasesMap, tt.expectedCompressor).Return(nil)
			cmd, err := rootCmd(m)
			if err != nil {
				t.Fatal(err)
			}
			cmd.SetArgs(append([]string{"restore"}, tt.args...))
			err = cmd.Execute()
			switch {
			case err == nil && tt.wantErr:
				t.Fatal("missing error")
			case err != nil && !tt.wantErr:
				t.Fatal(err)
			case err == nil:
				m.AssertExpectations(t)
				//m.AssertCalled(t, "restore", tt.expectedTarget, tt.expectedTargetFile, tt.expectedDbconn, tt.expectedDatabasesMap, tt.expectedCompressor)
			}

		})
	}
}

package cmd

import (
	"io"
	"net/url"
	"testing"

	"github.com/databacker/mysql-backup/pkg/compression"
	"github.com/databacker/mysql-backup/pkg/core"
	"github.com/databacker/mysql-backup/pkg/database"
	"github.com/databacker/mysql-backup/pkg/storage"
	"github.com/databacker/mysql-backup/pkg/storage/file"
	"github.com/go-test/deep"
	"github.com/stretchr/testify/mock"
)

func TestDumpCmd(t *testing.T) {
	t.Parallel()

	fileTarget := "file:///foo/bar"
	fileTargetURL, _ := url.Parse(fileTarget)
	tests := []struct {
		name                 string
		args                 []string // "dump" will be prepended automatically
		config               string
		wantErr              bool
		expectedDumpOptions  core.DumpOptions
		expectedTimerOptions core.TimerOptions
		expectedPruneOptions *core.PruneOptions
	}{
		// invalid ones
		{"missing server and target options", []string{""}, "", true, core.DumpOptions{}, core.TimerOptions{}, nil},
		{"invalid target URL", []string{"--server", "abc", "--target", "def"}, "", true, core.DumpOptions{DBConn: database.Connection{Host: "abc"}}, core.TimerOptions{}, nil},

		// file URL
		{"file URL", []string{"--server", "abc", "--target", "file:///foo/bar"}, "", false, core.DumpOptions{
			Targets:          []storage.Storage{file.New(*fileTargetURL)},
			MaxAllowedPacket: defaultMaxAllowedPacket,
			Compressor:       &compression.GzipCompressor{},
			DBConn:           database.Connection{Host: "abc", Port: defaultPort},
			FilenamePattern:  "db_backup_{{ .now }}.{{ .compression }}",
		}, core.TimerOptions{Frequency: defaultFrequency, Begin: defaultBegin}, nil},
		{"file URL with prune", []string{"--server", "abc", "--target", "file:///foo/bar", "--retention", "1h"}, "", false, core.DumpOptions{
			Targets:          []storage.Storage{file.New(*fileTargetURL)},
			MaxAllowedPacket: defaultMaxAllowedPacket,
			Compressor:       &compression.GzipCompressor{},
			DBConn:           database.Connection{Host: "abc", Port: defaultPort},
			FilenamePattern:  "db_backup_{{ .now }}.{{ .compression }}",
		}, core.TimerOptions{Frequency: defaultFrequency, Begin: defaultBegin}, &core.PruneOptions{Targets: []storage.Storage{file.New(*fileTargetURL)}, Retention: "1h"}},

		// database name and port
		{"database explicit name with default port", []string{"--server", "abc", "--target", "file:///foo/bar"}, "", false, core.DumpOptions{
			Targets:          []storage.Storage{file.New(*fileTargetURL)},
			MaxAllowedPacket: defaultMaxAllowedPacket,
			Compressor:       &compression.GzipCompressor{},
			DBConn:           database.Connection{Host: "abc", Port: defaultPort},
			FilenamePattern:  "db_backup_{{ .now }}.{{ .compression }}",
		}, core.TimerOptions{Frequency: defaultFrequency, Begin: defaultBegin}, nil},
		{"database explicit name with explicit port", []string{"--server", "abc", "--port", "3307", "--target", "file:///foo/bar"}, "", false, core.DumpOptions{
			Targets:          []storage.Storage{file.New(*fileTargetURL)},
			MaxAllowedPacket: defaultMaxAllowedPacket,
			Compressor:       &compression.GzipCompressor{},
			DBConn:           database.Connection{Host: "abc", Port: 3307},
			FilenamePattern:  "db_backup_{{ .now }}.{{ .compression }}",
		}, core.TimerOptions{Frequency: defaultFrequency, Begin: defaultBegin}, nil},

		// config file
		{"config file", []string{"--config-file", "testdata/config.yml"}, "", false, core.DumpOptions{
			Targets:          []storage.Storage{file.New(*fileTargetURL)},
			MaxAllowedPacket: defaultMaxAllowedPacket,
			Compressor:       &compression.GzipCompressor{},
			DBConn:           database.Connection{Host: "abcd", Port: 3306, User: "user2", Pass: "xxxx2"},
			FilenamePattern:  "db_backup_{{ .now }}.{{ .compression }}",
		}, core.TimerOptions{Frequency: defaultFrequency, Begin: defaultBegin}, &core.PruneOptions{Targets: []storage.Storage{file.New(*fileTargetURL)}, Retention: "1h"}},
		{"config file with port override", []string{"--config-file", "testdata/config.yml", "--port", "3307"}, "", false, core.DumpOptions{
			Targets:          []storage.Storage{file.New(*fileTargetURL)},
			MaxAllowedPacket: defaultMaxAllowedPacket,
			Compressor:       &compression.GzipCompressor{},
			DBConn:           database.Connection{Host: "abcd", Port: 3307, User: "user2", Pass: "xxxx2"},
			FilenamePattern:  "db_backup_{{ .now }}.{{ .compression }}",
		}, core.TimerOptions{Frequency: defaultFrequency, Begin: defaultBegin}, &core.PruneOptions{Targets: []storage.Storage{file.New(*fileTargetURL)}, Retention: "1h"}},
		{"config file with filename pattern override", []string{"--config-file", "testdata/pattern.yml", "--port", "3307"}, "", false, core.DumpOptions{
			Targets:          []storage.Storage{file.New(*fileTargetURL)},
			MaxAllowedPacket: defaultMaxAllowedPacket,
			Compressor:       &compression.GzipCompressor{},
			DBConn:           database.Connection{Host: "abcd", Port: 3307, User: "user2", Pass: "xxxx2"},
			FilenamePattern:  "foo_{{ .now }}.{{ .compression }}",
		}, core.TimerOptions{Frequency: defaultFrequency, Begin: defaultBegin}, &core.PruneOptions{Targets: []storage.Storage{file.New(*fileTargetURL)}, Retention: "1h"}},

		// timer options
		{"once flag", []string{"--server", "abc", "--target", "file:///foo/bar", "--once"}, "", false, core.DumpOptions{
			Targets:          []storage.Storage{file.New(*fileTargetURL)},
			MaxAllowedPacket: defaultMaxAllowedPacket,
			Compressor:       &compression.GzipCompressor{},
			DBConn:           database.Connection{Host: "abc", Port: defaultPort},
			FilenamePattern:  "db_backup_{{ .now }}.{{ .compression }}",
		}, core.TimerOptions{Once: true, Frequency: defaultFrequency, Begin: defaultBegin}, nil},
		{"cron flag", []string{"--server", "abc", "--target", "file:///foo/bar", "--cron", "0 0 * * *"}, "", false, core.DumpOptions{
			Targets:          []storage.Storage{file.New(*fileTargetURL)},
			MaxAllowedPacket: defaultMaxAllowedPacket,
			Compressor:       &compression.GzipCompressor{},
			DBConn:           database.Connection{Host: "abc", Port: defaultPort},
			FilenamePattern:  "db_backup_{{ .now }}.{{ .compression }}",
		}, core.TimerOptions{Frequency: defaultFrequency, Begin: defaultBegin, Cron: "0 0 * * *"}, nil},
		{"begin flag", []string{"--server", "abc", "--target", "file:///foo/bar", "--begin", "1234"}, "", false, core.DumpOptions{
			Targets:          []storage.Storage{file.New(*fileTargetURL)},
			MaxAllowedPacket: defaultMaxAllowedPacket,
			Compressor:       &compression.GzipCompressor{},
			DBConn:           database.Connection{Host: "abc", Port: defaultPort},
			FilenamePattern:  "db_backup_{{ .now }}.{{ .compression }}",
		}, core.TimerOptions{Frequency: defaultFrequency, Begin: "1234"}, nil},
		{"frequency flag", []string{"--server", "abc", "--target", "file:///foo/bar", "--frequency", "10"}, "", false, core.DumpOptions{
			Targets:          []storage.Storage{file.New(*fileTargetURL)},
			MaxAllowedPacket: defaultMaxAllowedPacket,
			Compressor:       &compression.GzipCompressor{},
			DBConn:           database.Connection{Host: "abc", Port: defaultPort},
			FilenamePattern:  "db_backup_{{ .now }}.{{ .compression }}",
		}, core.TimerOptions{Frequency: 10, Begin: defaultBegin}, nil},
		{"incompatible flags: once/cron", []string{"--server", "abc", "--target", "file:///foo/bar", "--once", "--cron", "0 0 * * *"}, "", true, core.DumpOptions{}, core.TimerOptions{}, nil},
		{"incompatible flags: once/begin", []string{"--server", "abc", "--target", "file:///foo/bar", "--once", "--begin", "1234"}, "", true, core.DumpOptions{}, core.TimerOptions{}, nil},
		{"incompatible flags: once/frequency", []string{"--server", "abc", "--target", "file:///foo/bar", "--once", "--frequency", "10"}, "", true, core.DumpOptions{}, core.TimerOptions{}, nil},
		{"incompatible flags: cron/begin", []string{"--server", "abc", "--target", "file:///foo/bar", "--cron", "0 0 * * *", "--begin", "1234"}, "", true, core.DumpOptions{}, core.TimerOptions{}, nil},
		{"incompatible flags: cron/frequency", []string{"--server", "abc", "--target", "file:///foo/bar", "--cron", "0 0 * * *", "--frequency", "10"}, "", true, core.DumpOptions{
			DBConn: database.Connection{Host: "abcd", Port: 3306, User: "user2", Pass: "xxxx2"},
		}, core.TimerOptions{Frequency: defaultFrequency, Begin: defaultBegin}, &core.PruneOptions{Targets: []storage.Storage{file.New(*fileTargetURL)}, Retention: "1h"}},

		// pre- and post-backup scripts
		{"prebackup scripts", []string{"--server", "abc", "--target", "file:///foo/bar", "--pre-backup-scripts", "/prebackup"}, "", false, core.DumpOptions{
			Targets:          []storage.Storage{file.New(*fileTargetURL)},
			MaxAllowedPacket: defaultMaxAllowedPacket,
			Compressor:       &compression.GzipCompressor{},
			DBConn:           database.Connection{Host: "abc", Port: defaultPort},
			PreBackupScripts: "/prebackup",
			FilenamePattern:  "db_backup_{{ .now }}.{{ .compression }}",
		}, core.TimerOptions{Frequency: defaultFrequency, Begin: defaultBegin}, nil},
		{"postbackup scripts", []string{"--server", "abc", "--target", "file:///foo/bar", "--post-backup-scripts", "/postbackup"}, "", false, core.DumpOptions{
			Targets:           []storage.Storage{file.New(*fileTargetURL)},
			MaxAllowedPacket:  defaultMaxAllowedPacket,
			Compressor:        &compression.GzipCompressor{},
			DBConn:            database.Connection{Host: "abc", Port: defaultPort},
			PostBackupScripts: "/postbackup",
			FilenamePattern:   "db_backup_{{ .now }}.{{ .compression }}",
		}, core.TimerOptions{Frequency: defaultFrequency, Begin: defaultBegin}, nil},
		{"prebackup and postbackup scripts", []string{"--server", "abc", "--target", "file:///foo/bar", "--post-backup-scripts", "/postbackup", "--pre-backup-scripts", "/prebackup"}, "", false, core.DumpOptions{
			Targets:           []storage.Storage{file.New(*fileTargetURL)},
			MaxAllowedPacket:  defaultMaxAllowedPacket,
			Compressor:        &compression.GzipCompressor{},
			DBConn:            database.Connection{Host: "abc", Port: defaultPort},
			PreBackupScripts:  "/prebackup",
			PostBackupScripts: "/postbackup",
			FilenamePattern:   "db_backup_{{ .now }}.{{ .compression }}",
		}, core.TimerOptions{Frequency: defaultFrequency, Begin: defaultBegin}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMockExecs()
			m.On("Dump", mock.MatchedBy(func(dumpOpts core.DumpOptions) bool {
				if equalIgnoreFields(dumpOpts, tt.expectedDumpOptions, []string{"Run"}) {
					return true
				}
				t.Errorf("dumpOpts compare failed: %#v %#v", dumpOpts, tt.expectedDumpOptions)
				return false
			})).Return(nil)
			m.On("Timer", mock.MatchedBy(func(timerOpts core.TimerOptions) bool {
				diff := deep.Equal(timerOpts, tt.expectedTimerOptions)
				if diff == nil {
					return true
				}
				t.Errorf("timerOpts compare failed: %v", diff)
				return false
			})).Return(nil)
			if tt.expectedPruneOptions != nil {
				m.On("Prune", mock.MatchedBy(func(pruneOpts core.PruneOptions) bool {
					if equalIgnoreFields(pruneOpts, *tt.expectedPruneOptions, []string{"Run"}) {
						return true
					}
					t.Errorf("pruneOpts compare failed: %#v %#v", pruneOpts, tt.expectedPruneOptions)
					return false
				})).Return(nil)
			}

			cmd, err := rootCmd(m)
			if err != nil {
				t.Fatal(err)
			}
			cmd.SetOutput(io.Discard)
			cmd.SetArgs(append([]string{"dump"}, tt.args...))
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

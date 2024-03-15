package cmd

import (
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
	}{
		{"missing server and target options", []string{""}, "", true, core.DumpOptions{}, core.TimerOptions{}},
		{"invalid target URL", []string{"--server", "abc", "--target", "def"}, "", true, core.DumpOptions{DBConn: database.Connection{Host: "abc"}}, core.TimerOptions{}},
		{"file URL", []string{"--server", "abc", "--target", "file:///foo/bar"}, "", false, core.DumpOptions{
			Targets:          []storage.Storage{file.New(*fileTargetURL)},
			MaxAllowedPacket: defaultMaxAllowedPacket,
			Compressor:       &compression.GzipCompressor{},
			DBConn:           database.Connection{Host: "abc"},
		}, core.TimerOptions{Frequency: defaultFrequency, Begin: defaultBegin}},
		{"once flag", []string{"--server", "abc", "--target", "file:///foo/bar", "--once"}, "", false, core.DumpOptions{
			Targets:          []storage.Storage{file.New(*fileTargetURL)},
			MaxAllowedPacket: defaultMaxAllowedPacket,
			Compressor:       &compression.GzipCompressor{},
			DBConn:           database.Connection{Host: "abc"},
		}, core.TimerOptions{Frequency: defaultFrequency, Begin: defaultBegin, Once: true}},
		{"cron flag", []string{"--server", "abc", "--target", "file:///foo/bar", "--cron", "0 0 * * *"}, "", false, core.DumpOptions{
			Targets:          []storage.Storage{file.New(*fileTargetURL)},
			MaxAllowedPacket: defaultMaxAllowedPacket,
			Compressor:       &compression.GzipCompressor{},
			DBConn:           database.Connection{Host: "abc"},
		}, core.TimerOptions{Frequency: defaultFrequency, Begin: defaultBegin, Cron: "0 0 * * *"}},
		{"begin flag", []string{"--server", "abc", "--target", "file:///foo/bar", "--begin", "1234"}, "", false, core.DumpOptions{
			Targets:          []storage.Storage{file.New(*fileTargetURL)},
			MaxAllowedPacket: defaultMaxAllowedPacket,
			Compressor:       &compression.GzipCompressor{},
			DBConn:           database.Connection{Host: "abc"},
		}, core.TimerOptions{Frequency: defaultFrequency, Begin: "1234"}},
		{"frequency flag", []string{"--server", "abc", "--target", "file:///foo/bar", "--frequency", "10"}, "", false, core.DumpOptions{
			Targets:          []storage.Storage{file.New(*fileTargetURL)},
			MaxAllowedPacket: defaultMaxAllowedPacket,
			Compressor:       &compression.GzipCompressor{},
			DBConn:           database.Connection{Host: "abc"},
		}, core.TimerOptions{Frequency: 10, Begin: defaultBegin}},
		{"config file", []string{"--config-file", "testdata/config.yml"}, "", false, core.DumpOptions{
			Targets:          []storage.Storage{file.New(*fileTargetURL)},
			MaxAllowedPacket: defaultMaxAllowedPacket,
			Compressor:       &compression.GzipCompressor{},
			DBConn:           database.Connection{Host: "abc", Port: 3306, User: "user", Pass: "xxxx"},
		}, core.TimerOptions{Frequency: defaultFrequency, Begin: defaultBegin}},
		{"incompatible flags: once/cron", []string{"--server", "abc", "--target", "file:///foo/bar", "--once", "--cron", "0 0 * * *"}, "", true, core.DumpOptions{}, core.TimerOptions{}},
		{"incompatible flags: once/begin", []string{"--server", "abc", "--target", "file:///foo/bar", "--once", "--begin", "1234"}, "", true, core.DumpOptions{}, core.TimerOptions{}},
		{"incompatible flags: once/frequency", []string{"--server", "abc", "--target", "file:///foo/bar", "--once", "--frequency", "10"}, "", true, core.DumpOptions{}, core.TimerOptions{}},
		{"incompatible flags: cron/begin", []string{"--server", "abc", "--target", "file:///foo/bar", "--cron", "0 0 * * *", "--begin", "1234"}, "", true, core.DumpOptions{}, core.TimerOptions{}},
		{"incompatible flags: cron/frequency", []string{"--server", "abc", "--target", "file:///foo/bar", "--cron", "0 0 * * *", "--frequency", "10"}, "", true, core.DumpOptions{}, core.TimerOptions{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMockExecs()
			m.On("timerDump", mock.MatchedBy(func(dumpOpts core.DumpOptions) bool {
				diff := deep.Equal(dumpOpts, tt.expectedDumpOptions)
				if diff == nil {
					return true
				}
				t.Errorf("dumpOpts compare failed: %v", diff)
				return false
			}), mock.MatchedBy(func(timerOpts core.TimerOptions) bool {
				diff := deep.Equal(timerOpts, tt.expectedTimerOptions)
				if diff == nil {
					return true
				}
				t.Errorf("timerOpts compare failed: %v", diff)
				return false
			})).Return(nil)

			cmd, err := rootCmd(m)
			if err != nil {
				t.Fatal(err)
			}
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

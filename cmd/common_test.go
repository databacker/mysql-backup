package cmd

import (
	"github.com/databacker/mysql-backup/pkg/compression"
	"github.com/databacker/mysql-backup/pkg/core"
	"github.com/databacker/mysql-backup/pkg/database"
	"github.com/databacker/mysql-backup/pkg/storage"
	"github.com/stretchr/testify/mock"
)

type mockExecs struct {
	mock.Mock
}

func newMockExecs() *mockExecs {
	m := &mockExecs{}
	return m
}

func (m *mockExecs) dump(opts core.DumpOptions) error {
	args := m.Called(opts)
	return args.Error(0)
}

func (m *mockExecs) restore(target storage.Storage, targetFile string, dbconn database.Connection, databasesMap map[string]string, compressor compression.Compressor) error {
	args := m.Called(target, targetFile, dbconn, databasesMap, compressor)
	return args.Error(0)
}

func (m *mockExecs) prune(opts core.PruneOptions) error {
	args := m.Called(opts)
	return args.Error(0)
}
func (m *mockExecs) timer(timerOpts core.TimerOptions, cmd func() error) error {
	args := m.Called(timerOpts)
	err := args.Error(0)
	if err != nil {
		return err
	}
	return cmd()
}

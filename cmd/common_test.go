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

func (m *mockExecs) timerDump(opts core.DumpOptions, timerOpts core.TimerOptions) error {
	args := m.Called(opts, timerOpts)
	return args.Error(0)
}

func (m *mockExecs) restore(target storage.Storage, targetFile string, dbconn database.Connection, databasesMap map[string]string, compressor compression.Compressor) error {
	args := m.Called(target, targetFile, dbconn, databasesMap, compressor)
	return args.Error(0)
}

package cmd

import (
	"context"
	"reflect"

	"github.com/databacker/mysql-backup/pkg/core"
	"github.com/go-test/deep"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/mock"
)

type mockExecs struct {
	mock.Mock
	logger *log.Logger
}

func newMockExecs() *mockExecs {
	m := &mockExecs{}
	return m
}

func (m *mockExecs) Dump(ctx context.Context, opts core.DumpOptions) (core.DumpResults, error) {
	args := m.Called(opts)
	return core.DumpResults{}, args.Error(0)
}

func (m *mockExecs) Restore(ctx context.Context, opts core.RestoreOptions) error {
	args := m.Called(opts)
	return args.Error(0)
}

func (m *mockExecs) Prune(ctx context.Context, opts core.PruneOptions) error {
	args := m.Called(opts)
	return args.Error(0)
}
func (m *mockExecs) Timer(timerOpts core.TimerOptions, cmd func() error) error {
	args := m.Called(timerOpts)
	err := args.Error(0)
	if err != nil {
		return err
	}
	return cmd()
}

func (m *mockExecs) SetLogger(logger *log.Logger) {
	m.logger = logger
}

func (m *mockExecs) GetLogger() *log.Logger {
	return m.logger
}

func equalIgnoreFields(a, b interface{}, fields []string) bool {
	va := reflect.ValueOf(a)
	vb := reflect.ValueOf(b)

	// Check if both a and b are struct types
	if va.Kind() != reflect.Struct || vb.Kind() != reflect.Struct {
		return false
	}

	// Make a map of fields to ignore for quick lookup
	ignoreMap := make(map[string]bool)
	for _, f := range fields {
		ignoreMap[f] = true
	}

	// Compare fields that are not in the ignore list
	for i := 0; i < va.NumField(); i++ {
		field := va.Type().Field(i).Name
		if !ignoreMap[field] {
			vaField := va.Field(i).Interface()
			vbField := vb.Field(i).Interface()
			diff := deep.Equal(vaField, vbField)
			if diff != nil {
				return false
			}
		}
	}

	return true
}

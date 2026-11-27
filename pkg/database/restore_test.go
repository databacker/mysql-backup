package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// recordingDriver records every statement executed through it.
type recordingDriver struct{ statements *[]string }

func (d recordingDriver) Open(string) (driver.Conn, error) { return recordingConn(d), nil }

type recordingConn recordingDriver

func (c recordingConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (c recordingConn) Close() error                        { return nil }
func (c recordingConn) Begin() (driver.Tx, error)           { return c, nil }
func (c recordingConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c, nil
}
func (c recordingConn) Commit() error   { return nil }
func (c recordingConn) Rollback() error { return nil }
func (c recordingConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	*c.statements = append(*c.statements, query)
	return driver.RowsAffected(0), nil
}

func TestRestoreRenamesDatabaseAfterHeaderComments(t *testing.T) {
	var statements []string
	sql.Register("recording-restore", recordingDriver{statements: &statements})
	db, err := sql.Open("recording-restore", "")
	require.NoError(t, err)

	dump := "--\n-- Current Database: `shop`\n--\n\nCREATE DATABASE /*!32312 IF NOT EXISTS*/ `shop`;\n\nUSE `shop`;\n"
	err = Restore(context.Background(), &Connection{sql: db}, map[string]string{"shop": "shop2"},
		[]io.ReadSeeker{strings.NewReader(dump)})

	require.NoError(t, err)
	require.Equal(t, []string{"CREATE DATABASE /*!32312 IF NOT EXISTS*/ `shop2`;", "USE `shop2`;"}, statements)
}

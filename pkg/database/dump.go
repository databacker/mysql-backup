package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/databacker/mysql-backup/pkg/database/mysql"
)

type DumpOpts struct {
	Compact             bool
	Triggers            bool
	Routines            bool
	SuppressUseDatabase bool
	MaxAllowedPacket    int
	// PostDumpDelay after each dump is complete, while holding connection open. Do not use outside of tests.
	PostDumpDelay time.Duration
}

func Dump(ctx context.Context, dbconn Connection, opts DumpOpts, writers []DumpWriter) error {

	// TODO: dump data for each writer:
	// per schema
	//    mysqldump --databases ${onedb} $MYSQLDUMP_OPTS
	// all at once
	//    mysqldump -A $MYSQLDUMP_OPTS
	// all at once limited to some databases
	//    mysqldump --databases $DB_DUMP_INCLUDE $MYSQLDUMP_OPTS
	for _, writer := range writers {
		db, err := sql.Open("mysql", dbconn.MySQL())
		if err != nil {
			return fmt.Errorf("failed to open connection to database: %v", err)
		}
		defer func() { _ = db.Close() }()
		for _, schema := range writer.Schemas {
			dumper := &mysql.Data{
				Out:                 writer.Writer,
				Connection:          db,
				Schema:              schema,
				Host:                dbconn.Host,
				Compact:             opts.Compact,
				Triggers:            opts.Triggers,
				Routines:            opts.Routines,
				SuppressUseDatabase: opts.SuppressUseDatabase,
				MaxAllowedPacket:    opts.MaxAllowedPacket,
				PostDumpDelay:       opts.PostDumpDelay,
			}
			if err := dumper.Dump(); err != nil {
				return fmt.Errorf("failed to dump database %s: %v", schema, err)
			}
		}
	}

	return nil
}

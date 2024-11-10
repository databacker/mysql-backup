package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/databacker/mysql-backup/pkg/database/mysql"
)

type DumpOpts struct {
	Compact             bool
	SuppressUseDatabase bool
	MaxAllowedPacket    int
}

func Dump(ctx context.Context, dbconn Connection, opts DumpOpts, writers []DumpWriter) error {

	// TODO: dump data for each writer:
	// per schema
	//    mysqldump --databases ${onedb} $MYSQLDUMP_OPTS
	// all at once
	//    mysqldump -A $MYSQLDUMP_OPTS
	// all at once limited to some databases
	//    mysqldump --databases $DB_NAMES $MYSQLDUMP_OPTS
	for _, writer := range writers {
		db, err := sql.Open("mysql", dbconn.MySQL())
		if err != nil {
			return fmt.Errorf("failed to open connection to database: %v", err)
		}
		defer db.Close()
		for _, schema := range writer.Schemas {
			dumper := &mysql.Data{
				Out:                 writer.Writer,
				Connection:          db,
				Schema:              schema,
				Host:                dbconn.Host,
				Compact:             opts.Compact,
				SuppressUseDatabase: opts.SuppressUseDatabase,
				MaxAllowedPacket:    opts.MaxAllowedPacket,
			}
			if err := dumper.Dump(); err != nil {
				return fmt.Errorf("failed to dump database %s: %v", schema, err)
			}
		}
	}

	return nil
}

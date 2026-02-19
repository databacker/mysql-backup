package database

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/databacker/mysql-backup/pkg/database/mysql"
)

type DumpOpts struct {
	Compact                 bool
	Triggers                bool
	Routines                bool
	SuppressUseDatabase     bool
	SkipExtendedInsert      bool
	MaxAllowedPacket        int
	IncludeGeneratedColumns bool
	// PostDumpDelay after each dump is complete, while holding connection open. Do not use outside of tests.
	PostDumpDelay time.Duration
	Parallelism   int
}

func Dump(ctx context.Context, dbconn *Connection, opts DumpOpts, writers []DumpWriter) error {

	// TODO: dump data for each writer:
	// per schema
	//    mysqldump --databases ${onedb} $MYSQLDUMP_OPTS
	// all at once
	//    mysqldump -A $MYSQLDUMP_OPTS
	// all at once limited to some databases
	//    mysqldump --databases $DB_DUMP_INCLUDE $MYSQLDUMP_OPTS
	db, err := dbconn.MySQL()
	if err != nil {
		return fmt.Errorf("failed to open connection to database: %v", err)
	}

	// limit to opts.Parallelism connections
	// if none is provided, default to 1, i.e. serial
	parallelism := opts.Parallelism
	if parallelism == 0 {
		parallelism = 1
	}
	sem := make(chan struct{}, parallelism)
	errCh := make(chan error, len(writers))
	var wg sync.WaitGroup
	for _, writer := range writers {
		sem <- struct{}{} // acquire a slot
		wg.Add(1)
		go func(writer DumpWriter) {
			defer wg.Done()
			defer func() { <-sem }()
			for _, schema := range writer.Schemas {
				dumper := &mysql.Data{
					Out:                     writer.Writer,
					Connection:              db,
					Schema:                  schema,
					Host:                    dbconn.Host,
					Compact:                 opts.Compact,
					Triggers:                opts.Triggers,
					Routines:                opts.Routines,
					SuppressUseDatabase:     opts.SuppressUseDatabase,
					SkipExtendedInsert:      opts.SkipExtendedInsert,
					MaxAllowedPacket:        opts.MaxAllowedPacket,
					PostDumpDelay:           opts.PostDumpDelay,
					IncludeGeneratedColumns: opts.IncludeGeneratedColumns,
				}
				// return on any error
				if err := dumper.Dump(); err != nil {
					errCh <- fmt.Errorf("failed to dump database %s: %v", schema, err)
					return
				}
			}
		}(writer)
	}
	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("one or more errors occurred: %v", errs)
	}
	return nil
}

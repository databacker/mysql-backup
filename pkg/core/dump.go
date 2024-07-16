package core

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/databacker/mysql-backup/pkg/archive"
	"github.com/databacker/mysql-backup/pkg/database"
)

// Dump run a single dump, based on the provided opts
func (e *Executor) Dump(opts DumpOptions) error {
	targets := opts.Targets
	safechars := opts.Safechars
	dbnames := opts.DBNames
	dbconn := opts.DBConn
	compressor := opts.Compressor
	compact := opts.Compact
	suppressUseDatabase := opts.SuppressUseDatabase
	maxAllowedPacket := opts.MaxAllowedPacket
	logger := e.Logger.WithField("run", opts.Run.String())

	now := time.Now()
	timepart := now.Format(time.RFC3339)
	logger.Infof("beginning dump %s", timepart)
	if safechars {
		timepart = strings.ReplaceAll(timepart, ":", "-")
	}

	// sourceFilename: file that the uploader looks for when performing the upload
	// targetFilename: the remote file that is actually uploaded
	sourceFilename := fmt.Sprintf("db_backup_%s.%s", timepart, compressor.Extension())
	targetFilename := sourceFilename

	// create a temporary working directory
	tmpdir, err := os.MkdirTemp("", "databacker_backup")
	if err != nil {
		return fmt.Errorf("failed to make temporary working directory: %v", err)
	}
	defer os.RemoveAll(tmpdir)
	// execute pre-backup scripts if any
	if err := preBackup(timepart, path.Join(tmpdir, targetFilename), tmpdir, opts.PreBackupScripts, logger.Level == log.DebugLevel); err != nil {
		return fmt.Errorf("error running pre-restore: %v", err)
	}

	// do the dump(s)
	workdir, err := os.MkdirTemp("", "databacker_cache")
	if err != nil {
		return fmt.Errorf("failed to make temporary cache directory: %v", err)
	}
	defer os.RemoveAll(workdir)

	dw := make([]database.DumpWriter, 0)

	// do we split the output by schema, or one big dump file?
	if len(dbnames) == 0 {
		if dbnames, err = database.GetSchemas(dbconn); err != nil {
			return fmt.Errorf("failed to list database schemas: %v", err)
		}
	}
	for _, s := range dbnames {
		outFile := path.Join(workdir, fmt.Sprintf("%s_%s.sql", s, timepart))
		f, err := os.Create(outFile)
		if err != nil {
			return fmt.Errorf("failed to create dump file '%s': %v", outFile, err)
		}
		dw = append(dw, database.DumpWriter{
			Schemas: []string{s},
			Writer:  f,
		})
	}
	if err := database.Dump(dbconn, database.DumpOpts{
		Compact:             compact,
		SuppressUseDatabase: suppressUseDatabase,
		MaxAllowedPacket:    maxAllowedPacket,
	}, dw); err != nil {
		return fmt.Errorf("failed to dump database: %v", err)
	}

	// create my tar writer to archive it all together
	// WRONG: THIS WILL CAUSE IT TO TRY TO LOOP BACK ON ITSELF
	outFile := path.Join(tmpdir, sourceFilename)
	f, err := os.OpenFile(outFile, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open output file '%s': %v", outFile, err)
	}
	defer f.Close()
	cw, err := compressor.Compress(f)
	if err != nil {
		return fmt.Errorf("failed to create compressor: %v", err)
	}
	if err := archive.Tar(workdir, cw); err != nil {
		return fmt.Errorf("error creating the compressed archive: %v", err)
	}
	// we need to close it explicitly before moving ahead
	f.Close()

	// execute post-backup scripts if any
	if err := postBackup(timepart, path.Join(tmpdir, targetFilename), tmpdir, opts.PostBackupScripts, logger.Level == log.DebugLevel); err != nil {
		return fmt.Errorf("error running pre-restore: %v", err)
	}

	// upload to each destination
	for _, t := range targets {
		logger.Debugf("uploading via protocol %s from %s", t.Protocol(), targetFilename)
		copied, err := t.Push(targetFilename, filepath.Join(tmpdir, sourceFilename), logger)
		if err != nil {
			return fmt.Errorf("failed to push file: %v", err)
		}
		logger.Debugf("completed copying %d bytes", copied)
	}

	return nil
}

// run pre-backup scripts, if they exist
func preBackup(timestamp, dumpfile, dumpdir, preBackupDir string, debug bool) error {
	// construct any additional environment
	env := map[string]string{
		"NOW":           timestamp,
		"DUMPFILE":      dumpfile,
		"DUMPDIR":       dumpdir,
		"DB_DUMP_DEBUG": fmt.Sprintf("%v", debug),
	}
	return runScripts(preBackupDir, env)
}

func postBackup(timestamp, dumpfile, dumpdir, postBackupDir string, debug bool) error {
	// construct any additional environment
	env := map[string]string{
		"NOW":           timestamp,
		"DUMPFILE":      dumpfile,
		"DUMPDIR":       dumpdir,
		"DB_DUMP_DEBUG": fmt.Sprintf("%v", debug),
	}
	return runScripts(postBackupDir, env)
}

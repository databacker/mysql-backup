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

const (
	sourceRenameCmd = "/scripts.d/source.sh"
	targetRenameCmd = "/scripts.d/target.sh"
)

// TimerDump runs a dump on a timer
func TimerDump(opts DumpOptions, timerOpts TimerOptions) error {
	c, err := Timer(timerOpts)
	if err != nil {
		log.Errorf("error creating timer: %v", err)
		os.Exit(1)
	}
	// block and wait for it
	for update := range c {
		if err := Dump(opts); err != nil {
			return fmt.Errorf("error backing up: %w", err)
		}
		if update.Last {
			break
		}
	}
	return nil
}

// Dump run a single dump, based on the provided opts
func Dump(opts DumpOptions) error {
	targets := opts.Targets
	safechars := opts.Safechars
	dbnames := opts.DBNames
	dbconn := opts.DBConn
	compressor := opts.Compressor
	compact := opts.Compact
	suppressUseDatabase := opts.SuppressUseDatabase
	maxAllowedPacket := opts.MaxAllowedPacket

	now := time.Now()
	timepart := now.Format(time.RFC3339)
	log.Infof("beginning dump %s", timepart)
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
	if err := preBackup(timepart, path.Join(tmpdir, targetFilename), tmpdir, opts.PreBackupScripts, log.GetLevel() == log.DebugLevel); err != nil {
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
	cw := compressor.Compress(f)
	if err := archive.Tar(workdir, cw); err != nil {
		return fmt.Errorf("error creating the compressed archive: %v", err)
	}
	// we need to close it explicitly before moving ahead
	f.Close()

	// execute post-backup scripts if any
	if err := postBackup(timepart, path.Join(tmpdir, targetFilename), tmpdir, opts.PostBackupScripts, log.GetLevel() == log.DebugLevel); err != nil {
		return fmt.Errorf("error running pre-restore: %v", err)
	}

	// perform any renaming
	newName, err := renameSource(timepart, path.Join(tmpdir, targetFilename), tmpdir, log.GetLevel() == log.DebugLevel)
	if err != nil {
		return fmt.Errorf("failed rename source: %v", err)
	}
	if newName != "" {
		sourceFilename = newName
	}

	// perform any renaming
	newName, err = renameTarget(timepart, path.Join(tmpdir, targetFilename), tmpdir, log.GetLevel() == log.DebugLevel)
	if err != nil {
		return fmt.Errorf("failed rename target: %v", err)
	}
	if newName != "" {
		targetFilename = newName
	}

	// upload to each destination
	for _, t := range targets {
		log.Debugf("uploading via protocol %s from %s", t.Protocol(), targetFilename)
		copied, err := t.Push(targetFilename, filepath.Join(tmpdir, sourceFilename))
		if err != nil {
			return fmt.Errorf("failed to push file: %v", err)
		}
		log.Debugf("completed copying %d bytes", copied)
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

func renameSource(timestamp, dumpfile, dumpdir string, debug bool) (string, error) {
	_, err := os.Stat(sourceRenameCmd)
	if err != nil && os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("error reading rename scrpt %s: %v", sourceRenameCmd, err)
	}
	env := map[string]string{
		"NOW":           timestamp,
		"DUMPFILE":      path.Join(dumpdir, dumpfile),
		"DUMPDIR":       dumpdir,
		"DB_DUMP_DEBUG": fmt.Sprintf("%v", debug),
	}

	// it exists so try to run it
	results, err := oneScript(sourceRenameCmd, env)
	if err != nil {
		return "", fmt.Errorf("error executing rename script %s: %v", sourceRenameCmd, err)
	}
	results = trimBadChars(results)
	newName := strings.TrimSpace(string(results))

	return newName, nil
}

func renameTarget(timestamp, dumpfile, dumpdir string, debug bool) (string, error) {
	_, err := os.Stat(targetRenameCmd)
	if err != nil && os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("error reading rename script %s: %v", targetRenameCmd, err)
	}
	env := map[string]string{
		"NOW":           timestamp,
		"DUMPFILE":      path.Join(dumpdir, dumpfile),
		"DUMPDIR":       dumpdir,
		"DB_DUMP_DEBUG": fmt.Sprintf("%v", debug),
	}

	// it exists so try to run it
	results, err := oneScript(targetRenameCmd, env)
	if err != nil {
		return "", fmt.Errorf("error executing rename script %s: %v", targetRenameCmd, err)
	}
	results = trimBadChars(results)
	newName := strings.TrimSpace(string(results))

	return newName, nil
}

// trimBadChars eliminate these characters '\040\011\012\015'
func trimBadChars(b []byte) []byte {
	out := make([]byte, 0)
	for _, c := range b {
		if c != 040 && c != 011 && c != 012 && c != 015 {
			out = append(out, c)
		}
	}
	return out
}

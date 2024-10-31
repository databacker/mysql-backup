package core

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/databacker/mysql-backup/pkg/archive"
	"github.com/databacker/mysql-backup/pkg/database"
)

// Dump run a single dump, based on the provided opts
func (e *Executor) Dump(opts DumpOptions) (DumpResults, error) {
	results := DumpResults{Start: time.Now()}
	defer func() { results.End = time.Now() }()

	targets := opts.Targets
	safechars := opts.Safechars
	dbnames := opts.DBNames
	dbconn := opts.DBConn
	compressor := opts.Compressor
	compact := opts.Compact
	suppressUseDatabase := opts.SuppressUseDatabase
	maxAllowedPacket := opts.MaxAllowedPacket
	filenamePattern := opts.FilenamePattern
	logger := e.Logger.WithField("run", opts.Run.String())
	logger.Level = e.Logger.Level

	now := time.Now()
	results.Time = now

	timepart := now.Format(time.RFC3339)
	logger.Infof("beginning dump %s", timepart)
	if safechars {
		timepart = strings.ReplaceAll(timepart, ":", "-")
	}
	results.Timestamp = timepart

	// sourceFilename: file that the uploader looks for when performing the upload
	// targetFilename: the remote file that is actually uploaded
	sourceFilename := fmt.Sprintf("db_backup_%s.%s", timepart, compressor.Extension())
	targetFilename, err := ProcessFilenamePattern(filenamePattern, now, timepart, compressor.Extension())
	if err != nil {
		return results, fmt.Errorf("failed to process filename pattern: %v", err)
	}

	// create a temporary working directory
	tmpdir, err := os.MkdirTemp("", "databacker_backup")
	if err != nil {
		return results, fmt.Errorf("failed to make temporary working directory: %v", err)
	}
	defer os.RemoveAll(tmpdir)
	// execute pre-backup scripts if any
	if err := preBackup(timepart, path.Join(tmpdir, sourceFilename), tmpdir, opts.PreBackupScripts, logger.Level == log.DebugLevel); err != nil {
		return results, fmt.Errorf("error running pre-restore: %v", err)
	}

	// do the dump(s)
	workdir, err := os.MkdirTemp("", "databacker_cache")
	if err != nil {
		return results, fmt.Errorf("failed to make temporary cache directory: %v", err)
	}
	defer os.RemoveAll(workdir)

	dw := make([]database.DumpWriter, 0)

	// do we split the output by schema, or one big dump file?
	if len(dbnames) == 0 {
		if dbnames, err = database.GetSchemas(dbconn); err != nil {
			return results, fmt.Errorf("failed to list database schemas: %v", err)
		}
	}
	for _, s := range dbnames {
		outFile := path.Join(workdir, fmt.Sprintf("%s_%s.sql", s, timepart))
		f, err := os.Create(outFile)
		if err != nil {
			return results, fmt.Errorf("failed to create dump file '%s': %v", outFile, err)
		}
		dw = append(dw, database.DumpWriter{
			Schemas: []string{s},
			Writer:  f,
		})
	}
	results.DumpStart = time.Now()
	if err := database.Dump(dbconn, database.DumpOpts{
		Compact:             compact,
		SuppressUseDatabase: suppressUseDatabase,
		MaxAllowedPacket:    maxAllowedPacket,
	}, dw); err != nil {
		return results, fmt.Errorf("failed to dump database: %v", err)
	}
	results.DumpEnd = time.Now()

	// create my tar writer to archive it all together
	// WRONG: THIS WILL CAUSE IT TO TRY TO LOOP BACK ON ITSELF
	outFile := path.Join(tmpdir, sourceFilename)
	f, err := os.OpenFile(outFile, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return results, fmt.Errorf("failed to open output file '%s': %v", outFile, err)
	}
	defer f.Close()
	cw, err := compressor.Compress(f)
	if err != nil {
		return results, fmt.Errorf("failed to create compressor: %v", err)
	}
	if err := archive.Tar(workdir, cw); err != nil {
		return results, fmt.Errorf("error creating the compressed archive: %v", err)
	}
	// we need to close it explicitly before moving ahead
	f.Close()

	// execute post-backup scripts if any
	if err := postBackup(timepart, path.Join(tmpdir, sourceFilename), tmpdir, opts.PostBackupScripts, logger.Level == log.DebugLevel); err != nil {
		return results, fmt.Errorf("error running pre-restore: %v", err)
	}

	// upload to each destination
	for _, t := range targets {
		uploadResult := UploadResult{Target: t.URL(), Start: time.Now()}
		targetCleanFilename := t.Clean(targetFilename)
		logger.Debugf("uploading via protocol %s from %s to %s", t.Protocol(), sourceFilename, targetCleanFilename)
		copied, err := t.Push(targetCleanFilename, filepath.Join(tmpdir, sourceFilename), logger)
		if err != nil {
			return results, fmt.Errorf("failed to push file: %v", err)
		}
		logger.Debugf("completed copying %d bytes", copied)
		uploadResult.Filename = targetCleanFilename
		uploadResult.End = time.Now()
		results.Uploads = append(results.Uploads, uploadResult)
	}

	return results, nil
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

// ProcessFilenamePattern takes a template pattern and processes it with the current time.
// Passes the timestamp as a string, because it sometimes gets changed for safechars.
func ProcessFilenamePattern(pattern string, now time.Time, timestamp, ext string) (string, error) {
	if pattern == "" {
		pattern = DefaultFilenamePattern
	}
	tmpl, err := template.New("filename").Parse(pattern)
	if err != nil {
		return "", fmt.Errorf("failed to parse filename pattern: %v", err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, map[string]string{
		"now":         timestamp,
		"year":        now.Format("2006"),
		"month":       now.Format("01"),
		"day":         now.Format("02"),
		"hour":        now.Format("15"),
		"minute":      now.Format("04"),
		"second":      now.Format("05"),
		"compression": ext,
	}); err != nil {
		return "", fmt.Errorf("failed to execute filename pattern: %v", err)
	}
	return buf.String(), nil
}

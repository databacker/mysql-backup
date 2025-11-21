package core

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/databacker/mysql-backup/pkg/archive"
	"github.com/databacker/mysql-backup/pkg/database"
	"github.com/databacker/mysql-backup/pkg/util"
)

// Dump run a single dump, based on the provided opts
func (e *Executor) Dump(ctx context.Context, opts DumpOptions) (DumpResults, error) {
	results := DumpResults{Start: time.Now()}
	tracer := util.GetTracerFromContext(ctx)
	ctx, span := tracer.Start(ctx, "dump")
	defer func() {
		results.End = time.Now()
		span.End()
	}()

	targets := opts.Targets
	safechars := opts.Safechars
	dbnames := opts.DBNames
	dbconn := opts.DBConn
	compressor := opts.Compressor
	encryptor := opts.Encryptor
	compact := opts.Compact
	triggers := opts.Triggers
	routines := opts.Routines
	suppressUseDatabase := opts.SuppressUseDatabase
	skipExtendedInsert := opts.SkipExtendedInsert
	maxAllowedPacket := opts.MaxAllowedPacket
	filenamePattern := opts.FilenamePattern
	parallelism := opts.Parallelism
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
	span.SetAttributes(attribute.String("timestamp", timepart))

	// sourceFilename: file that the uploader looks for when performing the upload
	// targetFilename: the remote file that is actually uploaded
	sourceFilename := fmt.Sprintf("db_backup_%s.%s", timepart, compressor.Extension())
	targetFilename, err := ProcessFilenamePattern(filenamePattern, now, timepart, compressor.Extension())
	if err != nil {
		return results, fmt.Errorf("failed to process filename pattern: %v", err)
	}
	span.SetAttributes(attribute.String("source-filename", sourceFilename), attribute.String("target-filename", targetFilename))

	// create a temporary working directory
	tmpdir, err := os.MkdirTemp("", "databacker_backup")
	if err != nil {
		return results, fmt.Errorf("failed to make temporary working directory: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpdir) }()
	// execute pre-backup scripts if any
	if err := preBackup(ctx, timepart, path.Join(tmpdir, sourceFilename), tmpdir, opts.PreBackupScripts, logger.Level == log.DebugLevel); err != nil {
		return results, fmt.Errorf("error running pre-restore: %v", err)
	}

	// do the dump(s)
	workdir, err := os.MkdirTemp("", "databacker_cache")
	if err != nil {
		return results, fmt.Errorf("failed to make temporary cache directory: %v", err)
	}
	defer func() { _ = os.RemoveAll(workdir) }()

	dw := make([]database.DumpWriter, 0)

	// do we back up all schemas, or just provided ones
	span.SetAttributes(attribute.Bool("provided-schemas", len(dbnames) != 0))
	if len(dbnames) == 0 {
		if dbnames, err = database.GetSchemas(dbconn); err != nil {
			return results, fmt.Errorf("failed to list database schemas: %v", err)
		}
	}
	span.SetAttributes(attribute.StringSlice("actual-schemas", dbnames))
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
	dbDumpCtx, dbDumpSpan := tracer.Start(ctx, "database_dump")
	if err := database.Dump(dbDumpCtx, dbconn, database.DumpOpts{
		Compact:                 compact,
		Triggers:                triggers,
		Routines:                routines,
		SuppressUseDatabase:     suppressUseDatabase,
		SkipExtendedInsert:      skipExtendedInsert,
		MaxAllowedPacket:        maxAllowedPacket,
		IncludeGeneratedColumns: opts.IncludeGeneratedColumns,
		PostDumpDelay:           opts.PostDumpDelay,
		Parallelism:             parallelism,
	}, dw); err != nil {
		dbDumpSpan.SetStatus(codes.Error, err.Error())
		dbDumpSpan.End()
		return results, fmt.Errorf("failed to dump database: %v", err)
	}
	results.DumpEnd = time.Now()
	dbDumpSpan.SetStatus(codes.Ok, "completed")
	dbDumpSpan.End()

	// create my tar writer to archive it all together
	// WRONG: THIS WILL CAUSE IT TO TRY TO LOOP BACK ON ITSELF
	_, tarSpan := tracer.Start(ctx, "output_tar")
	outFile := path.Join(tmpdir, sourceFilename)
	f, err := os.OpenFile(outFile, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		tarSpan.SetStatus(codes.Error, err.Error())
		tarSpan.End()
		return results, fmt.Errorf("failed to open output file '%s': %v", outFile, err)
	}
	defer func() { _ = f.Close() }()
	cw, err := compressor.Compress(f)
	if err != nil {
		tarSpan.SetStatus(codes.Error, err.Error())
		tarSpan.End()
		return results, fmt.Errorf("failed to create compressor: %v", err)
	}
	if encryptor != nil {
		cw, err = encryptor.Encrypt(cw)
		if err != nil {
			tarSpan.SetStatus(codes.Error, err.Error())
			tarSpan.End()
			return results, fmt.Errorf("failed to create encryptor: %v", err)
		}
	}
	if err := archive.Tar(workdir, cw); err != nil {
		tarSpan.SetStatus(codes.Error, err.Error())
		tarSpan.End()
		return results, fmt.Errorf("error creating the compressed archive: %v", err)
	}
	// we need to close it explicitly before moving ahead
	defer func() { _ = f.Close() }()
	tarSpan.SetStatus(codes.Ok, "completed")
	tarSpan.End()

	// execute post-backup scripts if any
	if err := postBackup(ctx, timepart, path.Join(tmpdir, sourceFilename), tmpdir, opts.PostBackupScripts, logger.Level == log.DebugLevel); err != nil {
		return results, fmt.Errorf("error running pre-restore: %v", err)
	}

	// upload to each destination
	uploadCtx, uploadSpan := tracer.Start(ctx, "upload")
	for _, t := range targets {
		uploadResult := UploadResult{Target: t.URL(), Start: time.Now()}
		targetCleanFilename := t.Clean(targetFilename)
		logger.Debugf("uploading via protocol %s from %s to %s", t.Protocol(), sourceFilename, targetCleanFilename)
		copied, err := t.Push(uploadCtx, targetCleanFilename, filepath.Join(tmpdir, sourceFilename), logger)
		if err != nil {
			uploadSpan.SetStatus(codes.Error, err.Error())
			uploadSpan.End()
			return results, fmt.Errorf("failed to push file: %v", err)
		}
		logger.Debugf("completed copying %d bytes", copied)
		uploadResult.Filename = targetCleanFilename
		uploadResult.End = time.Now()
		results.Uploads = append(results.Uploads, uploadResult)
	}
	uploadSpan.SetStatus(codes.Ok, "completed")
	uploadSpan.End()

	logger.Infof("finished dump %s", now.Format(time.RFC3339))

	return results, nil
}

// run pre-backup scripts, if they exist
func preBackup(ctx context.Context, timestamp, dumpfile, dumpdir, preBackupDir string, debug bool) error {
	// construct any additional environment
	env := map[string]string{
		"NOW":      timestamp,
		"DUMPFILE": dumpfile,
		"DUMPDIR":  dumpdir,
		"DB_DEBUG": fmt.Sprintf("%v", debug),
	}
	ctx, span := util.GetTracerFromContext(ctx).Start(ctx, "pre-backup")
	defer span.End()
	return runScripts(ctx, preBackupDir, env)
}

func postBackup(ctx context.Context, timestamp, dumpfile, dumpdir, postBackupDir string, debug bool) error {
	// construct any additional environment
	env := map[string]string{
		"NOW":      timestamp,
		"DUMPFILE": dumpfile,
		"DUMPDIR":  dumpdir,
		"DB_DEBUG": fmt.Sprintf("%v", debug),
	}
	ctx, span := util.GetTracerFromContext(ctx).Start(ctx, "post-backup")
	defer span.End()
	return runScripts(ctx, postBackupDir, env)
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

package core

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/databacker/mysql-backup/pkg/archive"
	"github.com/databacker/mysql-backup/pkg/database"
	"github.com/databacker/mysql-backup/pkg/util"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const (
	preRestoreDir  = "/scripts.d/pre-restore"
	postRestoreDir = "/scripts.d/post-restore"
	tmpRestoreFile = "/tmp/restorefile"
)

// Restore restore a specific backup into the database
func (e *Executor) Restore(ctx context.Context, opts RestoreOptions) error {
	tracer := util.GetTracerFromContext(ctx)
	ctx, span := tracer.Start(ctx, "restore")
	defer span.End()
	logger := e.Logger.WithField("run", opts.Run.String())
	logger.Level = e.Logger.Level

	logger.Info("beginning restore")
	// execute pre-restore scripts if any
	if err := preRestore(ctx, opts.Target.URL()); err != nil {
		return fmt.Errorf("error running pre-restore: %v", err)
	}

	logger.Debugf("restoring via %s protocol, temporary file location %s", opts.Target.Protocol(), tmpRestoreFile)

	_, pullSpan := tracer.Start(ctx, "pull file")
	pullSpan.SetAttributes(
		attribute.String("target", opts.Target.URL()),
		attribute.String("targetfile", opts.TargetFile),
		attribute.String("tmpfile", tmpRestoreFile),
	)
	copied, err := opts.Target.Pull(ctx, opts.TargetFile, tmpRestoreFile, logger)
	if err != nil {
		pullSpan.RecordError(err)
		pullSpan.End()
		return fmt.Errorf("failed to pull target %s: %v", opts.Target, err)
	}
	pullSpan.SetAttributes(
		attribute.Int64("copied", copied),
	)
	pullSpan.SetStatus(codes.Ok, "completed")
	pullSpan.End()
	logger.Debugf("completed copying %d bytes", copied)

	// successfully download file, now restore it
	tmpdir, err := os.MkdirTemp("", "restore")
	if err != nil {
		return fmt.Errorf("unable to create temporary working directory: %v", err)
	}
	defer os.RemoveAll(tmpdir)
	f, err := os.Open(tmpRestoreFile)
	if f == nil {
		return fmt.Errorf("unable to read the temporary download file: %v", err)
	}
	defer f.Close()
	defer os.Remove(tmpRestoreFile)

	// create my tar reader to put the files in the directory
	_, tarSpan := tracer.Start(ctx, "input_tar")
	cr, err := opts.Compressor.Uncompress(f)
	if err != nil {
		tarSpan.SetStatus(codes.Error, fmt.Sprintf("unable to create an uncompressor: %v", err))
		tarSpan.End()
		return fmt.Errorf("unable to create an uncompressor: %v", err)
	}
	if err := archive.Untar(cr, tmpdir); err != nil {
		tarSpan.SetStatus(codes.Error, fmt.Sprintf("error extracting the file: %v", err))
		tarSpan.End()
		return fmt.Errorf("error extracting the file: %v", err)
	}
	tarSpan.SetStatus(codes.Ok, "completed")
	tarSpan.End()

	// run through each file and apply it
	dbRestoreCtx, dbRestoreSpan := tracer.Start(ctx, "database_restore")
	files, err := os.ReadDir(tmpdir)
	if err != nil {
		dbRestoreSpan.SetStatus(codes.Error, fmt.Sprintf("failed to find extracted files to restore: %v", err))
		dbRestoreSpan.End()
		return fmt.Errorf("failed to find extracted files to restore: %v", err)
	}
	var (
		readers   = make([]io.ReadSeeker, 0)
		fileNames []string
	)
	for _, f := range files {
		// ignore directories
		if f.IsDir() {
			continue
		}
		file, err := os.Open(path.Join(tmpdir, f.Name()))
		if err != nil {
			continue
		}
		defer file.Close()
		readers = append(readers, file)
		fileNames = append(fileNames, f.Name())
	}
	dbRestoreSpan.SetAttributes(attribute.StringSlice("files", fileNames))
	if err := database.Restore(dbRestoreCtx, opts.DBConn, opts.DatabasesMap, readers); err != nil {
		dbRestoreSpan.SetStatus(codes.Error, fmt.Sprintf("failed to restore database: %v", err))
		dbRestoreSpan.End()
		return fmt.Errorf("failed to restore database: %v", err)
	}
	dbRestoreSpan.SetStatus(codes.Ok, "completed")
	dbRestoreSpan.End()

	// execute post-restore scripts if any
	if err := postRestore(ctx, opts.Target.URL()); err != nil {
		return fmt.Errorf("error running post-restove: %v", err)
	}
	return nil
}

// run pre-restore scripts, if they exist
func preRestore(ctx context.Context, target string) error {
	// construct any additional environment
	env := map[string]string{
		"DB_RESTORE_TARGET": target,
	}
	ctx, span := util.GetTracerFromContext(ctx).Start(ctx, "pre-restore")
	defer span.End()
	return runScripts(ctx, preRestoreDir, env)
}

func postRestore(ctx context.Context, target string) error {
	// construct any additional environment
	env := map[string]string{
		"DB_RESTORE_TARGET": target,
	}
	ctx, span := util.GetTracerFromContext(ctx).Start(ctx, "post-restore")
	defer span.End()
	return runScripts(ctx, postRestoreDir, env)
}

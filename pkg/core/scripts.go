package core

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"

	"github.com/databacker/mysql-backup/pkg/util"
	"go.opentelemetry.io/otel/codes"
)

// runScripts run scripts in a directory with a given environment.
func runScripts(ctx context.Context, dir string, env map[string]string) error {
	tracer := util.GetTracerFromContext(ctx)

	files, err := os.ReadDir(dir)
	// if the directory does not exist, do not worry about it
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	for _, f := range files {
		_, span := tracer.Start(ctx, f.Name())
		if err := runScript(ctx, dir, f, env); err != nil {
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		span.SetStatus(codes.Ok, "completed")
		span.End()
	}
	return nil
}

func runScript(ctx context.Context, dir string, f fs.DirEntry, env map[string]string) error {
	// ignore directories and any files we cannot execute
	fi, err := f.Info()
	if err != nil {
		return fmt.Errorf("error getting file info %s: %v", f.Name(), err)
	}
	if f.IsDir() || fi.Mode()&0111 == 0 {
		return nil
	}
	// execute the file
	envSlice := os.Environ()
	for k, v := range env {
		envSlice = append(envSlice, fmt.Sprintf("%s=%s", k, v))
	}
	cmd := exec.Command(path.Join(dir, f.Name()))
	cmd.Env = envSlice
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("error running file %s: %v", f.Name(), err)
	}
	return nil
}

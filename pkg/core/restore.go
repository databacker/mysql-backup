package core

import (
	"fmt"
	"io"
	"os"
	"path"

	log "github.com/sirupsen/logrus"

	"github.com/databacker/mysql-backup/pkg/archive"
	"github.com/databacker/mysql-backup/pkg/compression"
	"github.com/databacker/mysql-backup/pkg/database"
	"github.com/databacker/mysql-backup/pkg/storage"
)

const (
	preRestoreDir  = "/scripts.d/pre-restore"
	postRestoreDir = "/scripts.d/post-restore"
	tmpRestoreFile = "/tmp/restorefile"
)

// Restore restore a specific backup into the database
func Restore(target storage.Storage, targetFile string, dbconn database.Connection, databasesMap map[string]string, compressor compression.Compressor) error {
	log.Info("beginning restore")
	// execute pre-restore scripts if any
	if err := preRestore(target.URL()); err != nil {
		return fmt.Errorf("error running pre-restore: %v", err)
	}

	log.Debugf("restoring via %s protocol, temporary file location %s", target.Protocol(), tmpRestoreFile)

	copied, err := target.Pull(targetFile, tmpRestoreFile)
	if err != nil {
		return fmt.Errorf("failed to pull target %s: %v", target, err)
	}
	log.Debugf("completed copying %d bytes", copied)

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
	os.Remove(tmpRestoreFile)

	// create my tar reader to put the files in the directory
	cr, err := compressor.Uncompress(f)
	if err != nil {
		return fmt.Errorf("unable to create an uncompressor: %v", err)
	}
	if err := archive.Untar(cr, tmpdir); err != nil {
		return fmt.Errorf("error extracting the file: %v", err)
	}

	// run through each file and apply it
	files, err := os.ReadDir(tmpdir)
	if err != nil {
		return fmt.Errorf("failed to find extracted files to restore: %v", err)
	}
	readers := make([]io.ReadSeeker, 0)
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
	}
	if err := database.Restore(dbconn, databasesMap, readers); err != nil {
		return fmt.Errorf("failed to restore database: %v", err)
	}

	// execute post-restore scripts if any
	if err := postRestore(target.URL()); err != nil {
		return fmt.Errorf("error running post-restove: %v", err)
	}
	return nil
}

// run pre-restore scripts, if they exist
func preRestore(target string) error {
	// construct any additional environment
	env := map[string]string{
		"DB_RESTORE_TARGET": target,
	}
	return runScripts(preRestoreDir, env)
}

func postRestore(target string) error {
	// construct any additional environment
	env := map[string]string{
		"DB_RESTORE_TARGET": target,
	}
	return runScripts(postRestoreDir, env)
}

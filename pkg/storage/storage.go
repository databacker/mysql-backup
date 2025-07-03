package storage

import (
	"context"
	"io/fs"

	log "github.com/sirupsen/logrus"
)

type Storage interface {
	Protocol() string
	URL() string
	Clean(filename string) string
	Push(ctx context.Context, target, source string, logger *log.Entry) (int64, error)
	Pull(ctx context.Context, source, target string, logger *log.Entry) (int64, error)
	// Latest returns the latest, or most recent, file for a given target. Should return just the filename, relative to `target`, not the path.
	Latest(ctx context.Context, target string, logger *log.Entry) (string, error)
	ReadDir(ctx context.Context, dirname string, logger *log.Entry) ([]fs.FileInfo, error)
	// Remove remove a particular file
	Remove(ctx context.Context, target string, logger *log.Entry) error
}

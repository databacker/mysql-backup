package storage

import (
	"io/fs"

	log "github.com/sirupsen/logrus"
)

type Storage interface {
	Protocol() string
	URL() string
	Clean(filename string) string
	Push(target, source string, logger *log.Entry) (int64, error)
	Pull(source, target string, logger *log.Entry) (int64, error)
	ReadDir(dirname string, logger *log.Entry) ([]fs.FileInfo, error)
	// Remove remove a particular file
	Remove(target string, logger *log.Entry) error
}

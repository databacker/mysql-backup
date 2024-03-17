package storage

import "io/fs"

type Storage interface {
	Push(target, source string) (int64, error)
	Pull(source, target string) (int64, error)
	Protocol() string
	URL() string
	ReadDir(dirname string) ([]fs.FileInfo, error)
	// Remove remove a particular file
	Remove(string) error
}

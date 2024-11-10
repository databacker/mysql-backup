package file

import (
	"context"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

type File struct {
	url  url.URL
	path string
}

func New(u url.URL) *File {
	return &File{u, u.Path}
}

func (f *File) Pull(ctx context.Context, source, target string, logger *log.Entry) (int64, error) {
	return copyFile(path.Join(f.path, source), target)
}

func (f *File) Push(ctx context.Context, target, source string, logger *log.Entry) (int64, error) {
	return copyFile(source, filepath.Join(f.path, target))
}

func (f *File) Clean(filename string) string {
	return filename
}

func (f *File) Protocol() string {
	return "file"
}

func (f *File) URL() string {
	return f.url.String()
}

func (f *File) ReadDir(ctx context.Context, dirname string, logger *log.Entry) ([]fs.FileInfo, error) {

	entries, err := os.ReadDir(filepath.Join(f.path, dirname))
	if err != nil {
		return nil, err
	}
	var files []fs.FileInfo
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		files = append(files, info)
	}
	return files, nil
}

func (f *File) Remove(ctx context.Context, target string, logger *log.Entry) error {
	return os.Remove(filepath.Join(f.path, target))
}

// copyFile copy a file from to as efficiently as possible
func copyFile(from, to string) (int64, error) {
	src, err := os.Open(from)
	if err != nil {
		return 0, err
	}
	defer src.Close()

	dst, err := os.Create(to)
	if err != nil {
		return 0, err
	}
	defer dst.Close()
	n, err := io.Copy(dst, src)
	return n, err
}

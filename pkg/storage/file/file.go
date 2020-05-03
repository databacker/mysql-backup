package file

import (
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
)

type File struct {
	url  url.URL
	path string
}

func New(u url.URL) *File {
	return &File{u, u.Path}
}

func (f *File) Pull(source, target string) (int64, error) {
	return copyFile(path.Join(f.path, source), target)
}

func (f *File) Push(target, source string) (int64, error) {
	return copyFile(source, filepath.Join(f.path, target))
}

func (f *File) Protocol() string {
	return "file"
}

func (f *File) URL() string {
	return f.url.String()
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

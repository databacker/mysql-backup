package file

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
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
	// see if the target has `?latest` set, if so, we need to find the latest file
	sourceFile := filepath.Join(f.path, source)
	u, err := url.Parse(sourceFile)
	if err != nil {
		return 0, fmt.Errorf("failed to parse target URL %s: %v", source, err)
	}
	q := u.Query()
	if q.Has("latest") {
		latestFilename, err := f.Latest(ctx, u.Path, logger)
		if err != nil {
			return 0, fmt.Errorf("failed to find latest file for source %s: %v", u.Path, err)
		}
		logger.Debugf("latest file for target %s is %s", u.Path, latestFilename)
		sourceFile = filepath.Join(u.Path, latestFilename)
	}

	return copyFile(sourceFile, target)
}

func (f *File) Push(ctx context.Context, target, source string, logger *log.Entry) (int64, error) {
	return copyFile(source, filepath.Join(f.path, target))
}

func (f *File) Latest(ctx context.Context, target string, logger *log.Entry) (string, error) {
	fullTarget := filepath.Join(f.path, target)
	entries, err := os.ReadDir(fullTarget)
	if err != nil {
		return "", fmt.Errorf("failed to read directory %s: %w", f.path, err)
	}

	var latest string
	var latestModTime int64

	for _, entry := range entries {
		if entry.IsDir() || !entry.Type().IsRegular() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return "", fmt.Errorf("failed to get info for file %s: %w", entry.Name(), err)
		}
		if info.ModTime().Unix() > latestModTime {
			latest = entry.Name()
			latestModTime = info.ModTime().Unix()
		}
	}

	if latest == "" {
		return "", fmt.Errorf("no files found for target %s", target)
	}

	return latest, nil
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
		return 0, fmt.Errorf("failed to open source file %s: %w", from, err)
	}
	defer func() { _ = src.Close() }()

	dst, err := os.Create(to)
	if err != nil {
		return 0, fmt.Errorf("failed to create target file %s: %w", to, err)
	}
	defer func() { _ = dst.Close() }()
	n, err := io.Copy(dst, src)
	return n, err
}

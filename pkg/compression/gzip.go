package compression

import (
	"compress/gzip"
	"io"
)

var _ Compressor = &GzipCompressor{}

type GzipCompressor struct {
}

func (g *GzipCompressor) Uncompress(in io.Reader) (io.Reader, error) {
	return gzip.NewReader(in)
}

func (g *GzipCompressor) Compress(out io.Writer) (io.WriteCloser, error) {
	return gzip.NewWriter(out), nil
}
func (g *GzipCompressor) Extension() string {
	return "tgz"
}

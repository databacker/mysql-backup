package compression

import (
	"compress/gzip"
	"io"
)

type GzipCompressor struct {
}

func (g *GzipCompressor) Uncompress(in io.Reader) (io.Reader, error) {
	return gzip.NewReader(in)
}

func (g *GzipCompressor) Compress(out io.Writer) io.WriteCloser {
	return gzip.NewWriter(out)
}
func (g *GzipCompressor) Extension() string {
	return "tgz"
}

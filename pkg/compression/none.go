package compression

import (
	"io"
)

var _ Compressor = &NoCompressor{}

type NoCompressor struct {
}

func (n *NoCompressor) Uncompress(in io.Reader) (io.Reader, error) {
	return in, nil
}

func (n *NoCompressor) Compress(out io.Writer) (io.WriteCloser, error) {
	return &nopWriteCloser{out}, nil
}
func (n *NoCompressor) Extension() string {
	return "tar"
}

type nopWriteCloser struct {
	io.Writer
}

func (n *nopWriteCloser) Close() error {
	return nil
}

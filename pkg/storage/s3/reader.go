package s3

import (
	"io"
)

type CountingReader interface {
	io.Reader
	Bytes() int64
}

func NewCountingReader(r io.Reader) CountingReader {
	return &countingReader{r: r}
}

type countingReader struct {
	r     io.Reader
	bytes int64
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	cr.bytes += int64(n)
	return n, err
}

func (cr *countingReader) Bytes() int64 {
	return cr.bytes
}

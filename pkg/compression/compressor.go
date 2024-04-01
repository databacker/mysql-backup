package compression

import (
	"fmt"
	"io"
)

type Compressor interface {
	Uncompress(in io.Reader) (io.Reader, error)
	Compress(out io.Writer) (io.WriteCloser, error)
	Extension() string
}

func GetCompressor(name string) (Compressor, error) {
	switch name {
	case "gzip":
		return &GzipCompressor{}, nil
	case "bzip2":
		return &Bzip2Compressor{}, nil
	default:
		return nil, fmt.Errorf("unknown compression format: %s", name)
	}
}

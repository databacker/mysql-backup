package compression

import (
	"io"

	"github.com/dsnet/compress/bzip2"
)

type Bzip2Compressor struct {
}

func (b *Bzip2Compressor) Uncompress(in io.Reader) (io.Reader, error) {
	return bzip2.NewReader(in, nil)
}

func (b *Bzip2Compressor) Compress(out io.Writer) (io.WriteCloser, error) {
	return bzip2.NewWriter(out, nil)
}
func (b *Bzip2Compressor) Extension() string {
	return "tbz2"
}

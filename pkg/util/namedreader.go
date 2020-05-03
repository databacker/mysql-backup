package util

import (
	"io"
)

type NamedReader struct {
	Name string
	io.ReaderAt
	io.ReadSeeker
}

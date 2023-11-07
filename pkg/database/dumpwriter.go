package database

import (
	"io"
)

type DumpWriter struct {
	Schemas []string
	Writer  io.Writer
}

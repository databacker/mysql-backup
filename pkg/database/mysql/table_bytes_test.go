package mysql

import (
	"database/sql"
	"testing"
)

func TestRowBufferBytes(t *testing.T) {
	hash := []byte{0x2d, 0x00, '\'', '\\', 0xff}
	flags := []byte{0x27}
	var null []byte
	table := baseTable{values: []any{&hash, &flags, &null}}

	want := `(_binary '-\0\'\\` + "\xff" + `',_binary '\'',NULL)`
	if got := table.RowValues(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRowBufferEmptyBlob(t *testing.T) {
	empty := sql.RawBytes{}
	var null sql.RawBytes
	table := baseTable{values: []any{&empty, &null}}

	want := `(_binary '',NULL)`
	if got := table.RowValues(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

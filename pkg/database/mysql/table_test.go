package mysql

import (
	"database/sql"
	"testing"
	"time"
)

func TestRowBufferKeepsPrecision(t *testing.T) {
	lat := 52.520008123
	small := sql.NullFloat64{Float64: 0.00000025, Valid: true}
	f32 := float32(1.5e-7)
	updated := sql.NullTime{Time: time.Date(2026, 11, 19, 10, 20, 30, 123456000, time.UTC), Valid: true}
	whole := sql.NullTime{Time: time.Date(2026, 1, 15, 8, 0, 0, 0, time.UTC), Valid: true}
	table := baseTable{values: []any{&lat, &small, &f32, &updated, &whole}}

	want := `(52.520008123,2.5e-07,1.5e-07,'2026-11-19 10:20:30.123456','2026-01-15 08:00:00')`
	if got := table.RowValues(); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

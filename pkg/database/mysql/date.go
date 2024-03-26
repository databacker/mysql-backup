package mysql

import (
	"database/sql/driver"
	"fmt"
	"time"
)

// NullDate represents a time.Time that may be null.
// NullDate implements the Scanner interface so
// it can be used as a scan destination, similar to NullString.
// It is distinct from sql.NullTime in that it can output formats only as a date
type NullDate struct {
	Date  time.Time
	Valid bool
}

// Scan implements the Scanner interface.
func (n *NullDate) Scan(value any) error {
	if value == nil {
		n.Date, n.Valid = time.Time{}, false
		return nil
	}
	switch s := value.(type) {
	case string:
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			return err
		}
		n.Date = t
		n.Valid = true
		return nil
	case time.Time:
		n.Date = s
		n.Valid = true
		return nil
	}
	n.Valid = false
	return fmt.Errorf("unknown type %T for NullDate", value)
}

// Value implements the driver Valuer interface.
func (n NullDate) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return n.Date.Format("2006-01-02"), nil
}

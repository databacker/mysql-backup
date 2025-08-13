package test

import (
	"database/sql"
	"fmt"
	"strconv"

	_ "github.com/go-sql-driver/mysql"
)

func getStatus(db *sql.DB, name string) (int64, error) {
	var n, v string
	if err := db.QueryRow(fmt.Sprintf("SHOW GLOBAL STATUS LIKE '%s'", name)).Scan(&n, &v); err != nil {
		return 0, err
	}
	i, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return i, nil
}

func getProcesslistCounts(db *sql.DB) (total, active int64, err error) {
	rows, err := db.Query("SHOW FULL PROCESSLIST")
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = rows.Close() }()

	cols, err := rows.Columns()
	if err != nil {
		return 0, 0, err
	}

	// Map columns we care about (different MySQL variants can reorder)
	var idxUser, idxCmd = -1, -1
	for i, c := range cols {
		switch c {
		case "User":
			idxUser = i
		case "Command":
			idxCmd = i
		}
	}
	if idxUser == -1 || idxCmd == -1 {
		return 0, 0, fmt.Errorf("unexpected PROCESSLIST columns: %v", cols)
	}

	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return 0, 0, err
		}

		// Read values
		var user, cmd string
		if b, ok := vals[idxUser].([]byte); ok {
			user = string(b)
		}
		if b, ok := vals[idxCmd].([]byte); ok {
			cmd = string(b)
		}

		// Skip daemon/system threads
		if user == "event_scheduler" || cmd == "Daemon" {
			continue
		}

		total++
		if cmd != "Sleep" {
			active++
		}
	}
	return total, active, rows.Err()
}

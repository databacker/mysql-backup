package test

import (
	"database/sql"
	"fmt"
)

// setupLargeDatabase creates a large database with multiple schemas and a significant amount of data.
// It is used mainly so you can test the performance of the backup system with a large dataset.
// The size argument is the size in bytes. If it is not a multiple of MB (1024*1024), it will be rounded up to the next MB.
// The count is how many databases in the server.
func setupLargeDatabase(db *sql.DB, count uint, size uint64) error {
	var databases []string
	if count <= 0 {
		return fmt.Errorf("count must be greater than 0, got %d", count)
	}
	for i := uint(1); i <= count; i++ {
		databases = append(databases, fmt.Sprintf("db%02d", i))
	}
	// This function is a placeholder for setting up a large database.
	// It can be used to create a database with a significant amount of data
	// to test the performance and functionality of the backup system.
	// For now, it does nothing.
	var createDatabases = func(db *sql.DB, databases []string) error {
		createCmd := `CREATE DATABASE IF NOT EXISTS %s; USE %s;
		DROP TABLE IF EXISTS big1;
		CREATE TABLE big1 (
			id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
			payload LONGBLOB,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		) ENGINE=InnoDB;`
		for _, instance := range databases {
			sql := fmt.Sprintf(createCmd, instance, instance)
			if _, err := db.Exec(sql); err != nil {
				return fmt.Errorf("failed to create database %s: %v", instance, err)
			}
		}
		return nil
	}

	var insertRowsChunk = func(db *sql.DB, instance string, chunkSize uint64) error {
		insertCmd := fmt.Sprintf(`INSERT INTO big1 (payload)
    SELECT REPEAT('x', 4096)
    FROM
      (SELECT 0 n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4
       UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) d0
    CROSS JOIN
      (SELECT 0 n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4
       UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) d1
    CROSS JOIN
      (SELECT 0 n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4
       UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) d2
    CROSS JOIN
      (SELECT 0 n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4
       UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) d3
    CROSS JOIN
      (SELECT 0 n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4
       UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) d4
    LIMIT %d;`, chunkSize)
		if _, err := db.Exec(fmt.Sprintf("USE %s; %s", instance, insertCmd)); err != nil {
			return fmt.Errorf("failed to insert rows into %s: %v", instance, err)
		}
		return nil
	}

	// Insert N rows by slicing into <=100k chunks
	var insertRows = func(db *sql.DB, instance string, n uint64) error {
		var chunk uint64
		for n > 0 {
			if n > 100000 {
				chunk = 100000
			} else {
				chunk = n
			}
			if err := insertRowsChunk(db, instance, chunk); err != nil {
				return fmt.Errorf("failed to insert rows into %s: %v", instance, err)
			}
			n = n - chunk
		}
		return nil
	}

	// grow each database until an individual dump takes > target time (30 seconds)
	// calculate the desired size in MB
	desiredSizeMB := (size + 1023) / 1024 / 1024
	// calculate the number of rows needed to reach the desired size
	batch := desiredSizeMB * 1024 * 1024 / 4096
	if err := createDatabases(db, databases); err != nil {
		return fmt.Errorf("failed to create databases: %v", err)
	}
	for _, instance := range databases {
		if err := insertRows(db, instance, batch); err != nil {
			return fmt.Errorf("failed to insert rows into %s: %v", instance, err)
		}
	}
	return nil
}

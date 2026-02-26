package database

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"regexp"
	"strings"
)

var (
	useRegex    = regexp.MustCompile(`(?i)^(USE\s*` + "`" + `)([^\s]+)(` + "`" + `\s*;)$`)
	createRegex = regexp.MustCompile(`(?i)^(CREATE\s+DATABASE\s*(\/\*.*\*\/\s*)?` + "`" + `)([^\s]+)(` + "`" + `\s*(\s*\/\*.*\*\/\s*)?\s*;$)`)
)

func Restore(ctx context.Context, dbconn *Connection, databasesMap map[string]string, readers []io.ReadSeeker) error {
	db, err := dbconn.MySQL()
	if err != nil {
		return fmt.Errorf("failed to open connection to database: %v", err)
	}

	// load data into database by reading from each reader
	for _, r := range readers {
		tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return fmt.Errorf("failed to restore database: %w", err)
		}
		reader := bufio.NewReader(r)
		defaultDelimiter := ";" // default delimiter
		delimiter := defaultDelimiter
		var current string
		for {
			line, err := reader.ReadString('\n')
			if err != nil && err != io.EOF {
				_ = tx.Rollback()
				return fmt.Errorf("failed to restore database: %w", err)
			}
			// strip CRLF/newline
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				if err == io.EOF {
					break
				}
				continue
			}
			if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(line)), "DELIMITER") {
				// enter scope of delimiter
				// or
				// exit scope of delimiter
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					newDelimiter := strings.TrimSpace(parts[1])
					if newDelimiter == defaultDelimiter && delimiter != defaultDelimiter {
						// exit scope of delimiter
						// remove specific delimiter from current statement
						current = strings.TrimSuffix(strings.TrimSpace(current), delimiter) + "\n"
					}
					delimiter = newDelimiter
				}
			} else {
				current += line + "\n"
				if delimiter != defaultDelimiter {
					// in scope of delimiter, continue accumulating
					continue
				}
				// if the line does not end with a semicolon, keep accumulating
				if !strings.HasSuffix(line, delimiter) {
					if err == io.EOF {
						// EOF reached but statement not terminated; we'll try to execute below
						break
					}
					continue
				}
			}

			current = strings.TrimSpace(current)
			if current == "" {
				continue
			}

			// if we have the line that sets the database, and we need to replace, replace it
			if createRegex.MatchString(current) {
				dbName := createRegex.FindStringSubmatch(current)[3]
				if newName, ok := databasesMap[dbName]; ok {
					current = createRegex.ReplaceAllString(current, fmt.Sprintf("${1}%s${4}", newName))
				}
			}
			if useRegex.MatchString(current) {
				dbName := useRegex.FindStringSubmatch(current)[2]
				if newName, ok := databasesMap[dbName]; ok {
					current = useRegex.ReplaceAllString(current, fmt.Sprintf("${1}%s${3}", newName))
				}
			}
			// we hit a break, so we have the entire transaction
			if _, err := tx.Exec(current); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("failed to restore database: %w", err)
			}
			current = ""

			if err == io.EOF {
				break
			}
		}
		// if there's any leftover SQL (for example last statement without newline), execute it
		if strings.TrimSpace(current) != "" {
			if _, err := tx.Exec(current); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("failed to restore database: %w", err)
			}
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to restore database: %w", err)
		}
	}

	return nil
}

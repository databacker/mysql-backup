package database

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"regexp"
)

var (
	useRegex    = regexp.MustCompile(`(?i)^(USE\s*` + "`" + `)([^\s]+)(` + "`" + `\s*;)$`)
	createRegex = regexp.MustCompile(`(?i)^(CREATE\s+DATABASE\s*(\/\*.*\*\/\s*)?` + "`" + `)([^\s]+)(` + "`" + `\s*(\s*\/\*.*\*\/\s*)?\s*;$)`)
)

func Restore(ctx context.Context, dbconn Connection, databasesMap map[string]string, readers []io.ReadSeeker) error {
	db, err := sql.Open("mysql", dbconn.MySQL())
	if err != nil {
		return fmt.Errorf("failed to open connection to database: %v", err)
	}
	defer db.Close()

	// load data into database by reading from each reader
	for _, r := range readers {
		tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return fmt.Errorf("failed to restore database: %w", err)
		}
		scanner := bufio.NewScanner(r)
		var current string
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			current += line + "\n"
			if line[len(line)-1] != ';' {
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
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to restore database: %w", err)
		}
	}

	return nil
}

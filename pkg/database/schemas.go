package database

import (
	"database/sql"
	"fmt"
)

var (
	excludeSchemaList = []string{"information_schema", "performance_schema", "sys", "mysql"}
	excludeSchemas    = map[string]bool{}
)

func init() {
	for _, schema := range excludeSchemaList {
		excludeSchemas[schema] = true
	}
}

func GetSchemas(dbconn Connection) ([]string, error) {
	db, err := sql.Open("mysql", dbconn.MySQL())
	if err != nil {
		return nil, fmt.Errorf("failed to open connection to database: %v", err)
	}
	defer db.Close()

	// TODO: get list of schemas
	// mysql -h $DB_SERVER -P $DB_PORT $DBUSER $DBPASS -N -e 'show databases'
	rows, err := db.Query("show databases")
	if err != nil {
		return nil, fmt.Errorf("could not get schemas: %v", err)
	}
	defer rows.Close()

	names := []string{}
	for rows.Next() {
		var name string
		err := rows.Scan(&name)
		if err != nil {
			return nil, fmt.Errorf("error getting database name: %v", err)
		}
		if _, ok := excludeSchemas[name]; ok {
			continue
		}
		names = append(names, name)
	}

	return names, nil
}

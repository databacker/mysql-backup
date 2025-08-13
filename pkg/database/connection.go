package database

import (
	"database/sql"
	"fmt"
	"strings"

	mysql "github.com/go-sql-driver/mysql"
)

type Connection struct {
	User            string
	Pass            string
	Host            string
	Port            int
	MultiStatements bool

	// holds a connection to the database
	sql *sql.DB
}

// MySQL returns a MySQL connection for the Connection.
func (c *Connection) MySQL() (*sql.DB, error) {
	if c.sql == nil {

		config := mysql.NewConfig()
		config.User = c.User
		config.Passwd = c.Pass
		if strings.HasPrefix(c.Host, "/") {
			config.Net = "unix"
			config.Addr = c.Host
		} else {
			config.Net = "tcp"
			config.Addr = fmt.Sprintf("%s:%d", c.Host, c.Port)
		}
		config.ParseTime = true
		config.TLSConfig = "preferred"
		config.MultiStatements = c.MultiStatements
		dsn := config.FormatDSN()
		handle, err := sql.Open("mysql", dsn)
		if err != nil {
			return nil, fmt.Errorf("failed to open connection to database: %v", err)
		}
		c.sql = handle
	}
	return c.sql, nil

}

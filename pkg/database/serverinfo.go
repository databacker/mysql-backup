package database

import "fmt"

// ServerUUID queries MySQL for the server's global server_uuid variable.
// This is a stable identifier for the MySQL server instance across restarts
// (@@global.server_uuid).
func (c *Connection) ServerUUID() (string, error) {
	db, err := c.MySQL()
	if err != nil {
		return "", fmt.Errorf("failed to open connection to database: %v", err)
	}
	var serverUUID string
	if err := db.QueryRow("SELECT @@global.server_uuid").Scan(&serverUUID); err != nil {
		return "", fmt.Errorf("failed to query server_uuid: %v", err)
	}
	return serverUUID, nil
}

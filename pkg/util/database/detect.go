package database

import (
	"database/sql"
	"fmt"
	"strings"
)

// DetectVariant returns the variant of the database, which can affect some commands.
// It uses several heuristics to determine the variant based on the version and comment.
// None of this is 100% reliable, but it should work for most cases.
func DetectVariant(conn *sql.DB) (Variant, error) {
	// Check @@version and @@version_comment
	var version, comment string
	err := conn.QueryRow("SELECT @@version, @@version_comment").Scan(&version, &comment)
	if err != nil {
		return "", fmt.Errorf("failed to query version: %w", err)
	}

	versionLower := strings.ToLower(version)
	commentLower := strings.ToLower(comment)

	// Heuristic 1: version string or comment
	switch {
	case strings.Contains(versionLower, "mariadb") || strings.Contains(commentLower, "mariadb"):
		return VariantMariaDB, nil
	case strings.Contains(commentLower, "percona"):
		return VariantPercona, nil
	case strings.Contains(commentLower, "mysql"):
		return VariantMySQL, nil
	}

	// Heuristic 2: Check for Aria engine (MariaDB)
	var dummy string
	err = conn.QueryRow("SELECT 1 FROM information_schema.engines WHERE engine = 'Aria' LIMIT 1").Scan(&dummy)
	if err == nil {
		return VariantMariaDB, nil
	}

	// Heuristic 3: Percona plugins
	err = conn.QueryRow("SELECT 1 FROM information_schema.plugins WHERE plugin_name LIKE '%percona%' LIMIT 1").Scan(&dummy)
	if err == nil {
		return VariantPercona, nil
	}

	return VariantMySQL, nil
}

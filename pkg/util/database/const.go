package database

type Variant string

const (
	// VariantMariaDB is the MariaDB variant of MySQL.
	VariantMariaDB Variant = "mariadb"
	// VariantMySQL is the MySQL variant of MySQL.
	VariantMySQL Variant = "mysql"
	// VariantPercona is the Percona variant of MySQL.
	VariantPercona Variant = "percona"
)

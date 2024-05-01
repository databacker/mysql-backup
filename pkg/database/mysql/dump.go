/*
* with thanks to https://github.com/BrandonRoehl/go-mysqldump which required some changes,
* but was under MIT.
*
* We might have been able to use it as is, except for this when running `go get`:

go: finding module for package github.com/BrandonRoehl/go-mysqldump
go: found github.com/BrandonRoehl/go-mysqldump in github.com/BrandonRoehl/go-mysqldump v0.5.1
go: github.com/databacker/mysql-backup/pkg/database imports

	github.com/BrandonRoehl/go-mysqldump: github.com/BrandonRoehl/go-mysqldump@v0.5.1: parsing go.mod:
	module declares its path as: github.com/jamf/go-mysqldump
	        but was required as: github.com/BrandonRoehl/go-mysqldump
*/
package mysql

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"text/template"
	"time"
)

/*
Data struct to configure dump behavior

	Out:              Stream to wite to
	Connection:       Database connection to dump
	IgnoreTables:     Mark sensitive tables to ignore
	MaxAllowedPacket: Sets the largest packet size to use in backups
	LockTables:       Lock all tables for the duration of the dump
*/
type Data struct {
	Out                 io.Writer
	Connection          *sql.DB
	IgnoreTables        []string
	MaxAllowedPacket    int
	LockTables          bool
	Schema              string
	Compact             bool
	Host                string
	SuppressUseDatabase bool
	Charset             string
	Collation           string

	tx         *sql.Tx
	headerTmpl *template.Template
	footerTmpl *template.Template
	err        error
}

type metaData struct {
	DumpVersion   string
	ServerVersion string
	CompleteTime  string
	Host          string
	Database      string
	Charset       string
	Collation     string
}

const (
	// Version of this plugin for easy reference
	Version = "0.6.0"

	defaultMaxAllowedPacket = 4194304
)

// takes a *metaData
const headerTmpl = `-- Go SQL Dump {{ .DumpVersion }}
--
-- Host: {{.Host}}    Database: {{.Database}}
-- ------------------------------------------------------
-- Server version	{{ .ServerVersion }}

/*!40101 SET @OLD_CHARACTER_SET_CLIENT=@@CHARACTER_SET_CLIENT */;
/*!40101 SET @OLD_CHARACTER_SET_RESULTS=@@CHARACTER_SET_RESULTS */;
/*!40101 SET @OLD_COLLATION_CONNECTION=@@COLLATION_CONNECTION */;
/*!50503 SET NAMES {{ .Charset }} */;
/*!40103 SET @OLD_TIME_ZONE=@@TIME_ZONE */;
/*!40103 SET TIME_ZONE='+00:00' */;
/*!40014 SET @OLD_UNIQUE_CHECKS=@@UNIQUE_CHECKS, UNIQUE_CHECKS=0 */;
/*!40014 SET @OLD_FOREIGN_KEY_CHECKS=@@FOREIGN_KEY_CHECKS, FOREIGN_KEY_CHECKS=0 */;
/*!40101 SET @OLD_SQL_MODE=@@SQL_MODE, SQL_MODE='NO_AUTO_VALUE_ON_ZERO' */;
/*!40111 SET @OLD_SQL_NOTES=@@SQL_NOTES, SQL_NOTES=0 */;

--
-- Current Database: ` + "`{{.Database}}`" + `
--
`

const createUseDatabaseHeader = `
CREATE DATABASE /*!32312 IF NOT EXISTS*/ ` + "`{{.Database}}`" + ` /*!40100 DEFAULT CHARACTER SET {{ .Charset }} COLLATE {{ .Collation }} */ /*!80016 DEFAULT ENCRYPTION='N' */;

USE ` + "`{{.Database}}`;"

// takes a *metaData
const footerTmpl = `/*!40103 SET TIME_ZONE=@OLD_TIME_ZONE */;

/*!40101 SET SQL_MODE=@OLD_SQL_MODE */;
/*!40014 SET FOREIGN_KEY_CHECKS=@OLD_FOREIGN_KEY_CHECKS */;
/*!40014 SET UNIQUE_CHECKS=@OLD_UNIQUE_CHECKS */;
/*!40101 SET CHARACTER_SET_CLIENT=@OLD_CHARACTER_SET_CLIENT */;
/*!40101 SET CHARACTER_SET_RESULTS=@OLD_CHARACTER_SET_RESULTS */;
/*!40101 SET COLLATION_CONNECTION=@OLD_COLLATION_CONNECTION */;
/*!40111 SET SQL_NOTES=@OLD_SQL_NOTES */;

-- Dump completed on {{ .CompleteTime }}`

const footerTmplCompact = ``

const nullType = "NULL"

// Dump data using struct
func (data *Data) Dump() error {
	meta := metaData{
		DumpVersion: Version,
		Host:        data.Host,
		Database:    data.Schema,
	}

	if data.MaxAllowedPacket == 0 {
		data.MaxAllowedPacket = defaultMaxAllowedPacket
	}

	if err := data.getTemplates(); err != nil {
		return err
	}

	if err := data.selectSchema(); err != nil {
		return err
	}

	// Start the read only transaction and defer the rollback until the end
	// This way the database will have the exact state it did at the begining of
	// the backup and nothing can be accidentally committed
	if err := data.begin(); err != nil {
		return err
	}
	defer func() {
		_ = data.rollback()
	}()

	if err := data.getCharsetCollections(); err != nil {
		return err
	}

	if err := meta.updateMetadata(data); err != nil {
		return err
	}

	if err := data.headerTmpl.Execute(data.Out, meta); err != nil {
		return err
	}

	tables, err := data.getTables()
	if err != nil {
		return err
	}

	// Lock all tables before dumping if present
	if data.LockTables && len(tables) > 0 {
		var b bytes.Buffer
		b.WriteString("LOCK TABLES ")
		for index, table := range tables {
			if index != 0 {
				b.WriteString(",")
			}
			b.WriteString("`" + table.Name() + "` READ /*!32311 LOCAL */")
		}

		if _, err := data.Connection.Exec(b.String()); err != nil {
			return err
		}

		defer func() {
			_, _ = data.Connection.Exec("UNLOCK TABLES")
		}()
	}

	for _, name := range tables {
		if err := data.dumpTable(name); err != nil {
			return err
		}
	}
	if data.err != nil {
		return data.err
	}

	meta.CompleteTime = time.Now().UTC().Format("2006-01-02 15:04:05")
	return data.footerTmpl.Execute(data.Out, meta)
}

// MARK: - Private methods

// selectSchema selects a specific schema to use
func (data *Data) selectSchema() error {
	if data.Schema == "" {
		return errors.New("cannot select schema when one is not provided")
	}
	_, err := data.Connection.Exec("USE `" + data.Schema + "`")
	return err
}

// begin starts a read only transaction that will be whatever the database was
// when it was called
func (data *Data) begin() (err error) {
	data.tx, err = data.Connection.BeginTx(context.Background(), &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	return
}

// rollback cancels the transaction
func (data *Data) rollback() error {
	return data.tx.Rollback()
}

// MARK: writter methods

func (data *Data) dumpTable(table Table) error {
	if data.err != nil {
		return data.err
	}
	if err := table.Init(); err != nil {
		return err
	}
	return table.Execute(data.Out, data.Compact)
}

// MARK: get methods

// getTemplates initializes the templates on data from the constants in this file
func (data *Data) getTemplates() (err error) {
	var hTmpl string
	fTmpl := footerTmpl
	if data.Compact {
		fTmpl = footerTmplCompact
	} else {
		hTmpl = headerTmpl
	}
	// do we include the `USE database;` in the dump?
	if !data.SuppressUseDatabase {
		hTmpl += createUseDatabaseHeader
		// non-compact has an extra carriage return; no idea why
		if !data.Compact {
			hTmpl += "\n"
		}
	}
	data.headerTmpl, err = template.New("mysqldumpHeader").Parse(hTmpl)
	if err != nil {
		return
	}

	data.footerTmpl, err = template.New("mysqldumpFooter").Parse(fTmpl)
	if err != nil {
		return
	}
	return
}

func (data *Data) getTables() ([]Table, error) {
	tables := make([]Table, 0)

	rows, err := data.tx.Query("SHOW FULL TABLES")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var tableName, tableType sql.NullString
		if err := rows.Scan(&tableName, &tableType); err != nil {
			return nil, err
		}
		if !tableName.Valid || data.isIgnoredTable(tableName.String) {
			continue
		}
		table := baseTable{
			name:     tableName.String,
			data:     data,
			database: data.Schema,
		}
		switch tableType.String {
		case "VIEW":
			tables = append(tables, &view{baseTable: table})
		case "BASE TABLE":
			tables = append(tables, &table)
		default:
			return nil, errors.New("unknown table type: " + tableType.String)
		}
	}
	return tables, rows.Err()
}

func (data *Data) getCharsetCollections() error {
	rows, err := data.tx.Query("SELECT @@character_set_database, @@collation_database")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var charset, collation sql.NullString
		if err := rows.Scan(&charset, &collation); err != nil {
			return err
		}
		if !charset.Valid || !collation.Valid {
			continue
		}
		data.Charset = charset.String
		data.Collation = collation.String
		break
	}
	return rows.Err()
}

func (data *Data) isIgnoredTable(name string) bool {
	for _, item := range data.IgnoreTables {
		if item == name {
			return true
		}
	}
	return false
}

func (meta *metaData) updateMetadata(data *Data) (err error) {
	var serverVersion sql.NullString
	err = data.tx.QueryRow("SELECT version()").Scan(&serverVersion)
	meta.ServerVersion = serverVersion.String
	meta.Collation = data.Collation
	meta.Charset = data.Charset
	return
}

func sub(a, b int) int {
	return a - b
}

func esc(in string) string {
	return "`" + in + "`"
}

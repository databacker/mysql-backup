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
	"fmt"
	"io"
	"reflect"
	"strings"
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

	tx         *sql.Tx
	headerTmpl *template.Template
	tableTmpl  *template.Template
	footerTmpl *template.Template
	err        error
}

type table struct {
	Name string
	Err  error

	cols   []string
	data   *Data
	rows   *sql.Rows
	values []interface{}
}

type metaData struct {
	DumpVersion   string
	ServerVersion string
	CompleteTime  string
	Host          string
	Database      string
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
/*!50503 SET NAMES utf8mb4 */;
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
CREATE DATABASE /*!32312 IF NOT EXISTS*/ ` + "`{{.Database}}`" + ` /*!40100 DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci */ /*!80016 DEFAULT ENCRYPTION='N' */;

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

// Takes a *table
const tableTmpl = `
--
-- Table structure for table {{ .NameEsc }}
--

DROP TABLE IF EXISTS {{ .NameEsc }};
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
{{ .CreateSQL }};
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Dumping data for table {{ .NameEsc }}
--

LOCK TABLES {{ .NameEsc }} WRITE;
/*!40000 ALTER TABLE {{ .NameEsc }} DISABLE KEYS */;
{{ range $value := .Stream }}
{{- $value }}
{{ end -}}
/*!40000 ALTER TABLE {{ .NameEsc }} ENABLE KEYS */;
UNLOCK TABLES;
`

const tableTmplCompact = `
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
{{ .CreateSQL }};
/*!40101 SET character_set_client = @saved_cs_client */;
{{ range $value := .Stream }}
{{- $value }}
{{ end -}}
`

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

	if err := meta.updateServerVersion(data); err != nil {
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
		for index, name := range tables {
			if index != 0 {
				b.WriteString(",")
			}
			b.WriteString("`" + name + "` READ /*!32311 LOCAL */")
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

func (data *Data) dumpTable(name string) error {
	if data.err != nil {
		return data.err
	}
	table := data.createTable(name)
	return data.writeTable(table)
}

func (data *Data) writeTable(table *table) error {
	if err := data.tableTmpl.Execute(data.Out, table); err != nil {
		return err
	}
	return table.Err
}

// MARK: get methods

// getTemplates initializes the templates on data from the constants in this file
func (data *Data) getTemplates() (err error) {
	var hTmpl string
	fTmpl := footerTmpl
	tTmpl := tableTmpl
	if data.Compact {
		fTmpl = footerTmplCompact
		tTmpl = tableTmplCompact
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

	data.tableTmpl, err = template.New("mysqldumpTable").Parse(tTmpl)
	if err != nil {
		return
	}

	data.footerTmpl, err = template.New("mysqldumpTable").Parse(fTmpl)
	if err != nil {
		return
	}
	return
}

func (data *Data) getTables() ([]string, error) {
	tables := make([]string, 0)

	rows, err := data.tx.Query("SHOW TABLES")
	if err != nil {
		return tables, err
	}
	defer rows.Close()

	for rows.Next() {
		var table sql.NullString
		if err := rows.Scan(&table); err != nil {
			return tables, err
		}
		if table.Valid && !data.isIgnoredTable(table.String) {
			tables = append(tables, table.String)
		}
	}
	return tables, rows.Err()
}

func (data *Data) isIgnoredTable(name string) bool {
	for _, item := range data.IgnoreTables {
		if item == name {
			return true
		}
	}
	return false
}

func (meta *metaData) updateServerVersion(data *Data) (err error) {
	var serverVersion sql.NullString
	err = data.tx.QueryRow("SELECT version()").Scan(&serverVersion)
	meta.ServerVersion = serverVersion.String
	return
}

// MARK: create methods

func (data *Data) createTable(name string) *table {
	return &table{
		Name: name,
		data: data,
	}
}

func (table *table) NameEsc() string {
	return "`" + table.Name + "`"
}

func (table *table) CreateSQL() (string, error) {
	var tableReturn, tableSQL sql.NullString
	if err := table.data.tx.QueryRow("SHOW CREATE TABLE "+table.NameEsc()).Scan(&tableReturn, &tableSQL); err != nil {
		return "", err
	}

	if tableReturn.String != table.Name {
		return "", errors.New("Returned table is not the same as requested table")
	}

	return tableSQL.String, nil
}

func (table *table) initColumnData() error {
	colInfo, err := table.data.tx.Query("SHOW COLUMNS FROM " + table.NameEsc())
	if err != nil {
		return err
	}
	defer colInfo.Close()

	cols, err := colInfo.Columns()
	if err != nil {
		return err
	}

	fieldIndex, extraIndex := -1, -1
	for i, col := range cols {
		switch col {
		case "Field", "field":
			fieldIndex = i
		case "Extra", "extra":
			extraIndex = i
		}
		if fieldIndex >= 0 && extraIndex >= 0 {
			break
		}
	}
	if fieldIndex < 0 || extraIndex < 0 {
		return errors.New("database column information is malformed")
	}

	info := make([]sql.NullString, len(cols))
	scans := make([]interface{}, len(cols))
	for i := range info {
		scans[i] = &info[i]
	}

	var result []string
	for colInfo.Next() {
		// Read into the pointers to the info marker
		if err := colInfo.Scan(scans...); err != nil {
			return err
		}

		// Ignore the virtual columns
		if !info[extraIndex].Valid || !strings.Contains(info[extraIndex].String, "VIRTUAL") {
			result = append(result, info[fieldIndex].String)
		}
	}
	table.cols = result
	return nil
}

func (table *table) columnsList() string {
	return "`" + strings.Join(table.cols, "`, `") + "`"
}

func (table *table) Init() error {
	if len(table.values) != 0 {
		return errors.New("can't init twice")
	}

	if err := table.initColumnData(); err != nil {
		return err
	}

	if len(table.cols) == 0 {
		// No data to dump since this is a virtual table
		return nil
	}

	var err error
	table.rows, err = table.data.tx.Query("SELECT " + table.columnsList() + " FROM " + table.NameEsc())
	if err != nil {
		return err
	}

	tt, err := table.rows.ColumnTypes()
	if err != nil {
		return err
	}

	table.values = make([]interface{}, len(tt))
	for i, tp := range tt {
		table.values[i] = reflect.New(reflectColumnType(tp)).Interface()
	}
	return nil
}

func reflectColumnType(tp *sql.ColumnType) reflect.Type {
	// reflect for scanable
	switch tp.ScanType().Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return reflect.TypeOf(sql.NullInt64{})
	case reflect.Float32, reflect.Float64:
		return reflect.TypeOf(sql.NullFloat64{})
	case reflect.String:
		return reflect.TypeOf(sql.NullString{})
	}

	// determine by name
	switch tp.DatabaseTypeName() {
	case "BLOB", "BINARY":
		return reflect.TypeOf(sql.RawBytes{})
	case "VARCHAR", "TEXT", "DECIMAL":
		return reflect.TypeOf(sql.NullString{})
	case "BIGINT", "TINYINT", "INT":
		return reflect.TypeOf(sql.NullInt64{})
	case "DOUBLE":
		return reflect.TypeOf(sql.NullFloat64{})
	}

	// unknown datatype
	return tp.ScanType()
}

func (table *table) Next() bool {
	if table.rows == nil {
		if err := table.Init(); err != nil {
			table.Err = err
			return false
		}
	}
	// Fallthrough
	if table.rows.Next() {
		if err := table.rows.Scan(table.values...); err != nil {
			table.Err = err
			return false
		} else if err := table.rows.Err(); err != nil {
			table.Err = err
			return false
		}
	} else {
		table.rows.Close()
		table.rows = nil
		return false
	}
	return true
}

func (table *table) RowValues() string {
	return table.RowBuffer().String()
}

func (table *table) RowBuffer() *bytes.Buffer {
	var b bytes.Buffer
	b.WriteString("(")

	for key, value := range table.values {
		if key != 0 {
			b.WriteString(",")
		}
		switch s := value.(type) {
		case nil:
			b.WriteString(nullType)
		case *sql.NullString:
			if s.Valid {
				fmt.Fprintf(&b, "'%s'", sanitize(s.String))
			} else {
				b.WriteString(nullType)
			}
		case *sql.NullInt64:
			if s.Valid {
				fmt.Fprintf(&b, "%d", s.Int64)
			} else {
				b.WriteString(nullType)
			}
		case *sql.NullFloat64:
			if s.Valid {
				fmt.Fprintf(&b, "%f", s.Float64)
			} else {
				b.WriteString(nullType)
			}
		case *sql.RawBytes:
			if len(*s) == 0 {
				b.WriteString(nullType)
			} else {
				fmt.Fprintf(&b, "_binary '%s'", sanitize(string(*s)))
			}
		default:
			fmt.Fprintf(&b, "'%s'", value)
		}
	}
	b.WriteString(")")

	return &b
}

func (table *table) Stream() <-chan string {
	valueOut := make(chan string, 1)
	go func() {
		defer close(valueOut)
		var insert bytes.Buffer

		for table.Next() {
			b := table.RowBuffer()
			// Truncate our insert if it won't fit
			if insert.Len() != 0 && insert.Len()+b.Len() > table.data.MaxAllowedPacket-1 {
				_, _ = insert.WriteString(";")
				valueOut <- insert.String()
				insert.Reset()
			}

			if insert.Len() == 0 {
				_, _ = fmt.Fprint(&insert, strings.Join(
					// extra "" at the end so we get an extra whitespace as needed
					[]string{"INSERT", "INTO", table.NameEsc(), "(" + table.columnsList() + ")", "VALUES", ""},
					" "))
			} else {
				_, _ = insert.WriteString(",")
			}
			_, _ = b.WriteTo(&insert)
		}
		if insert.Len() != 0 {
			_, _ = insert.WriteString(";")
			valueOut <- insert.String()
		}
	}()
	return valueOut
}

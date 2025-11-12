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
	"slices"
	"strings"
	"text/template"
	"time"

	dbutil "github.com/databacker/mysql-backup/pkg/util/database"
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
	Out                    io.Writer
	Connection             *sql.DB
	IgnoreTables           []string
	MaxAllowedPacket       int
	LockTables             bool
	Schema                 string
	Compact                bool
	Triggers               bool
	Routines               bool
	Host                   string
	SuppressUseDatabase    bool
	SkipExtendedInsert     bool
	Charset                string
	Collation              string
	PostDumpDelay          time.Duration
	IncludeGeneratedColumns bool

	tx                 *sql.Tx
	headerTmpl         *template.Template
	footerTmpl         *template.Template
	routinesHeaderTmpl *template.Template
	err                error
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

const routinesHeader = `
--
-- Dumping routines for database '{{ .Database }}'
--
`

const routinesHeaderCompact = ``

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

	tables, views, err := data.getTables()
	if err != nil {
		return err
	}

	// Lock all tables before dumping if present
	if data.LockTables && (len(tables) > 0 || len(views) > 0) {
		lockCommand, err := data.getBackupLockCommand(tables, views)
		if err != nil {
			return fmt.Errorf("failed to get lock command: %w", err)
		}
		if _, err := data.Connection.Exec(lockCommand); err != nil {
			return err
		}

		defer func() {
			_, _ = data.Connection.Exec("UNLOCK TABLES")
		}()
	}

	// get the triggers for the current schema, structured by table
	var triggers map[string][]string
	if data.Triggers {
		triggers, err = data.dumpTriggers()
		if err != nil {
			return err
		}
	}

	slices.SortFunc(tables, func(a, b Table) int {
		return strings.Compare(strings.ToLower(a.Name()), strings.ToLower(b.Name()))
	})
	slices.SortFunc(views, func(a, b Table) int {
		return strings.Compare(strings.ToLower(a.Name()), strings.ToLower(b.Name()))
	})
	for _, name := range tables {
		if err := data.dumpTable(name, 0); err != nil {
			return err
		}
		// dump triggers for the current table
		if len(triggers) > 0 {
			if trigger, ok := triggers[name.Name()]; ok {
				for _, t := range trigger {
					if _, err := data.Out.Write([]byte(t)); err != nil {
						return err
					}
				}
			}
		}
	}

	// Dump the dummy views, if any
	for _, name := range views {
		if err := data.dumpTable(name, 0); err != nil {
			return err
		}
	}

	// Dump routines (functions and procedures)
	if data.Routines {
		if err := data.routinesHeaderTmpl.Execute(data.Out, meta); err != nil {
			return err
		}
		if err := data.dumpFunctions(); err != nil {
			return err
		}
		if err := data.dumpProcedures(); err != nil {
			return err
		}
	}

	// Dump the actual views
	for _, name := range views {
		if err := data.dumpTable(name, 1); err != nil {
			return err
		}
	}

	if data.err != nil {
		return data.err
	}

	if data.PostDumpDelay > 0 {
		time.Sleep(data.PostDumpDelay)
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

func (data *Data) dumpTable(table Table, part int) error {
	if data.err != nil {
		return data.err
	}
	if err := table.Init(); err != nil {
		return err
	}
	return table.Execute(data.Out, data.Compact, part)
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

	// routines header
	var routinesHeaderTmpl = routinesHeader
	if data.Compact {
		routinesHeaderTmpl = routinesHeaderCompact
	}

	data.headerTmpl, err = template.New("mysqldumpHeader").Parse(hTmpl)
	if err != nil {
		return
	}

	data.footerTmpl, err = template.New("mysqldumpFooter").Parse(fTmpl)
	if err != nil {
		return
	}

	data.routinesHeaderTmpl, err = template.New("mysqldumpRoutinesHeader").Parse(routinesHeaderTmpl)
	return
}

func (data *Data) getTables() (tables []Table, views []Table, err error) {
	tables = make([]Table, 0)
	views = make([]Table, 0)

	rows, err := data.tx.Query("SHOW FULL TABLES")
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var tableName, tableType sql.NullString
		if err := rows.Scan(&tableName, &tableType); err != nil {
			return nil, nil, err
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
			views = append(views, &view{baseTable: table})
		case "BASE TABLE":
			tables = append(tables, &table)
		default:
			return nil, nil, errors.New("unknown table type: " + tableType.String)
		}
	}
	return tables, views, rows.Err()
}

func (data *Data) getCharsetCollections() error {
	rows, err := data.tx.Query("SELECT @@character_set_database, @@collation_database")
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

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

// dumpTriggers dump the triggers for the current schema into a list by table
func (data *Data) dumpTriggers() (map[string][]string, error) {
	var triggers = make(map[string][]string)
	rows, err := data.tx.Query("SHOW TRIGGERS FROM `" + data.Schema + "`")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var triggerName, event, table, statement, timing, sqlMode, definer, charset, collationConnection, databaseCollection sql.NullString
		var created sql.NullTime
		if err := rows.Scan(&triggerName, &event, &table, &statement, &timing, &created, &sqlMode, &definer, &charset, &collationConnection, &databaseCollection); err != nil {
			return nil, err
		}
		if !triggerName.Valid || !statement.Valid {
			continue
		}
		var definerUser, definerHost string
		if definer.Valid {
			// definer is in the format `user`@`host`
			// split it into user and host
			parts := bytes.Split([]byte(definer.String), []byte{'@'})
			if len(parts) == 2 {
				definerUser = string(parts[0])
				definerHost = string(parts[1])
			} else {
				definerUser = definer.String
				definerHost = "%"
			}
		}
		triggers[table.String] = append(triggers[table.String], `
/*!50003 SET @saved_cs_client      = @@character_set_client */ ;
/*!50003 SET @saved_cs_results     = @@character_set_results */ ;
/*!50003 SET @saved_col_connection = @@collation_connection */ ;
/*!50003 SET character_set_client  = latin1 */ ;
/*!50003 SET character_set_results = latin1 */ ;
/*!50003 SET collation_connection  = latin1_swedish_ci */ ;
/*!50003 SET @saved_sql_mode       = @@sql_mode */ ;
/*!50003 SET sql_mode              = 'ONLY_FULL_GROUP_BY,STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION' */ ;
DELIMITER ;;
`+fmt.Sprintf("/*!50003 CREATE*/ /*!50017 DEFINER=`%s`@`%s`*/ /*!50003 TRIGGER `%s` AFTER %s ON `%s` FOR EACH ROW %s */;;", definerUser, definerHost, triggerName.String, event.String, table.String, statement.String)+`
DELIMITER ;
/*!50003 SET sql_mode              = @saved_sql_mode */ ;
/*!50003 SET character_set_client  = @saved_cs_client */ ;
/*!50003 SET character_set_results = @saved_cs_results */ ;
/*!50003 SET collation_connection  = @saved_col_connection */ ;
`)
	}
	return triggers, nil
}

// dumpFunctions dump the functions for the current schema
func (data *Data) dumpFunctions() error {
	return data.dumpProceduresOrFunctions("FUNCTION")
}

// dumpProcedures dump the procedures for the current schema
func (data *Data) dumpProcedures() error {
	return data.dumpProceduresOrFunctions("PROCEDURE")
}

// dumpProceduresOrFunctions dump the procedures or functions for the current schema
func (data *Data) dumpProceduresOrFunctions(t string) error {
	createQueries, err := data.getProceduresOrFunctionsCreateQueries(t)
	if err != nil {
		return err
	}
	for _, createQuery := range createQueries {
		var name, sqlMode, createStmt, charset, collationConnection, databaseCollation sql.NullString
		if err := data.tx.QueryRow(createQuery).Scan(&name, &sqlMode, &createStmt, &charset, &collationConnection, &databaseCollation); err != nil {
			return err
		}
		if createStmt.Valid {
			// TODO: the first line should only be there if it is full, not compact
			var sql string
			if !data.Compact {
				sql = fmt.Sprintf(`
/*!50003 DROP %s IF EXISTS `+"`%s`"+` */;
`, t, name.String)
			}
			sql += fmt.Sprintf(`
/*!50003 SET @saved_cs_client      = @@character_set_client */ ;
/*!50003 SET @saved_cs_results     = @@character_set_results */ ;
/*!50003 SET @saved_col_connection = @@collation_connection */ ;
/*!50003 SET character_set_client  = latin1 */ ;
/*!50003 SET character_set_results = latin1 */ ;
/*!50003 SET collation_connection  = latin1_swedish_ci */ ;
/*!50003 SET @saved_sql_mode       = @@sql_mode */ ;
/*!50003 SET sql_mode              = 'ONLY_FULL_GROUP_BY,STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION' */ ;
DELIMITER ;;
%s ;;
DELIMITER ;
/*!50003 SET sql_mode              = @saved_sql_mode */ ;
/*!50003 SET character_set_client  = @saved_cs_client */ ;
/*!50003 SET character_set_results = @saved_cs_results */ ;
/*!50003 SET collation_connection  = @saved_col_connection */ ;
`, createStmt.String)
			if _, err := data.Out.Write([]byte(sql)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (data *Data) getProceduresOrFunctionsCreateQueries(t string) ([]string, error) {
	query := fmt.Sprintf("SHOW %s STATUS WHERE Db = '%s'", t, data.Schema)
	var toGet []string
	rows, err := data.tx.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	//  | Db     | Name       | Type      | Language | Definer | Modified            | Created             | Security_type | Comment | character_set_client | collation_connection | Database Collation |
	for rows.Next() {
		var (
			db, name, typeDef, language, definer, securityType, comment, charset, collationConnection, databaseCollation sql.NullString
			created, modified                                                                                            sql.NullTime
		)
		if err := rows.Scan(&db, &name, &typeDef, &language, &definer, &modified, &created, &securityType, &comment, &charset, &collationConnection, &databaseCollation); err != nil {
			return nil, err
		}
		if name.Valid && typeDef.Valid {
			createQuery := fmt.Sprintf("SHOW CREATE %s `%s`", typeDef.String, name.String)
			toGet = append(toGet, createQuery)
		}
	}
	return toGet, nil
}

// getBackupLockCommand returns the SQL command to lock the tables for backup
// It may vary depending on the database variant or version, so it is generated dynamically
func (data *Data) getBackupLockCommand(tables, views []Table) (string, error) {
	dbVar, err := dbutil.DetectVariant(data.Connection)
	if err != nil {
		return "", fmt.Errorf("failed to determine database variant: %w", err)
	}
	var lockString string
	switch dbVar {
	case dbutil.VariantMariaDB:
		lockString = "LOCK TABLES"
	case dbutil.VariantMySQL:
		lockString = "LOCK TABLES"
	case dbutil.VariantPercona:
		// Percona just use the simple LOCK TABLES FOR BACKUP command
		return "LOCK TABLES FOR BACKUP", nil
	default:
		lockString = "LOCK TABLES"
	}
	var b bytes.Buffer
	b.WriteString(lockString + " ")
	for index, table := range append(tables, views...) {
		if index != 0 {
			b.WriteString(",")
		}
		b.WriteString("`" + table.Name() + "` READ /*!32311 LOCAL */")
	}
	return b.String(), nil
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

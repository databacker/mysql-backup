package mysql

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"text/template"
)

var tableFullTemplate, tableCompactTemplate *template.Template

func init() {
	tmpl, err := template.New("mysqldumpTable").Funcs(template.FuncMap{
		"sub": sub,
		"esc": esc,
	}).Parse(tableTmpl)
	if err != nil {
		panic(fmt.Errorf("could not parse table template: %w", err))
	}
	tableFullTemplate = tmpl

	tmpl, err = template.New("mysqldumpTableCompact").Funcs(template.FuncMap{
		"sub": sub,
		"esc": esc,
	}).Parse(tableTmplCompact)
	if err != nil {
		panic(fmt.Errorf("could not parse table compact template: %w", err))
	}
	tableCompactTemplate = tmpl
}

type Table interface {
	Name() string
	Err() error
	Database() string
	Columns() []string
	Init() error
	Start() error
	Next() bool
	RowValues() string
	RowBuffer() *bytes.Buffer
	Execute(io.Writer, bool, int) error
	Stream() <-chan string
}

var _ Table = &baseTable{}

type baseTable struct {
	name string
	err  error

	cols     []string
	data     *Data
	rows     *sql.Rows
	database string
	values   []interface{}
}

func (table *baseTable) Name() string {
	return table.name
}

func (table *baseTable) Err() error {
	return table.err
}

func (table *baseTable) Columns() []string {
	return table.cols
}
func (table *baseTable) Database() string {
	return table.database
}

func (table *baseTable) CreateSQL() ([]string, error) {
	var tableReturn, tableSQL sql.NullString
	if err := table.data.tx.QueryRow("SHOW CREATE TABLE "+esc(table.Name())).Scan(&tableReturn, &tableSQL); err != nil {
		return nil, err
	}

	if tableReturn.String != table.name {
		return nil, errors.New("returned table is not the same as requested table")
	}

	return []string{strings.TrimSpace(tableSQL.String)}, nil
}

func (table *baseTable) initColumnData() error {
	colInfo, err := table.data.tx.Query("SHOW COLUMNS FROM " + esc(table.Name()))
	if err != nil {
		return err
	}
	defer func() { _ = colInfo.Close() }()

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

		// Ignore the virtual columns and generated columns
		// if there is an Extra column and it is a valid string, then only include this column if
		// the column is not marked as VIRTUAL or GENERATED
		// Unless IncludeGeneratedColumns is true, in which case include them
		shouldInclude := true
		if info[extraIndex].Valid {
			extra := info[extraIndex].String
			if strings.Contains(extra, "VIRTUAL") {
				shouldInclude = false
			} else if strings.Contains(extra, "GENERATED") && !table.data.IncludeGeneratedColumns {
				shouldInclude = false
			}
		}
		if shouldInclude {
			result = append(result, info[fieldIndex].String)
		}
	}
	table.cols = result
	return nil
}

func (table *baseTable) columnsList() string {
	return "`" + strings.Join(table.cols, "`, `") + "`"
}

func (table *baseTable) Init() error {
	return table.initColumnData()
}

func (table *baseTable) Start() error {
	if table.rows != nil {
		return errors.New("can't start twice")
	}

	if len(table.cols) == 0 {
		// No data to dump since this is a virtual table
		return nil
	}

	var err error
	table.rows, err = table.data.tx.Query("SELECT " + table.columnsList() + " FROM " + esc(table.Name()))
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

func (table *baseTable) Next() bool {
	if table.rows == nil {
		if err := table.Start(); err != nil {
			table.err = err
			return false
		}
	}
	// Fallthrough
	if table.rows.Next() {
		if err := table.rows.Scan(table.values...); err != nil {
			table.err = err
			return false
		} else if err := table.rows.Err(); err != nil {
			table.err = err
			return false
		}
	} else {
		_ = table.rows.Close()
		table.rows = nil
		return false
	}
	return true
}

func (table *baseTable) RowValues() string {
	return table.RowBuffer().String()
}

func (table *baseTable) RowBuffer() *bytes.Buffer {
	var b bytes.Buffer
	b.WriteString("(")

	for key, value := range table.values {
		if key != 0 {
			b.WriteString(",")
		}
		// Scan() returns all of the following types, according to https://pkg.go.dev/database/sql#Rows.Scan
		//*string
		//*[]byte
		//*int, *int8, *int16, *int32, *int64
		//*uint, *uint8, *uint16, *uint32, *uint64
		//*bool
		//*float32, *float64
		//*interface{}
		//*RawBytes
		//*Rows (cursor value)
		//any type implementing Scanner (see Scanner docs) (i.e. the Null* types)

		switch s := value.(type) {
		case nil:
			b.WriteString(nullType)
		case *string:
			fmt.Fprintf(&b, "'%s'", sanitize(*s))
		case *int:
			fmt.Fprintf(&b, "%d", *s)
		case *int8:
			fmt.Fprintf(&b, "%d", *s)
		case *int16:
			fmt.Fprintf(&b, "%d", *s)
		case *int32:
			fmt.Fprintf(&b, "%d", *s)
		case *int64:
			fmt.Fprintf(&b, "%d", *s)
		case *uint:
			fmt.Fprintf(&b, "%d", *s)
		case *uint8:
			fmt.Fprintf(&b, "%d", *s)
		case *uint16:
			fmt.Fprintf(&b, "%d", *s)
		case *uint32:
			fmt.Fprintf(&b, "%d", *s)
		case *uint64:
			fmt.Fprintf(&b, "%d", *s)
		case *float32:
			fmt.Fprintf(&b, "%f", *s)
		case *float64:
			fmt.Fprintf(&b, "%f", *s)
		case *bool:
			if *s {
				b.WriteString("1")
			} else {
				b.WriteString("0")
			}
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
		case *NullDate:
			if s.Valid {
				fmt.Fprintf(&b, "'%s'", sanitize(s.Date.Format("2006-01-02")))
			} else {
				b.WriteString(nullType)
			}
		case *sql.NullTime:
			if s.Valid {
				fmt.Fprintf(&b, "'%s'", sanitize(s.Time.Format("2006-01-02 15:04:05")))
			} else {
				b.WriteString(nullType)
			}
		default:
			fmt.Fprintf(&b, "'%s'", value)
		}
	}
	b.WriteString(")")

	return &b
}

func (table *baseTable) Stream() <-chan string {
	valueOut := make(chan string, 1)
	go func() {
		defer close(valueOut)
		var insert bytes.Buffer

		for table.Next() {
			b := table.RowBuffer()
			// If we're skipping extended inserts, we write each row individually
			if table.data.SkipExtendedInsert {
				_, _ = fmt.Fprint(&insert, strings.Join(
					// extra "" at the end so we get an extra whitespace as needed
					[]string{"INSERT", "INTO", esc(table.Name()), "(" + table.columnsList() + ")", "VALUES", ""},
					" "))
				_, _ = b.WriteTo(&insert)
				_, _ = insert.WriteString(";")
				valueOut <- insert.String()
				insert.Reset()
				continue
			}

			// Truncate our insert if it won't fit
			if insert.Len() != 0 && insert.Len()+b.Len() > table.data.MaxAllowedPacket-1 {
				_, _ = insert.WriteString(";")
				valueOut <- insert.String()
				insert.Reset()
			}

			if insert.Len() == 0 {
				_, _ = fmt.Fprint(&insert, strings.Join(
					// extra "" at the end so we get an extra whitespace as needed
					[]string{"INSERT", "INTO", esc(table.Name()), "(" + table.columnsList() + ")", "VALUES", ""},
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

func (table *baseTable) Execute(out io.Writer, compact bool, part int) error {
	if part > 0 {
		return fmt.Errorf("part %d is not supported for tables", part)
	}
	tmpl := tableFullTemplate
	if compact {
		tmpl = tableCompactTemplate
	}
	return tmpl.Execute(out, table)
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
	case "TIMESTAMP", "DATETIME":
		return reflect.TypeOf(sql.NullTime{})
	case "DATE":
		return reflect.TypeOf(NullDate{})
	case "TIME":
		return reflect.TypeOf(sql.NullString{})
	case "JSON":
		return reflect.TypeOf(sql.NullString{})
	}

	// unknown datatype
	return tp.ScanType()
}

// Takes a Table, but is a baseTable
const tableTmpl = `
--
-- Table structure for table {{ esc .Name }}
--

DROP TABLE IF EXISTS {{ esc .Name }};
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
{{ index .CreateSQL 0 }};
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Dumping data for table {{ esc .Name }}
--

LOCK TABLES {{ esc .Name }} WRITE;
/*!40000 ALTER TABLE {{ esc .Name }} DISABLE KEYS */;
{{ range $value := .Stream }}
{{- $value }}
{{ end -}}
/*!40000 ALTER TABLE {{ esc .Name }} ENABLE KEYS */;
UNLOCK TABLES;
`

const tableTmplCompact = `
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
{{ index .CreateSQL 0 }};
/*!40101 SET character_set_client = @saved_cs_client */;
{{ range $value := .Stream }}{{- $value }}{{ end -}}
`

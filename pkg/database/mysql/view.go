package mysql

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/template"
)

type view struct {
	baseTable
	charset   string
	collation string
}

var viewFullTemplate, viewCompactTemplate *template.Template

func init() {
	tmpl, err := template.New("mysqldumpView").Funcs(template.FuncMap{
		"sub": sub,
		"esc": esc,
	}).Parse(viewTmpl)
	if err != nil {
		panic(fmt.Errorf("could not parse view template: %w", err))
	}
	viewFullTemplate = tmpl

	tmpl, err = template.New("mysqldumpViewCompact").Funcs(template.FuncMap{
		"sub": sub,
		"esc": esc,
	}).Parse(viewTmplCompact)
	if err != nil {
		panic(fmt.Errorf("could not parse view compact template: %w", err))
	}
	viewCompactTemplate = tmpl
}

func (v *view) CreateSQL() ([]string, error) {
	var tableReturn, tableSQL, charSetClient, collationConnection sql.NullString
	if err := v.data.tx.QueryRow("SHOW CREATE VIEW "+esc(v.Name())).Scan(&tableReturn, &tableSQL, &charSetClient, &collationConnection); err != nil {
		return nil, err
	}

	if tableReturn.String != v.Name() {
		return nil, errors.New("returned view is not the same as requested view")
	}

	// this comes in one string, which we need to break down into 3 parts for the template
	// CREATE ALGORITHM=UNDEFINED DEFINER=`testadmin`@`%` SQL SECURITY DEFINER VIEW `view1` AS select `t1`.`id` AS `id`,`t1`.`name` AS `name` from `t1`
	// becomes:
	// CREATE ALGORITHM=UNDEFINED
	// DEFINER=`testadmin`@`%` SQL SECURITY DEFINER
	// VIEW `view1` AS select `t1`.`id` AS `id`,`t1`.`name` AS `name` from `t1`
	in := tableSQL.String
	indexDefiner := strings.Index(in, "DEFINER")
	indexView := strings.Index(in, "VIEW")

	parts := make([]string, 3)
	parts[0] = strings.TrimSpace(in[:indexDefiner])
	parts[1] = strings.TrimSpace(in[indexDefiner:indexView])
	parts[2] = strings.TrimSpace(in[indexView:])

	v.charset = charSetClient.String
	v.collation = collationConnection.String

	return parts, nil
}

// SELECT TABLE_NAME,CHARACTER_SET_CLIENT,COLLATION_CONNECTION FROM INFORMATION_SCHEMA.VIEWS;
func (v *view) Init() error {
	if err := v.initColumnData(); err != nil {
		return fmt.Errorf("failed to initialize column data for view %s: %w", v.name, err)
	}
	var tableName, charSetClient, collationConnection sql.NullString

	if err := v.data.tx.QueryRow("SELECT TABLE_NAME,CHARACTER_SET_CLIENT,COLLATION_CONNECTION FROM INFORMATION_SCHEMA.VIEWS WHERE table_name = '"+v.name+"'").Scan(&tableName, &charSetClient, &collationConnection); err != nil {
		return fmt.Errorf("failed to get view information schema for view %s: %w", v.name, err)
	}
	if tableName.String != v.name {
		return fmt.Errorf("returned view name %s is not the same as requested view %s", tableName.String, v.name)
	}
	if !charSetClient.Valid {
		return fmt.Errorf("returned charset is not valid for view %s", v.name)
	}
	if !collationConnection.Valid {
		return fmt.Errorf("returned collation is not valid for view %s", v.name)
	}
	v.charset = charSetClient.String
	v.collation = collationConnection.String
	return nil
}

func (v *view) Execute(out io.Writer, compact bool) error {
	tmpl := viewFullTemplate
	if compact {
		tmpl = viewCompactTemplate
	}
	return tmpl.Execute(out, v)
}

func (v *view) Charset() string {
	return v.charset
}

func (v *view) Collation() string {
	return v.collation
}

// takes a Table, but is a view
const viewTmpl = `
--
-- Temporary view structure for view {{ esc .Name }}
--

DROP TABLE IF EXISTS {{ esc .Name }};
/*!50001 DROP VIEW IF EXISTS {{ esc .Name }}*/;
SET @saved_cs_client     = @@character_set_client;
/*!50503 SET character_set_client = utf8mb4 */;
/*!50001 CREATE VIEW {{ esc .Name }} AS SELECT 
{{ $columns := .Columns }}{{ range $index, $column := .Columns }} 1 AS {{ esc $column }}{{ if ne $index (sub (len $columns) 1) }},{{ printf "%c" 10 }}{{ else }}*/;{{ end }}{{ end }}
SET character_set_client = @saved_cs_client;

--
-- Current Database: {{ esc .Database }}
--

USE {{ esc .Database }};

--
-- Final view structure for view {{ esc .Name }}
--

/*!50001 DROP VIEW IF EXISTS {{ esc .Name }}*/;
/*!50001 SET @saved_cs_client          = @@character_set_client */;
/*!50001 SET @saved_cs_results         = @@character_set_results */;
/*!50001 SET @saved_col_connection     = @@collation_connection */;
/*!50001 SET character_set_client      = {{ .Charset }} */;
/*!50001 SET character_set_results     = {{ .Charset }} */;
/*!50001 SET collation_connection      = {{ .Collation }} */;
/*!50001 {{ $sql := .CreateSQL }}{{ index $sql 0 }} */
/*!50013 {{ index $sql 1 }} */
/*!50001 {{ index $sql 2 }} */;
/*!50001 SET character_set_client      = @saved_cs_client */;
/*!50001 SET character_set_results     = @saved_cs_results */;
/*!50001 SET collation_connection      = @saved_col_connection */;
`
const viewTmplCompact = `
SET @saved_cs_client     = @@character_set_client;
/*!50503 SET character_set_client = utf8mb4 */;
/*!50001 CREATE VIEW {{ esc .Name }} AS SELECT 
{{ $columns := .Columns }}{{ range $index, $column := .Columns }} 1 AS {{ esc $column }}{{ if ne $index (sub (len $columns) 1) }},{{ printf "%c" 10 }}{{ else }}*/;{{ end }}{{ end }}
SET character_set_client = @saved_cs_client;

USE {{ esc .Database }};
/*!50001 DROP VIEW IF EXISTS {{ esc .Name }}*/;
/*!50001 SET @saved_cs_client          = @@character_set_client */;
/*!50001 SET @saved_cs_results         = @@character_set_results */;
/*!50001 SET @saved_col_connection     = @@collation_connection */;
/*!50001 SET character_set_client      = {{ .Charset }} */;
/*!50001 SET character_set_results     = {{ .Charset }} */;
/*!50001 SET collation_connection      = {{ .Collation }} */;
/*!50001 {{ $sql := .CreateSQL }}{{ index $sql 0 }} */
/*!50013 {{ index $sql 1 }} */
/*!50001 {{ index $sql 2 }} */;
/*!50001 SET character_set_client      = @saved_cs_client */;
/*!50001 SET character_set_results     = @saved_cs_results */;
/*!50001 SET collation_connection      = @saved_col_connection */;
`

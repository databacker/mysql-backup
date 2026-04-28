package mysql

import "testing"

func TestIsIgnoredTable(t *testing.T) {
	tests := []struct {
		name         string
		schema       string
		ignoreTables []string
		tableName    string
		expected     bool
	}{
		{"exact table name match", "mydb", []string{"mytable"}, "mytable", true},
		{"table name no match", "mydb", []string{"othertable"}, "mytable", false},
		{"qualified match same schema", "backuppc", []string{"backuppc.hosts"}, "hosts", true},
		{"qualified match wrong schema", "otherdb", []string{"backuppc.hosts"}, "hosts", false},
		{"qualified match wrong table", "backuppc", []string{"backuppc.hosts"}, "summary", false},
		{"multiple entries with qualified match", "backuppc", []string{"otherdb.foo", "backuppc.hosts"}, "hosts", true},
		{"multiple entries no match", "backuppc", []string{"otherdb.foo", "otherdb.bar"}, "hosts", false},
		{"mixed qualified and unqualified", "mydb", []string{"backuppc.hosts", "globaltable"}, "globaltable", true},
		{"empty ignore list", "mydb", []string{}, "mytable", false},
		{"nil ignore list", "mydb", nil, "mytable", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &Data{
				Schema:       tt.schema,
				IgnoreTables: tt.ignoreTables,
			}
			got := data.isIgnoredTable(tt.tableName)
			if got != tt.expected {
				t.Errorf("isIgnoredTable(%q) = %v, want %v (schema=%q, ignoreTables=%v)",
					tt.tableName, got, tt.expected, tt.schema, tt.ignoreTables)
			}
		})
	}
}

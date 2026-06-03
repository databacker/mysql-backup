package core

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/databacker/api/go/api"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// BuildSelectionMode derives the selection mode from the configured include and
// exclude lists:
//
//	include — one or more databases are explicitly named for inclusion.
//	exclude — all databases except a named subset (include is empty, exclude non-empty).
//	all     — entire server/cluster (both lists are empty).
func BuildSelectionMode(include, exclude []string) api.BackupProtectedTargetSelectionMode {
	if len(include) > 0 {
		return api.BackupProtectedTargetSelectionModeInclude
	}
	if len(exclude) > 0 {
		return api.BackupProtectedTargetSelectionModeExclude
	}
	return api.BackupProtectedTargetSelectionModeAll
}

// BuildProtectedTargetIdentity produces a stable, deterministic identity string
// for a MySQL protected target.
//
// Format: mysql:{mode}:{native_id}[:{sorted_db1}:{sorted_db2}:...]
//
// For include and exclude modes, the sorted configured database names are
// appended as additional colon-separated components. nativeID may be empty
// if the server UUID could not be retrieved.
//
// The same configured target backed up by different engine instances will
// produce the same identity when connected to the same MySQL server with the
// same selection mode and configured database list.
func BuildProtectedTargetIdentity(mode api.BackupProtectedTargetSelectionMode, nativeID string, configuredDBNames []string) string {
	parts := []string{"mysql", string(mode), nativeID}
	if (mode == api.BackupProtectedTargetSelectionModeInclude || mode == api.BackupProtectedTargetSelectionModeExclude) && len(configuredDBNames) > 0 {
		sorted := make([]string, len(configuredDBNames))
		copy(sorted, configuredDBNames)
		sort.Strings(sorted)
		parts = append(parts, sorted...)
	}
	return strings.Join(parts, ":")
}

// SetRunSpanProtectedTargetAttrs sets the configuration-based protected-target
// attributes on the root run span:
//
//	backup.protected_target.selection_mode    — how databases are selected
//	backup.protected_target.configured_databases — the include/exclude list (omitted for all mode)
//	backup.protected_target.identity          — stable deterministic identity
//	db.server.native_id                       — MySQL server_uuid (when available)
func SetRunSpanProtectedTargetAttrs(span trace.Span, mode api.BackupProtectedTargetSelectionMode, nativeID string, configuredDBNames []string) {
	identity := BuildProtectedTargetIdentity(mode, nativeID, configuredDBNames)
	attrs := []attribute.KeyValue{
		attribute.String(string(api.BackupAttrProtectedTargetSelectionMode), string(mode)),
		attribute.String(string(api.BackupAttrProtectedTargetIdentity), identity),
	}
	if nativeID != "" {
		attrs = append(attrs, attribute.String(string(api.BackupAttrDBServerNativeID), nativeID))
	}
	if (mode == api.BackupProtectedTargetSelectionModeInclude || mode == api.BackupProtectedTargetSelectionModeExclude) && len(configuredDBNames) > 0 {
		sorted := make([]string, len(configuredDBNames))
		copy(sorted, configuredDBNames)
		sort.Strings(sorted)
		dbJSON, _ := json.Marshal(sorted)
		attrs = append(attrs, attribute.String(string(api.BackupAttrProtectedTargetConfiguredDatabases), string(dbJSON)))
	}
	span.SetAttributes(attrs...)
}

// SetDumpSpanProtectedTargetAttrs sets protected-target attributes on dump and
// database_dump spans. It emits selection_mode, identity, and native_id
// (derived from the configuration) plus the actual database_count and databases
// resolved at runtime after schema discovery and exclude-list filtering.
func SetDumpSpanProtectedTargetAttrs(span trace.Span, mode api.BackupProtectedTargetSelectionMode, nativeID string, configuredDBNames []string, actualDBNames []string) {
	identity := BuildProtectedTargetIdentity(mode, nativeID, configuredDBNames)
	attrs := []attribute.KeyValue{
		attribute.String(string(api.BackupAttrProtectedTargetSelectionMode), string(mode)),
		attribute.String(string(api.BackupAttrProtectedTargetIdentity), identity),
	}
	if nativeID != "" {
		attrs = append(attrs, attribute.String(string(api.BackupAttrDBServerNativeID), nativeID))
	}
	if len(actualDBNames) > 0 {
		sorted := make([]string, len(actualDBNames))
		copy(sorted, actualDBNames)
		sort.Strings(sorted)
		dbJSON, _ := json.Marshal(sorted)
		attrs = append(attrs,
			attribute.Int(string(api.BackupAttrProtectedTargetDatabaseCount), len(sorted)),
			attribute.String(string(api.BackupAttrProtectedTargetDatabases), string(dbJSON)),
		)
	}
	span.SetAttributes(attrs...)
}

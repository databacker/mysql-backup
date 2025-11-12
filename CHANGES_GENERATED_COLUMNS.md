# Add Configuration Option to Include Generated Columns

## Overview
Added a new configuration key `include-generated-columns` (or environment variable `DB_DUMP_INCLUDE_GENERATED_COLUMNS`) to allow users to control whether generated columns should be included in database dumps.

## Problem
Previously, the code would always exclude columns marked as `VIRTUAL` or `GENERATED` in the "Extra" field of MySQL's `SHOW COLUMNS` output. This was hardcoded behavior with no way to override it.

## Solution
Implemented a configurable option to control this behavior by:

1. Adding a new `IncludeGeneratedColumns` boolean field throughout the configuration chain
2. Modifying the column filtering logic to respect this setting

## Files Modified

### 1. `pkg/database/mysql/dump.go`
- Added `IncludeGeneratedColumns bool` field to the `Data` struct
- This field controls whether generated columns should be included in dumps

### 2. `pkg/database/mysql/table.go`
- Modified `initColumnData()` function to use `IncludeGeneratedColumns` setting
- **Before**: Columns with `GENERATED` or `VIRTUAL` were always excluded
- **After**: 
  - Columns with `VIRTUAL` are always excluded (computed columns that can't be stored)
  - Columns with `GENERATED` are excluded UNLESS `IncludeGeneratedColumns` is `true`

### 3. `pkg/database/dump.go`
- Added `IncludeGeneratedColumns bool` to `DumpOpts` struct
- Updated the `Dump()` function to pass this setting to `mysql.Data`

### 4. `pkg/core/dumpoptions.go`
- Added `IncludeGeneratedColumns bool` to `DumpOptions` struct
- This carries the setting from CLI/config through to the database layer

### 5. `pkg/core/dump.go`
- Updated `Dump()` function to include `IncludeGeneratedColumns` when creating `database.DumpOpts`

### 6. `cmd/dump.go`
- Added `--include-generated-columns` CLI flag (boolean, defaults to false)
- Added parsing logic to read the flag value
- Added `includeGeneratedColumns` variable to the command execution path
- Passed the setting to `core.DumpOptions` struct

## Usage

### Command Line
```bash
# Enable inclusion of generated columns in dump
db-backup dump --server myhost --target file:///backups --include-generated-columns

# Or using environment variable
export DB_DUMP_INCLUDE_GENERATED_COLUMNS=true
db-backup dump --server myhost --target file:///backups
```

### Default Behavior
By default (`--include-generated-columns` is not set), the behavior is unchanged:
- `VIRTUAL` columns are excluded
- `GENERATED` columns are excluded

### With the Flag
When `--include-generated-columns` is set:
- `VIRTUAL` columns are still excluded (they cannot be restored)
- `GENERATED` columns are included in the dump

## Environment Variable
The setting can also be controlled via environment variable:
- **Variable name**: `DB_DUMP_INCLUDE_GENERATED_COLUMNS`
- **Values**: `true` or `false`
- **Example**: `export DB_DUMP_INCLUDE_GENERATED_COLUMNS=true`

## Notes
- Config file support would require updates to the external `api.Dump` type in the databacker/api repository
- The change maintains backward compatibility; existing behavior is preserved when the flag is not used
- `VIRTUAL` columns are always excluded as they cannot be dumped and restored

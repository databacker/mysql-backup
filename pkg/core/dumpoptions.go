package core

import (
	"github.com/databacker/mysql-backup/pkg/compression"
	"github.com/databacker/mysql-backup/pkg/database"
	"github.com/databacker/mysql-backup/pkg/storage"
	"github.com/google/uuid"
)

type DumpOptions struct {
	Targets             []storage.Storage
	Safechars           bool
	DBNames             []string
	DBConn              database.Connection
	Compressor          compression.Compressor
	Exclude             []string
	PreBackupScripts    string
	PostBackupScripts   string
	Compact             bool
	SuppressUseDatabase bool
	MaxAllowedPacket    int
	Run                 uuid.UUID
	FilenamePattern     string
}

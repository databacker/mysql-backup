package core

import (
	"github.com/databacker/mysql-backup/pkg/compression"
	"github.com/databacker/mysql-backup/pkg/database"
	"github.com/databacker/mysql-backup/pkg/storage"
	"github.com/google/uuid"
)

type RestoreOptions struct {
	Target       storage.Storage
	TargetFile   string
	DBConn       database.Connection
	DatabasesMap map[string]string
	Compressor   compression.Compressor
	Run          uuid.UUID
}

package core

import (
	"time"

	"github.com/databacker/mysql-backup/pkg/storage"
	"github.com/google/uuid"
)

type PruneOptions struct {
	Targets   []storage.Storage
	Retention string
	Now       time.Time
	Run       uuid.UUID
}

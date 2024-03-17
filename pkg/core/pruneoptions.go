package core

import (
	"time"

	"github.com/databacker/mysql-backup/pkg/storage"
)

type PruneOptions struct {
	Targets   []storage.Storage
	Retention string
	Now       time.Time
}

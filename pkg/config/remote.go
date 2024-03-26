package config

import (
	"github.com/databacker/mysql-backup/pkg/remote"
)

type RemoteSpec struct {
	remote.Connection
}

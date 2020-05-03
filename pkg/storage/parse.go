package storage

import (
	"fmt"

	"github.com/databacker/mysql-backup/pkg/storage/credentials"
	"github.com/databacker/mysql-backup/pkg/storage/file"
	"github.com/databacker/mysql-backup/pkg/storage/s3"
	"github.com/databacker/mysql-backup/pkg/storage/smb"
	"github.com/databacker/mysql-backup/pkg/util"
)

func ParseURL(url string, creds credentials.Creds) (Storage, error) {
	// parse the target URL
	u, err := util.SmartParse(url)
	if err != nil {
		return nil, fmt.Errorf("invalid target url%v", err)
	}

	// do the upload
	var store Storage
	switch u.Scheme {
	case "file":
		store = file.New(*u)
	case "smb":
		opts := []smb.Option{}
		if creds.SMBCredentials.Domain != "" {
			opts = append(opts, smb.WithDomain(creds.SMBCredentials.Domain))
		}
		if creds.SMBCredentials.Username != "" {
			opts = append(opts, smb.WithUsername(creds.SMBCredentials.Username))
		}
		if creds.SMBCredentials.Password != "" {
			opts = append(opts, smb.WithPassword(creds.SMBCredentials.Password))
		}
		store = smb.New(*u, opts...)
	case "s3":
		opts := []s3.Option{}
		if creds.AWSEndpoint != "" {
			opts = append(opts, s3.WithEndpoint(creds.AWSEndpoint))
		}
		store = s3.New(*u, opts...)
	default:
		return nil, fmt.Errorf("unknown url protocol: %s", u.Scheme)
	}
	return store, nil
}

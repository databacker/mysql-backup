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
		if creds.SMB.Domain != "" {
			opts = append(opts, smb.WithDomain(creds.SMB.Domain))
		}
		if creds.SMB.Username != "" {
			opts = append(opts, smb.WithUsername(creds.SMB.Username))
		}
		if creds.SMB.Password != "" {
			opts = append(opts, smb.WithPassword(creds.SMB.Password))
		}
		store = smb.New(*u, opts...)
	case "s3":
		opts := []s3.Option{}
		if creds.AWS.Endpoint != "" {
			opts = append(opts, s3.WithEndpoint(creds.AWS.Endpoint))
		}
		if creds.AWS.Region != "" {
			opts = append(opts, s3.WithRegion(creds.AWS.Region))
		}
		if creds.AWS.AccessKeyID != "" {
			opts = append(opts, s3.WithAccessKeyId(creds.AWS.AccessKeyID))
		}
		if creds.AWS.SecretAccessKey != "" {
			opts = append(opts, s3.WithSecretAccessKey(creds.AWS.SecretAccessKey))
		}
		if creds.AWS.S3UsePathStyle {
			opts = append(opts, s3.WithPathStyle())
		}
		store = s3.New(*u, opts...)
	default:
		return nil, fmt.Errorf("unknown url protocol: %s", u.Scheme)
	}
	return store, nil
}

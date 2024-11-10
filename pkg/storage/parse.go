package storage

import (
	"fmt"

	"github.com/databacker/api/go/api"
	"github.com/databacker/mysql-backup/pkg/storage/credentials"
	"github.com/databacker/mysql-backup/pkg/storage/file"
	"github.com/databacker/mysql-backup/pkg/storage/s3"
	"github.com/databacker/mysql-backup/pkg/storage/smb"
	"github.com/databacker/mysql-backup/pkg/util"
	"gopkg.in/yaml.v3"
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
		if creds.AWS.PathStyle {
			opts = append(opts, s3.WithPathStyle())
		}
		store = s3.New(*u, opts...)
	default:
		return nil, fmt.Errorf("unknown url protocol: %s", u.Scheme)
	}
	return store, nil
}

// FromTarget parse an api.Target and return something that implements the Storage interface
func FromTarget(target api.Target) (store Storage, err error) {
	u, err := util.SmartParse(target.URL)
	if err != nil {
		return nil, err
	}
	switch target.Type {
	case api.TargetTypeS3:
		var spec api.S3
		specBytes, err := yaml.Marshal(target.Spec)
		if err != nil {
			return nil, fmt.Errorf("error marshalling spec part of target: %w", err)
		}
		if err := yaml.Unmarshal(specBytes, &spec); err != nil {
			return nil, fmt.Errorf("parsed yaml had kind S3, but spec invalid")
		}

		opts := []s3.Option{}
		if spec.Region != nil && *spec.Region != "" {
			opts = append(opts, s3.WithRegion(*spec.Region))
		}
		if spec.Endpoint != nil && *spec.Endpoint != "" {
			opts = append(opts, s3.WithEndpoint(*spec.Endpoint))
		}
		if spec.AccessKeyID != nil && *spec.AccessKeyID != "" {
			opts = append(opts, s3.WithAccessKeyId(*spec.AccessKeyID))
		}
		if spec.SecretAccessKey != nil && *spec.SecretAccessKey != "" {
			opts = append(opts, s3.WithSecretAccessKey(*spec.SecretAccessKey))
		}
		store = s3.New(*u, opts...)
	case api.TargetTypeSmb:
		var spec api.SMB
		specBytes, err := yaml.Marshal(target.Spec)
		if err != nil {
			return nil, fmt.Errorf("error marshalling spec part of target: %w", err)
		}
		if err := yaml.Unmarshal(specBytes, &spec); err != nil {
			return nil, fmt.Errorf("parsed yaml had kind SMB, but spec invalid")
		}

		opts := []smb.Option{}
		if spec.Domain != nil && *spec.Domain != "" {
			opts = append(opts, smb.WithDomain(*spec.Domain))
		}
		if spec.Username != nil && *spec.Username != "" {
			opts = append(opts, smb.WithUsername(*spec.Username))
		}
		if spec.Password != nil && *spec.Password != "" {
			opts = append(opts, smb.WithPassword(*spec.Password))
		}
		store = smb.New(*u, opts...)
	case api.TargetTypeFile:
		store, err = ParseURL(target.URL, credentials.Creds{})
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown target type: %s", target.Type)
	}
	return store, nil
}

package config

import (
	"fmt"

	"github.com/databacker/mysql-backup/pkg/storage"
	"github.com/databacker/mysql-backup/pkg/storage/credentials"
	"github.com/databacker/mysql-backup/pkg/storage/s3"
	"github.com/databacker/mysql-backup/pkg/storage/smb"
	"github.com/databacker/mysql-backup/pkg/util"
	"gopkg.in/yaml.v3"
)

type logLevel string

//nolint:unused // we expect to use these going forward
const (
	configType = "config.databack.io"
	version    = "1"

	logLevelError   logLevel = "error"
	logLevelWarning logLevel = "warning"
	logLevelInfo    logLevel = "info"
	logLevelDebug   logLevel = "debug"
	logLevelTrace   logLevel = "trace"
	logLevelDefault logLevel = logLevelInfo
)

type Config struct {
	Type     string   `yaml:"type"`
	Version  string   `yaml:"version"`
	Logging  logLevel `yaml:"logging"`
	Dump     Dump     `yaml:"dump"`
	Restore  Restore  `yaml:"restore"`
	Database Database `yaml:"database"`
	Targets  Targets  `yaml:"targets"`
	Prune    Prune    `yaml:"prune"`
}

type Dump struct {
	Include          []string      `yaml:"include"`
	Exclude          []string      `yaml:"exclude"`
	Safechars        bool          `yaml:"safechars"`
	NoDatabaseName   bool          `yaml:"no-database-name"`
	Schedule         Schedule      `yaml:"schedule"`
	Compression      string        `yaml:"compression"`
	Compact          bool          `yaml:"compact"`
	MaxAllowedPacket int           `yaml:"max-allowed-packet"`
	TmpPath          string        `yaml:"tmp-path"`
	FilenamePattern  string        `yaml:"filename-pattern"`
	Scripts          BackupScripts `yaml:"scripts"`
	Targets          []string      `yaml:"targets"`
}

type Prune struct {
	Retention string `yaml:"retention"`
}

type Schedule struct {
	Once      bool   `yaml:"once"`
	Cron      string `yaml:"cron"`
	Frequency int    `yaml:"frequency"`
	Begin     string `yaml:"begin"`
}

type BackupScripts struct {
	PreBackup  string `yaml:"pre-backup"`
	PostBackup string `yaml:"post-backup"`
}

type Restore struct {
	Scripts RestoreScripts `yaml:"scripts"`
}

type RestoreScripts struct {
	PreRestore  string `yaml:"pre-restore"`
	PostRestore string `yaml:"post-restore"`
}

type Database struct {
	Server      string        `yaml:"server"`
	Port        int           `yaml:"port"`
	Credentials DBCredentials `yaml:"credentials"`
}

type DBCredentials struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

var _ yaml.Unmarshaler = &Target{}

type Targets map[string]Target

type Target struct {
	Storage
}

type Storage interface {
	Storage() (storage.Storage, error) // convert to a storage.Storage instance
}

func (t *Target) UnmarshalYAML(n *yaml.Node) error {
	type T struct {
		Type    string    `yaml:"type"`
		URL     string    `yaml:"url"`
		Details yaml.Node `yaml:",inline"`
	}
	obj := &T{}
	if err := n.Decode(obj); err != nil {
		return err
	}
	// based on the type, load the rest of the data
	switch obj.Type {
	case "s3":
		var s3Target S3Target
		if err := n.Decode(&s3Target); err != nil {
			return err
		}
		t.Storage = s3Target
	case "smb":
		var smbTarget SMBTarget
		if err := n.Decode(&smbTarget); err != nil {
			return err
		}
		t.Storage = smbTarget
	case "file":
		var fileTarget FileTarget
		if err := n.Decode(&fileTarget); err != nil {
			return err
		}
		t.Storage = fileTarget
	default:
		return fmt.Errorf("unknown target type: %s", obj.Type)
	}

	return nil
}

type S3Target struct {
	Type        string         `yaml:"type"`
	URL         string         `yaml:"url"`
	Region      string         `yaml:"region"`
	Endpoint    string         `yaml:"endpoint"`
	Credentials AWSCredentials `yaml:"credentials"`
}

func (s S3Target) Storage() (storage.Storage, error) {
	u, err := util.SmartParse(s.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid target url%v", err)
	}
	opts := []s3.Option{}
	if s.Region != "" {
		opts = append(opts, s3.WithRegion(s.Region))
	}
	if s.Endpoint != "" {
		opts = append(opts, s3.WithEndpoint(s.Endpoint))
	}
	if s.Credentials.AccessKeyId != "" {
		opts = append(opts, s3.WithAccessKeyId(s.Credentials.AccessKeyId))
	}
	if s.Credentials.SecretAccessKey != "" {
		opts = append(opts, s3.WithSecretAccessKey(s.Credentials.SecretAccessKey))
	}
	store := s3.New(*u, opts...)
	return store, nil
}

type AWSCredentials struct {
	AccessKeyId     string `yaml:"access-key-id"`
	SecretAccessKey string `yaml:"secret-access-key"`
}

type SMBTarget struct {
	Type        string         `yaml:"type"`
	URL         string         `yaml:"url"`
	Credentials SMBCredentials `yaml:"credentials"`
}

func (s SMBTarget) Storage() (storage.Storage, error) {
	u, err := util.SmartParse(s.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid target url%v", err)
	}
	opts := []smb.Option{}
	if s.Credentials.Domain != "" {
		opts = append(opts, smb.WithDomain(s.Credentials.Domain))
	}
	if s.Credentials.Username != "" {
		opts = append(opts, smb.WithUsername(s.Credentials.Username))
	}
	if s.Credentials.Password != "" {
		opts = append(opts, smb.WithPassword(s.Credentials.Password))
	}
	store := smb.New(*u, opts...)
	return store, nil
}

type SMBCredentials struct {
	Domain   string `yaml:"domain"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type FileTarget struct {
	Type string `yaml:"type"`
	URL  string `yaml:"url"`
}

func (f FileTarget) Storage() (storage.Storage, error) {
	return storage.ParseURL(f.URL, credentials.Creds{})
}

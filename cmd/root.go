package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/databacker/mysql-backup/pkg/compression"
	"github.com/databacker/mysql-backup/pkg/config"
	"github.com/databacker/mysql-backup/pkg/core"
	"github.com/databacker/mysql-backup/pkg/database"
	"github.com/databacker/mysql-backup/pkg/storage"
	"github.com/databacker/mysql-backup/pkg/storage/credentials"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

type execs interface {
	timerDump(opts core.DumpOptions, timerOpts core.TimerOptions) error
	restore(target storage.Storage, targetFile string, dbconn database.Connection, databasesMap map[string]string, compressor compression.Compressor) error
}

type subCommand func(execs) (*cobra.Command, error)

var subCommands = []subCommand{dumpCmd, restoreCmd}

const (
	defaultPort = 3306
)

var (
	dbconn        database.Connection
	creds         credentials.Creds
	compressor    compression.Compressor
	configuration *config.Config
)

func rootCmd(execs execs) (*cobra.Command, error) {
	var (
		v   *viper.Viper
		cmd *cobra.Command
	)
	cmd = &cobra.Command{
		Use:   "mysql-backup",
		Short: "backup or restore one or more mysql-compatible databases",
		Long: `Backup or restore one or more mysql-compatible databases.
		In addition to the provided command-line flag options and environment variables,
		when using s3-storage, supports the standard AWS options:
		
		AWS_ACCESS_KEY_ID: AWS Key ID
		AWS_SECRET_ACCESS_KEY: AWS Secret Access Key
		AWS_REGION: Region in which the bucket resides
		`,
		PersistentPreRunE: func(c *cobra.Command, args []string) error {
			var err error
			bindFlags(cmd, v)
			logLevel := v.GetInt("verbose")
			switch logLevel {
			case 0:
				log.SetLevel(log.InfoLevel)
			case 1:
				log.SetLevel(log.DebugLevel)
			case 2:
				log.SetLevel(log.TraceLevel)
			}

			// read the config file, if needed; the structure of the config differs quite some
			// from the necessarily flat env vars/CLI flags, so we can't just use viper's
			// automatic config file support.
			if configFile := v.GetString("config-file"); configFile != "" {
				var (
					f      *os.File
					err    error
					config config.Config
				)
				if f, err = os.Open(configFile); err != nil {
					return fmt.Errorf("fatal error config file: %w", err)
				}
				defer f.Close()
				decoder := yaml.NewDecoder(f)
				if err := decoder.Decode(&config); err != nil {
					return fmt.Errorf("fatal error config file: %w", err)
				}
				configuration = &config
			}

			// the structure of our config file is more complex and with relationships than our config/env var
			// so we cannot use a single viper structure, as described above.

			// set up database connection
			var dbconn database.Connection

			if configuration != nil {
				if configuration.Database.Server != "" {
					dbconn.Host = configuration.Database.Server
				}
				if configuration.Database.Port != 0 {
					dbconn.Port = configuration.Database.Port
				}
				if configuration.Database.Credentials.Username != "" {
					dbconn.User = configuration.Database.Credentials.Username
				}
				if configuration.Database.Credentials.Password != "" {
					dbconn.Pass = configuration.Database.Credentials.Password
				}
			}
			// override config with env var or CLI flag, if set
			dbHost := v.GetString("server")
			if dbHost != "" {
				dbconn.Host = dbHost
			}
			dbPort := v.GetInt("port")
			if dbPort != 0 {
				dbconn.Port = dbPort
			}
			dbUser := v.GetString("user")
			if dbUser != "" {
				dbconn.User = dbUser
			}
			dbPass := v.GetString("pass")
			if dbPass != "" {
				dbconn.Pass = dbPass
			}

			// compression algorithm: check config, then CLI/env var overrides
			var compressionAlgo string
			if configuration != nil {
				compressionAlgo = configuration.Dump.Compression
			}
			compressionVar := v.GetString("compression")
			if compressionVar != "" {
				compressionAlgo = compressionVar
			}
			if compressionAlgo != "" {
				compressor, err = compression.GetCompressor(compressionAlgo)
				if err != nil {
					return fmt.Errorf("failure to get compression '%s': %v", compressionAlgo, err)
				}
			}

			// these are not from the config file, as they are generic credentials, used across all targets.
			// the config file uses specific ones per target
			creds = credentials.Creds{
				AWSEndpoint: v.GetString("aws-endpoint-url"),
				SMBCredentials: credentials.SMBCreds{
					Username: v.GetString("smb-user"),
					Password: v.GetString("smb-pass"),
					Domain:   v.GetString("smb-domain"),
				},
			}
			return nil
		},
	}

	v = viper.New()
	v.SetEnvPrefix("db")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()

	// server hostname via CLI or env var
	pflags := cmd.PersistentFlags()
	pflags.String("server", "", "hostname for database server")
	if err := cmd.MarkPersistentFlagRequired("server"); err != nil {
		return nil, err
	}

	// base of temporary directory to use
	pflags.String("tmp", os.TempDir(), "temporary directory base for working directory, defaults to OS")

	pflags.String("config-file", "", "config file to use, if any; individual CLI flags override config file")

	// server port via CLI or env var or default
	pflags.Int("port", defaultPort, "port for database server")

	// user via CLI or env var
	pflags.String("user", "", "username for database server")

	// pass via CLI or env var
	pflags.String("pass", "", "password for database server")

	// debug via CLI or env var or default
	pflags.IntP("verbose", "v", 0, "set log level, 1 is debug, 2 is trace")

	// aws options
	pflags.String("aws-endpoint-url", "", "Specify an alternative endpoint for s3 interoperable systems e.g. Digitalocean; ignored if not using s3.")
	pflags.String("aws-access-key-id", "", "Access Key for s3 and s3 interoperable systems; ignored if not using s3.")
	pflags.String("aws-secret-access-key", "", "Secret Access Key for s3 and s3 interoperable systems; ignored if not using s3.")
	pflags.String("aws-region", "", "Region for s3 and s3 interoperable systems; ignored if not using s3.")

	// smb options
	pflags.String("smb-user", "", "SMB username")
	pflags.String("smb-pass", "", "SMB username")
	pflags.String("smb-domain", "", "SMB domain")

	for _, subCmd := range subCommands {
		if sc, err := subCmd(execs); err != nil {
			return nil, err
		} else {
			cmd.AddCommand(sc)
		}
	}

	return cmd, nil
}

// Bind each cobra flag to its associated viper configuration (config file and environment variable)
func bindFlags(cmd *cobra.Command, v *viper.Viper) {
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		// Determine the naming convention of the flags when represented in the config file
		configName := f.Name
		_ = v.BindPFlag(configName, f)
		// Apply the viper config value to the flag when the flag is not set and viper has a value
		if !f.Changed && v.IsSet(configName) {
			val := v.Get(configName)
			_ = cmd.Flags().Set(f.Name, fmt.Sprintf("%v", val))
		}
	})
}

// Execute primary function for cobra
func Execute() {
	rootCmd, err := rootCmd(nil)
	if err != nil {
		log.Fatal(err)
	}
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

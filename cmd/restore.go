package cmd

import (
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/databacker/mysql-backup/pkg/compression"
	"github.com/databacker/mysql-backup/pkg/core"
	"github.com/databacker/mysql-backup/pkg/storage"
	"github.com/databacker/mysql-backup/pkg/util"
)

func restoreCmd(execs execs) (*cobra.Command, error) {
	var v *viper.Viper
	var cmd = &cobra.Command{
		Use:   "restore",
		Short: "restore a dump",
		Long:  `Restore a database dump from a given location.`,
		PreRun: func(cmd *cobra.Command, args []string) {
			bindFlags(cmd, v)
		},
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Debug("starting restore")
			targetFile := args[0]
			target := v.GetString("target")
			// get databases namesand mappings
			databasesMap := make(map[string]string)
			databases := strings.TrimSpace(v.GetString("database"))
			if databases != "" {
				for _, db := range strings.Split(databases, ",") {
					parts := strings.SplitN(db, ":", 2)
					if len(parts) != 2 {
						return fmt.Errorf("invalid database mapping: %s", db)
					}
					databasesMap[parts[0]] = parts[1]
				}
			}

			// compression algorithm: check config, then CLI/env var overrides
			var (
				compressionAlgo string
				compressor      compression.Compressor
				err             error
			)
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

			// target URL can reference one from the config file, or an absolute one
			// if it's not in the config file, it's an absolute one
			// if it is in the config file, it's a reference to one of the targets in the config file
			u, err := util.SmartParse(target)
			if err != nil {
				return fmt.Errorf("invalid target url: %v", err)
			}
			var store storage.Storage
			if u.Scheme == "config" {
				// get the target name
				targetName := u.Host
				// get the target from the config file
				if configuration == nil {
					return fmt.Errorf("no configuration file found")
				}
				if target, ok := configuration.Targets[targetName]; !ok {
					return fmt.Errorf("target %s not found in configuration", targetName)
				} else {
					if store, err = target.Storage.Storage(); err != nil {
						return fmt.Errorf("error creating storage for target %s: %v", targetName, err)
					}
				}
				// need to add the path to the specific target file
			} else {
				// parse the target URL
				store, err = storage.ParseURL(target, creds)
				if err != nil {
					return fmt.Errorf("invalid target url: %v", err)
				}
			}
			restore := core.Restore
			if execs != nil {
				restore = execs.restore
			}
			if err := restore(store, targetFile, dbconn, databasesMap, compressor); err != nil {
				return fmt.Errorf("error restoring: %v", err)
			}
			log.Info("Restore complete")
			return nil
		},
	}
	// target - where the backup is
	v = viper.New()
	v.SetEnvPrefix("db_restore")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()

	flags := cmd.Flags()
	flags.String("target", "", "full URL target to the backup that you wish to restore")
	if err := cmd.MarkFlagRequired("target"); err != nil {
		return nil, err
	}

	// compression
	flags.String("compression", defaultCompression, "Compression to use. Supported are: `gzip`, `bzip2`")

	// specific database to which to restore
	flags.String("database", "", "Mapping of from:to database names to which to restore, comma-separated, e.g. foo:bar,buz:qux. Replaces the `USE <database>` clauses in a backup file. If blank, uses the file as is.")

	// pre-restore scripts
	flags.String("pre-restore-scripts", "", "Directory wherein any file ending in `.sh` will be run after retrieving the dump file but pre-restore.")

	// post-restore scripts
	flags.String("post-restore-scripts", "", "Directory wherein any file ending in `.sh` will be run post-restore.")

	return cmd, nil
}

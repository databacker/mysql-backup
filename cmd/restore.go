package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/databacker/api/go/api"
	"github.com/databacker/mysql-backup/pkg/compression"
	"github.com/databacker/mysql-backup/pkg/core"
	"github.com/databacker/mysql-backup/pkg/storage"
	"github.com/databacker/mysql-backup/pkg/util"
)

func restoreCmd(passedExecs execs, cmdConfig *cmdConfiguration) (*cobra.Command, error) {
	if cmdConfig == nil {
		return nil, fmt.Errorf("cmdConfig is nil")
	}
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
			cmdConfig.logger.Debug("starting restore")
			ctx := context.Background()
			tracer := getTracer("restore")
			defer func() {
				tp := getTracerProvider()
				tp.ForceFlush(ctx)
				_ = tp.Shutdown(ctx)
			}()
			ctx = util.ContextWithTracer(ctx, tracer)
			_, startupSpan := tracer.Start(ctx, "startup")
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
			if cmdConfig.configuration != nil && cmdConfig.configuration.Dump.Compression != nil {
				compressionAlgo = *cmdConfig.configuration.Dump.Compression
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
				if cmdConfig.configuration == nil {
					return fmt.Errorf("no configuration file found")
				}
				var targetStructures map[string]api.Target
				if cmdConfig.configuration.Targets != nil {
					targetStructures = *cmdConfig.configuration.Targets
				}

				if target, ok := targetStructures[targetName]; !ok {
					return fmt.Errorf("target %s not found in configuration", targetName)
				} else {
					if store, err = storage.FromTarget(target); err != nil {
						return fmt.Errorf("error creating storage for target %s: %v", targetName, err)
					}
				}
				// need to add the path to the specific target file
			} else {
				// parse the target URL
				store, err = storage.ParseURL(target, cmdConfig.creds)
				if err != nil {
					return fmt.Errorf("invalid target url: %v", err)
				}
			}
			var executor execs
			executor = &core.Executor{}
			if passedExecs != nil {
				executor = passedExecs
			}
			executor.SetLogger(cmdConfig.logger)

			// at this point, any errors should not have usage
			cmd.SilenceUsage = true
			uid := uuid.New()
			restoreOpts := core.RestoreOptions{
				Target:       store,
				TargetFile:   targetFile,
				Compressor:   compressor,
				DatabasesMap: databasesMap,
				DBConn:       cmdConfig.dbconn,
				Run:          uid,
			}
			startupSpan.End()
			if err := executor.Restore(ctx, restoreOpts); err != nil {
				return fmt.Errorf("error restoring: %v", err)
			}
			executor.GetLogger().Info("Restore complete")
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

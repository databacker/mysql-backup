package cmd

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.opentelemetry.io/otel/codes"

	"github.com/databacker/api/go/api"
	"github.com/databacker/mysql-backup/pkg/compression"
	"github.com/databacker/mysql-backup/pkg/core"
	"github.com/databacker/mysql-backup/pkg/encrypt"
	"github.com/databacker/mysql-backup/pkg/storage"
	"github.com/databacker/mysql-backup/pkg/util"
)

const (
	defaultCompression      = "gzip"
	defaultBegin            = "+0"
	defaultFrequency        = 1440
	defaultMaxAllowedPacket = 4194304
	defaultFilenamePattern  = core.DefaultFilenamePattern
)

func dumpCmd(passedExecs execs, cmdConfig *cmdConfiguration) (*cobra.Command, error) {
	if cmdConfig == nil {
		return nil, fmt.Errorf("cmdConfig is nil")
	}
	var v *viper.Viper
	var cmd = &cobra.Command{
		Use:     "dump",
		Aliases: []string{"backup"},
		Short:   "backup a database",
		Long: `Backup a database to a target location, once or on a schedule.
		Can choose to dump all databases, only some by name, or all but excluding some.
		The databases "information_schema", "performance_schema", "sys" and "mysql" are
		excluded by default, unless you explicitly list them.`,
		PreRun: func(cmd *cobra.Command, args []string) {
			bindFlags(cmd, v)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			// this is the tracer that we will use throughout the entire run
			tracer := getTracer("dump")
			ctx = util.ContextWithTracer(ctx, tracer)
			_, startupSpan := tracer.Start(ctx, "startup")
			cmdConfig.logger.Debug("starting dump")
			defer func() {
				tp := getTracerProvider()
				_ = tp.ForceFlush(ctx)
				_ = tp.Shutdown(ctx)
			}()
			// check targets
			targetURLs := v.GetStringSlice("target")
			var (
				dumpConfig    *api.Dump
				scriptsConfig *api.Scripts
			)
			if cmdConfig.configuration != nil {
				dumpConfig = cmdConfig.configuration.Dump
				if dumpConfig != nil {
					scriptsConfig = dumpConfig.Scripts
				}
			}
			targets, err := parseTargets(targetURLs, cmdConfig)
			if err != nil {
				return fmt.Errorf("error parsing targets: %v", err)
			}
			if len(targets) == 0 {
				return fmt.Errorf("no targets specified")
			}
			safechars := v.GetBool("safechars")
			if !v.IsSet("safechars") && dumpConfig != nil && dumpConfig.Safechars != nil {
				safechars = *dumpConfig.Safechars
			}
			include := v.GetStringSlice("include")
			if len(include) == 0 && dumpConfig != nil && dumpConfig.Include != nil {
				include = *dumpConfig.Include
			}
			// make this slice nil if it's empty, so it is consistent; used mainly for test consistency
			if len(include) == 0 {
				include = nil
			}
			exclude := v.GetStringSlice("exclude")
			if len(exclude) == 0 && dumpConfig != nil && dumpConfig.Exclude != nil {
				exclude = *dumpConfig.Exclude
			}
			// make this slice nil if it's empty, so it is consistent; used mainly for test consistency
			if len(exclude) == 0 {
				exclude = nil
			}

			// how many databases to back up in parallel
			parallel := v.GetInt("parallelism")
			if !v.IsSet("parallelism") && dumpConfig != nil && dumpConfig.Parallelism != nil {
				parallel = *dumpConfig.Parallelism
			}

			preBackupScripts := v.GetString("pre-backup-scripts")
			if preBackupScripts == "" && scriptsConfig != nil && scriptsConfig.PreBackup != nil {
				preBackupScripts = *scriptsConfig.PreBackup
			}
			postBackupScripts := v.GetString("post-backup-scripts")
			if postBackupScripts == "" && scriptsConfig != nil && scriptsConfig.PostBackup != nil {
				postBackupScripts = *scriptsConfig.PostBackup
			}
			noDatabaseName := v.GetBool("no-database-name")
			if !v.IsSet("no-database-name") && dumpConfig != nil && dumpConfig.NoDatabaseName != nil {
				noDatabaseName = *dumpConfig.NoDatabaseName
			}
			skipExtendedInsert := v.GetBool("skip-extended-insert")
			if !v.IsSet("skip-extended-insert") && dumpConfig != nil && dumpConfig.SkipExtendedInsert != nil {
				skipExtendedInsert = *dumpConfig.SkipExtendedInsert
			}
			includeGeneratedColumns := v.GetBool("include-generated-columns")
			// Note: config file support for include-generated-columns would require updates to api.Dump
			compact := v.GetBool("compact")
			if !v.IsSet("compact") && dumpConfig != nil && dumpConfig.Compact != nil {
				compact = *dumpConfig.Compact
			}
			// should we dump triggers and functions and procedures?
			triggers := v.GetBool("triggers")
			if !v.IsSet("triggers") && dumpConfig != nil && dumpConfig.Triggers != nil {
				triggers = *dumpConfig.Triggers
			}
			routines := v.GetBool("routines")
			if !v.IsSet("routines") && dumpConfig != nil && dumpConfig.Routines != nil {
				routines = *dumpConfig.Routines
			}
			maxAllowedPacket := v.GetInt("max-allowed-packet")
			if !v.IsSet("max-allowed-packet") && dumpConfig != nil && dumpConfig.MaxAllowedPacket != nil && *dumpConfig.MaxAllowedPacket != 0 {
				maxAllowedPacket = *dumpConfig.MaxAllowedPacket
			}

			// compression algorithm: check config, then CLI/env var overrides
			var (
				compressionAlgo string
				compressor      compression.Compressor
			)
			if cmdConfig.configuration != nil && dumpConfig != nil && dumpConfig.Compression != nil {
				compressionAlgo = *dumpConfig.Compression
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

			// encryption algorithm: check config, then CLI/env var overrides
			var (
				encryptionAlgo string
				encryptionKey  []byte
				encryptor      encrypt.Encryptor
			)
			if cmdConfig.configuration != nil && dumpConfig != nil && dumpConfig.Encryption != nil {
				if dumpConfig.Encryption.Algorithm == nil {
					return fmt.Errorf("encryption algorithm must be set in config file")
				}
				encryptionAlgo = string(*dumpConfig.Encryption.Algorithm)
				switch {
				case dumpConfig.Encryption.Key != nil && *dumpConfig.Encryption.Key != "" && dumpConfig.Encryption.KeyPath != nil && *dumpConfig.Encryption.KeyPath != "":
					return fmt.Errorf("encryption key and path cannot both be set in config file")
				case dumpConfig.Encryption.Key != nil && *dumpConfig.Encryption.Key == "" && dumpConfig.Encryption.KeyPath != nil && *dumpConfig.Encryption.KeyPath == "":
					return fmt.Errorf("must set at least one of encryption key or path in config file")
				case dumpConfig.Encryption.Key != nil && *dumpConfig.Encryption.Key != "":
					encryptionKey, err = base64.StdEncoding.DecodeString(*dumpConfig.Encryption.Key)
					if err != nil {
						return fmt.Errorf("error decoding encryption key from config file: %v", err)
					}
				case dumpConfig.Encryption.KeyPath != nil && *dumpConfig.Encryption.KeyPath != "":
					key, err := os.ReadFile(*dumpConfig.Encryption.KeyPath)
					if err != nil {
						return fmt.Errorf("error reading encryption key from path: %v", err)
					}
					encryptionKey = key
				}
			}
			encryptionVar := v.GetString("encryption")
			if encryptionVar != "" {
				encryptionAlgo = encryptionVar
			}
			if encryptionAlgo != "" {
				keyContent := v.GetString("encryption-key")
				keyPath := v.GetString("encryption-key-path")
				switch {
				case keyContent != "" && keyPath != "":
					return fmt.Errorf("encryption key and path cannot both be set in CLI")
				case keyContent == "" && keyPath == "":
					return fmt.Errorf("must set at least one of encryption key or path in CLI")
				case keyContent != "":
					encryptionKey, err = base64.StdEncoding.DecodeString(keyContent)
					if err != nil {
						return fmt.Errorf("error decoding encryption key from CLI flag: %v", err)
					}
				case keyPath != "":
					key, err := os.ReadFile(keyPath)
					if err != nil {
						return fmt.Errorf("error reading encryption key from path: %v", err)
					}
					encryptionKey = key
				}

				encryptor, err = encrypt.GetEncryptor(encryptionAlgo, encryptionKey)
				if err != nil {
					return fmt.Errorf("failure to get encryptor '%s': %v", encryptionAlgo, err)
				}
			}

			// retention, if enabled
			retention := v.GetString("retention")
			if retention == "" && cmdConfig.configuration != nil && cmdConfig.configuration.Prune != nil && cmdConfig.configuration.Prune.Retention != nil {
				retention = *cmdConfig.configuration.Prune.Retention
			}
			filenamePattern := v.GetString("filename-pattern")

			if !v.IsSet("filename-pattern") && dumpConfig != nil && dumpConfig.FilenamePattern != nil {
				filenamePattern = *dumpConfig.FilenamePattern
			}
			if filenamePattern == "" {
				filenamePattern = defaultFilenamePattern
			}

			// timer options
			timerOpts := parseTimerOptions(v, cmdConfig.configuration)

			var executor execs
			executor = &core.Executor{}
			if passedExecs != nil {
				executor = passedExecs
			}
			executor.SetLogger(cmdConfig.logger)

			// at this point, any errors should not have usage
			cmd.SilenceUsage = true

			// done with the startup
			startupSpan.End()

			if err := executor.Timer(timerOpts, func() error {
				// start a new span for the dump, should not be a child of the startup one
				tracerCtx, dumpSpan := tracer.Start(ctx, "run")
				defer dumpSpan.End()
				uid := uuid.New()
				dumpOpts := core.DumpOptions{
					Targets:                 targets,
					Safechars:               safechars,
					DBNames:                 include,
					DBConn:                  cmdConfig.dbconn,
					Compressor:              compressor,
					Encryptor:               encryptor,
					Exclude:                 exclude,
					PreBackupScripts:        preBackupScripts,
					PostBackupScripts:       postBackupScripts,
					SuppressUseDatabase:     noDatabaseName,
					SkipExtendedInsert:      skipExtendedInsert,
					Compact:                 compact,
					Triggers:                triggers,
					Routines:                routines,
					IncludeGeneratedColumns: includeGeneratedColumns,
					MaxAllowedPacket:        maxAllowedPacket,
					Run:                     uid,
					FilenamePattern:         filenamePattern,
					Parallelism:             parallel,
				}
				_, err := executor.Dump(tracerCtx, dumpOpts)
				if err != nil {
					dumpSpan.SetStatus(codes.Error, fmt.Sprintf("error running dump: %v", err))
					return fmt.Errorf("error running dump: %w", err)
				}
				if retention != "" {
					if err := executor.Prune(tracerCtx, core.PruneOptions{Targets: targets, Retention: retention, Run: uid}); err != nil {
						dumpSpan.SetStatus(codes.Error, fmt.Sprintf("error running prune: %v", err))
						return fmt.Errorf("error running prune: %w", err)
					}
				}
				dumpSpan.SetStatus(codes.Ok, "dump complete")
				return nil
			}); err != nil {
				return fmt.Errorf("error running command: %w", err)
			}
			executor.GetLogger().Info("Backup complete")
			return nil
		},
	}

	v = viper.New()
	v.SetEnvPrefix("db_dump")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()

	flags := cmd.Flags()
	// target - where the backup is to be saved
	flags.StringSlice("target", []string{}, `full URL target to where the backups should be saved. Should be a directory. Accepts multiple targets. Supports three formats:
Local: If if starts with a "/" character of "file:///", will dump to a local path, which should be volume-mounted.
SMB: If it is a URL of the format smb://hostname/share/path/ then it will connect via SMB.
S3: If it is a URL of the format s3://bucketname/path then it will connect via S3 protocol.`)

	// include - include of databases to back up
	flags.StringSlice("include", []string{}, "names of databases to dump; empty to do all")

	// exclude
	flags.StringSlice("exclude", []string{}, "databases to exclude from the dump.")

	// single database, do not include `USE database;` in dump
	flags.Bool("no-database-name", false, "Omit `USE <database>;` in the dump, so it can be restored easily to a different database.")

	// skip extended insert in dump; instead, one INSERT per record in each table
	flags.Bool("skip-extended-insert", false, "Skip extended insert in dump; instead, one INSERT per record in each table.")

	// include generated columns in dump
	flags.Bool("include-generated-columns", false, "Include generated columns in the dump. By default, generated and virtual columns are excluded.")

	// frequency
	flags.Int("frequency", defaultFrequency, "how often to run backups, in minutes")

	// begin
	flags.String("begin", defaultBegin, "What time to do the first dump. Must be in one of two formats: Absolute: HHMM, e.g. `2330` or `0415`; or Relative: +MM, i.e. how many minutes after starting the container, e.g. `+0` (immediate), `+10` (in 10 minutes), or `+90` in an hour and a half")

	// cron
	flags.String("cron", "", "Set the dump schedule using standard [crontab syntax](https://en.wikipedia.org/wiki/Cron), a single line.")

	// once
	flags.Bool("once", false, "Override all other settings and run the dump once immediately and exit. Useful if you use an external scheduler (e.g. as part of an orchestration solution like Cattle or Docker Swarm or [kubernetes cron jobs](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/)) and don't want the container to do the scheduling internally.")

	// parallelism - how many databases (and therefore connections) to back up at once
	flags.Int("parallelism", 1, "How many databases to back up in parallel.")

	// safechars
	flags.Bool("safechars", false, "The dump filename usually includes the character `:` in the date, to comply with RFC3339. Some systems and shells don't like that character. If true, will replace all `:` with `-`.")

	// compression
	flags.String("compression", defaultCompression, "Compression to use. Supported are: `gzip`, `bzip2`, `none`")

	// source filename pattern
	flags.String("filename-pattern", defaultFilenamePattern, "Pattern to use for filename in target. See documentation.")

	// pre-backup scripts
	flags.String("pre-backup-scripts", "", "Directory wherein any file ending in `.sh` will be run pre-backup.")

	// post-backup scripts
	flags.String("post-backup-scripts", "", "Directory wherein any file ending in `.sh` will be run post-backup but pre-send to target.")

	// max-allowed-packet size
	flags.Int("max-allowed-packet", defaultMaxAllowedPacket, "Maximum size of the buffer for client/server communication, similar to mysqldump's max_allowed_packet. 0 means to use the default size.")

	// whether to include triggers and functions
	flags.Bool("triggers-and-functions", false, "Whether to include triggers and functions in the dump.")

	cmd.MarkFlagsMutuallyExclusive("once", "cron")
	cmd.MarkFlagsMutuallyExclusive("once", "begin")
	cmd.MarkFlagsMutuallyExclusive("once", "frequency")
	cmd.MarkFlagsMutuallyExclusive("cron", "begin")
	cmd.MarkFlagsMutuallyExclusive("cron", "frequency")
	// retention
	flags.String("retention", "", "Retention period for backups. Optional. If not specified, no pruning will be done. Can be number of backups or time-based. For time-based, the format is: 1d, 1w, 1m, 1y for days, weeks, months, years, respectively. For number-based, the format is: 1c, 2c, 3c, etc. for the count of backups to keep.")

	// encryption options
	flags.String("encryption", "", fmt.Sprintf("Encryption algorithm to use, none if blank. Supported are: %s. Format must match the specific algorithm.", strings.Join(encrypt.All, ", ")))
	flags.String("encryption-key", "", "Encryption key to use, base64-encoded. Useful for debugging, not recommended for production. If encryption is enabled, and both are provided or neither is provided, returns an error.")
	flags.String("encryption-key-path", "", "Path to encryption key file. If encryption is enabled, and both are provided or neither is provided, returns an error.")
	return cmd, nil
}

func parseTimerOptions(v *viper.Viper, config *api.ConfigSpec) core.TimerOptions {
	var scheduleConfig *api.Schedule
	if config != nil {
		dumpConfig := config.Dump
		if dumpConfig != nil {
			scheduleConfig = dumpConfig.Schedule
		}
	}
	once := v.GetBool("once")
	if !v.IsSet("once") && scheduleConfig != nil && scheduleConfig.Once != nil {
		once = *scheduleConfig.Once
	}
	cron := v.GetString("cron")
	if cron == "" && scheduleConfig != nil && scheduleConfig.Cron != nil {
		cron = *scheduleConfig.Cron
	}
	begin := v.GetString("begin")
	if begin == "" && scheduleConfig != nil && scheduleConfig.Begin != nil {
		begin = fmt.Sprintf("%d", *scheduleConfig.Begin)
	}
	frequency := v.GetInt("frequency")
	if frequency == 0 && scheduleConfig != nil && scheduleConfig.Frequency != nil {
		frequency = *scheduleConfig.Frequency
	}
	return core.TimerOptions{
		Once:      once,
		Cron:      cron,
		Begin:     begin,
		Frequency: frequency,
	}

}

func parseTargets(urls []string, cmdConfig *cmdConfiguration) ([]storage.Storage, error) {
	var targets []storage.Storage
	if len(urls) > 0 {
		for _, t := range urls {
			store, err := storage.ParseURL(t, cmdConfig.creds)
			if err != nil {
				return nil, fmt.Errorf("invalid target url: %v", err)
			}
			targets = append(targets, store)
		}
	} else {
		// try the config file
		if cmdConfig.configuration != nil {
			// parse the target objects, then the ones listed for the backup
			var (
				targetStructures map[string]api.Target
				dumpTargets      []string
			)
			if cmdConfig.configuration.Targets != nil {
				targetStructures = *cmdConfig.configuration.Targets
			}
			if cmdConfig.configuration != nil && cmdConfig.configuration.Dump != nil && cmdConfig.configuration.Dump.Targets != nil {
				dumpTargets = *cmdConfig.configuration.Dump.Targets
			}
			for _, t := range dumpTargets {
				var (
					store storage.Storage
					err   error
				)
				if target, ok := targetStructures[t]; !ok {
					return nil, fmt.Errorf("target %s from dump configuration not found in targets configuration", t)
				} else {
					store, err = storage.FromTarget(target)
					if err != nil {
						return nil, fmt.Errorf("target %s from dump configuration has invalid URL: %v", t, err)
					}
				}
				targets = append(targets, store)
			}
		}
	}
	return targets, nil
}

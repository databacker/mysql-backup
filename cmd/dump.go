package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.opentelemetry.io/otel/codes"

	"github.com/databacker/api/go/api"
	"github.com/databacker/mysql-backup/pkg/compression"
	"github.com/databacker/mysql-backup/pkg/core"
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
				tp.ForceFlush(ctx)
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
			compact := v.GetBool("compact")
			if !v.IsSet("compact") && dumpConfig != nil && dumpConfig.Compact != nil {
				compact = *dumpConfig.Compact
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
			if cmdConfig.configuration != nil && dumpConfig.Compression != nil {
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

			// retention, if enabled
			retention := v.GetString("retention")
			if retention == "" && cmdConfig.configuration != nil && cmdConfig.configuration.Prune.Retention != nil {
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
					Targets:             targets,
					Safechars:           safechars,
					DBNames:             include,
					DBConn:              cmdConfig.dbconn,
					Compressor:          compressor,
					Exclude:             exclude,
					PreBackupScripts:    preBackupScripts,
					PostBackupScripts:   postBackupScripts,
					SuppressUseDatabase: noDatabaseName,
					Compact:             compact,
					MaxAllowedPacket:    maxAllowedPacket,
					Run:                 uid,
					FilenamePattern:     filenamePattern,
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

	// frequency
	flags.Int("frequency", defaultFrequency, "how often to run backups, in minutes")

	// begin
	flags.String("begin", defaultBegin, "What time to do the first dump. Must be in one of two formats: Absolute: HHMM, e.g. `2330` or `0415`; or Relative: +MM, i.e. how many minutes after starting the container, e.g. `+0` (immediate), `+10` (in 10 minutes), or `+90` in an hour and a half")

	// cron
	flags.String("cron", "", "Set the dump schedule using standard [crontab syntax](https://en.wikipedia.org/wiki/Cron), a single line.")

	// once
	flags.Bool("once", false, "Override all other settings and run the dump once immediately and exit. Useful if you use an external scheduler (e.g. as part of an orchestration solution like Cattle or Docker Swarm or [kubernetes cron jobs](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/)) and don't want the container to do the scheduling internally.")

	// safechars
	flags.Bool("safechars", false, "The dump filename usually includes the character `:` in the date, to comply with RFC3339. Some systems and shells don't like that character. If true, will replace all `:` with `-`.")

	// compression
	flags.String("compression", defaultCompression, "Compression to use. Supported are: `gzip`, `bzip2`")

	// source filename pattern
	flags.String("filename-pattern", defaultFilenamePattern, "Pattern to use for filename in target. See documentation.")

	// pre-backup scripts
	flags.String("pre-backup-scripts", "", "Directory wherein any file ending in `.sh` will be run pre-backup.")

	// post-backup scripts
	flags.String("post-backup-scripts", "", "Directory wherein any file ending in `.sh` will be run post-backup but pre-send to target.")

	// max-allowed-packet size
	flags.Int("max-allowed-packet", defaultMaxAllowedPacket, "Maximum size of the buffer for client/server communication, similar to mysqldump's max_allowed_packet. 0 means to use the default size.")

	cmd.MarkFlagsMutuallyExclusive("once", "cron")
	cmd.MarkFlagsMutuallyExclusive("once", "begin")
	cmd.MarkFlagsMutuallyExclusive("once", "frequency")
	cmd.MarkFlagsMutuallyExclusive("cron", "begin")
	cmd.MarkFlagsMutuallyExclusive("cron", "frequency")
	// retention
	flags.String("retention", "", "Retention period for backups. Optional. If not specified, no pruning will be done. Can be number of backups or time-based. For time-based, the format is: 1d, 1w, 1m, 1y for days, weeks, months, years, respectively. For number-based, the format is: 1c, 2c, 3c, etc. for the count of backups to keep.")

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

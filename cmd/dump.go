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
)

const (
	defaultCompression      = "gzip"
	defaultBegin            = "+0"
	defaultFrequency        = 1440
	defaultMaxAllowedPacket = 4194304
)

func dumpCmd(execs execs) (*cobra.Command, error) {
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
			log.Debug("starting dump")
			// check targets
			targetURLs := v.GetStringSlice("target")
			var (
				targets []storage.Storage
				err     error
			)
			if len(targetURLs) > 0 {
				for _, t := range targetURLs {
					store, err := storage.ParseURL(t, creds)
					if err != nil {
						return fmt.Errorf("invalid target url: %v", err)
					}
					targets = append(targets, store)
				}
			} else {
				// try the config file
				if configuration != nil {
					// parse the target objects, then the ones listed for the backup
					targetStructures := configuration.Targets
					dumpTargets := configuration.Dump.Targets
					for _, t := range dumpTargets {
						var store storage.Storage
						if target, ok := targetStructures[t]; !ok {
							return fmt.Errorf("target %s from dump configuration not found in targets configuration", t)
						} else {
							store, err = target.Storage.Storage()
							if err != nil {
								return fmt.Errorf("target %s from dump configuration has invalid URL: %v", t, err)
							}
						}
						targets = append(targets, store)
					}
				}
			}
			if len(targets) == 0 {
				return fmt.Errorf("no targets specified")
			}
			safechars := v.GetBool("safechars")
			if !v.IsSet("safechars") && configuration != nil {
				safechars = configuration.Dump.Safechars
			}
			include := v.GetStringSlice("include")
			if len(include) == 0 && configuration != nil {
				include = configuration.Dump.Include
			}
			// make this slice nil if it's empty, so it is consistent; used mainly for test consistency
			if len(include) == 0 {
				include = nil
			}
			exclude := v.GetStringSlice("exclude")
			if len(exclude) == 0 && configuration != nil {
				exclude = configuration.Dump.Exclude
			}
			// make this slice nil if it's empty, so it is consistent; used mainly for test consistency
			if len(exclude) == 0 {
				exclude = nil
			}
			preBackupScripts := v.GetString("pre-backup-scripts")
			if preBackupScripts == "" && configuration != nil {
				preBackupScripts = configuration.Dump.Scripts.PreBackup
			}
			noDatabaseName := v.GetBool("no-database-name")
			if !v.IsSet("no-database-name") && configuration != nil {
				noDatabaseName = configuration.Dump.NoDatabaseName
			}
			compact := v.GetBool("compact")
			if !v.IsSet("compact") && configuration != nil {
				compact = configuration.Dump.Compact
			}
			maxAllowedPacket := v.GetInt("max-allowed-packet")
			if !v.IsSet("max-allowed-packet") && configuration != nil && configuration.Dump.MaxAllowedPacket != 0 {
				maxAllowedPacket = configuration.Dump.MaxAllowedPacket
			}

			// compression algorithm: check config, then CLI/env var overrides
			var (
				compressionAlgo string
				compressor      compression.Compressor
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
			dumpOpts := core.DumpOptions{
				Targets:             targets,
				Safechars:           safechars,
				DBNames:             include,
				DBConn:              dbconn,
				Compressor:          compressor,
				Exclude:             exclude,
				PreBackupScripts:    preBackupScripts,
				PostBackupScripts:   preBackupScripts,
				SuppressUseDatabase: noDatabaseName,
				Compact:             compact,
				MaxAllowedPacket:    maxAllowedPacket,
			}

			// timer options
			once := v.GetBool("once")
			if !v.IsSet("once") && configuration != nil {
				once = configuration.Dump.Schedule.Once
			}
			cron := v.GetString("cron")
			if cron == "" && configuration != nil {
				cron = configuration.Dump.Schedule.Cron
			}
			begin := v.GetString("begin")
			if begin == "" && configuration != nil {
				begin = configuration.Dump.Schedule.Begin
			}
			frequency := v.GetInt("frequency")
			if frequency == 0 && configuration != nil {
				frequency = configuration.Dump.Schedule.Frequency
			}
			timerOpts := core.TimerOptions{
				Once:      once,
				Cron:      cron,
				Begin:     begin,
				Frequency: frequency,
			}
			dump := core.TimerDump
			if execs != nil {
				dump = execs.timerDump
			}
			if err := dump(dumpOpts, timerOpts); err != nil {
				return err
			}
			log.Info("Backup complete")
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
	flags.String("filename-pattern", "db_backup_{{ .now }}.{{ .compression }}", "Pattern to use for filename in target. See documentation.")

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

	return cmd, nil
}

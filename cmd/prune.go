package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/databacker/mysql-backup/pkg/core"
	"github.com/databacker/mysql-backup/pkg/util"
)

func pruneCmd(passedExecs execs, cmdConfig *cmdConfiguration) (*cobra.Command, error) {
	if cmdConfig == nil {
		return nil, fmt.Errorf("cmdConfig is nil")
	}
	var v *viper.Viper
	var cmd = &cobra.Command{
		Use:   "prune",
		Short: "prune older backups",
		Long: `Prune older backups based on a retention period. Can be number of backups or time-based.
		For time-based, the format is: 1d, 1w, 1m, 1y for days, weeks, months, years, respectively.
		For number-based, the format is: 1c, 2c, 3c, etc. for the count of backups to keep.
		
		For time-based, prune always converts the time to hours, and then rounds up. This means that 2d is treated as 48h, and
		any backups must be at least 48 full hours ago to be pruned.
		`,
		PreRun: func(cmd *cobra.Command, args []string) {
			bindFlags(cmd, v)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cmdConfig.logger.Debug("starting prune")
			ctx := context.Background()
			// this is the tracer that we will use throughout the entire run
			defer func() {
				tp := getTracerProvider()
				tp.ForceFlush(ctx)
				_ = tp.Shutdown(ctx)
			}()
			tracer := getTracer("prune")
			ctx = util.ContextWithTracer(ctx, tracer)
			_, startupSpan := tracer.Start(ctx, "startup")
			retention := v.GetString("retention")
			targetURLs := v.GetStringSlice("target")

			targets, err := parseTargets(targetURLs, cmdConfig)
			if err != nil {
				return fmt.Errorf("error parsing targets: %v", err)
			}
			if len(targets) == 0 {
				return fmt.Errorf("no targets specified")
			}

			if retention == "" && cmdConfig.configuration != nil && cmdConfig.configuration.Prune.Retention != nil {
				retention = *cmdConfig.configuration.Prune.Retention
			}

			// timer options
			timerOpts := parseTimerOptions(v, cmdConfig.configuration)

			var executor execs
			executor = &core.Executor{}
			if passedExecs != nil {
				executor = passedExecs
			}
			executor.SetLogger(cmdConfig.logger)
			// done with the startup
			startupSpan.End()

			if err := executor.Timer(timerOpts, func() error {
				uid := uuid.New()
				return executor.Prune(ctx, core.PruneOptions{Targets: targets, Retention: retention, Run: uid})
			}); err != nil {
				return fmt.Errorf("error running prune: %w", err)
			}
			executor.GetLogger().Info("Pruning complete")
			return nil
		},
	}
	// target - where the backup is
	v = viper.New()
	v.SetEnvPrefix("db_restore")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()

	flags := cmd.Flags()
	flags.String("target", "", "full URL target to the directory where the backups are stored. Can be a file URL, or a reference to a target in the configuration file, e.g. `config://targetname`.")

	// retention
	flags.String("retention", "", "Retention period for backups. REQUIRED. Can be number of backups or time-based. For time-based, the format is: 1d, 1w, 1m, 1y for days, weeks, months, years, respectively. For number-based, the format is: 1c, 2c, 3c, etc. for the count of backups to keep.")

	// frequency
	flags.Int("frequency", defaultFrequency, "how often to run prunes, in minutes")

	// begin
	flags.String("begin", defaultBegin, "What time to do the first prune. Must be in one of two formats: Absolute: HHMM, e.g. `2330` or `0415`; or Relative: +MM, i.e. how many minutes after starting the container, e.g. `+0` (immediate), `+10` (in 10 minutes), or `+90` in an hour and a half")

	// cron
	flags.String("cron", "", "Set the prune schedule using standard [crontab syntax](https://en.wikipedia.org/wiki/Cron), a single line.")

	// once
	flags.Bool("once", false, "Override all other settings and run the prune once immediately and exit. Useful if you use an external scheduler (e.g. as part of an orchestration solution like Cattle or Docker Swarm or [kubernetes cron jobs](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/)) and don't want the container to do the scheduling internally.")

	return cmd, nil
}

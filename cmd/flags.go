package cmd

import "github.com/spf13/pflag"

func addTimerFlags(flags *pflag.FlagSet) {
	// frequency
	flags.Int("frequency", defaultFrequency, "how often to run, in minutes")

	// begin
	flags.String("begin", defaultBegin, "What time to do the first run, as absolute or relative time. Absolute times may be UTC (`0400Z`), include a UTC offset (`0400+08:00`), or use the platform's local timezone (`0400@local`). Relative times use +MM, i.e. minutes after starting the run, such as `+0`, `+10`, or `+90`. A zoneless time (`0400`) is legacy and should not be used, but is interpreted as UTC.")

	// cron
	flags.String("cron", "", "Set the run schedule using standard [crontab syntax](https://en.wikipedia.org/wiki/Cron), a single line.")

	// once
	flags.Bool("once", false, "Override all other settings and run once immediately and exit. Useful if you use an external scheduler (e.g. as part of an orchestration solution like Cattle or Docker Swarm or [kubernetes cron jobs](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/)) and don't want the container to do the scheduling internally.")
}

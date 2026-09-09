package cmd

import "github.com/spf13/pflag"

func addTimerFlags(flags *pflag.FlagSet) {
	// frequency
	flags.Int("frequency", defaultFrequency, "how often to run, in minutes")

	// begin
	flags.String("begin", defaultBegin, "What time to do the first run. Absolute times may be UTC (`0400` or `0400Z`) or include a UTC offset (`0400+08:00`). A zoneless time is interpreted as UTC. Relative times use +MM, i.e. minutes after starting the container, such as `+0`, `+10`, or `+90`")

	// cron
	flags.String("cron", "", "Set the run schedule using standard [crontab syntax](https://en.wikipedia.org/wiki/Cron), a single line.")

	// once
	flags.Bool("once", false, "Override all other settings and run once immediately and exit. Useful if you use an external scheduler (e.g. as part of an orchestration solution like Cattle or Docker Swarm or [kubernetes cron jobs](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/)) and don't want the container to do the scheduling internally.")
}

# Backup Scheduling

`mysql-backup` can be run either once, doing a backup and exiting, or as a long-running task,
backing up on schedule.

There are several options for scheduling how often a backup should run:

* run just once and exit.
* run every x minutes, optionally delaying the first one by a certain amount of time
* run on a schedule.


## Order of Priority

The scheduling options have an order of priority:

1. If run once is set, it will run immediately and exit, ignoring all other scheduling options.
2. If cron is set, it runs according to the cron schedule, ignoring frequency and delayed start.
3. Frequency and optionally delayed start are used.

## Scheduling Options

### Run once

You can set it to run just once via:

* Environment variable: `DB_DUMP_ONCE=true`
* CLI flag: `dump --once`
* Config file:
```yaml
dump:
    run-once: true
```

If you set it to run just once, the backup will run once and then exit.

**This overrides all other scheduling options**.

This is useful for one-offs, or if `mysql-backup` is being run via an external scheduler, such as cron
or [kubernetes cron jobs](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/), and thus
don't want `mysql-backup` to do the scheduling internally.

### Cron Scheduling

You can set a cron schedule via:

* Environment variable: `CRON_SCHEDULE=0 * * * *`
* CLI flag: `dump --cron="0 * * * *"`
* Config file:
```yaml
dump:
    cron: 0 * * * *
```

The cron dump schedule option uses standard [crontab syntax](https://en.wikipedia.org/wiki/Cron), a
single line.

If a cron-scheduled backup takes longer than the beginning of the next backup window, it will be skipped. For example, if your cron line is scheduled to backup every hour, and the backup that runs at 13:00 finishes at 14:05, the next backup will not be immediate, but rather at 15:00.

### Frequency and Delayed Start

If neither run once nor cron is set, then `mysql-backup` will use the frequency and optional delayed start options.

The value for each is minutes. Thus, you can set backup to run every hour by setting the frequency to `60`.
Similarly, you can delay start by 2 hours by setting the delayed start to `120`.

You can set the frequency start via:

* Environment variable: `DB_DUMP_FREQUENCY=60`
* CLI flag: `dump --frequency=60`
* Config file:
```yaml
dump:
    frequency: 60
```

You can set the delayed start via:

* Environment variable: `DB_DUMP_DELAY=120`
* CLI flag: `dump --delay=120`
* Config file:
```yaml
dump:
    delay: 120
```

# Pruning

Pruning is the process of removing backups that no longer are needed.

mysql-backup does **not** do this by default; it is up to you to enable this feature, if you want it.

## Launching Pruning

Pruning happens only in the following scenarios:

* During pruning runs
* During backup runs

It does not occur during restore runs.

### Pruning Runs

You can start `mysql-backup` with the command `prune` to run a pruning operation. It will prune any backups that are no longer needed.

Like backups, it can run once and then exit, or it can run on a schedule.

It uses the same configuration options for scheduling as backups, see the [scheduling](./scheduling.md) documentation for more information,
specifically the section about [Scheduling Options](./scheduling.md#scheduling-options).

### Backup Runs

When running `mysql-backup` in backup mode, it _optionally_ can also prune older backups before each backup run.
When enabled, it will prune any backups that fit the pruning criteria.

## Pruning Criteria

Pruning can be on the basis of the _age_ of a specific backup, or the _number_ of backups. Both are set by the configuration setting:

* Environment variable: `DB_DUMP_RETENTION=<value>`
* CLI flag: `dump --retention=<value>` or `prune --retention=<value>`
* Config file:
```yaml
prune:
    retention: <value>
```

The value of retention always is an integer followed by a letter. The letter can one of:

* `h` - hours, e.g. `2h`
* `d` - days, e.g. `3d`
* `w` - weeks, e.g. `4w`
* `m` - months, e.g. `3m`
* `y` - years, e.g. `5y`
* `c` - count, how many backup to keep, e.g. `10c`; this could have been simply `10`, but was kept as `c` to avoid accidental confusion with the other options.

Most of these are interchangeable, e.g. `3d` is the same as `72h`, and `4w` is the same as `28d` is the same as `672h`.

When calculating whether or not to prune, `mysql-backup` __always__ converts the amount to hours, and then errs on the side of caution.
For example, if provided `7d`, it will convert that to `168h`, and then prune any backups older than 168 full hours. If it is 167 hours and 59 minutes old, it
will not be pruned.

## Determining backup age

Pruning depends on the name of the backup file, rather than the timestamp on the target filesystem, as the latter can be unreliable.
This means that the filename must be of a known pattern.

As of this writing, pruning only work for backup files whose filename uses the default naming scheme, as described in
["Dump File" in backup documentation](./backup.md#dump-file). We hope to support custom filenames in the future.

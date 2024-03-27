# Configuring mysql-backup

`mysql-backup` can be configured using one or more of:

* environment variables
* CLI flags
* a configuration file

In all cases, the command line flag option takes precedence over the environment variable which takes
precedence over the config file option.

The environment variables, CLI flag options and config file options are similar, but not exactly the same,
due to variances in how the various are structured. As a general rule:

* Environment variables are all uppercase, with words separated by underscores, and most start with `DB_DUMP`. For example, `DB_DUMP_FREQ=60`.
* CLI flags are all lowercase, with words separated by hyphens. Since the CLI has sub-commands, the `dump-` and `restore-` are unnecessary. For example, `mysql-backup dump --frequency=60` or `mysql-backup restore --target=/foo/file.gz`.

For example, the following are equivalent.

Set dump frequency to 60 minutes:

* Environment variable: `DB_DUMP_FREQ=60`
* CLI flag: `mysql-backup dump --frequency=60`
* Config file:
```yaml
dump:
  schedule:
    frequency: 60
```

Set the dump target to the directory `/db`:

* Environment variable: `DB_DUMP_TARGET=/db`
* CLI flag: `mysql-backup dump --target=/db`
* Config file:
```yaml
dump:
  targets:
  - file

targets:
  file:
    url: /db
```

**Security Notices**

If using environment variables with any credentials in a container, you should consider the [use of `--env-file=`](https://docs.docker.com/engine/reference/commandline/run/#set-environment-variables-e-env-env-file), [docker secrets](https://docs.docker.com/engine/swarm/secrets/) to keep your secrets out of your shell history

If using CLI flags with any credentials, you should consider using a config file instead of directly
placing credentials in the flags, where they may be kept in shell history.

There is **no** default configuration file. To use a configuration file, you **must** specify it with the `--config` flag.

## Sample Configuration Files

Various sample configuration files are available in the [sample-configs](../sample-configs/) directory.

## Configuration Options

The following are the environment variables, CLI flags and configuration file options for: backup(B), restore (R), prune (P).

| Purpose | Backup / Restore | CLI Flag | Env Var | Config Key | Default |
| --- | --- | --- | --- | --- | --- |
| hostname or unix domain socket path (starting with a slash) to connect to database. Required. | BR | `server` | `DB_SERVER` | `database.server` |  |
| port to use to connect to database. Optional. | BR | `port` | `DB_PORT` | `database.port` | 3306 |
| username for the database | BR | `user` | `DB_USER` | `database.credentials.username` |  |
| password for the database | BR | `pass` | `DB_PASS` | `database.credentials.password` |  |
| names of databases to dump, comma-separated | B | `include` | `DB_NAMES` | `database.include` | all databases in the server |
| names of databases to exclude from the dump | B | `exclude` | `DB_NAMES_EXCLUDE` | `database.exclude` |  |
| do not include `USE <database>;` statement in the dump | B | `no-database-name` | `NO_DATABASE_NAME` | `database.no-database-name` | `false` |
| restore to a specific database | R | `restore --database` | `RESTORE_DATABASE` | `restore.database` |  |
| how often to do a dump or prune, in minutes | BP | `dump --frequency` | `DB_DUMP_FREQ` | `dump.schedule.frequency` | `1440` (in minutes), i.e. once per day |
| what time to do the first dump or prune | BP | `dump --begin` | `DB_DUMP_BEGIN` | `dump.schedule.begin` | `0`, i.e. immediately |
| cron schedule for dumps or prunes | BP | `dump --cron` | `DB_DUMP_CRON` | `dump.schedule.cron` |  |
| run the backup or prune a single time and exit | BP | `dump --once` | `RUN_ONCE` | `dump.schedule.once` | `false` |
| enable debug logging | BRP | `debug` | `DEBUG` | `logging: debug` | `false` |
| where to put the dump file; see [backup](./backup.md) | BP | `dump --target` | `DB_DUMP_TARGET` | `dump.targets` |  |
| where the restore file exists; see [restore](./restore.md) | R | `restore --target` | `DB_RESTORE_TARGET` | `restore.target` |  |
| replace any `:` in the dump filename with `-` | BP | `dump --safechars` | `DB_DUMP_SAFECHARS` | `database.safechars` | `false` |
| AWS access key ID, used only if a target does not have one | BRP | `aws-access-key-id` | `AWS_ACCESS_KEY_ID` | `dump.targets[s3-target].credentials.access-key-id` |  |
| AWS secret access key, used only if a target does not have one | BRP | `aws-secret-access-key` | `AWS_SECRET_ACCESS_KEY` | `dump.targets[s3-target].credentials.secret-access-key` |  |
| AWS default region, used only if a target does not have one | BRP | `aws-region` | `AWS_REGION` | `dump.targets[s3-target].region` |  |
| alternative endpoint URL for S3-interoperable systems, used only if a target does not have one | BR | `aws-endpoint-url` | `AWS_ENDPOINT_URL` | `dump.targets[s3-target].endpoint` |  |
| SMB username, used only if a target does not have one | BRP | `smb-user` | `SMB_USER` | `dump.targets[smb-target].credentials.username` |  |
| SMB password, used only if a target does not have one | BRP | `smb-pass` | `SMB_PASS` | `dump.targets[smb-target].credentials.password` |  |
| compression to use, one of: `bzip2`, `gzip` | BP | `compression` | `COMPRESSION` | `dump.compression` | `gzip` |
| when in container, run the dump or restore with `nice`/`ionice` | BR | `` | `NICE` | `` | `false` |
| tmp directory to be used during backup creation and other operations | BR | `tmp` | `TMP_PATH` | `tmp` | system-defined |
| filename to save the target backup file | B | `dump --filename-pattern` | `DB_DUMP_FILENAME_PATTERN` | `dump.filename-pattern` |  |
| directory with scripts to execute before backup | B | `dump --pre-backup-scripts` | `DB_DUMP_PRE_BACKUP_SCRIPTS` | `dump.scripts.pre-backup` | in container, `/scripts.d/pre-backup/` |
| directory with scripts to execute after backup | B | `dump --post-backup-scripts` | `DB_DUMP_POST_BACKUP_SCRIPTS` | `dump.scripts.post-backup` | in container, `/scripts.d/post-backup/` |
| directory with scripts to execute before restore | R | `restore --pre-restore-scripts` | `DB_DUMP_PRE_RESTORE_SCRIPTS` | `dump.pre-restore-scripts` | in container, `/scripts.d/pre-restore/` |
| directory with scripts to execute after restore | R | `restore --post-restore-scripts` | `DB_DUMP_POST_RESTORE_SCRIPTS` | `dump.post-restore-scripts` | in container, `/scripts.d/post-restore/` |
| retention policy for backups | BP | `dump --retention` | `RETENTION` | `prune.retention` | Infinite |

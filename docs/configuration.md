# Configuring mysql-backup

`mysql-backup` can be configured using one or more of:

* environment variables
* CLI flags
* a configuration file

In all cases, the command line flag option takes precedence over the environment variable which takes
precedence over the config file option.

The environment variables, CLI flag options and config file options are similar, but not exactly the same,
due to variances in how the various are structured. As a general rule:

* Environment variables are all uppercase, with words separated by underscores, and most start with `DB_DUMP`. For example, `DB_DUMP_FREQUENCY=60`.
* CLI flags are all lowercase, with words separated by hyphens, a.k.a. kebab-case. Since the CLI has sub-commands, the `dump-` and `restore-` are unnecessary. For example, `mysql-backup dump --frequency=60` or `mysql-backup restore --target=/foo/file.gz`.
* Config file keys are camelCase, for example, `dump.maxAllowedPacket=6000`.

For example, the following are equivalent.

Set dump frequency to 60 minutes:

* Environment variable: `DB_DUMP_FREQUENCY=60`
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

Various sample configuration files are available in the [examples/configs](../examples/configs/) directory.

## Configuration Options

The following are the environment variables, CLI flags and configuration file options for: backup(B), restore (R), prune (P).

| Purpose | Backup / Restore / Prune | CLI Flag | Env Var | Config Key | Default |
| --- | --- | --- | --- | --- | --- |
| config file path | BRP | `config` | `DB_DUMP_CONFIG` |  |  |
| hostname or unix domain socket path (starting with a slash) to connect to database. Required. | BR | `server` | `DB_SERVER` | `database.server` |  |
| port to use to connect to database. Optional. | BR | `port` | `DB_PORT` | `database.port` | 3306 |
| username for the database | BR | `user` | `DB_USER` | `database.credentials.username` |  |
| password for the database | BR | `pass` | `DB_PASS` | `database.credentials.password` |  |
| names of databases to dump, comma-separated | B | `include` | `DB_NAMES` | `dump.include` | all databases in the server |
| names of databases to exclude from the dump | B | `exclude` | `DB_NAMES_EXCLUDE` | `dump.exclude` |  |
| do not include `USE <database>;` statement in the dump | B | `no-database-name` | `NO_DATABASE_NAME` | `dump.noDatabaseName` | `false` |
| restore to a specific database | R | `restore --database` | `RESTORE_DATABASE` | `restore.database` |  |
| how often to do a dump or prune, in minutes | BP | `dump --frequency` | `DB_DUMP_FREQUENCY` | `dump.schedule.frequency` | `1440` (in minutes), i.e. once per day |
| what time to do the first dump or prune | BP | `dump --begin` | `DB_DUMP_BEGIN` | `dump.schedule.begin` | `0`, i.e. immediately |
| cron schedule for dumps or prunes | BP | `dump --cron` | `DB_DUMP_CRON` | `dump.schedule.cron` |  |
| run the backup or prune a single time and exit | BP | `dump --once` | `DB_DUMP_ONCE` | `dump.schedule.once` | `false` |
| enable debug logging | BRP | `debug` | `DB_DEBUG` | `logging` | `false` |
| where to put the dump file; see [backup](./backup.md) | BP | `dump --target` | `DB_DUMP_TARGET` | `dump.targets` |  |
| where the restore file exists; see [restore](./restore.md) | R | `restore --target` | `DB_RESTORE_TARGET` | `restore.target` |  |
| replace any `:` in the dump filename with `-` | BP | `dump --safechars` | `DB_DUMP_SAFECHARS` | `database.safechars` | `false` |
| AWS access key ID, used only if a target does not have one | BRP | `aws-access-key-id` | `AWS_ACCESS_KEY_ID` | `dump.targets[s3-target].accessKeyID` |  |
| AWS secret access key, used only if a target does not have one | BRP | `aws-secret-access-key` | `AWS_SECRET_ACCESS_KEY` | `dump.targets[s3-target].secretAccessKey` |  |
| AWS default region, used only if a target does not have one | BRP | `aws-region` | `AWS_REGION` | `dump.targets[s3-target].region` |  |
| alternative endpoint URL for S3-interoperable systems, used only if a target does not have one | BR | `aws-endpoint-url` | `AWS_ENDPOINT_URL` | `dump.targets[s3-target].endpoint` |  |
| path-style addressing for S3 bucket instead of default virtual-host-style addressing | BR | `aws-path-style` | `AWS_PATH_STYLE` | `dump.targets[s3-target].pathStyle` |  |
| SMB username, used only if a target does not have one | BRP | `smb-user` | `SMB_USER` | `dump.targets[smb-target].username` |  |
| SMB password, used only if a target does not have one | BRP | `smb-pass` | `SMB_PASS` | `dump.targets[smb-target].password` |  |
| compression to use, one of: `bzip2`, `gzip` | BP | `compression` | `DB_DUMP_COMPRESSION` | `dump.compression` | `gzip` |
| when in container, run the dump or restore with `nice`/`ionice` | BR | `` | `NICE` | `` | `false` |
| filename to save the target backup file | B | `dump --filename-pattern` | `DB_DUMP_FILENAME_PATTERN` | `dump.filenamePattern` |  |
| directory with scripts to execute before backup | B | `dump --pre-backup-scripts` | `DB_DUMP_PRE_BACKUP_SCRIPTS` | `dump.scripts.preBackup` | in container, `/scripts.d/pre-backup/` |
| directory with scripts to execute after backup | B | `dump --post-backup-scripts` | `DB_DUMP_POST_BACKUP_SCRIPTS` | `dump.scripts.postBackup` | in container, `/scripts.d/post-backup/` |
| directory with scripts to execute before restore | R | `restore --pre-restore-scripts` | `DB_DUMP_PRE_RESTORE_SCRIPTS` | `restore.scripts.preRestore` | in container, `/scripts.d/pre-restore/` |
| directory with scripts to execute after restore | R | `restore --post-restore-scripts` | `DB_DUMP_POST_RESTORE_SCRIPTS` | `restore.scripts.postRestore` | in container, `/scripts.d/post-restore/` |
| retention policy for backups | BP | `dump --retention` | `DB_DUMP_RETENTION` | `prune.retention` | Infinite |

## Configuration File

### Format

The config file is a YAML file. You can write the yaml configuration file by hand. Alternatively, you can use an online service
to generate one for you. Referenced services will be listed here in the future.

The keys are:

* `version`: the version of configuration, must be `config.databack.io/v1`
* `kind`: the kind of configuration, must be one of:
  * `local`: local configuration
  * `remote`: remote configuration
* `metadata`: metadata about the configuration. Not required. Used primarily for validating or optional information.
  * `name` (optional): the name of the configuration
  * `description` (optional): a description of the configuration
  * `digest` (optional): the digest of the configuration, excluding the `digest` key itself. Everything else, including optional metadata, is included.
  * `created` (optional): the date the configuration was created in [ISO8601 date format](https://en.wikipedia.org/wiki/ISO_8601), e.g. `2021-01-01T00:00:00Z`. The timezone always should be `Z` for UTC.
* `spec`: the specification. Varies by the `kind` of configuration.

The contents of `spec` depend on the kind of configuration.

#### Local Configuration

For local configuration, the `spec` is composed of the following. See the [Configuration Options](#configuration-options)
for details of each.

* `dump`: the dump configuration
  * `include`: list of tables to include
  * `exclude`: list of tables to exclude
  * `safechars`: safe characters in filename
  * `noDatabaseName`: remove `USE <database>` from dumpfile
  * `schedule`: the schedule configuration
    * `frequency`: the frequency of the schedule
    * `begin`: the time to begin the schedule
    * `cron`: the cron schedule
    * `once`: run once and exit
  * `compression`: the compression to use
  * `compact`: compact the dump
  * `maxAllowedPacket`: max packet size
  * `filenamePattern`: the filename pattern
  * `scripts`:
    * `preBackup`: path to directory with pre-backup scripts
    * `postBackup`: path to directory with post-backup scripts
  * `targets`: list of names of known targets, defined in the `targets` section, where to save the backup
* `restore`: the restore configuration
  * `scripts`:
    * `preRestore`: path to directory with pre-restore scripts
    * `postRestore`: path to directory with post-restore scripts
* `database`: the database configuration
  * `server`: host:port
  * `port`: port (deprecated)
  * `credentials`: access credentials for the database
    * `username`: user
    * `password`: password
* `prune`: the prune configuration
  * `retention`: retention policy
* `targets`: target configurations, each of which can be reference by other sections. Key is the name of the target that is referenced elsewhere. Each one has the following structure:
  * `type`: the type of target, one of: file, s3, smb
  * `url`: the URL of the target
  * `spec`: access details for the target, depends on target type:
    * Type s3:
      * `region`: the region
      * `endpoint`: the endpoint
      * `pathStyle` (boolean): use path-style bucket addressing instead of virtual-host style bucket addressing, see [AWS docs](https://docs.aws.amazon.com/AmazonS3/latest/userguide/VirtualHosting.html)
      * `accessKeyID`: the access key ID
      * `secretAccessKey`: the secret access key
    * Type smb:
      * `domain`: the domain
      * `username`: the username
      * `password`: the password
* `logging`: the log level, one of: error,warning,info,debug,trace; default is info
* `telemetry`: configuration for sending telemetry data (optional)
  * `url`: URL to telemetry service
  * `certificate`: the certificate for the telemetry server or a CA that signed the server's TLS certificate. Not required if telemetry server does not use TLS, or if the system's certificate store already contains the server's cert or CA.
  * `credentials`: unique token provided by the remote service as credentials, base64-encoded

#### Remote Configuration

For remote configuration, the `spec` is composed of the following:

* `url`: the URL of the remote configuration; required
* `certificate`: the certificate for the server or a CA that signed the server's TLS certificate. Not required if remote server does not use TLS, or if the system's certificate store already contains the server's cert or CA.
* `credentials`: unique token provided by the remote service as credentials, base64-encoded

The configuration file retrieved from a remote **always** has the same structure as any config file. It even can be
saved locally and used as a local configuration. This means it also can
reference another remote configuration, just like a local one. That can in turn reference another
and so on, ad infinitum. In practice, remote service will avoid this.

### Multiple Configurations

As of version 1.0 of `mysql-backup`, there is support only for one config file. This means:

* The `--config` flag can be used only once.
* The config file does not support [multiple yaml documents in a single file](https://yaml.org/spec/1.2.2/). If you ask it to read a yaml file with multiple documents sepaarted by `---`, it will read only the first one.
* You can have chaining, as described in the [remote configuration](#remote-configuration) section, where one file of kind `remote` references another, which itself is `remote`, etc. But only the final one will be used. It is not merging.

# mysql-backup

Back up mysql databases to... anywhere!

## Overview

mysql-backup is a simple way to do MySQL database backups and restores, as well as manage your backups.

It has the following features:

* dump and restore
* dump to local filesystem or to SMB server
* select database user and password
* connect to any container running on the same system
* select how often to run a dump
* select when to start the first dump, whether time of day or relative to container start time
* prune backups older than a specific time period or quantity

Please see [CONTRIBUTORS.md](./CONTRIBUTORS.md) for a list of contributors.

## Versions

This is the latest version, based on the complete rebuild of the codebase for 1.0.0 release based on
golang, completed in late 2023.

## Support

Support is available at the [databack Slack channel](http://databack.slack.com); register [here](https://join.slack.com/t/databack/shared_invite/zt-1cnbo2zfl-0dQS895icOUQy31RAruf7w). We accept issues here and general support questions on Slack.

If you are interested in commercial support, please contact us via Slack above.

## Running `mysql-backup`

`mysql-backup` is available both as a single standalone binary, and as a container image.

## Backup

To run a backup, launch `mysql-backup` - as a container or as a binary - with the correct parameters. 

For example:

````bash
docker run -d --restart=always -e DB_DUMP_FREQUENCY=60 -e DB_DUMP_BEGIN=2330 -e DB_DUMP_TARGET=/local/file/path -e DB_SERVER=my-db-address -v /local/file/path:/db databack/mysql-backup dump

# or

mysql-backup dump --frequency=60 --begin=2330 --target=/local/file/path --server=my-db-address

# or to connect to a local mysqld via the unix domain socket as the current user

mysql-backup dump --frequency=60 --begin=2330 --target=/local/file/path --server=/run/mysqld/mysqld.sock
````

Or `mysql-backup --config-file=/path/to/config/file.yaml` where `/path/to/config/file.yaml` is a file
with the following contents:

```yaml
server: my-db-address
dump:
  frequency: 60
  begin: 2330
  target: /local/file/path
```

The above will run a dump every 60 minutes, beginning at the next 2330 local time, from the database accessible in the container `my-db-address`.

````bash
docker run -d --restart=always -e DB_USER=user123 -e DB_PASS=pass123 -e DB_DUMP_FREQUENCY=60 -e DB_DUMP_BEGIN=2330 -e DB_DUMP_TARGET=/db -e DB_SERVER=my-db-address -v /local/file/path:/db databack/mysql-backup dump

# or

mysql-backup dump --user=user123 --pass=pass123 --frequency=60 --begin=2330 --target=/local/file/path --server=my-db-address --port=3306
````

See [backup](./docs/backup.md) for a more detailed description of performing backups.

See [configuration](./docs/configuration.md) for a detailed list of all configuration options.


## Restore

To perform a restore, you simply run the process in reverse. You still connect to a database, but instead of the
dump command, you pass it the restore command. Instead of a dump target, you pass it a restore target.

### Dump Restore

If you wish to run a restore to an existing database, you can use mysql-backup to do a restore.

You need only the following environment variables:

__You should consider the [use of `--env-file=`](https://docs.docker.com/engine/reference/commandline/run/#set-environment-variables-e-env-env-file) to keep your secrets out of your shell history__

* `DB_SERVER`: hostname or unix domain socket path (starting with a slash) to connect to database. Required.
* `DB_PORT`: port to use to connect to database. Optional, defaults to `3306`
* `DB_USER`: username for the database
* `DB_PASS`: password for the database
* `DB_NAMES`: names of databases to restore separated by spaces. Required if `SINGLE_DATABASE=true`.
* `SINGLE_DATABASE`: If is set to `true`, `DB_NAMES` is required and must contain exactly one database name. Mysql command will then run with `--database=$DB_NAMES` flag. This avoids the need of `USE <database>;` statement, which is useful when restoring from a file saved with `SINGLE_DATABASE` set to `true`.
* `DB_RESTORE_TARGET`: path to the actual restore file, which should be a compressed dump file. The target can be an absolute path, which should be volume mounted, an smb or S3 URL, similar to the target.
* `DB_DUMP_DEBUG`: if `true`, dump copious outputs to the container logs while restoring.
* To use the S3 driver `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` and `AWS_DEFAULT_REGION` will need to be defined.

Examples:

1. Restore from a local file: `docker run -e DB_SERVER=gotodb.example.com -e DB_USER=user123 -e DB_PASS=pass123 -e DB_RESTORE_TARGET=/backup/db_backup_201509271627.gz -v /local/path:/backup databack/mysql-backup restore`
2. Restore from a local file using ssl: `docker run -e DB_SERVER=gotodb.example.com -e DB_USER=user123 -e DB_PASS=pass123 -e DB_RESTORE_TARGET=/backup/db_backup_201509271627.gz -e RESTORE_OPTS="--ssl-cert /certs/client-cert.pem --ssl-key /certs/client-key.pem" -v /local/path:/backup -v /local/certs:/certs databack/mysql-backup restore`
2. Restore from an SMB file: `docker run -e DB_SERVER=gotodb.example.com -e DB_USER=user123 -e DB_PASS=pass123 -e DB_RESTORE_TARGET=smb://smbserver/share1/backup/db_backup_201509271627.gz databack/mysql-backup restore`
3. Restore from an S3 file: `docker run -e DB_SERVER=gotodb.example.com -e AWS_ACCESS_KEY_ID=awskeyid -e AWS_SECRET_ACCESS_KEY=secret -e AWS_REGION=eu-central-1 -e DB_USER=user123 -e DB_PASS=pass123 -e DB_RESTORE_TARGET=s3://bucket/path/db_backup_201509271627.gz databack/mysql-backup restore `

### Restore specific databases
If you have multiple schemas in your database, you can choose to restore only some of them.

To do this, you must restore using `DB_NAMES` to specify the schemas you want restored.

When doing this, schemas will be restored with their original name. To restore under other names, you must use `SINGLE_DATABASE=true` on both dump and restore, and you can only do it one schema at a time.

#### Examples:
1. Dump a multi-schemas database and restore only some of them:
   * `docker run -e DB_SERVER=gotodb.example.com -e DB_USER=user123 -e DB_PASS=pass123 -v /local/path:/backup databack/mysql-backup dump `
   * `docker run -e DB_SERVER=gotodb.example.com -e DB_USER=user123 -e DB_PASS=pass123 -e DB_RESTORE_TARGET=/backup/db_backup_201509271627.gz -e DB_NAMES="database1 database3" -v /local/path:/backup databack/mysql-backup restore`
2. Dump and restore a schema under a different name:
   * `docker run -e DB_SERVER=gotodb.example.com -e DB_USER=user123 -e DB_PASS=pass123 -e SINGLE_DATABASE=true -e DB_NAMES=database1 -v /local/path:/backup databack/mysql-backup dump`
   * `docker run -e DB_SERVER=gotodb.example.com -e DB_USER=user123 -e DB_PASS=pass123 -e DB_RESTORE_TARGET=/backup/db_backup_201509271627.gz -e SINGLE_DATABASE=true DB_NAMES=newdatabase1 -v /local/path:/backup databack/mysql-backup restore`

See [restore](./docs/restore.md) for a more detailed description of performing restores.

See [configuration](./docs/configuration.md) for a detailed list of all configuration options.

## License
Released under the MIT License.
Copyright Avi Deitcher https://github.com/deitch

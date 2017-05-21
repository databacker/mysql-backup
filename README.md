# mysql-backup

## Overview
mysql-backup is a simple way to do MySQL database backups and restores when the database is running in a container.

It has the following features:

* dump and restore
* dump to local filesystem or to SMB server
* select database user and password
* connect to any container running on the same system
* select how often to run a dump
* select when to start the first dump, whether time of day or relative to container start time


## Backup
To run a backup, launch `mysql-backup` image as a container with the correct parameters. Everything is controlled by environment variables passed to the container.

For example:

````bash
docker run -d --restart=always -e DB_DUMP_FREQ=60 -e DB_DUMP_BEGIN=2330 -e DB_DUMP_TARGET=/db --link my-db-container:db -v /local/file/path:/db deitch/mysql-backup
````

The above will run a dump every 60 minutes, beginning at the next 2330 local time, from the database accessible in the container `my-db-container`.

The following are the environment variables for a backup:

__You should consider the [use of `--env-file=`](https://docs.docker.com/engine/reference/commandline/run/#set-environment-variables-e-env-env-file) to keep your secrets out of your shell history__

* `DB_USER`: username for the database
* `DB_PASS`: password for the database
* `DB_NAMES`: names of databases to dump; defaults to all databases in the database server
* `DB_DUMP_FREQ`: How often to do a dump, in minutes. Defaults to 1440 minutes, or once per day.
* `DB_DUMP_BEGIN`: What time to do the first dump. Defaults to immediate. Must be in one of two formats:
 * Absolute: HHMM, e.g. `2330` or `0415`
 * Relative: +MM, i.e. how many minutes after starting the container, e.g. `+0` (immediate), `+10` (in 10 minutes), or `+90` in an hour and a half
* `DB_DUMP_DEBUG`: If set to `true`, print copious shell script messages to the container log. Otherwise only basic messages are printed.
* `DB_DUMP_TARGET`: Where to put the dump file, should be a directory. Supports three formats:
 * Local: If the value of `DB_DUMP_TARGET` starts with a `/` character, will dump to a local path, which should be volume-mounted.
 * SMB: If the value of `DB_DUMP_TARGET` is a URL of the format `smb://hostname/share/path/` then it will connect via SMB.
 * S3: If the value of `DB_DUMP_TARGET` is a URL of the format `s3://bucketname/path` then it will connect via awscli.
  * `AWS_ACCESS_KEY_ID`: AWS Key ID
  * `AWS_SECRET_ACCESS_KEY`: AWS Secret Access Key
  * `AWS_DEFAULT_REGION`: Region in which the bucket resides


### Database Container
In order to perform the actual dump, `mysql-backup` needs to connect to the database container. You should link to the container by passing the `--link` option to the `mysql-backup` container. The linked container should **always** be aliased to `db`. E.g.:

````bash
docker run -d --restart=always -e DB_USER=user123 -e DB_PASS=pass123 -e DB_DUMP_FREQ=60 -e DB_DUMP_BEGIN=2330 -e DB_DUMP_TARGET=/db --link my-db-container:db -v /local/file/path:/db deitch/mysql-backup
````

### Dump Target
The dump target is where you want the backup files to be saved. The backup file *always* is a gzipped file the following format:

`db_backup_YYYYMMDDHHmm.sql.gz`

Where:

* YYYY = year in 4 digits
* MM = month number from 01-12
* DD = date for 01-31
* HH = hour from 00-23
* mm = minute from 00-59

The time used is UTC time at the moment the dump begins.

The dump target is the location where the dump should be placed, defaults to `/backup` in the container. Of course, having the backup in the container does not help very much, so we very strongly recommend you volume mount it outside somewhere. See the above example.

If you use a URL like `smb://host/share/path`, you can have it save to an SMB server. If you need loging credentials, use `smb://user:pass@host/share/path`.

Note that for smb, if the username includes a domain, e.g. your user is `mydom\myuser`, then you should use the samb convention of replacing the '\' with a ';'. In other words `smb://mydom;myuser:pass@host/share/path`

If you use a URL like `s3://bucket/path`, you can have it save to an S3 bucket.

Note that for s3, you'll need to specify your AWS credentials and default AWS region via `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` and `AWS_DEFAULT_REGION`

### Dump Restore
If you wish to run a restore to an existing database, you can use mysql-backup to do a restore.

You need only the following environment variables:

__You should consider the [use of `--env-file=`](https://docs.docker.com/engine/reference/commandline/run/#set-environment-variables-e-env-env-file) to keep your secrets out of your shell history__

* `DB_USER`: username for the database
* `DB_PASS`: password for the database
* `DB_RESTORE_TARGET`: path to the actual restore file, which should be a gzip of an sql dump file. The target can be an absolute path, which should be volume mounted, an smb or S3 URL, similar to the target.
* `DB_DUMP_DEBUG`: if `true`, dump copious outputs to the container logs while restoring.
* To use the S3 driver `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` and `AWS_DEFAULT_REGION` will need to be defined.


Examples:

1. Restore from a local file: `docker run  -e DB_USER=user123 -e DB_PASS=pass123 -e DB_RESTORE_TARGET=/backup/db_backup_201509271627.sql.gz -v /local/path:/backup deitch/mysql-backup`
2. Restore from an SMB file: `docker run  -e DB_USER=user123 -e DB_PASS=pass123 -e DB_RESTORE_TARGET=smb://smbserver/share1/backup/db_backup_201509271627.sql.gz deitch/mysql-backup`
3. Restore from an S3 file: `docker run  -e AWS_ACCESS_KEY_ID=awskeyid -e AWS_SECRET_ACCESS_KEY=secret -e AWS_DEFAULT_REGION=eu-central-1 -e DB_USER=user123 -e DB_PASS=pass123 -e DB_RESTORE_TARGET=s3://bucket/path/db_backup_201509271627.sql.gz deitch/mysql-backup`


### Automated Build
This gituhub repo is the source for the mysql-backup image. The actual image is stored on the docker hub at `deitch/mysql-backup`, and is triggered with each commit to the source by automated build via Webhooks.

There are 2 builds: 1 for version based on the git tag, and another for the particular version number.

## License
Released under the MIT License.
Copyright Avi Deitcher https://github.com/deitch

Thanks to the kind contributions and support of [TraderTools](http://www.tradertools.com).

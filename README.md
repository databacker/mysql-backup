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

Please see [CONTRIBUTORS.md](./CONTRIBUTORS.md) for a list of contributors.

## Backup
To run a backup, launch `mysql-backup` image as a container with the correct parameters. Everything is controlled by environment variables passed to the container.

For example:

````bash
docker run -d --restart=always -e DB_DUMP_FREQ=60 -e DB_DUMP_BEGIN=2330 -e DB_DUMP_TARGET=/db -e DBSERVER=my-db-container -v /local/file/path:/db deitch/mysql-backup
````

The above will run a dump every 60 minutes, beginning at the next 2330 local time, from the database accessible in the container `my-db-container`.

The following are the environment variables for a backup:

__You should consider the [use of `--env-file=`](https://docs.docker.com/engine/reference/commandline/run/#set-environment-variables-e-env-env-file) to keep your secrets out of your shell history__

* `DBSERVER`: hostname to connect to database. Required.
* `DBPORT`: port to use to connect to database. Optional, defaults to `3306`
* `DB_USER`: username for the database
* `DB_PASS`: password for the database
* `DB_NAMES`: names of databases to dump; defaults to all databases in the database server
* `DB_DUMP_FREQ`: How often to do a dump, in minutes. Defaults to 1440 minutes, or once per day.
* `DB_DUMP_BEGIN`: What time to do the first dump. Defaults to immediate. Must be in one of two formats:
    * Absolute: HHMM, e.g. `2330` or `0415`
    * Relative: +MM, i.e. how many minutes after starting the container, e.g. `+0` (immediate), `+10` (in 10 minutes), or `+90` in an hour and a half
* `RUN_ONCE`: Run the backup once and exit if `RUN_ONCE` is set. Useful if you use an external scheduler (e.g. as part of an orchestration solution like Cattle or Docker Swarm) and don't want the container to do the scheduling internally. If you use this option, the `DB_DUMP_FREQ` and `DB_DUMP_BEGIN` become obsolete. 
* `DB_DUMP_DEBUG`: If set to `true`, print copious shell script messages to the container log. Otherwise only basic messages are printed.
* `DB_DUMP_TARGET`: Where to put the dump file, should be a directory. Supports three formats:
    * Local: If the value of `DB_DUMP_TARGET` starts with a `/` character, will dump to a local path, which should be volume-mounted.
    * SMB: If the value of `DB_DUMP_TARGET` is a URL of the format `smb://hostname/share/path/` then it will connect via SMB.
    * S3: If the value of `DB_DUMP_TARGET` is a URL of the format `s3://bucketname/path` then it will connect via awscli.
* `AWS_ACCESS_KEY_ID`: AWS Key ID
* `AWS_SECRET_ACCESS_KEY`: AWS Secret Access Key
* `AWS_DEFAULT_REGION`: Region in which the bucket resides
* `SMB_USER`: SMB username. May also be specified in `DB_DUMP_TARGET` with an `smb://` url. If both specified, this variable overrides the value in the URL.
* `SMB_PASS`: SMB password. May also be specified in `DB_DUMP_TARGET` with an `smb://` url. If both specified, this variable overrides the value in the URL.


### Database Container
In order to perform the actual dump, `mysql-backup` needs to connect to the database container. You **must** pass the database hostname - which can be another container or any database process accessible from the backup container - by passing the environment variable `DBSERVER` with the hostname or IP address of the database. You **may** override the default port of `3306` by passing the environment variable `DBPORT`.

````bash
docker run -d --restart=always -e DB_USER=user123 -e DB_PASS=pass123 -e DB_DUMP_FREQ=60 -e DB_DUMP_BEGIN=2330 -e DB_DUMP_TARGET=/db -e DBSERVER=my-db-container -v /local/file/path:/db deitch/mysql-backup
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

### Backup pre and post processing

Any executable script with _.sh_ extension in _/scripts.d/pre-backup/_ or _/scripts.d/post-backup/_ directories in the container will be executed before
and after the backup dump process has finished respectively, but **before**
uploading the backup file to its ultimate target. This is useful if you need to
include some files along with the database dump, for example, to backup a
_WordPress_ install.

To use them you need to add a host volume that points to the post-backup scripts in the docker host. Start the container like this:

````bash
docker run -d --restart=always -e DB_USER=user123 -e DB_PASS=pass123 -e DB_DUMP_FREQ=60 \
  -e DB_DUMP_BEGIN=2330 -e DB_DUMP_TARGET=/db -e DBSERVER=my-db-container:db \
  -v /path/to/pre-backup/scripts:/scripts.d/pre-backup \
  -v /path/to/post-backup/scripts:/scripts.d/post-backup \
  -v /local/file/path:/db \
  deitch/mysql-backup
````

Or, if you prefer compose:

```yml
version: '2.1'
services:
  backup:
    image: deitch/mysql-backup
    restart: always
    volumes:
     - /local/file/path:/db
     - /path/to/pre-backup/scripts:/scripts.d/pre-backup
     - /path/to/post-backup/scripts:/scripts.d/post-backup
    env:
     - DB_DUMP_TARGET=/db
     - DB_USER=user123
     - DB_PASS=pass123
     - DB_DUMP_FREQ=60
     - DB_DUMP_BEGIN=2330
     - DBSERVER=mysql_db
  mysql_db:
    image: mysql
    ....
```

The scripts are _executed_ in the [entrypoint](https://github.com/deitch/mysql-backup/blob/master/entrypoint) script, which means it has access to all exported environment variables. The following are available, but we are happy to export more as required (just open an issue or better yet, a pull request):

* `DUMPFILE`: full path in the container to the output file
* `NOW`: date of the backup, as included in `DUMPFILE` and given by `date -u +"%Y%m%d%H%M%S"`
* `DUMPDIR`: path to the destination directory so for example you can copy a new tarball including some other files along with the sql dump.
* `DB_DUMP_DEBUG`: To enable debug mode in post-backup scripts.

In addition, all of the environment variables set for the container will be available to the script.

For example, the following script will rename the backup file after the dump is done:

````bash
#!/bin/bash
# Rename backup file.
if [[ -n "$DB_DUMP_DEBUG" ]]; then
  set -x
fi

if [ -e ${DUMPFILE} ];
then
  now=$(date +"%Y-%m-%d-%H_%M")
  new_name=db_backup-${now}.gz
  old_name=$(basename ${DUMPFILE})
  echo "Renaming backup file from ${old_name} to ${new_name}"
  mv ${DUMPFILE} ${DUMPDIR}/${new_name}
else
  echo "ERROR: Backup file ${DUMPFILE} does not exist!"
fi

````

You can think of this as a sort of basic plugin system. Look at the source of the [entrypoint](https://github.com/deitch/mysql-backup/blob/master/entrypoint) script for other variables that can be used.

## Restore
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

### Restore pre and post processing

As with backups pre and post processing, you can do the same with restore operations.
Any executable script with _.sh_ extension in _/scripts.d/pre-restore/_ or
_/scripts.d/post-restore/_ directories in the container will be executed before the restore process starts and after it finishes respectively. This is useful if you need to
restore a backup file that includes some files along with the database dump.

For example, to restore a _WordPress_ install, you would uncompress a tarball containing
the db backup and a second tarball with the contents of a WordPress install on
`pre-restore`. Then on `post-restore`, uncompress the WordPress files on the container's web server root directory.

For an example take a look at the post-backup examples, all variables defined for post-backup scripts are available for pre-processing too. Also don't forget to add the same host volumes for `pre-restore` and `post-restore` directories as described for post-backup processing.

### Automated Build
This gituhub repo is the source for the mysql-backup image. The actual image is stored on the docker hub at `deitch/mysql-backup`, and is triggered with each commit to the source by automated build via Webhooks.

There are 2 builds: 1 for version based on the git tag, and another for the particular version number.

## License
Released under the MIT License.
Copyright Avi Deitcher https://github.com/deitch

Thanks to the kind contributions and support of [TraderTools](http://www.tradertools.com).

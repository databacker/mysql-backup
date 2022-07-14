# mysql-backup
Back up mysql databases to... anywhere!

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

## Support

Support is available at the [databack Slack channel](http://databack.slack.com); register [here](https://join.slack.com/t/databack/shared_invite/zt-1cnbo2zfl-0dQS895icOUQy31RAruf7w). We accept issues here and general support questions on Slack.

## Backup
To run a backup, launch `mysql-backup` image as a container with the correct parameters. Everything is controlled by environment variables passed to the container.

For example:

````bash
docker run -d --restart=always -e DB_DUMP_FREQ=60 -e DB_DUMP_BEGIN=2330 -e DB_DUMP_TARGET=/db -e DB_SERVER=my-db-container -v /local/file/path:/db databack/mysql-backup
````

The above will run a dump every 60 minutes, beginning at the next 2330 local time, from the database accessible in the container `my-db-container`.

The following are the environment variables for a backup:

__You should consider the [use of `--env-file=`](https://docs.docker.com/engine/reference/commandline/run/#set-environment-variables-e-env-env-file), [docker secrets](https://docs.docker.com/engine/swarm/secrets/) to keep your secrets out of your shell history__

* `DB_SERVER`: hostname to connect to database. Required.
* `DB_PORT`: port to use to connect to database. Optional, defaults to `3306`
* `DB_USER`: username for the database
* `DB_PASS`: password for the database
* `DB_NAMES`: names of databases to dump (separated by space); defaults to all databases in the database server
* `DB_NAMES_EXCLUDE`: names of databases (separated by space) to exclude from the dump; `information_schema`. `performance_schema`, `sys` and `mysql` are excluded by default. This only applies if `DB_DUMP_BY_SCHEMA` is set to `true`. For example, if you set `DB_NAMES_EXCLUDE=database1 db2` and `DB_DUMP_BY_SCHEMA=true` then these two databases will not be dumped by mysqldump
* `SINGLE_DATABASE`: If is set to `true`, mysqldump command will run without `--databases` flag. This avoid `USE <database>;` statement which is useful for the cases in which you want to import the dumpfile into a database with a different name.
* `DB_DUMP_FREQ`: How often to do a dump, in minutes. Defaults to 1440 minutes, or once per day.
* `DB_DUMP_BEGIN`: What time to do the first dump. Defaults to immediate. Must be in one of two formats:
    * Absolute: HHMM, e.g. `2330` or `0415`
    * Relative: +MM, i.e. how many minutes after starting the container, e.g. `+0` (immediate), `+10` (in 10 minutes), or `+90` in an hour and a half
* `DB_DUMP_CRON`: Set the dump schedule using standard [crontab syntax](https://en.wikipedia.org/wiki/Cron), a single line.
* `RUN_ONCE`: Run the backup once and exit if `RUN_ONCE` is set. Useful if you use an external scheduler (e.g. as part of an orchestration solution like Cattle or Docker Swarm or [kubernetes cron jobs](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/)) and don't want the container to do the scheduling internally. If you use this option, all other scheduling options, like `DB_DUMP_FREQ` and `DB_DUMP_BEGIN` and `DB_DUMP_CRON`, become obsolete.
* `DB_DUMP_DEBUG`: If set to `true`, print copious shell script messages to the container log. Otherwise only basic messages are printed.
* `DB_DUMP_TARGET`: Where to put the dump file, should be a directory. Supports four formats:
    * Local: If the value of `DB_DUMP_TARGET` starts with a `/` character, will dump to a local path, which should be volume-mounted.
    * SMB: If the value of `DB_DUMP_TARGET` is a URL of the format `smb://hostname/share/path/` then it will connect via SMB.
    * S3: If the value of `DB_DUMP_TARGET` is a URL of the format `s3://bucketname/path` then it will connect via awscli.
    * Multiple: If the value of `DB_DUMP_TARGET` contains multiple targets, the targets should be separated by a whitespace **and** the value surrounded by quotes, e.g. `"/db s3://bucketname/path"`.
* `DB_DUMP_SAFECHARS`: The dump filename usually includes the character `:` in the date, to comply with RFC3339. Some systems and shells don't like that character. If this environment variable is set, it will replace all `:` with `-`.
* `AWS_ACCESS_KEY_ID`: AWS Key ID
* `AWS_SECRET_ACCESS_KEY`: AWS Secret Access Key
* `AWS_DEFAULT_REGION`: Region in which the bucket resides
* `AWS_ENDPOINT_URL`: Specify an alternative endpoint for s3 interopable systems e.g. Digitalocean
* `AWS_CLI_OPTS`: Additional arguments to be passed to the `aws` part of the `aws s3 cp` command, click [here](https://docs.aws.amazon.com/cli/latest/reference/#options) for a list. _Be careful_, as you can break something!
* `AWS_CLI_S3_CP_OPTS`: Additional arguments to be passed to the `s3 cp` part of the `aws s3 cp` command, click [here](https://docs.aws.amazon.com/cli/latest/reference/s3/cp.html#options) for a list. If you are using AWS KMS, `sse`, `sse-kms-key-id`, etc., may be of interest.
* `SMB_USER`: SMB username. May also be specified in `DB_DUMP_TARGET` with an `smb://` url. If both specified, this variable overrides the value in the URL.
* `SMB_PASS`: SMB password. May also be specified in `DB_DUMP_TARGET` with an `smb://` url. If both specified, this variable overrides the value in the URL.
* `COMPRESSION`: Compression to use. Supported are: `gzip` (default), `bzip2`
* `DB_DUMP_BY_SCHEMA`: Whether to use separate files per schema in the compressed file (`true`), or a single dump file (`false`). Defaults to `false`.
* `DB_DUMP_KEEP_PERMISSIONS`: Whether to keep permissions for a file target. By default, `mysql-backup` copies the backup compressed file to the target with `cp -a`. In certain filesystems with certain permissions, this may cause errors. You can disable the `-a` flag by setting `DB_DUMP_KEEP_PERMISSIONS=false`. Defaults to `true`.
* `MYSQLDUMP_OPTS`: A string of options to pass to `mysqldump`, e.g. `MYSQLDUMP_OPTS="--opt abc --param def --max_allowed_packet=123455678"` will run `mysqldump --opt abc --param def --max_allowed_packet=123455678`
* `NICE`: true to perform mysqldump with ionice and nice option:- check for more information :- http://eosrei.net/articles/2013/03/forcing-mysqldump-always-be-nice-cpu-and-io
* `TMP_PATH`: tmp directory to be used during backup creation and other operations. Optional, defaults to `/tmp`

### Scheduling
There are several options for scheduling how often a backup should run:

* `RUN_ONCE`: run just once and exit.
* `DB_DUMP_FREQ` and `DB_DUMP_BEGIN`: run every x minutes, and run the first one at a particular time.
* `DB_DUMP_CRON`: run on a schedule.

#### Cron Scheduling
If a cron-scheduled backup takes longer than the beginning of the next backup window, it will be skipped. For example, if your cron line is scheduled to backup every hour, as follows:

```
0 * * * *
```

And the backup that runs at 13:00 finishes at 14:05, the next backup will not be immediate, but rather at 15:00.

The cron algorithm is as follows: after each backup run, calculate the next time that the cron statement will be true and schedule the backup then.

#### Order of Priority
The scheduling options have an order of priority:

1. `RUN_ONCE` runs once, immediately, and exits, ignoring everything else.
2. `DB_DUMP_CRON`: runs according to the cron schedule, ignoring `DB_DUMP_FREQ` and `DB_DUMP_BEGIN`.
3. `DB_DUMP_FREQ` and `DB_DUMP_BEGIN`: if nothing else is set.



### Permissions
By default, the backup/restore process does **not** run as root (UID O). Whenever possible, you should run processes (not just in containers) as users other than root. In this case, it runs as username `appuser` with UID/GID `1005`.

In most scenarios, this will not affect your backup process negatively. However, if you are using the "Local" dump target, i.e. your `DB_DUMP_TARGET` starts with `/` - and, most likely, is a volume mounted into the container - you can run into permissions issues. For example, if your mounted directory is owned by root on the host, then the backup process will be unable to write to it.

In this case, you have two options:

* Run the container as root, `docker run --user 0 ... ` or, in i`docker-compose.yml`, `user: "0"`
* Ensure your mounted directory is writable as UID or GID `1005`.


### Database Container
In order to perform the actual dump, `mysql-backup` needs to connect to the database container. You **must** pass the database hostname - which can be another container or any database process accessible from the backup container - by passing the environment variable `DB_SERVER` with the hostname or IP address of the database. You **may** override the default port of `3306` by passing the environment variable `DB_PORT`.

````bash
docker run -d --restart=always -e DB_USER=user123 -e DB_PASS=pass123 -e DB_DUMP_FREQ=60 -e DB_DUMP_BEGIN=2330 -e DB_DUMP_TARGET=/db -e DB_SERVER=my-db-container -v /local/file/path:/db databack/mysql-backup
````

### Dump Target

The dump target is where you want the backup files to be saved. The backup file *always* is a compressed file the following format:

`db_backup_YYYY-MM-DDTHH:mm:ssZ.<compression>`

Where the date is RFC3339 date format, excluding the milliseconds portion.

* YYYY = year in 4 digits
* MM = month number from 01-12
* DD = date for 01-31
* HH = hour from 00-23
* mm = minute from 00-59
* ss = seconds from 00-59
* T = literal character `T`, indicating the separation between date and time portions
* Z = literal character `Z`, indicating that the time provided is UTC, or "Zulu"
* compression = appropriate file ending for selected compression, one of: `gz` (gzip, default); `bz2` (bzip2)

The time used is UTC time at the moment the dump begins.

Notes on format:

* SMB does not allow for `:` in a filename (depending on server options), so they are replaced with the `-` character when writing to SMB.
* Some shells do not handle a `:` in the filename gracefully. Although these usually are legitimate characters as far as the _filesystem_ is concerned, your shell may not like it. To avoid this issue, you can set the "no-colons" options with the environment variable `DB_DUMP_SAFECHARS`

The dump target is the location where the dump should be placed, defaults to `/backup` in the container. Of course, having the backup in the container does not help very much, so we very strongly recommend you volume mount it outside somewhere. See the above example.

If you use a URL like `smb://host/share/path`, you can have it save to an SMB server. If you need loging credentials, use `smb://user:pass@host/share/path`.

Note that for smb, if the username includes a domain, e.g. your user is `mydom\myuser`, then you should use the samb convention of replacing the '\' with a ';'. In other words `smb://mydom;myuser:pass@host/share/path`

If you use a URL like `s3://bucket/path`, you can have it save to an S3 bucket.

Note that for s3, you'll need to specify your AWS credentials and default AWS region via `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` and `AWS_DEFAULT_REGION`

Also note that if you are using an s3 interopable storage system like DigitalOcean you can use that as the target by setting `AWS_ENDPOINT_URL` to `${REGION_NAME}.digitaloceanspaces.com` and setting `DB_DUMP_TARGET` to `s3://bucketname/path`.   

#### Custom backup source file name
There may be use-cases where you need to modify the source path of the backup file **before** it gets uploaded to the dump target.
An example is combining multiple compressed files into one and giving it a new name, i.e. ```db-other-files-combined.tar.gz```.
To do that, place an executable file called `source.sh` in the following path:

      /scripts.d/source.sh

Whatever your script returns to _stdout_ will be used as the source name for the backup file.

The following exported environment variables will be available to the script above:

* `DUMPFILE`: full path in the container to the output file
* `NOW`: date of the backup, as included in `DUMPFILE` and given by `date -u +"%Y-%m-%dT%H:%M:%SZ"`
* `DUMPDIR`: path to the destination directory so for example you can copy a new tarball including some other files along with the sql dump.
* `DB_DUMP_DEBUG`: To enable debug mode in post-backup scripts.

**Example run:**

      NOW=20180930151304 DUMPFILE=/tmp/backups/db_backup_201809301513.gz DUMPDIR=/backup DB_DUMP_DEBUG=true /scripts.d/source.sh

**Example custom source script:**

```bash
  #!/bin/bash

  # Rename source file
  echo -n "db-plus-wordpress_${NOW}.gz"
```           

#### Custom backup target file name
There may be use-cases where you need to modify the target upload path of the backup file **before** it gets uploaded.
An example is uploading a backup to a date stamped object key path in S3, i.e. ```s3://bucket/2018/08/23/path```.
To do that, place an executable file called ```target.sh``` in the following path:

      /scripts.d/target.sh

Whatever your script returns to _stdout_ will be used as the name for the backup file.

The following exported environment variables will be available to the script above:

* `DUMPFILE`: full path in the container to the output file
* `NOW`: date of the backup, as included in `DUMPFILE` and given by `date -u +"%Y-%m-%dT%H:%M:%SZ"`
* `DUMPDIR`: path to the destination directory so for example you can copy a new tarball including some other files along with the sql dump.
* `DB_DUMP_DEBUG`: To enable debug mode in post-backup scripts.

**Example run:**

      NOW=20180930151304 DUMPFILE=/tmp/backups/db_backup_201809301513.gz DUMPDIR=/backup DB_DUMP_DEBUG=true /scripts.d/target.sh

**Example custom target script:**

```bash
  #!/bin/bash

  # Rename target file
  echo -n "db-plus-wordpress-uploaded_${NOW}.gz"
```

### Backup pre and post processing

Any executable script with _.sh_ extension in _/scripts.d/pre-backup/_ or _/scripts.d/post-backup/_ directories in the container will be executed before
and after the backup dump process has finished respectively, but **before**
uploading the backup file to its ultimate target. This is useful if you need to
include some files along with the database dump, for example, to backup a
_WordPress_ install.

To use them you need to add a host volume that points to the post-backup scripts in the docker host. Start the container like this:

````bash
docker run -d --restart=always -e DB_USER=user123 -e DB_PASS=pass123 -e DB_DUMP_FREQ=60 \
  -e DB_DUMP_BEGIN=2330 -e DB_DUMP_TARGET=/db -e DB_SERVER=my-db-container:db \
  -v /path/to/pre-backup/scripts:/scripts.d/pre-backup \
  -v /path/to/post-backup/scripts:/scripts.d/post-backup \
  -v /local/file/path:/db \
  databack/mysql-backup
````

Or, if you prefer compose:

```yml
version: '2.1'
services:
  backup:
    image: databack/mysql-backup
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
     - DB_SERVER=mysql_db
  mysql_db:
    image: mysql
    ....
```

The scripts are _executed_ in the [entrypoint](https://github.com/databack/mysql-backup/blob/master/entrypoint) script, which means it has access to all exported environment variables. The following are available, but we are happy to export more as required (just open an issue or better yet, a pull request):

* `DUMPFILE`: full path in the container to the output file
* `NOW`: date of the backup, as included in `DUMPFILE` and given by `date -u +"%Y-%m-%dT%H:%M:%SZ"`
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

You can think of this as a sort of basic plugin system. Look at the source of the [entrypoint](https://github.com/databack/mysql-backup/blob/master/entrypoint) script for other variables that can be used.

### Encrypting the Backup

Post-processing also give you options to encrypt the backup using openssl. The openssl binary is available
to the processing scripts.

The sample [examples/encrypt.sh](./examples/encrypt.sh) provides a sample post-processing script that you can use
to encrypt your backup with AES256.

## Restore
### Dump Restore
If you wish to run a restore to an existing database, you can use mysql-backup to do a restore.

You need only the following environment variables:

__You should consider the [use of `--env-file=`](https://docs.docker.com/engine/reference/commandline/run/#set-environment-variables-e-env-env-file) to keep your secrets out of your shell history__

* `DB_SERVER`: hostname to connect to database. Required.
* `DB_PORT`: port to use to connect to database. Optional, defaults to `3306`
* `DB_USER`: username for the database
* `DB_PASS`: password for the database
* `DB_NAMES`: name of database to restore to. Required if `SINGLE_DATABASE=true`, otherwise has no effect. Although the name is plural, it must contain exactly one database name.
* `SINGLE_DATABASE`: If is set to `true`, `DB_NAMES` is required and mysql command will run with `--database=$DB_NAMES` flag. This avoids the need of `USE <database>;` statement, which is useful when restoring from a file saved with `SINGLE_DATABASE` set to `true`.
* `DB_RESTORE_TARGET`: path to the actual restore file, which should be a compressed dump file. The target can be an absolute path, which should be volume mounted, an smb or S3 URL, similar to the target.
* `DB_DUMP_DEBUG`: if `true`, dump copious outputs to the container logs while restoring.
* To use the S3 driver `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` and `AWS_DEFAULT_REGION` will need to be defined.


Examples:

1. Restore from a local file: `docker run -e DB_SERVER=gotodb.example.com -e DB_USER=user123 -e DB_PASS=pass123 -e DB_RESTORE_TARGET=/backup/db_backup_201509271627.gz -v /local/path:/backup databack/mysql-backup`
2. Restore from an SMB file: `docker run -e DB_SERVER=gotodb.example.com -e DB_USER=user123 -e DB_PASS=pass123 -e DB_RESTORE_TARGET=smb://smbserver/share1/backup/db_backup_201509271627.gz databack/mysql-backup`
3. Restore from an S3 file: `docker run -e DB_SERVER=gotodb.example.com -e AWS_ACCESS_KEY_ID=awskeyid -e AWS_SECRET_ACCESS_KEY=secret -e AWS_DEFAULT_REGION=eu-central-1 -e DB_USER=user123 -e DB_PASS=pass123 -e DB_RESTORE_TARGET=s3://bucket/path/db_backup_201509271627.gz databack/mysql-backup`

### Restore when using docker-compose
`docker-compose` automagically creates a network when started. `docker run` simply attaches to the bridge network. If you are trying to communicate with a mysql container started by docker-compose, you'll need to specify the network in your command arguments. You can use `docker network ls` to see what network is being used, or you can declare a network in your docker-compose.yml.

#### Example:
`docker run -e DB_SERVER=gotodb.example.com -e DB_USER=user123 -e DB_PASS=pass123 -e DB_RESTORE_TARGET=/backup/db_backup_201509271627.gz -v /local/path:/backup --network="skynet" databack/mysql-backup`

### Using docker (or rancher) secrets
Environment variables used in this image can be passed in files as well. This is useful when you are using docker (or rancher) secrets for storing sensitive information.

As you can set environment variable with `-e ENVIRONMENT_VARIABLE=value`, you can also use `-e ENVIRONMENT_VARIABLE_FILE=/path/to/file`. Contents of that file will be assigned to the environment variable.

**Example:**

```bash
docker run -d \
  -e DB_HOST_FILE=/run/secrets/DB_HOST \
  -e DB_USER_FILE=/run/secrets/DB_USER \
  -e DB_PASS_FILE=/run/secrets/DB_PASS \
  -v /local/file/path:/db \
  databack/mysql-backup
```

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
This github repo is the source for the mysql-backup image. The actual image is stored on the docker hub at `databack/mysql-backup`, and is triggered with each commit to the source by automated build via Webhooks.

There are 2 builds: 1 for version based on the git tag, and another for the particular version number.

## Tests

The tests all run in docker containers, to avoid the need to install anything other than `make` and `docker`, and even can run over remote docker connections, avoiding any local bind-mounts. To run all tests:

```
make test
```

To run with debugging

```
make test DEBUG=debug
```

The above will generate _copious_ outputs, so you might want to redirect stdout and stderr to a file.

This runs each of the several testing targets, each of which is a script in `test/test_*.sh`, which sets up tests, builds containers, runs the tests, and collects the output.

## License
Released under the MIT License.
Copyright Avi Deitcher https://github.com/deitch

# Backing Up

Backing up is the process of taking backups from your database via `mysql-backup`, and saving the backup file
to a target. That target can be one of:

* local file
* SMB remote file
* S3 bucket

## Instructions and Examples for Backup Configuration Options

### Database Names

By default, all databases in the database server are backed up, and the system databases
named `information_schema`, `performance_schema`, `sys` and `mysql` are excluded.
For example, if you set `DB_NAMES_EXCLUDE=database1 db2` then these two databases will not be dumped.

**Dumping just some databases**

* Environment variable: `DB_NAMES=db1,db2,db3`
* CLI flag: `--include=db1 --include=db2 --include=db3`
* Config file:
```yaml
dump:
  include:
  - db1
  - db2
  - db3
```

**Dumping all databases**

* Environment variable: `DB_NAMES=`
* CLI flag: `--include=`
* Config file:
```yaml
dump:
  include:
```

Note that you do not need to set those explicitly; these are the defaults for those settings.

**Dumping all databases except for one**

* Environment variable: `DB_NAMES_EXCLUDE=notme,notyou`
* CLI flag: `--exclude=notme,notyou`
* Config file:
```yaml
dump:
  exclude:
  - notme
  - notyou
```

### No Database Name

By default, the backup assumes you will restore the dump into a database with the same name as the
one that you backed up. This means it will include the `USE <database>;` statement in the dump, so
it will switch to the correct database when you restore the dump.

If you do not want the `USE` statement in the backup file, for example if you might want to restore the dump to a different
database, you need to remove the `USE <database>;` statement from the dump. `mysql-backup` does this for you when you set:

* Environment variable: `NO_DATABASE_NAME=true`.
* CLI flag: `--no-database-name=true`
* Config file:
```yaml
dump:
  no-database-name: true
```

Remember that each database schema will be in its own file, so you can determine the original by looking at the filename.

### Dump File

The backup file itself *always* is a compressed file the following format:

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
* Some shells do not handle a `:` in the filename gracefully. Although these usually are legitimate characters as far as the _filesystem_ is concerned, your shell may not like it. To avoid this issue, you can set the "no-colons" options with the "safechars" configuration:

* Environment variable: `DB_DUMP_SAFECHARS=true`
* CLI flag: `dump --safechars=true`
* Config file:
```yaml
dump:
  safechars: true
```

### Dump Target

You set where to put the dump file via configuration. The format is different between using environment variables
and CLI flags, vs config file. The environment variable and CLI support only simple formats, i.e. a single URL for a target.
For more advanced options, such as specific credentials and settings, use the config file.

#### Environment Variables and CLI

For example, to set it to a local directory named `/db`:

* Environment variable: `DB_DUMP_TARGET=/db`
* CLI flag: `dump --target=/db`

It **must** be a directory.

The value of the environment variable or CLI target can be one of three formats, depending on the type of target:

* Local: If it starts with a `/` character or `file:///` url, it will dump to a local path. If in a container, you should have it volume-mounted.
* SMB: If it is a URL of the format `smb://hostname/share/path/` then it will connect via SMB.
* S3: If it is a URL of the format `s3://bucketname.fqdn.com/path` then it will connect via using the S3 protocol.

In addition, you can send to multiple targets by separating them with a whitespace for the environment variable,
or native multiple options for other configuration options. For example, to send to a local directory and an SMB share:

* Environment variable: `DB_DUMP_TARGET="/db smb://hostname/share/path/"`
* CLI flag: `dump --target=/db --target=smb://hostname/share/path/"`

##### Local File

If the target starts with `/` or is a `file:///` then it is assumed to be a directory. The file will be written to that
directory.

The target **must** be to a directory, wherein the dump file will be saved, using the naming
convention listed above.

* Environment variable: `DB_DUMP_TARGET=/db`
* CLI flag: `dump --target=/db`

If running in a container, you will need to ensure that the directory target is mounted. See
[container considerations](./container_considerations.md).

##### SMB

If you use a URL that begins with `smb://`, for example `smb://host/share/path`, the dump file will be saved
to an SMB server.

The full URL **must** be to a directory on the SMB server, wherein the dump file will be saved, using the naming
convention listed above.

If you need login credentials, you can either use the URL format `smb://user:pass@host/share/path`,
or you can use the SMB user and password options:

* Environment variable: `SMB_USER=user SMB_PASS=pass`
* CLI flag: `--smb-user=user --smb-pass=pass`

The explicit credentials in `SMB_USER` and `SMB_PASS` override user and pass values in the URL.

Note that for smb, if the username includes a domain, e.g. your user is `mydom\myuser`, then you should use the smb convention of replacing the '\' with a ';'. In other words `smb://mydom;myuser:pass@host/share/path`

##### S3

If you use a URL that begins with `s3://`, for example `s3://bucket/path`, the dump file will be saved to the S3 bucket.

The full URL **must** be to a directory in the S3 bucket, wherein the dump file will be saved, using the naming
convention listed above.

Note that for s3, you'll need to specify your AWS credentials and default AWS region via the appropriate
settings.

For example, to set the AWS credentials:

* Environment variable: `AWS_ACCESS_KEY_ID=accesskey AWS_SECRET_ACCESS_KEY=secretkey AWS_REGION=us-east-1`
* CLI flag: `--aws-access-key-id=accesskey --aws-secret-access-key=secretkey --aws-region=us-east-1`

If you are using an s3-interoperable storage system like DigitalOcean you will need to
set the AWS endpoint URL via the AWS endpoint URL setting.

For example, to use Digital Ocean, whose endpoint URL is `${REGION_NAME}.digitaloceanspaces.com`:

* Environment variable: `AWS_ENDPOINT_URL=https://nyc3.digitaloceanspaces.com`
* CLI flag: `--aws-endpoint-url=https://nyc3.digitaloceanspaces.com`

Note that if you have multiple S3-compatible backup targets, each with its own set of credentials, region
or endpoint, then you _must_ use the config file. There is no way to distinguish between multiple sets of
credentials via the environment variables or CLI flags, while the config file provides credentials for each
target.
 
#### Configuration File

The configuration file is the most flexible way to configure the dump target. It allows you to specify
multiple targets, along with credentials and options for each target. It also keeps credentials in a file,
rather than in the shell history, and makes the command-line much simpler. Finally, of course, it allows you to
track the history of the file.

In the configuration file, a main section lists all potential targets, along with their configuration.

```yaml
targets:
  s3:
    type: s3
    url: s3://bucket.us-west.amazonaws.com/databackup
    region: us-west-1
    endpoint: https://s3.us-west-1.amazonaws.com
    credentials:
      access-key-id: access_key_id
      secret-access-key: secret_access_key
  file:
    type: file
    url: file:///tmp/databackup
  otherfile:
    type: file
    url: /tmp/databackup
  smbshare:
    type: smb
    url: smb://cifshost:2125/databackup
    credentials:
      domain: mydomain
      username: user
      password: password
```

Notice that each section is a key-value, where the key is the unique name for that target. It need not
have any meaning, other than a useful reference to you. For example, one of our targets is named `s3`,
while another is named `otherfile`.

The uniquely named targets allow you to have separate configuration and credentials. For example,
you can have two distinct s3-compatible targets, each with its own endpoint, region, and credentials. That
would not be possible with the CLI or environment variables, as they rely on the common `AWS_ACCESS_KEY_ID`
environment variable, or its CLI flag equivalent.

Once the targets are defined, you can reference them in the `dump` section by their unique keyed name:

```yaml
dump:
  targets:
  - s3
  - file
  - otherfile
```

 ##### Custom backup file name

There may be use-cases where you need to modify the name and path of the backup file when it gets uploaded to the dump target.

For example, if you need the filename not to be `<root-dir>/db_backup_<timestamp>.gz` but perhaps `<root-dir>/<year>/<month>/<day>/mybackup_<timestamp>.gz`.

To do that, configure the environment variable `DB_DUMP_FILENAME_PATTERN` or its CLI flag or config file equivalent.

The content is a string that contains a pattern to be used for the filename. The pattern can contain the following placeholders:

* `{{.now}}` - date of the backup, as included in `{{.dumpfile}}` and given by `date -u +"%Y-%m-%dT%H:%M:%SZ"`
* `{{.year}}`
* `{{.month}}`
* `{{.day}}`
* `{{.hour}}`
* `{{.minute}}`
* `{{.second}}`
* `{{.compression}}` - appropriate extension for the compression used, for example, `.gz` or `.bz2`

**Example run:**

```sh
mysql-backup dump --source-filename-pattern="db-plus-wordpress_{{.now}}.gz"
```

If the execution time was `20180930151304`, then the file will be named `plus-wordpress_20180930151304.gz`.

### Backup pre and post processing

`mysql-backup` is capable of running arbitrary scripts for pre-backup and post-backup (but pre-upload)
processing. This is useful if you need to include some files along with the database dump, for example,
to backup a _WordPress_ install.

In order to execute those scripts, you deposit them in appropriate dedicated directories and
inform `mysql-backup` about the directories. Any file ending in `.sh` in the directory will be executed.

* When using the binary, set the directories via the environment variable `DB_DUMP_PRE_BACKUP_SCRIPTS` or `DB_DUMP_POST_BACKUP_SCRIPTS`, or their CLI flag or config file equivalents.
* When using the `mysql-backup` container, these are automatically set to the directories `/scripts.d/pre-backup/` and `/scripts.d/post-backup/`, inside the container respectively. It is up to you to mount them.

**Example run binary:**

```bash
mysql-backup dump --pre-backup-scripts=/path/to/pre-backup/scripts --post-backup-scripts=/path/to/post-backup/scripts
```

**Example run container:**

```bash
docker run -d --restart=always -e DB_USER=user123 -e DB_PASS=pass123 -e DB_DUMP_FREQUENCY=60 \
  -e DB_DUMP_BEGIN=2330 -e DB_DUMP_TARGET=/db -e DB_SERVER=my-db-container:db \
  -v /path/to/pre-backup/scripts:/scripts.d/pre-backup \
  -v /path/to/post-backup/scripts:/scripts.d/post-backup \
  -v /local/file/path:/db \
  databack/mysql-backup
```

Or, if you prefer [docker compose](https://docs.docker.com/compose/):

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
     - DB_DUMP_FREQUENCY=60
     - DB_DUMP_BEGIN=2330
     - DB_SERVER=mysql_db
    command: dump
  mysql_db:
    image: mysql
    ....
```

The following environment variables are available:

* `DUMPFILE`: full path in the container to the dump output file, and the file that will be uploaded to the target
* `NOW`: date of the backup, as included in `DUMPFILE` and given by `date -u +"%Y-%m-%dT%H:%M:%SZ"`
* `DUMPDIR`: path to the destination directory so for example you can copy a new tarball including some other files along with the sql dump.
* `DEBUG`: To enable debug mode in post-backup scripts.

In addition, all of the environment variables set for the container will be available to the script.

For example, the following script will append data to the backup file after the dump is done:

```bash
#!/bin/bash
# Append data from /path/to/extra/files to the backup file after the dump is done.
if [[ -n "$DEBUG" ]]; then
  set -x
fi

if [ -e ${DUMPFILE} ];
then
  mv ${DUMPFILE} ${DUMPFILE}.tmp
  tar -zcvf /tmp/extra-files.tgz /path/to/extra/files
  cat ${DUMPFILE}.tmp /tmp/extra-files.tgz > ${DUMPFILE}
else
  echo "ERROR: Backup file ${DUMPFILE} does not exist!"
fi
```

**Important:** For post-processing, remember that at the end of the script, the dump file must be in
the location specified by the `DUMPFILE` variable. If you move it, you **must** move it back.

### Encrypting the Backup

Post-processing gives you options to encrypt the backup using openssl or any other tools. You will need to have it
available on your system. When running in the `mysql-backup` container, the openssl binary is available
to the processing scripts.

The sample [examples/encrypt.sh](./examples/encrypt.sh) provides a sample post-processing script that you can use
to encrypt your backup with AES256.

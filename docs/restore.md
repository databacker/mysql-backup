# Restoring

Restoring uses the same database, SMB and S3 configuration options as [backup](./backup.md).

Like dump, you point it at a target, which is a location for backups, select a backup file,
and it will restore the database from that file in that target.
The primary difference is the use of restore target, instead of a dump target. This follows the same syntax as
the dump target, but instead of a dump _directory_, it is the actual restore _file_, which should be a
compressed dump file.

In order to restore, you need the following:

* A storage target - directory, SMB or S3 - to restore from
* A dump file in the storage target, which can come from any of your targets or a local file (which itself is a target)
* A database to restore to, along with access credentials
* Optionally, pre- and post-restore processing scripts

## Configuring restore

`restore` **always** must have one argument, the name of the file in the target from which to restore. E.g.

```bash
$ restore db_backup_201509271627.gz
```

You can provide the target via environment variables, CLI or the config file.

### Environment variables and CLI

From a local file:

* Environment variable: `DB_RESTORE_TARGET=/backup/ restore db_backup_201509271627.gz`
* Command line: `restore --target=/backup/ db_backup_201509271627.gz`

From S3:

* Environment variable: `DB_RESTORE_TARGET=s3://mybucket/ restore db_backup_201509271627.gz`
* Command line: `restore --target=s3://mybucket/ db_backup_201509271627.gz`

From SMB:

* Environment variable: `DB_RESTORE_TARGET=smb://myserver/myshare/ restore db_backup_201509271627.gz`
* Command line: `restore --target=smb://myserver/myshare/ restore db_backup_201509271627.gz`

The credentials are provided using the same CLI flags and/or environment variables as described in [backup](./docs/backup.md).

### Config file

A config file may already contain much useful information:

* targets and their credentials
* database connectivity information and credentials
* pre- and post-restore processing scripts

In order to restore from a config file, you provide a `--target` that references one of the existing targets. The URL
begins with `config://` as the scheme, followed by the name of the target. For example, if you have a target named
`mybucket`, then you can restore to it with:

```bash
$ mysql-backup restore --target=config://mybucket/ db_backup_201509271627.gz
```

Since the target is `config://`, it will use the configuration information for that target from the config file.
It references the target named `mybucket`, including the provided configuration and credentials. Within that target,
it then retrieves the file named `db_backup_201509271627.gz` and restores it.

As you did not specify a database, it will use the database information from the config file as well.

### Restore when using docker-compose

`docker-compose` automagically creates a network when started. `docker run` simply attaches to the bridge network. If you are trying to communicate with a mysql container started by docker-compose, you'll need to specify the network in your command arguments. You can use `docker network ls` to see what network is being used, or you can declare a network in your docker-compose.yml.

#### Example:

`docker run -e DB_SERVER=gotodb.example.com -e DB_USER=user123 -e DB_PASS=pass123 -e DB_RESTORE_TARGET=/backup/ -v /local/path:/backup --network="skynet" databack/mysql-backup restore db_backup_201509271627.gz`

### Using docker secrets

Environment variables used in this image can be passed in files as well. This is useful when you are using docker secrets for storing sensitive information.

As you can set environment variable with `-e ENVIRONMENT_VARIABLE=value`, you can also use `-e ENVIRONMENT_VARIABLE_FILE=/path/to/file`. Contents of that file will be assigned to the environment variable.

**Example:**

```bash
$ docker run -d \
  -e DB_HOST_FILE=/run/secrets/DB_HOST \
  -e DB_USER_FILE=/run/secrets/DB_USER \
  -e DB_PASS_FILE=/run/secrets/DB_PASS \
  -v /local/file/path:/db \
  databack/mysql-backup
```

### Restore pre and post processing

As with backups pre and post processing, you have pre- and post-restore processing.

This is useful if you need to restore a backup file that includes some files along with the database dump.
For example, to restore a _WordPress_ install, you would uncompress a tarball containing
the db backup and a second tarball with the contents of a WordPress install on
`pre-restore`. Then on `post-restore`, uncompress the WordPress files on the container's web server root directory.

In order to perform pre-restore processing, set the pre-restore processing directory, and `mysql-backup`
will execute any file that ends in `.sh`. For example:

* Environment variable: `DB_DUMP_PRE_RESTORE_SCRIPTS=/scripts.d/pre-restore`
* Command line: `restore --pre-restore-scripts=/scripts.d/pre-restore`
* Config file:
```yaml
restore:
    scripts:
        pre-restore: /scripts.d/pre-restore
```

When running in a container, these are set automatically to `/scripts.d/pre-restore` and `/scripts.d/post-restore`
respectively.

For an example take a look at the post-backup examples, all variables defined for post-backup scripts are available for pre-processing too. Also don't forget to add the same host volumes for `pre-restore` and `post-restore` directories as described for post-backup processing.

### Restoring to a different database

The dump files normally contain a `CREATE DATABASE <database>` statement, to create the database if it
does not exist, followed by a `USE <database>;` statement, which tells MySQL which database to continue the restore into.

Sometimes, you wish to restore a dump file to a different database.
For example, you dumped a database named `FOO`, and wish to restore it to a database named `BAR`.
The dump file will have:

```sql
CREATE DATABASE `FOO`;
USE `FOO`;
```

`mysql-backup` can be instructed to restore `FOO` into `BAR` instead, as well as ensuring `BAR` exists.
Use the `--database` option to to provide a mapping of `FROM` to `TO` database names.

Continuing our example, to restore a dump file that has `USE FOO;` in it,

* Environment variable: `DB_RESTORE_DATABASE=FOO:BAR`
* Command line: `restore --database=FOO:BAR`

You can have multiple mappings by separating them with commas. For example:

* Environment variable: `DB_RESTORE_DATABASE=FOO:BAR,BAZ:QUX`
* Command line: `restore --database=FOO:BAR,BAZ:QUX`

Database names are case-insensitive, as they are in mysql.

There is no config file support for mappings.

When the restore runs, it will do the following:

1. If the dump file has `USE <database>;` in it, it will be replaced with `USE <database>;` where `<database>` is the `TO` database name.
1. Run the restore, which will restore into the `TO` database name.

If the dump file does *not* have the `USE <database>;` statement in it, for example, if it was created with
`mysql-backup dump --no-database-name`, then it simply restores as is. Be careful with this.

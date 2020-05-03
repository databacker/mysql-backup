The dump file is a tar.gz with one file per database.

The backup file _always_ dumps at the database server level, i.e. it will
call `USE DATABASE <database>` for each database to be backed up,
and will include `CREATE DATABASE` and `USE DATABASE` in the backup file.

This is equivalent to passing [`--databases <db1,db2,...>`](https://dev.mysql.com/doc/refman/8.0/en/mysqldump.html#option_mysqldump_databases) to `mysqldump`:

> With this option, it treats all name arguments as database names. CREATE DATABASE and USE statements are included in the output before each new database.

Or [`--all-databases`](https://dev.mysql.com/doc/refman/8.0/en/mysqldump.html#option_mysqldump_all-databases):

> This is the same as using the --databases option and naming all the databases on the command line.



# Connecting to the Database

In order to perform the actual dump or restore, `mysql-backup` needs to connect to the database. You **must** pass the database address via configuration. For example, to set the address to `my-db-address`:

* Environment variable: `DB_SERVER=my-db-address`
* CLI flag: `--server=my-db-address`
* Config file:
```yaml
db-server: my-db-address
```

The address itself, in the above example `my-db-address`, can be a hostname, ip address, or path to a unix domain socket , as long as it is
accessible from where the `mysql-backup` runs.

The default port is `3306`, the normal default port for mysql. You can override the default port of `3306` via
configuration. For example, to set the port to `3456`:

* Environment variable: `DB_PORT=3456`
* CLI flag: `--port=3456`
* Config file:
```yaml
db-port: 3456
```

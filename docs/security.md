# Security

## Database and Targets

`mysql-backup` uses standard libraries for accessing remote services, including the database to backup
or restore, and targets for saving backups, restoring backups, or pruning.

## Logs

Logs never should include credentials or other secrets, including at the most detailed level like `trace`. If, despite our efforts,
you see confidential information in logs, please report an issue immediately. 

## Telemetry

Remote telemetry services store your logs, as well as details about when backups occurred, if there were any errors,
and how long they took. This means that the telemetry services knows:

* The names of the databases you back up
* The names of the targets you use
* The times of backups
* The duration of backups
* The success or failure of backups
* Backup logs. As described above in [Logs](#logs), logs never should include credentials or other secrets.

Telemetry services do not store your credentials or other secrets, nor do they store the contents of your backups.
They _do_ know the names of your database tables, as those appear in the logs.

## Remote Configuration

Remote configuration services store your configuration, including the names of your databases and targets, as well as
credentials. However, they only have that data encrypted in a way that only you can decrypt. When you load configuration
into the remote service, it is encrypted locally to you, and then stored as an encrypted blob. The remote service never
sees your unencrypted data.

The data is decrypted by `mysql-backup` locally on your machine, when you retrieve the configuration.

Your access token to the remote service, stored in your local configuration file, is a
[Curve25519 private key](https://en.wikipedia.org/wiki/Curve25519), which authenticates
you to the remote service. The remote service never sees this key, only the public key, which is used to verify your identity.

This key is then used to decrypt the configuration blob, which is used to configure `mysql-backup`.

In configuration files, the key is stored base64-encoded.

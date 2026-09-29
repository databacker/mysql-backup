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

An engine credential contains one 32-byte random seed. HKDF derives
separate purpose-specific keys from it: Ed25519 signs each HTTP request, and
X25519 decrypts configuration envelopes. Neither the seed nor a derived
private key is sent to the remote service. The service stores only deterministic
public-key fingerprints and public keys.

HTTP signatures cover the exact method, authority, escaped path, and query. For
telemetry they also cover the content digest, content type, and idempotency key.
The configured remote and telemetry URLs are service base URLs. The engine
derives the self-only `/engines/config` and `/engines/telemetry/traces` routes;
the verified signature key identifies the engine, so no database-assigned
engine ID is sent in either route.
HTTPS is strongly recommended because it authenticates the server, protects
request metadata and telemetry confidentiality, and protects plaintext remote
configuration responses. Plain HTTP remains available when the deployment has
other transport protections or explicitly accepts those risks. When HTTPS is
used, a configured certificate fingerprint is only a fallback for a private or
pinned deployment; hostname, validity, and server-usage checks remain mandatory.

Encrypted configuration uses X25519, HKDF-SHA-256, and ChaCha20-Poly1305 with
authenticated envelope metadata. The engine accepts only the active or an
explicitly retained positive configuration-key generation. Rollback-state
persistence is not yet implemented, so operators must not treat the current
client as enforcing monotonic configuration versions across process restarts.

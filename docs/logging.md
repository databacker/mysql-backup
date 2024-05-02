# Logging

Logging is provided on standard out (stdout) and standard error (stderr). The log level can be set
using `--v` to the following levels:

- `--v=0` (default): log level set to `INFO`
- `--v=1`: log level set to `DEBUG`
- `--v=2`: log level set to `TRACE`

Log output and basic metrics can be sent to a remote service, using the
[configuration options](./configuration.md).

The remote log service includes the following information:

- backup start timestamp
- backup config, including command-line options (scrubbed of sensitive data)
- backup logs
- backup success or failure timestamp and duration

Log levels up to debug are sent to the remote service. Trace logs are not sent to the remote service, and are
used for local debugging only.

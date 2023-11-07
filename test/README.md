# Integration Tests

This folder contains integration tests. They are executed only if the go tag `integration` is set, e.g.

```bash
go test -tags=integration
```

As part of the process, it starts mysql, smb and s3 containers, and then runs the tests against them.
When it is done, it tears them down.

If you wish to keep the containers, for example, for inspection, then run it with the `keepcontainers` tag, e.g.

```bash
go test -tags=integration,keepcontainers
```

If you wish to see the logs from the various containers - smb, s3, mysql - before they are torn down, then run it
with the `logs` tag, e.g.

```bash
go test -tags=integration,logs
```

## How it works

There are three containers started:

* mysql
* smb
* s3

These are all started using the golang docker API. Each of these has their port exposed to the host machine.
The startup process lets docker pick the port, and then finds it.

At that point, each test in the list of tests is run bu invoking `mysql-backup` directly on the host machine,
pointing it at the various targets. `mysql-backup` is **not** invoked as a subprocess, but rather as a library call.
This does leave the possibility of a bug in how the CLI calls the library, but we accept that risk as reasonable.

Because the SMB and S3 containers save to local directories, the place to check the results needs to be mounted into
the containers.

On startup, the test creates a temporary working directory, henceforth called `base`. All files are saved to somewhere
inside base, whether as a file target for backups with target of file://dir or /dir, or for an S3 or SMB target inside
their respective containers, or for storing pre/post backup/restore scripts.

The structure of the base directory is as follows. Keep in mind that we have one single SMB and S3 container each, so the
directory is shared among different backups. That means we need to distinguish among targets that we pass to the
containers. Further, they might run in parallel, so it is important that the different activities do not trounce each other.

We resolve this by having each backup target get its own directory under `base/backups/`. The name of the directory
cannot be based just on the target, as that might be reused. We also try to avoid sequence numbers, as they are not very
helpful. Instead, each target gets a random directory name. This is then appended to the target.

Here are some examples, assuming that the base is `/tmp/mysql-backup-test-abcd123` and the random generated number
is `115647`:

* `file://dir` -> `/tmp/mysql-backup-test-abcd123/backups/dir/115647`
* `s3://s3/bucket1` -> `s3://s3/bucket1/115647` ; which, since `/tmp/mysql-backup-test-abcd123/` is mounted to the
  container, becomes `/tmp/mysql-backup-test-abcd123/backups/s3/bucket1/115647`
* `smb://smb/path2` -> `smb://smb/path2/115647` ; which, since `/tmp/mysql-backup-test-abcd123/` is mounted to the
  container, becomes `/tmp/mysql-backup-test-abcd123/backups/smb/path2/115647`

In order to keep it simple, we have the test target be the basic, e.g. `smb://smb/noauth` or `/backups`, and then we
add the rest of the path to the caller before passing it on to `mysql-backup`.

Structure of base is:

base/ - base of the backup area
    backup.sql - the backup we take manually at the beginning, for comparison
	backups/ - the directory where backups are stored
		15674832/ - one target's backup
		88725436/ - another target's backup

# Contributing

## Build Process

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

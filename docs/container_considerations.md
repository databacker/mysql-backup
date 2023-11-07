# Container Considerations

There are certain special considerations when running in a container.

## Permissions

By default, the backup/restore process does **not** run as root (UID O). Whenever possible, you should run processes (not just in containers) as users other than root. In this case, it runs as username `appuser` with UID/GID `1005`.

In most scenarios, this will not affect your backup process negatively. However, if you are using the "Local" dump target, i.e. your `DB_DUMP_TARGET` starts with `/` - and, most likely, is a volume mounted into the container - you can run into permissions issues. For example, if your mounted directory is owned by root on the host, then the backup process will be unable to write to it.

In this case, you have two options:

* Run the container as root, `docker run --user 0 ... ` or, in i`docker-compose.yml`, `user: "0"`
* Ensure your mounted directory is writable as UID or GID `1005`.

## Nice

mysql backups can be resource intensive. When running using the CLI, it is up to you to use
`nice`/`ionice` to control it, if you so desire. If running in a container, you can tell the
container to be "nicer" but setting `NICE=true`.

For more information, see https://13rac1.com/articles/2013/03/forcing-mysqldump-always-be-nice-cpu-and-io/

## File Dump Target

When backing up, the dump target is the location where the dump should be placed. When running in a container,
defaults to `/backup` in the container. Of course, having the backup in the container does not help very much, so we very strongly recommend you volume mount it outside somewhere. For example:

```bash
docker run -v /path/to/backup:/mybackup -e DB_DUMP_TARGET=/mybackup ...
```
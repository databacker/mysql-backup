# mysql backup image
FROM golang:1.22.9-alpine3.20 AS build

COPY . /src/mysql-backup
WORKDIR /src/mysql-backup

RUN mkdir /out && go build -o /out/mysql-backup .

# we would do from scratch, but we need basic utilities in order to support pre/post scripts
FROM alpine:3.20 AS runtime
LABEL org.opencontainers.image.authors="https://github.com/databacker"

# set us up to run as non-root user
RUN apk add --no-cache bash && \
    addgroup -g 1005 appuser && \
    adduser -u 1005 -G appuser -D appuser

USER appuser

COPY --from=build /out/mysql-backup /mysql-backup
COPY entrypoint /entrypoint

ENV DB_DUMP_PRE_BACKUP_SCRIPTS="/scripts.d/pre-backup/"
ENV DB_DUMP_POST_BACKUP_SCRIPTS="/scripts.d/post-backup/"
ENV DB_DUMP_PRE_RESTORE_SCRIPTS="/scripts.d/pre-restore/"
ENV DB_DUMP_POST_RESTORE_SCRIPTS="/scripts.d/post-restore/"

# start
ENTRYPOINT ["/entrypoint"]

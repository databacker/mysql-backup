# mysql backup image
FROM golang:1.19.6-alpine3.17 as build

COPY . /src/mysql-backup
WORKDIR /src/mysql-backup

RUN mkdir /out && go build -o /out/mysql-backup .

# we would do from scratch, but we need basic utilities in order to support pre/post scripts
FROM alpine:3.17
LABEL org.opencontainers.image.authors="https://github.com/databacker"

# set us up to run as non-root user
RUN apk add bash
RUN addgroup -g 1005 appuser && \
    adduser -u 1005 -G appuser -D appuser
USER appuser

COPY --from=build /out/mysql-backup /mysql-backup

COPY entrypoint /entrypoint

# start
ENTRYPOINT ["/entrypoint"]

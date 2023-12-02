# mysql backup image
FROM alpine:3.17
LABEL org.opencontainers.image.authors="https://github.com/deitch"

# install the necessary client
# the mysql-client must be 10.3.15 or later
RUN apk add --update 'mariadb-client>10.3.15' mariadb-connector-c bash python3 py3-pip samba-client shadow openssl coreutils && \
    rm -rf /var/cache/apk/* && \
    touch /etc/samba/smb.conf && \
    pip3 install awscli

# set us up to run as non-root user
RUN groupadd -g 1005 appuser && \
    useradd -r -u 1005 -g appuser appuser
# add home directory for user so IRSA AWS auth works
RUN mkdir -p /home/appuser && chmod 0755 /home/appuser && chown appuser /home/appuser
# ensure smb stuff works correctly
RUN mkdir -p /var/cache/samba && chmod 0755 /var/cache/samba && chown appuser /var/cache/samba && chown appuser /var/lib/samba/private
USER appuser

# install the entrypoint
COPY functions.sh /
COPY entrypoint /entrypoint

# start
ENTRYPOINT ["/entrypoint"]

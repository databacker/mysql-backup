# rancher server backup image
FROM alpine:3.2
MAINTAINER software@tradertools.com

# install the necessary client
RUN apk add --update mysql-client bash py-pip samba-client && \
    rm -rf /var/cache/apk/* && \
    touch /etc/samba/smb.conf && \
    pip install awscli

# install the entrypoint
COPY entrypoint /entrypoint

# start
ENTRYPOINT ["/entrypoint"]


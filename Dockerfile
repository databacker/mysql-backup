# rancher server backup image
FROM alpine
MAINTAINER software@tradertools.com

# install the necessary client
RUN apk add --update mysql-client bash samba-client && rm -rf /var/cache/apk/* && touch /etc/samba/smb.conf

# install the entrypoint
COPY entrypoint /entrypoint

# start
ENTRYPOINT ["/entrypoint"]


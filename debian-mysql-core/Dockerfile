# mysql backup image
FROM debian:stretch-slim
MAINTAINER Karthik

RUN apt-get update && apt-get install -y gnupg dirmngr

RUN set -ex; \
# gpg: key 5072E1F5: public key "MySQL Release Engineering <mysql-build@oss.oracle.com>" imported
	key='A4A9406876FCBD3C456770C88C718D3B5072E1F5'; \
	export GNUPGHOME="$(mktemp -d)"; \
#	gpg --batch --keyserver ha.pool.sks-keyservers.net --recv-keys "$key"; \
	gpg --batch --keyserver ipv4.pool.sks-keyservers.net --recv-keys "$key"; \
	gpg --batch --export "$key" > /etc/apt/trusted.gpg.d/mysql.gpg; \
	gpgconf --kill all; \
	rm -rf "$GNUPGHOME"; \
	apt-key list > /dev/null

ENV MYSQL_MAJOR 5.7
ENV MYSQL_VERSION 5.7.26-1debian9
RUN echo "deb http://repo.mysql.com/apt/debian/ stretch mysql-${MYSQL_MAJOR}" > /etc/apt/sources.list.d/mysql.list

# install the necessary client
RUN apt-get update && \
    apt-get install -y mysql-client bash python3 python3-pip samba-client && \
    rm -rf /var/cache/apk/* && \
    touch /etc/samba/smb.conf && \
    pip3 install awscli

# set us up to run as non-root user
RUN groupadd -g 1005 appuser && \
    useradd -r -u 1005 -g appuser appuser

# ensure smb stuff works correctly
#RUN mkdir -p /var/cache/samba && chmod 0755 /var/cache/samba && chown appuser /var/cache/samba
USER appuser

# install the entrypoint
COPY ../functions.sh /
COPY ../entrypoint /entrypoint
#RUN chmod +x /entrypoint

# start
ENTRYPOINT ["/entrypoint"]

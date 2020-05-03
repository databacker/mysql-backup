FROM mysql:8.0

## MYSQL


FROM alpine:3.17

## SAMBA

# smb port
EXPOSE 445

# install the necessary client
RUN apk add --update bash samba-server && rm -rf /var/cache/apk/* && touch /etc/samba/smb.conf

# enter smb.conf
COPY smb.conf /etc/samba/
COPY smbusers /etc/samba/
COPY *.tdb /var/lib/samba/private/
# create a user with no home directory but the right password
RUN adduser user -D -H
RUN echo user:pass | chpasswd

### s3
RUN apk add --update minio

# start samba
#CMD /usr/sbin/smbd -F --debug-stdout -d 4 --no-process-group

# start minio
#RUN minio server /path/to/s3

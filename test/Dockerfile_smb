FROM alpine:3.9
MAINTAINER https://github.com/deitch

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

# run samba in the foreground
CMD /usr/sbin/smbd -F -S -d 4 --no-process-group

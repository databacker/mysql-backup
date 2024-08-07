#!/bin/bash

#
# post-processing backup script used to encrypt the backup file.
# Many thanks to Sascha Schieferdecker https://github.com/sascha-schieferdecker
# for providing it.
#
# to use, make available to the path of DB_DUMP_POST_BACKUP_SCRIPTS 
# or --post-backup-scripts
#
# the symmetric encryption key should be available at /scripts.d/post-backup/mysqldump-key.pub.pem

# Encrypt and chmod backup file.
if [[ -n "$DB_DUMP_DEBUG" ]]; then
  set -x
fi

if [ -e ${DUMPFILE} ];
then
  openssl smime -encrypt -binary -text -aes256 -in ${DUMPFILE} -out ${DUMPFILE}.enc -outform DER /scripts.d/post-backup/mysqldump-key.pub.pem
  mv ${DUMPFILE}.enc ${DUMPFILE}
  chmod 600 ${DUMPFILE}
else
  echo "ERROR: Backup file ${DUMPFILE} does not exist!"
fi


#!/bin/bash
# Rename backup file.
if [[ -n "$DB_DUMP_DEBUG" ]]; then
  set -x
fi

if [ -e ${DUMPFILE} ];
then
  now=$(date +"%Y-%m-%d-%H_%M")
  new_name=db_backup-${now}.gz
  old_name=$(basename ${DUMPFILE})
  echo "Renaming backup file from ${old_name} to ${new_name}"
  mv ${DUMPFILE} ${DESTDIR}/${new_name}
else
  echo "ERROR: Backup file ${DUMPFILE} does not exist!"
fi

#!/bin/bash
# Restore a backup (db + files).
if [[ -n "$DB_DUMP_DEBUG" ]]; then
  set -x
fi

if [ -n ${DB_RESTORE_TARGET} ];
then
  backups_dir=$(dirname ${DB_RESTORE_TARGET})
  backup_filename=$(basename ${DB_RESTORE_TARGET})
  tmp_dir=$(mktemp -d)
  backup_files=""
  sql_dump=""

  >&2 echo "Getting contents of tarball"
  IFS_ORIG=${IFS}
  backup_files="$(tar tvf ${DB_RESTORE_TARGET}|awk '{ print $6 }' )"
  backup_files="$(echo ${backup_files}|  tr '\n' ' ')"
  if [ "${backup_files}" == "" ];then
    >&2 echo "ERROR: Empty tarball!" && exit 1
  fi

  >&2 echo "Uncompressing tarball"
  cd /${tmp_dir}
  tar zxf ${DB_RESTORE_TARGET}
  [[ $? -gt 0 ]] && echo "Could not uncompress tarball!" && exit 1

  for i in ${backup_files}; do
    #Get the sql dump and put it back on DB_RESTORE_TARGET as mysql-backup expects
    if [ "${i/db_backup}" != ${i} ]; then
      >&2 echo "Found SQL dump tarball: ${i}"
      sql_dump=${i}
    else
      >&2 echo "Uncompressing tarball: ${i}"
      tar zxf ${i} -C /
      [[ $? -gt 0 ]] && >&2 echo "ERROR: Could not uncompress backup file: ${i}!" && exit 1
    fi
  done
else
  echo >&2 "ERROR: Backup file ${DB_RESTORE_TARGET} or restore directory does not exist!"
fi

#Echo the name of the sql dump file so the backup can continue as usual.
echo ${backups_dir}/${sql_dump}

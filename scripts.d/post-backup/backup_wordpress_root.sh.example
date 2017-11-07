#!/bin/bash
# Backup a WordPress site (db + files).
if [[ -n "$DB_DUMP_DEBUG" ]]; then
  set -x
fi

NOW=$(date +"%Y-%m-%d-%H_%M")

if [ -e ${DUMPFILE} ];
then
  backups_dir=$(dirname ${DUMPFILE})
  tmp_dir=$(mktemp -d)
  wordpress_files="${tmp_dir}/wordpress-${NOW}-files.tar.gz"
  wordpress_full_backup="wordpress-${NOW}-full.tar.gz"

  echo "Backing up WordPress directory"
  tar zcf ${wordpress_files} /var/www/html/wp-content
  [[ $? -gt 0 ]] && echo "Could not compress WordPress directory!" && exit 1

  echo "Creating new tarball"
  tar zcf ${tmp_dir}/${wordpress_full_backup} ${wordpress_files} ${DUMPFILE}
  [[ $? -gt 0 ]] && echo "Could not create WordPress full backup file!" && exit 1

  echo "Moving new backup file to: ${DUMPDIR}"
  mv ${restore_dir}/${wordpress_full_backup} ${DUMPDIR}
  [[ $? -gt 0 ]] && echo "Could not move the backup file to ${DUMPDIR}!" && exit 1

  #cleanup
  rm -fr ${tmp_dir}
else
  echo "ERROR: Backup file ${DUMPFILE} does not exist!"
fi

#!/bin/bash
set -e

# create a tmp backupfile
function create_backup_file() {
  local target=$1 
  echo 'use tester; create table t1 (id INT, name VARCHAR(20)); INSERT INTO t1 (id,name) VALUES (1, "John"), (2, "Jill"), (3, "Sam"), (4, "Sarah");' | $db_connect
  tmpdumpdir=/tmp/backup.$$
  rm -rf $tmpdumpdir
  mkdir $tmpdumpdir
  tmpdumpfile=backup.sql
  docker exec $mysql_cid mysqldump -hlocalhost --protocol=tcp -A -u$MYSQLUSER -p$MYSQLPW > $tmpdumpdir/$tmpdumpfile
  tar -C $tmpdumpdir -cvf - $tmpdumpfile | gzip > ${target}
  rm -rf $tmpdumpdir
}

# Configure backup directory
function configure_backup_directory_target() {
    local t=$1
	local seqno=$2
	# where will we store
	# create the backups directory
	# clear the target
	# replace SEQ if needed
	t2=${t/SEQ/${seqno}}
	mkdir -p ${BACKUP_DIRECTORY_BASE}/${seqno}/data
	chmod -R 0777 ${BACKUP_DIRECTORY_BASE}/${seqno}
	echo "target: ${t2}" >> ${BACKUP_DIRECTORY_BASE}/${seqno}/list

	# are we working with nopath?
	if [[ "$t2" =~ nopath ]]; then
		rm -f ${BACKUP_DIRECTORY_BASE}/nopath
		ln -s ${seqno}/data ${BACKUP_DIRECTORY_BASE}/nopath
	fi

	echo ${t2}
}

function get_default_source() {
    echo "db_backup_*.tgz"
}

#!/bin/bash
set -e

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
	echo "target: ${t2}" >> ${BACKUP_DIRECTORY_BASE}/${seqno}/list

	# are we working with nopath?
	if [[ "$t2" =~ nopath ]]; then
		rm -f ${BACKUP_DIRECTORY_BASE}/nopath
		ln -s ${seqno}/data ${BACKUP_DIRECTORY_BASE}/nopath
	fi

	echo ${t2}
}

function get_default_source() {
    echo "db_backup_*.gz"
}
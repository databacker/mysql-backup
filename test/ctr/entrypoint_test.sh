#!/bin/bash
set -e

DEBUG=${DEBUG:-0}
[[ -n "$DEBUG" && "$DEBUG" == "verbose" ]] && DEBUG=1
[[ -n "$DEBUG" && "$DEBUG" == "debug" ]] && DEBUG=2

[[ "$DEBUG" == "2" ]] && set -x

MYSQLDUMP=/backups/valid.tgz

function populate_vol() {
	local t=$1
        local sequence=$2
	subseq=0
        # we might have multiple targets
        for target in $t ; do
          seqno="${sequence}-${subseq}"
	  # where will we store
  	  # create the backups directory
	  # clear the target
	  # replace SEQ if needed
          t2=${t/SEQ/${seqno}}
          mkdir -p /backups/${seqno}/data
          chmod -R 0777 /backups/${seqno}
          echo "target: ${t2}" >> /backups/${seqno}/list

	  # are we working with nopath?
	  if [[ "$t2" =~ nopath ]]; then
		rm -f /backups/nopath
		ln -s ${seqno}/data /backups/nopath
	  fi

	  ((subseq++)) || true
        done
}

function populate_pre_post() {
        local sequence=$1
        # Create a test script for the post backup processing test
        mkdir -p /backups/${sequence}/{pre-backup,post-backup,pre-restore,post-restore}
        echo touch /backups/${sequence}/post-backup/post-backup.txt > /backups/${sequence}/post-backup/test.sh
        echo touch /backups/${sequence}/post-restore/post-restore.txt > /backups/${sequence}/post-restore/test.sh
        echo touch /backups/${sequence}/pre-backup/pre-backup.txt > /backups/${sequence}/pre-backup/test.sh
        echo touch /backups/${sequence}/pre-restore/pre-restore.txt > /backups/${sequence}/pre-restore/test.sh
        chmod -R 0777 /backups/${sequence}
        chmod 755 /backups/${sequence}/*/test.sh
}

#
function checktest() {
	local t=$1
	local sequence=$2
	

        # to make it easier to hunt through output logs
        echo >&2
        echo "*** CHECKING SEQUENCE ${sequence} ***" >&2

        # all of it is in the volume we created, so check from there
        POST_BACKUP_OUT_FILE="/backups/${sequence}/post-backup/post-backup.txt"
	PRE_BACKUP_OUT_FILE="/backups/${sequence}/pre-backup/pre-backup.txt"
	POST_RESTORE_OUT_FILE="/backups/${sequence}/post-restore/post-restore.txt"
	PRE_RESTORE_OUT_FILE="/backups/${sequence}/pre-restore/pre-restore.txt"
        if [[ -e "${POST_BACKUP_OUT_FILE}" ]]; then
	  pass+=("$sequence post-backup")
          rm -fr ${POST_BACKUP_OUT_FILE}
        else
	  fail+=("$sequence $t pre-backup script didn't run, output file doesn't exist")
        fi
	if [[ -e "${PRE_BACKUP_OUT_FILE}" ]]; then
	  pass+=("$sequence pre-backup")
          rm -fr ${PRE_BACKUP_OUT_FILE}
        else
	  fail+=("$sequence $t post-backup script didn't run, output file doesn't exist")
        fi

        # we might have multiple targets
        local subseq=0
        for target in $t ; do
          seqno="${sequence}-${subseq}"
	  # where do we expect backups?
	  bdir=/backups/${seqno}/data		# change our target
	  if [[ "$DEBUG" != "0" ]]; then
  		ls -la $bdir >&2
  	  fi


	  # check that the expected backups are in the right place
	  # need temporary places to hold files
	  TMP1=/backups/check1
	  TMP2=/backups/check2

	  BACKUP_FILE=$(ls -d1 $bdir/db_backup_*.tgz 2>/dev/null)

	  # check for the directory
	  if [[ ! -d "$bdir" ]]; then
	  	fail+=("$seqno $t missing $bdir")
	  elif [[ -z "$BACKUP_FILE" ]]; then
	  	fail+=("$seqno $t missing missing backup zip file")
	  else
		# what if it was s3?
		[[ -f "${BACKUP_FILE}/.fakes3_metadataFFF/content" ]] && BACKUP_FILE="${BACKUP_FILE}/.fakes3_metadataFFF/content"

		# extract the actual data, but filter out lines we do not care about
		# " | cat " at the end so it returns true because we run "set -e"
		cat ${BACKUP_FILE} | tar -xOzf - | sed -e 's/^\/\*![0-9]\{5\}.*\/;$//g' | sed 's/^.*SET character_set_client.*$//g' | cat > $TMP1
		cat ${MYSQLDUMP} | tar -xOzf - | sed -e 's/^\/\*![0-9]\{5\}.*\/;$//g' | sed 's/^.*SET character_set_client.*$//g' |cat > $TMP2

		# check the file contents against the source directory
		# " | cat " at the end so it returns true because we run "set -e"
		diffout=$(diff $TMP1 $TMP2 | cat)
		if [[ -z "$diffout" ]]; then
	  		pass+=("$sequence dump-contents")
		else
	  		fail+=("$seqno $t tar contents do not match actual dump")
		fi

	  fi
	  if [ -n "$TESTRESTORE" ]; then
		if [[ -e "${POST_RESTORE_OUT_FILE}" ]]; then
	  	  pass+=("$sequence post-restore")
		  rm -fr ${POST_RESTORE_OUT_FILE}
		else
	  		fail+=("$seqno $t post-restore script didn't run, output file doesn't exist")
		fi
		if [[ -e "${PRE_RESTORE_OUT_FILE}" ]]; then
	  	  pass+=("$sequence pre-restore")
		  rm -fr ${PRE_RESTORE_OUT_FILE}
		else
	  		fail+=("$seqno $t pre-restore script didn't run, output file doesn't exist")
		fi
	  fi
	  ((subseq++)) || true
        done
}

function check_source_target_test() {
        local t=$1
        local sequence=$2
        local cid=$3
        local SOURCE_FILE=$4
        local TARGET_FILE=$5

        # to make it easier to hunt through output logs
        echo >&2
        echo "*** CHECKING SEQUENCE ${sequence} ***" >&2

        # we might have multiple targets
        local subseq=0
        for target in $t ; do
          seqno="${sequence}-${subseq}"
          # where do we expect backups?
          bdir=/backups/${seqno}/data             # change our target
          if [[ "$DEBUG" != "0" ]]; then
                  ls -la $bdir
          fi

          # check that the expected backups are in the right place
          BACKUP_FILE=$(ls -d1 $bdir/${SOURCE_FILE} 2>/dev/null)

          [[ "$DEBUG" != "0" ]] && echo "Checking target backup file exists for target ${target}"

          # check for the directory
          if [[ ! -d "$bdir" ]]; then
                  fail+=("$seqno: $target missing $bdir")
          elif [[ -z "$BACKUP_FILE" ]]; then
                  fail+=("$seqno: $target missing zip file")
          else
              pass+=($seqno)
          fi

          if [[ ! -z ${TARGET_FILE} ]]; then
            [[ "$DEBUG" != "0" ]] && echo "Checking target backup filename matches expected ${target}"
            local BACKUP_FILE_BASENAME = ${BACKUP_FILE##*/}
            [[ ${BACKUP_FILE_BASENAME} == ${TARGET_FILE} ]] && pass+=($seqno) || fail+=("${seqno}: ${target} uploaded target file name does not match expected. Found: ${BACKUP_FILE_BASENAME}")
          fi
        done
}

function print_pass_fail() {
        for ((i=0; i< ${#pass[@]}; i++)); do
                echo "PASS: ${pass[$i]}"
        done
        for ((i=0; i< ${#fail[@]}; i++)); do
                echo "FAIL: ${fail[$i]}"
        done
}

# we do whichever commands were requested
cmd="$1"
target="$2"
seq="$3"
source_file="$4"
target_file="$5"

declare -a fail
declare -a pass

case $cmd in
prepare_pre_post)
	populate_pre_post $seq
	;;
populate)
	populate_vol "$target" $seq
	;;
check)
	checktest "$target" $seq
	print_pass_fail
	;;
check_source_target)
	check_source_target_test "$target" $seq $source_file $target_file
	print_pass_fail
	;;
save_dump)
	cat > $MYSQLDUMP
	;;
cron)
	/cron_test.sh
	;;
esac



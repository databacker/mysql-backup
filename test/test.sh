#!/bin/bash
set -e


DEBUG=${DEBUG:-0}
[[ -n "$DEBUG" && "$DEBUG" == "verbose" ]] && DEBUG=1
[[ -n "$DEBUG" && "$DEBUG" == "debug" ]] && DEBUG=2

[[ "$DEBUG" == "2" ]] && set -x

TESTS=$2
[[ -n "$TESTS" ]] && TESTS=all

BACKUP_IMAGE=mysqlbackup_backup_test:latest
SMB_IMAGE=mysqlbackup_smb_test:latest

RWD=${PWD}
MYSQLUSER=user
MYSQLPW=abcdefg
MYSQLDUMP=/tmp/source/backup.gz

mkdir -p /tmp/source

# list of sources and targets
declare -a targets


# localhost is not going to work, because it is across containers!!
# fill in with a var
targets=(
"/backups/SEQ/data"
"file:///backups/SEQ/data"
"smb://smb/noauth/SEQ/data"
"smb://smb/nopath"
"smb://user:pass@smb/auth/SEQ/data"
"smb://CONF;user:pass@smb/auth/SEQ/data"
"s3://mybucket/SEQ/data"
)

function runtest() {
	local t=$1
	local seqno=$2
	# where will we store
	# create the backups directory
	# clear the target
	# replace SEQ if needed
	t2=${t/SEQ/${seqno}}
	mkdir -p /tmp/backups/${seqno}/data
	echo "target: ${t2}" >> /tmp/backups/${seqno}/list

	# are we working with nopath?
	if [[ "$t2" =~ nopath ]]; then
		rm -f /tmp/backups/nopath
		ln -s ${seqno}/data /tmp/backups/nopath
	fi

  #Create a test script for the post backup processing test
  mkdir -p /tmp/backups/${seqno}/{pre-backup,post-backup,pre-restore,post-restore}
  echo touch /scripts.d/post-backup/post-backup.txt > /tmp/backups/${seqno}/post-backup/test.sh
	echo touch /scripts.d/post-restore/post-restore.txt > /tmp/backups/${seqno}/post-restore/test.sh
	echo touch /scripts.d/pre-backup/pre-backup.txt > /tmp/backups/${seqno}/pre-backup/test.sh
	echo touch /scripts.d/pre-restore/pre-restore.txt > /tmp/backups/${seqno}/pre-restore/test.sh
	chmod -R 0777 /tmp/backups/${seqno}
  chmod 755 /tmp/backups/${seqno}/*/test.sh

	# if in DEBUG, make sure backup also runs in DEBUG
	if [[ "$DEBUG" != "0" ]]; then
		DBDEBUG="-e DB_DUMP_DEBUG=2"
	else
		DBDEBUG=
	fi


	# change our target
  cid=$(docker run --net mysqltest -d $DBDEBUG -e DB_USER=$MYSQLUSER -e DB_PASS=$MYSQLPW -e DB_DUMP_FREQ=60 -e DB_DUMP_BEGIN=+0 -e DB_DUMP_TARGET=${t2} -e AWS_ACCESS_KEY_ID=abcdefg -e AWS_SECRET_ACCESS_KEY=1234567 -e AWS_ENDPOINT_URL=http://s3:443/ -v /tmp/backups/${seqno}/:/scripts.d/ -v /tmp/backups:/backups -e DB_SERVER=mysql --link ${s3_cid}:mybucket.s3.amazonaws.com ${BACKUP_IMAGE})
	echo $cid
}

# THIS WILL FAIL BECAUSE OF:
#1c1
#< -- MySQL dump 10.13  Distrib 5.7.10, for Linux (x86_64)
#---
#> -- MySQL dump 10.14  Distrib 5.5.46-MariaDB, for Linux (x86_64)
#3c3
#< -- Host: localhost    Database:
#---
#> -- Host: db    Database:
#
# so we filter those lines out; they are not relevant to the backup anyways
#
function checktest() {
	local t=$1
	local seqno=$2
	local cid=$3
	# where do we expect backups?
	bdir=/tmp/backups/${seqno}/data		# change our target
	if [[ "$DEBUG" != "0" ]]; then
		ls -la $bdir
	fi

	# stop and remove the container
	[[ "$DEBUG" != "0" ]] && echo "Stopping and removing ${cid}"
	CMD1="docker kill ${cid}"
	CMD2="docker rm ${cid}"
	if [[ "$DEBUG" == "0" ]]; then
		$CMD1 > /dev/null 2>&1
		$CMD2 > /dev/null 2>&1
	else
		# keep the logs
		docker logs ${cid}
		$CMD1
		$CMD2
	fi

	# check that the expected backups are in the right place
	# need temporary places to hold files
	TMP1=/tmp/backups/check1
	TMP2=/tmp/backups/check2

	BACKUP_FILE=$(ls -d1 $bdir/db_backup_*.gz 2>/dev/null)
  POST_BACKUP_OUT_FILE="/tmp/backups/${seqno}/post-backup/post-backup.txt"
	PRE_BACKUP_OUT_FILE="/tmp/backups/${seqno}/pre-backup/pre-backup.txt"
	POST_RESTORE_OUT_FILE="/tmp/backups/${seqno}/post-restore/post-restore.txt"
	PRE_RESTORE_OUT_FILE="/tmp/backups/${seqno}/pre-restore/pre-restore.txt"

	# check for the directory
	if [[ ! -d "$bdir" ]]; then
		fail+=("$seqno: $t missing $bdir")
	elif [[ -z "$BACKUP_FILE" ]]; then
		fail+=("$seqno: $t missing zip file")
	else
		# what if it was s3?
		[[ -f "${BACKUP_FILE}/.fakes3_metadataFFF/content" ]] && BACKUP_FILE="${BACKUP_FILE}/.fakes3_metadataFFF/content"

		# extract the actual data, but filter out lines we do not care about
		# " | cat " at the end so it returns true because we run "set -e"
		cat ${BACKUP_FILE} | gunzip | grep -v '^-- MySQL' | grep -v '^-- Host:' | grep -v '^-- Dump completed' | cat > $TMP1
		cat ${MYSQLDUMP} | gunzip | grep -v '^-- MySQL' | grep -v '^-- Host:' | grep -v '^-- Dump completed' | cat > $TMP2

		# check the file contents against the source directory
		# " | cat " at the end so it returns true because we run "set -e"
		diffout=$(diff $TMP1 $TMP2 | cat)
		if [[ -z "$diffout" ]]; then
			pass+=($seqno)
		else
			fail+=("$seqno: $item $t tar contents do not match actual dump")
		fi

	fi
  if [[ -e "${POST_BACKUP_OUT_FILE}" ]]; then
    pass+=($seqno)
    rm -fr ${POST_BACKUP_OUT_FILE}
  else
    fail+=("$seqno: $item $t Post-backup script didn't run, output file doesn't exist")
  fi
	if [[ -e "${PRE_BACKUP_OUT_FILE}" ]]; then
    pass+=($seqno)
    rm -fr ${PRE_BACKUP_OUT_FILE}
  else
    fail+=("$seqno: $item $t Pre-backup script didn't run, output file doesn't exist")
  fi
	if [ -n "$TESTRESTORE" ]; then
		if [[ -e "${POST_RESTORE_OUT_FILE}" ]]; then
		  pass+=($seqno)
		  rm -fr ${POST_RESTORE_OUT_FILE}
		else
		  fail+=("$seqno: $item $t Post-restore script didn't run, output file doesn't exist")
		fi
		if [[ -e "${PRE_RESTORE_OUT_FILE}" ]]; then
		  pass+=($seqno)
		  rm -fr ${PRE_RESTORE_OUT_FILE}
		else
		  fail+=("$seqno: $item $t Pre-restore script didn't run, output file doesn't exist")
		fi
	fi
}

# we need to run through each each target and test the backup.
# before the first run, we:
# - start the sql database
# - populate it with a few inserts/creates
# - run a single clear backup
# for each stage, we:
# - clear the target
# - run the backup
# - check that the backup now is there in the right format
# - clear the target

declare -a cids
# make the parent for the backups

[[ "$DEBUG" != "0" ]] && echo "Resetting backups directory"

/bin/rm -rf /tmp/backups
mkdir -p /tmp/backups
chmod -R 0777 /tmp/backups
#setfacl -d -m g::rwx /tmp/backups
#setfacl -d -m o::rwx /tmp/backups


# build the core images
QUIET="-q"
[[ "$DEBUG" != "0" ]] && QUIET=""
[[ "$DEBUG" != "0" ]] && echo "Creating backup image"
docker build $QUIET -t ${BACKUP_IMAGE} -f ../Dockerfile ../

# build the test images we need
[[ "$DEBUG" != "0" ]] && echo "Creating smb image"
docker build $QUIET -t ${SMB_IMAGE} -f ./Dockerfile_smb .

# create the network we need
[[ "$DEBUG" != "0" ]] && echo "Creating the test network"
docker network create mysqltest

# run the test images we need
[[ "$DEBUG" != "0" ]] && echo "Running smb, s3 and mysql containers"
[[ "$DEBUG" != "0" ]] && SMB_IMAGE="$SMB_IMAGE -F -d 25"
smb_cid=$(docker run --net mysqltest --name=smb  -d -p 445:445 -v /tmp/backups:/share/backups -t ${SMB_IMAGE})
mysql_cid=$(docker run --net mysqltest --name mysql -d -v /tmp/source:/tmp/source -e MYSQL_ROOT_PASSWORD=root -e MYSQL_DATABASE=tester -e MYSQL_USER=$MYSQLUSER -e MYSQL_PASSWORD=$MYSQLPW mysql:5.7)
s3_cid=$(docker run --net mysqltest --name s3 -d -v /tmp/backups:/fakes3_root/s3/mybucket lphoward/fake-s3 -r /fakes3_root -p 443)


# Allow up to 20 seconds for the database to be ready
db_connect="docker exec -i $mysql_cid mysql -u$MYSQLUSER -p$MYSQLPW --protocol=tcp -h127.0.0.1 --wait --connect_timeout=20 tester"
retry_count=0
retryMax=20
retrySleep=1
until [[ $retry_count -ge $retryMax ]]; do
	set +e
	$db_connect -e 'select 1;'
	success=$?
	set -e
	[[ $success == 0 ]] && break
	((retry_count  ++)) || true
	sleep $retrySleep
done
# did we succeed?
if [[ $success != 0 ]]; then
	echo -n "failed to connect to database after $retryMax tries." >&2
fi

echo 'use tester; create table t1 (id INT, name VARCHAR(20)); INSERT INTO t1 (id,name) VALUES (1, "John"), (2, "Jill"), (3, "Sam"), (4, "Sarah");' | $db_connect
docker exec $mysql_cid mysqldump -hlocalhost --protocol=tcp -A -u$MYSQLUSER -p$MYSQLPW | gzip > ${MYSQLDUMP}

# keep track of the sequence
seq=0

#


#
# do the file tests
[[ "$DEBUG" != "0" ]] && echo "Doing tests"
# create each target
[[ "$DEBUG" != "0" ]] && echo "Running backups for each target"
for ((i=0; i< ${#targets[@]}; i++)); do
	t=${targets[$i]}
	cids[$seq]=$(runtest $t $seq)
	# increment our counter
	((seq++)) || true
done
total=$seq

# now wait for everything
waittime=10
[[ "$DEBUG" != "0" ]] && echo "Waiting ${waittime} seconds to complete backup runs"
sleep ${waittime}s


# get logs from the tests
if [[ "$DEBUG" == "2" ]]; then
	echo
	echo "SMB LOGS:"
	docker logs $smb_cid
	echo
	echo "MYSQL LOGS:"
	docker logs $mysql_cid
	echo
	echo "S3 LOGS:"
	docker logs $s3_cid
fi
# now check each result
[[ "$DEBUG" != "0" ]] && echo "Checking results"
declare -a fail
declare -a pass
seq=0
for ((i=0; i< ${#targets[@]}; i++)); do
	t=${targets[$i]}
	checktest $t $seq ${cids[$seq]}
	# increment our counter
	((seq++)) || true
done

[[ "$DEBUG" != "0" ]] && echo "Stopping and removing smb and mysql containers"
CMD1="docker kill $smb_cid $mysql_cid $s3_cid"
CMD2="docker rm $smb_cid $mysql_cid $s3_cid"
if [[ "$DEBUG" == "0" ]]; then
	$CMD1 > /dev/null 2>&1
	$CMD2 > /dev/null 2>&1
else
	$CMD1
	$CMD2
fi

[[ "$DEBUG" != "0" ]] && echo "Removing docker network"
docker network rm mysqltest

# report results
echo "Passed: ${#pass[@]}"
echo "Failed: ${#fail[@]}"

if [[ "${#fail[@]}" != "0" ]]; then
	for ((i=0; i< ${#fail[@]}; i++)); do
		echo "${fail[$i]}"
	done
	exit 1
else
	exit 0
fi

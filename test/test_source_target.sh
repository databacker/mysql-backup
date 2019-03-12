#!/bin/bash
set -e

source ./_functions.sh

DEBUG=${DEBUG:-0}
[[ -n "$DEBUG" && "$DEBUG" == "verbose" ]] && DEBUG=1
[[ -n "$DEBUG" && "$DEBUG" == "debug" ]] && DEBUG=2

[[ "$DEBUG" == "2" ]] && set -x

BACKUP_IMAGE=mysqlbackup_backup_test:latest
SMB_IMAGE=mysqlbackup_smb_test:latest
BACKUP_DIRECTORY_BASE=/tmp/backups.$$

RWD=${PWD}
MYSQLUSER=user
MYSQLPW=abcdefg
MYSQLDUMP=/tmp/source/backup.tgz

mkdir -p /tmp/source

# list of sources and targets
declare -a targets

# localhost is not going to work, because it is across containers!!
# fill in with a var
targets=(
"file:///backups/SEQ/data"
"smb://user:pass@smb/auth/SEQ/data"
"s3://mybucket/SEQ/data"
)

function run_default_source_target_test() {
    local t=$1
	local seqno=$2

	local t2=$(configure_backup_directory_target ${t} ${seqno})

    # if in DEBUG, make sure backup also runs in DEBUG
	if [[ "$DEBUG" != "0" ]]; then
		DBDEBUG="-e DB_DUMP_DEBUG=2"
	else
		DBDEBUG=
	fi

    # change our target
    cid=$(docker run --label mysqltest --net mysqltest -d $DBDEBUG -e DB_USER=$MYSQLUSER -e DB_PASS=$MYSQLPW -e DB_DUMP_FREQ=60 -e DB_DUMP_BEGIN=+0 -e DB_DUMP_TARGET=${t2} -e AWS_ACCESS_KEY_ID=abcdefg -e AWS_SECRET_ACCESS_KEY=1234567 -e AWS_ENDPOINT_URL=http://s3:443/ -v ${BACKUP_DIRECTORY_BASE}/${seqno}/:/scripts.d/ -v ${BACKUP_DIRECTORY_BASE}:/backups -e DB_SERVER=mysql --link ${s3_cid}:mybucket.s3.amazonaws.com ${BACKUP_IMAGE})
	echo $cid
}

# check test
function checktest() {
	local t=$1
	local seqno=$2
	local cid=$3
	local SOURCE_FILE=$4
	local TARGET_FILE=$5

	# where do we expect backups?
	bdir=${BACKUP_DIRECTORY_BASE}/${seqno}/data		# change our target
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
	BACKUP_FILE=$(ls -d1 $bdir/${SOURCE_FILE} 2>/dev/null)

    [[ "$DEBUG" != "0" ]] && echo "Checking target backup file exists for target ${t}"

	# check for the directory
	if [[ ! -d "$bdir" ]]; then
		fail+=("$seqno: $t missing $bdir")
	elif [[ -z "$BACKUP_FILE" ]]; then
		fail+=("$seqno: $t missing zip file")
	else
	    pass+=($seqno)
	fi

    if [[ ! -z ${TARGET_FILE} ]]; then
        [[ "$DEBUG" != "0" ]] && echo "Checking target backup filename matches expected ${t}"
        local BACKUP_FILE_BASENAME = ${BACKUP_FILE##*/}
        [[ ${BACKUP_FILE_BASENAME} == ${TARGET_FILE} ]] && pass+=($seqno) || fail+=("${seqno}: ${t} uploaded target file name does not match expected. Found: ${BACKUP_FILE_BASENAME}")
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

/bin/rm -rf ${BACKUP_DIRECTORY_BASE}
mkdir -p ${BACKUP_DIRECTORY_BASE}
chmod -R 0777 ${BACKUP_DIRECTORY_BASE}

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
docker network create mysqltest --label mysqltest

# run the test images we need
[[ "$DEBUG" != "0" ]] && echo "Running smb, s3 and mysql containers"
smb_cid=$(docker run --label mysqltest --net mysqltest --name=smb  -d -p 445:445 -v ${BACKUP_DIRECTORY_BASE}:/share/backups:z -t ${SMB_IMAGE})
mysql_cid=$(docker run --label mysqltest --net mysqltest --name mysql -d -v /tmp/source:/tmp/source:z -e MYSQL_ROOT_PASSWORD=root -e MYSQL_DATABASE=tester -e MYSQL_USER=$MYSQLUSER -e MYSQL_PASSWORD=$MYSQLPW mysql:8.0)
s3_cid=$(docker run --label mysqltest --net mysqltest --name s3 -d -v ${BACKUP_DIRECTORY_BASE}:/fakes3_root/s3/mybucket:z lphoward/fake-s3 -r /fakes3_root -p 443)

# Allow up to 20 seconds for the database to be ready
db_connect="docker exec -i $mysql_cid mysql -u$MYSQLUSER -p$MYSQLPW --wait --connect_timeout=20 tester"
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

create_backup_file $MYSQLDUMP

# keep track of the sequence
seq=0

# do the file tests
[[ "$DEBUG" != "0" ]] && echo "Doing tests"
# create each target
[[ "$DEBUG" != "0" ]] && echo "Running backups for each target"
for ((i=0; i< ${#targets[@]}; i++)); do
	t=${targets[$i]}
	cids[$seq]=$(run_default_source_target_test $t $seq)
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
	checktest $t $seq ${cids[$seq]} $(get_default_source)
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

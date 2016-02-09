#!/bin/bash

DEBUG=${DEBUG:-0}
[[ -n "$DEBUG" && "$DEBUG" == "verbose" ]] && DEBUG=1
[[ -n "$DEBUG" && "$DEBUG" == "debug" ]] && DEBUG=2

[[ "$DEBUG" == "2" ]] && set -x

TESTS=$2
[[ -n "$TESTS" ]] && TESTS=all

RWD=${PWD}
MYSQLPW=abcdefg
MYSQLDUMP=/tmp/source/backup.gz

# list of sources and targets
declare -a targets

# this is global, so has to be set outside
declare -A uri
declare -A proto

# localhost is not going to work, because it is across containers!!
# fill in with a var
targets=(
"/backups/data"
"file:///backups/data"
"smb://smb/noauth/backups/SEQ/data"
"smb://user:pass@smb/auth/backups/SEQ/data"
#"smb://domain;user:pass@smb/domauth/backups/SEQ/data"
)


function uri_parser() {
  uri=()
  # uri capture
  full="$@"

    # safe escaping
    full="${full//\`/%60}"
    full="${full//\"/%22}"

		# URL that begins with '/' is like 'file:///'
		if [[ "${full:0:1}" == "/" ]]; then
			full="file://localhost${full}"
		fi
		# file:/// should be file://localhost/
		if [[ "${full:0:8}" == "file:///" ]]; then
			full="${full/file:\/\/\//file://localhost/}"
		fi
		
    # top level parsing
    pattern='^(([a-z]{3,5})://)?((([^:\/]+)(:([^@\/]*))?@)?([^:\/?]+)(:([0-9]+))?)(\/[^?]*)?(\?[^#]*)?(#.*)?$'
    [[ "$full" =~ $pattern ]] || return 1;

    # component extraction
    full=${BASH_REMATCH[0]}
		uri[uri]="$full"
    uri[schema]=${BASH_REMATCH[2]}
    uri[address]=${BASH_REMATCH[3]}
    uri[user]=${BASH_REMATCH[5]}
    uri[password]=${BASH_REMATCH[7]}
    uri[host]=${BASH_REMATCH[8]}
    uri[port]=${BASH_REMATCH[10]}
    uri[path]=${BASH_REMATCH[11]}
    uri[query]=${BASH_REMATCH[12]}
    uri[fragment]=${BASH_REMATCH[13]}
		if [[ ${uri[schema]} == "smb" && ${uri[path]} =~ ^/([^/]*)(/?.*)$ ]]; then
			uri[share]=${BASH_REMATCH[1]}
			uri[sharepath]=${BASH_REMATCH[2]}
		fi
		
		# does the user have a domain?
		if [[ -n ${uri[user]} && ${uri[user]} =~ ^([^;]+);(.+)$ ]]; then
			uri[userdomain]=${BASH_REMATCH[1]}
			uri[user]=${BASH_REMATCH[2]}
		fi
		return 0
}


function runtest() {
	t=$1
	seqno=$2
	# where will we store 
	# create the backups directory
	# clear the target
	# replace SEQ if needed
	t2=${t/SEQ/${seqno}}
	mkdir -p /tmp/backups/${seq}
	echo "target: ${t2}" >> /tmp/backups/${seqno}/list

	mkdir -p /tmp/backups/${seqno}
	# change our target
	cid=$(docker run -d --restart=always -e DB_USER=root -e DB_PASS=$MYSQLPW -e DB_DUMP_FREQ=60 -e DB_DUMP_BEGIN=2330 -e DB_DUMP_TARGET=$s --link ${mysql_cid}:db backup)	
	cids[$seqno]=$cid
}

function checktest() {
	t=$1
	seqno=$2
	# where do we expect backups?
	bdir=/tmp/backups/${seq}		# change our target
	# stop and remove the container
	[[ "$DEBUG" != "0" ]] && echo "Stopping and removing ${cids[$seq]}"
	CMD1="docker kill ${cids[$seqno]}"
	CMD2="docker rm ${cids[$seqno]}"
	if [[ "$DEBUG" == "0" ]]; then
		$CMD1 > /dev/null 2>&1
		$CMD2 > /dev/null 2>&1
	else
		$CMD1
		$CMD2
	fi
	# check that the expected backups are in the right place
	# if this was git, we need to clone it
	if [[ "$DEBUG" == "0" ]]; then
		$CMD1 > /dev/null 2>&1
	else
		$CMD1
	fi

	# check for the directory
	if [[ ! -d "$bdir" ]]; then
		fail+=("$seqno: $item $t missing $bdir")
	elif [[ $(ls -1 $bdir/db_backup_*.gz | wc -l) =~ ^[[:space:]]*0[[:space:]]*$ ]]; then
		fail+=("$seqno: $item $t missing zip file")
	else
		# check the file contents against the source directory
		diffout=$(diff $bdir/db_backup_*.gz ${MYSQLDUMP})
		if [[ -z "$diffout" ]]; then
			pass+=($seqno)
		else
			fail+=("$seqno: $item $t tar contents do not match actual dump")
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
setfacl -d -m g::rwx /tmp/backups
setfacl -d -m o::rwx /tmp/backups


# build the core images
QUIET="-q"
[[ "$DEBUG" != "0" ]] && QUIET=""
[[ "$DEBUG" != "0" ]] && echo "Creating backup image"
docker build $QUIET -t backup -f ../Dockerfile ../

# build the test images we need
[[ "$DEBUG" != "0" ]] && echo "Creating smb image"
docker build $QUIET -t smb -f ./Dockerfile_smb .

# run the test images we need
[[ "$DEBUG" != "0" ]] && echo "Running smb and mysql containers"
smb_cid=$(docker run -d -p 445:445 -v /tmp/backups:/share/backups smb)
mysql_cid=$(docker run -d -v /tmp/source:/tmp/source -e MYSQL_ROOT_PASSWORD=$MYSQLPW mysql)


# initiate the database and do a dump
docker exec -it $mysql_cid mysql -uroot -p$MYSQLPW << MYSQL_END
create table t1 (id INT, name VARCHAR(20));
INSERT INTO t1 (id,name) VALUES (1, "John"), (2, "Jill"), (3, "Sam"), (4, "Sarah");
MYSQL_END

docker exec -it $mysql_cid mysqldump -A -uroot -p$MYSQLPW | gzip > $(MYSQLDUMP)

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
	runtest $t $seq
	# increment our counter
	((seq++))
done
total=$seq

# now wait for everything
[[ "$DEBUG" != "0" ]] && echo "Waiting 30 seconds to complete backup runs"
sleep 30s

# now check each result
[[ "$DEBUG" != "0" ]] && echo "Checking results"
declare -a fail
declare -a pass
seq=0
for ((i=0; i< ${#targets[@]}; i++)); do
	t=${targets[$i]}
	checktest $t $seq
	# increment our counter
	((seq++))
done

[[ "$DEBUG" != "0" ]] && echo "Stopping and removing smb and mysql containers"
CMD1="docker kill $smb_cid $mysql_cid"
CMD2="docker rm $smb_cid $mysql_cid"
if [[ "$DEBUG" == "0" ]]; then
	$CMD1 > /dev/null 2>&1
	$CMD2 > /dev/null 2>&1
else
	$CMD1
	$CMD2
fi

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


#!/bin/bash
# Function definitions used in the entrypoint file.

#
# Environment variable reading function
#
# The function enables reading environment variable from file.
#
# usage: file_env VAR [DEFAULT]
#    ie: file_env 'XYZ_DB_PASSWORD' 'example'
# (will allow for "$XYZ_DB_PASSWORD_FILE" to fill in the value of
#  "$XYZ_DB_PASSWORD" from a file, especially for Docker's secrets feature
function file_env() {
  local var="$1"
  local fileVar="${var}_FILE"
  local def="${2:-}"
  if [ "${!var:-}" ] && [ "${!fileVar:-}" ]; then
    echo >&2 "error: both $var and $fileVar are set (but are exclusive)"
    exit 1
  fi
  local val="$def"
  if [ "${!var:-}" ]; then
    val="${!var}"
  elif [ "${!fileVar:-}" ]; then
    val="$(< "${!fileVar}")"
  fi
  export "$var"="$val"
  unset "$fileVar"
}


#
# URI parsing function
#
# The function creates global variables with the parsed results.
# It returns 0 if parsing was successful or non-zero otherwise.
#
# [schema://][user[:password]@]host[:port][/path][?[arg1=val1]...][#fragment]
#
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
    pattern='^(([a-z0-9]{2,5})://)?((([^:\/]+)(:([^@\/]*))?@)?([^:\/?]+)(:([0-9]+))?)(\/[^?]*)?(\?[^#]*)?(#.*)?$'
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
		if [[ -n ${uri[user]} && ${uri[user]} =~ ^([^\;]+)\;(.+)$ ]]; then
			uri[userdomain]=${BASH_REMATCH[1]}
			uri[user]=${BASH_REMATCH[2]}
		fi
		return 0
}



#
# execute actual backup
#
function do_dump() {
  # what is the name of our source and target?
  now=$(date -u +"%Y%m%d%H%M%S")
  # SOURCE: file that the uploader looks for when performing the upload
  # TARGET: the remote file that is actually uploaded
  SOURCE=db_backup_${now}.gz
  TARGET=${SOURCE}

  # Execute additional scripts for pre processing. For example, uncompress a
  # backup file containing this db backup and a second tar file with the
  # contents of a wordpress install so they can be restored.
  if [ -d /scripts.d/pre-backup/ ]; then
    for i in $(ls /scripts.d/pre-backup/*.sh); do
      if [ -x $i ]; then
        NOW=${now} DUMPFILE=${TMPDIR}/${TARGET} DUMPDIR=${TMPDIR} DB_DUMP_DEBUG=${DB_DUMP_DEBUG} $i
      fi
    done
  fi

  if [[ -n "$DB_NAMES" ]]; then
    DB_LIST="--databases $DB_NAMES"
  else
    DB_LIST="-A"
  fi

  # do the dump
  mysqldump -h $DB_SERVER -P $DB_PORT $DBUSER $DBPASS $DB_LIST $DUMPVARS | gzip > ${TMPDIR}/${SOURCE}

  # Execute additional scripts for post processing. For example, create a new
  # backup file containing this db backup and a second tar file with the
  # contents of a wordpress install.
  if [ -d /scripts.d/post-backup/ ]; then
    for i in $(ls /scripts.d/post-backup/*.sh); do
      if [ -x $i ]; then
        NOW=${now} DUMPFILE=${TMPDIR}/${SOURCE} DUMPDIR=${TMPDIR} DB_DUMP_DEBUG=${DB_DUMP_DEBUG} $i
      fi
    done
  fi

  # Execute a script to modify the name of the source file path before uploading to the dump target
  # For example, modifying the name of the source dump file from the default, e.g. db-other-files-combined.tar.gz
  if [ -f /scripts.d/source.sh ] && [ -x /scripts.d/source.sh ]; then
      SOURCE=$(NOW=${now} DUMPFILE=${TMPDIR}/${SOURCE} DUMPDIR=${TMPDIR} DB_DUMP_DEBUG=${DB_DUMP_DEBUG} /scripts.d/source.sh | tr -d '\040\011\012\015')

      if [ -z "${SOURCE}" ]; then
          echo "Your source script located at /scripts.d/source.sh must return a value to stdout"
          exit 1
      fi
  fi
  # Execute a script to modify the name of the target file before uploading to the dump target.
  # For example, uploading to a date stamped object key path in S3, i.e. s3://bucket/2018/08/23/path
  if [ -f /scripts.d/target.sh ] && [ -x /scripts.d/target.sh ]; then
      TARGET=$(NOW=${now} DUMPFILE=${TMPDIR}/${SOURCE} DUMPDIR=${TMPDIR} DB_DUMP_DEBUG=${DB_DUMP_DEBUG} /scripts.d/target.sh | tr -d '\040\011\012\015')

      if [ -z "${TARGET}" ]; then
          echo "Your target script located at /scripts.d/target.sh must return a value to stdout"
          exit 1
      fi
  fi


}

#
# place the backup in appropriate location(s)
#
function backup_target() {
  local target=$1
  # determine target proto
  uri_parser ${target}

  # what kind of target do we have? Plain filesystem? smb?
  case "${uri[schema]}" in
    "file")
      mkdir -p ${uri[path]}
      cp -a ${TMPDIR}/${SOURCE} ${uri[path]}/${TARGET}
      ;;
    "s3")
      # allow for endpoint url override
      [[ -n "$AWS_ENDPOINT_URL" ]] && AWS_ENDPOINT_OPT="--endpoint-url $AWS_ENDPOINT_URL"
      aws ${AWS_ENDPOINT_OPT} s3 cp ${TMPDIR}/${SOURCE} "${DB_DUMP_TARGET}/${TARGET}"
      ;;
    "smb")
      if [[ -n "$SMB_USER" ]]; then
        UPASSARG="-U"
        UPASS="${SMB_USER}%${SMB_PASS}"
      elif [[ -n "${uri[user]}" ]]; then
        UPASSARG="-U"
        UPASS="${uri[user]}%${uri[password]}"
      else
        UPASSARG=
        UPASS=
      fi
      if [[ -n "${uri[userdomain]}" ]]; then
        UDOM="-W ${uri[userdomain]}"
      else
        UDOM=
      fi

      smbclient -N "//${uri[host]}/${uri[share]}" ${UPASSARG} "${UPASS}" ${UDOM} -c "cd ${uri[sharepath]}; put ${TMPDIR}/${SOURCE} ${TARGET}"
     ;;
  esac
}

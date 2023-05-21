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
  now=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
  # SOURCE: file that the uploader looks for when performing the upload
  # TARGET: the remote file that is actually uploaded

  # option to replace
  if [ -n "$DB_DUMP_SAFECHARS" ]; then
    now=${now//:/-}
  fi
  SOURCE=db_backup_${now}.$EXTENSION
  TARGET=${SOURCE}

  # Execute additional scripts for pre processing. For example, uncompress a
  # backup file containing this db backup and a second tar file with the
  # contents of a wordpress install so they can be restored.
  for i in /scripts.d/pre-backup/*.sh; do
    if [ -x $i ]; then
      NOW=${now} DUMPFILE=${TMPDIR}/${TARGET} DUMPDIR=${TMPDIR} DB_DUMP_DEBUG=${DB_DUMP_DEBUG} $i
      [ $? -ne 0 ] && return 1
    fi
  done

  # do the dump
  workdir="${TMP_PATH}/backup.$$"
  rm -rf $workdir
  mkdir -p $workdir
  NICE_CMD=
  # if we asked to do by schema, then we need to get a list of all of the databases, take each, and then tar and zip them
  if [ "$NICE" = "true" ]; then
    NICE_CMD="nice -n19 ionice -c2"
  fi
  if [ -n "$DB_DUMP_BY_SCHEMA" -a "$DB_DUMP_BY_SCHEMA" = "true" ]; then
    if [[ -z "$DB_NAMES" ]]; then
      DB_LIST=$(mysql -h $DB_SERVER -P $DB_PORT $DBUSER $DBPASS -N -e 'show databases')
      [ $? -ne 0 ] && return 1
    else
      DB_LIST="$DB_NAMES"
    fi
    if [ -z "$DB_NAMES_EXCLUDE" ]; then
      DB_NAMES_EXCLUDE="information_schema performance_schema mysql sys"
    fi
    declare -A exclude_list
    for i in $DB_NAMES_EXCLUDE; do
      exclude_list[$i]="true"
    done
    for onedb in $DB_LIST; do
      if [[ -v exclude_list[$onedb] ]]; then
        # skip db if it is in the exclude list
        continue
      fi
      $NICE_CMD mysqldump -h $DB_SERVER -P $DB_PORT $DBUSER $DBPASS --databases ${onedb} $MYSQLDUMP_OPTS > $workdir/${onedb}_${now}.sql
      [ $? -ne 0 ] && return 1
    done
  else
    # just a single command
    if [ "$SINGLE_DATABASE" = "true" ]; then
      DB_LIST="$DB_NAMES"
    elif [[ -n "$DB_NAMES" ]]; then
      DB_LIST="--databases $DB_NAMES"
    else
      DB_LIST="-A"
    fi
    $NICE_CMD mysqldump -h $DB_SERVER -P $DB_PORT $DBUSER $DBPASS $DB_LIST $MYSQLDUMP_OPTS > $workdir/backup_${now}.sql
    [ $? -ne 0 ] && return 1
  fi
  tar -C $workdir -cvf - . | $COMPRESS > ${TMPDIR}/${SOURCE}
  [ $? -ne 0 ] && return 1
  rm -rf $workdir
  [ $? -ne 0 ] && return 1

  # Execute additional scripts for post processing. For example, create a new
  # backup file containing this db backup and a second tar file with the
  # contents of a wordpress install.
  for i in ls /scripts.d/post-backup/*.sh; do
    if [ -x $i ]; then
      NOW=${now} DUMPFILE=${TMPDIR}/${SOURCE} DUMPDIR=${TMPDIR} DB_DUMP_DEBUG=${DB_DUMP_DEBUG} $i
      [ $? -ne 0 ] && return 1
    fi
  done

  # Execute a script to modify the name of the source file path before uploading to the dump target
  # For example, modifying the name of the source dump file from the default, e.g. db-other-files-combined.tar.$EXTENSION
  if [ -f /scripts.d/source.sh ] && [ -x /scripts.d/source.sh ]; then
      SOURCE=$(NOW=${now} DUMPFILE=${TMPDIR}/${SOURCE} DUMPDIR=${TMPDIR} DB_DUMP_DEBUG=${DB_DUMP_DEBUG} /scripts.d/source.sh | tr -d '\040\011\012\015')
      [ $? -ne 0 ] && return 1

      if [ -z "${SOURCE}" ]; then
          echo "Your source script located at /scripts.d/source.sh must return a value to stdout"
          exit 1
      fi
  fi
  # Execute a script to modify the name of the target file before uploading to the dump target.
  # For example, uploading to a date stamped object key path in S3, i.e. s3://bucket/2018/08/23/path
  if [ -f /scripts.d/target.sh ] && [ -x /scripts.d/target.sh ]; then
      TARGET=$(NOW=${now} DUMPFILE=${TMPDIR}/${SOURCE} DUMPDIR=${TMPDIR} DB_DUMP_DEBUG=${DB_DUMP_DEBUG} /scripts.d/target.sh | tr -d '\040\011\012\015')
      [ $? -ne 0 ] && return 1

      if [ -z "${TARGET}" ]; then
          echo "Your target script located at /scripts.d/target.sh must return a value to stdout"
          exit 1
      fi
  fi

  return 0
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
      cpOpts="-a"
      [ -n "$DB_DUMP_KEEP_PERMISSIONS" -a "$DB_DUMP_KEEP_PERMISSIONS" = "false" ] && cpOpts=""
      cp $cpOpts ${TMPDIR}/${SOURCE} ${uri[path]}/${TARGET}
      ;;
    "s3")
      # allow for endpoint url override
      [[ -n "$AWS_ENDPOINT_URL" ]] && AWS_ENDPOINT_OPT="--endpoint-url $AWS_ENDPOINT_URL"
      aws ${AWS_CLI_OPTS} ${AWS_ENDPOINT_OPT} s3 cp ${AWS_CLI_S3_CP_OPTS} ${TMPDIR}/${SOURCE} "${target}/${TARGET}"
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

      # smb has issues with the character `:` in filenames, so replace with `-`
      smbTargetName=${TARGET//:/-}
      smbclient -N "//${uri[host]}/${uri[share]}" ${UPASSARG} "${UPASS}" ${UDOM} -c "cd ${uri[sharepath]}; put ${TMPDIR}/${SOURCE} ${smbTargetName}"
     ;;
  esac
  [ $? -ne 0 ] && return 1
  return 0
}

#
# calculate seconds until next cron match
#
function wait_for_cron() {
  local cron="$1"
  local compare="$2"
  local last_run="$3"
  # we keep a copy of the actual compare time, because we might shift the compare time in a moment
  local comparesec=$compare
  # there must be at least 60 seconds between last run and next run, so if it is less than 60 seconds,
  #   add differential seconds to $compare
  local compareDiff=$(($compare - $last_run))
  if [ $compareDiff -lt 60 ]; then
    compare=$(($compare + $(( 60-$compareDiff )) ))
  fi

  # cron only works in minutes, so we want to round down to the current minute
  # e.g. if we are at 20:06:25, we need to treat it as 20:06:00, or else our waittime will be -25
  # on the other hand, if we are at 20:06:00, do not round it down
  local current_seconds=$(getepochas "$comparesec" +"%-S")
  if [ $current_seconds -ne 0 ]; then
    comparesec=$(( $comparesec - $current_seconds ))
  fi

  # reminder, cron format is:
  # minute(0-59)
  #   hour(0-23)
  #     day of month(1-31)
  #       month(1-12)
  #         day of week(0-6 = Sunday-Saturday)
  local cron_minute=$(echo -n "$cron" | awk '{print $1}')
  local cron_hour=$(echo -n "$cron" | awk '{print $2}')
  local cron_dom=$(echo -n "$cron" | awk '{print $3}')
  local cron_month=$(echo -n "$cron" | awk '{print $4}')
  local cron_dow=$(echo -n "$cron" | awk '{print $5}')

  local success=1

  # when is the next time we hit that month?
  local next_minute=$(getepochas "$compare" +"%-M")
  local next_hour=$(getepochas "$compare" +"%-H")
  local next_dom=$(getepochas "$compare" +"%-d")
  local next_month=$(getepochas "$compare" +"%-m")
  local next_dow=$(getepochas "$compare" +"%-u")
  local next_year=$(getepochas "$compare" +"%-Y")

  # date returns DOW as 1-7/Mon-Sun, we need 0-6/Sun-Sat
  next_dow=$(( $next_dow % 7 ))

  local cron_next=

  # logic for determining next time to run
  # start by assuming our current min/hr/dom/month/dow is good, store it as "next"
  # go through each section: if it matches, keep going; if it does not, make it match or move ahead

  while [ "$success" != "0" ]; do
    # minute:
    # if minute matches, move to next step
    # if minute does not match, move "next" minute to the time that does match in cron
    #   if "next" minute is ahead of cron minute, then increment "next" hour by one
    #   move to hour
    cron_next=$(next_cron_expression "$cron_minute" 59 "$next_minute")
    if [ "$cron_next" != "$next_minute" ]; then
      if [ "$next_minute" -gt "$cron_next" ]; then
        next_hour=$(( $next_hour + 1 ))
      fi
      next_minute=$cron_next
    fi

    # hour:
    # if hour matches, move to next step
    # if hour does not match:
    #   if "next" hour is ahead of cron hour, then increment "next" day by one
    #   set "next" hour to cron hour, set "next" minute to 0, return to beginning of loop
    cron_next=$(next_cron_expression "$cron_hour" 23 "$next_hour")
    if [ "$cron_next" != "$next_hour" ]; then
      if [ "$next_hour" -gt "$cron_next" ]; then
        next_dom=$(( $next_dom + 1 ))
      fi
      next_hour=$cron_next
      next_minute=0
    fi

    # weekday:
    # if weekday matches, move to next step
    # if weekday does not match:
    #   move "next" weekday to next matching weekday, accounting for overflow at end of week
    #   reset "next" hour to 0, reset "next" minute to 0, return to beginning of loop
    cron_next=$(next_cron_expression "$cron_dow" 6 "$next_dow")
    if [ "$cron_next" != "$next_dow" ]; then
      dowDiff=$(( $cron_next - $next_dow ))
      if [ "$dowDiff" -lt "0" ]; then
        dowDiff=$(( $dowDiff + 7 ))
      fi
      next_dom=$(( $next_dom + $dowDiff ))
      next_hour=0
      next_minute=0
    fi

    # dom:
    # if dom matches, move to next step
    # if dom does not match:
    #   if "next" dom is ahead of cron dom OR "next" month does not have crom dom (e.g. crom dom = 30 in Feb),
    #       increment "next" month, reset "next" day to 1, reset "next" minute to 0, reset "next" hour to 0, return to beginning of loop
    #   else set "next" day to cron day, reset "next" minute to 0, reset "next" hour to 0, return to beginning of loop
    maxDom=$(max_day_in_month $next_month $next_year)
    cron_next=$(next_cron_expression "$cron_dom" 30 "$next_dom")
    if [ "$cron_next" != "$next_dom" ]; then
      next_hour=0
      next_minute=0
    fi
    if [ $next_dom -gt $cron_next -o $next_dom -gt $maxDom ]; then
      next_month=$(( $next_month + 1 ))
      if [ $next_month -gt 12 ]; then
        next_month=$(( $next_month - 12))
        next_year=$(( $next_year + 1 ))
      fi
      next_dom=1
    else
      next_dom=$cron_next
    fi


    # month:
    # if month matches, move to next step
    # if month does not match:
    #   if "next" month is ahead of cron month, increment "next" year by 1
    #   set "next" month to cron month, set "next" day to 1, set "next" minute to 0, set "next" hour to 0
    #   return to beginning of loop
    cron_next=$(next_cron_expression "$cron_month" 12 "$next_month")
    if [ "$cron_next" != "$next_month" ]; then
      # must be sure to roll month if needed
      if [ $cron_next -gt 12 ]; then
        next_year=$(( $next_year + 1 ))
        cron_next=$(( $cron_next - 12 ))
      fi
      if [ $next_month -gt $cron_next ]; then
        next_year=$(( $next_year + 1 ))
      fi
      next_month=$cron_next
      next_day=1
      next_minute=0
      next_hour=0
    fi

    success=0
  done
  # success: "next" is now set to the next match!

  local future=$(getdateas "${next_year}-${next_month}-${next_dom}T${next_hour}:${next_minute}:00" "+%s")
  local futurediff=$(($future - $comparesec))
  echo $futurediff
}

# next_cron_expression function that takes a cron term, e.g. "3", "4-7", "*", "3,4-7", "*/5", "3-25/5",
# and calculates the lowest term that fits the cron expression that is equal to or greater than some number.
# uses the "max" argument to determine the maximum
# For example, given the arguments, these are the results and why:
# "*" "60" "4"       -> "4"   4 is the number that is greater than or equal to  "*"
# "4" "60" "4"       -> "4"   4 is the number that is greater than or equal to  "4"
# "5" "60" "4"       -> "5"   5 is the next number that matches "5", and is >= 4
# "3-7" "60" "4"     -> "4"   4 is the number that fits within 3-7
# "3-7" "60" "9"     -> "3"    no number in the range 3-7 ever is >= 9, so next one will be 3 when we circle back
# "*/2" "60" "4"     -> "4"   4 is divisible by 2
# "*/5" "60" "4"     -> "5"   5 is the next number in the range * that is divisible by 5, and is >= 4
# "0-20/5" "60" "4"  -> "5"   5 is the next number in the range 0-20 that is divisible by 5, and is >= 4
# "15-30/5" "60" "4" -> "15"  15 is the next number in the range 15-30 that is in increments of 5, and is >= 4
# "15-30/5" "60" "20"-> "20"  20 is the next number in the range 15-30 that is in increments of 5, and is >= 20
# "15-30/5" "60" "35"-> "15"    no number in the range 15-30/5 will ever be >=35, so 15 is the first circle back
# "*/10" "12" "11"   -> "0"    the next match after 11 would be 20, but that would be greater than the maximum, so we circle back to 0
#
function next_cron_expression() {
  local crex="$1"
  local max="$2"
  local num="$3"

  # expand the list - note that this can handle a single-element list
  local allvalid=""
  local tmpvalid=""
  # take each comma-separated expression
  local parts=${crex//,/ }
  # replace * with # so that we can handle * as one of comma-separated terms without doing shell expansion
  parts=${parts//\*/#}
  for i in $parts; do
    # if it is a * or exact match, just add the number
    if [ "$i" = "#" -o "$i" = "$num" ]; then
      echo $num
      return 0
    fi

    # it might be a step function, so we will have to reduce from the total range
    partstep=${i##*\/}
    partnum=${i%%\/*}
    tmpvalid=""
    local start=
    local end=
    if [ "${partnum}" = "#" ]; then
      # calculate all of the numbers until the max
      start=0
      end=$max
    else
      # handle a range like 3-7, which includes a single number like 4
      start=${partnum%%-*}
      end=${partnum##*-}
    fi
    # calculate the valid ones just for this range
    tmpvalid=$(seq $start $end)

    # it is a step function if the partstep is not the same as the whole thing
    if [ "$partstep" != "$i" ]; then
      # add to allvalid only the ones that match the term
      # there are two possible use cases:
      # first number is 0: any divisible by the partstep, i.e. j%partstep
      # first number is not 0: start at first and increment by partstep until we run out
      #    this latter one is just the equivalent of dropping all numbers by (first) and then seeing if divisible
      for j in $tmpvalid; do
        if [ $(( (${j} - ${start}) % ${partstep} )) -eq 0 ]; then
          allvalid="$allvalid $j"
        fi
      done
    else
      # if it is not a step function, just add the tmpvalid to the allvalid
      allvalid="$allvalid $tmpvalid"
    fi
  done

  # sort for deduplication and ordering
  allvalid=$(echo $allvalid | tr ' ' '\n' | sort -n -u | tr '\n' ' ')
  for i in $allvalid; do
    if [ "$i" -ge "$num" ]; then
      echo $i
      return 0
    fi
  done
  # if we got here, no number matched, so take the very first one
  echo ${allvalid%% *}
}

function max_day_in_month() {
  local month="$1"
  local year="$1"

  case $month in
    "1"|"3"|"5"|"7"|"8"|"10"|"12")
      echo 31
      ;;
    "2")
      local div4=$(( $year % 4 ))
      local div100=$(( $year % 100 ))
      local div400=$(( $year % 400 ))
      local days=28
      if [ "$div4" = "0" -a "$div100" != "0" ]; then
        days=29
      fi
      if [ "$div400" = "0" ]; then
        days=29
      fi
      echo $days
      ;;
    *)
      echo 30
      ;;
  esac
}

function getdateas() {
        local input="$1"
	local outformat="$2"
	local os=$(uname -s | tr '[A-Z]' '[a-z]')
        case "$os" in
        linux)
                date --date="$input" "$outformat"
                ;;
        darwin)
		# need to determine if it was Zulu time or local
		lastchar="${input: -1}"
		format="%Y-%m-%dT%H:%M:%S"
		uarg="-u"
		if [ "$lastchar" = "Z" ]; then
			format="${format}Z"
			uarg="-u"
		fi
                date $uarg -j -f "$format" "$input" "$outformat"
                ;;
        *)
                echo "unknown OS $os" >&2
                exit 1
        esac
}
function getepochas() {
        local input="$1"
	local format="$2"
	local os=$(uname -s | tr '[A-Z]' '[a-z]')
        case "$os" in
        linux)
                date --date="@$input" "$format"
                ;;
        darwin)
                date -u -j -r "$input" "$format"
                ;;
        *)
                echo "unknown OS $os" >&2
                exit 1
        esac
}

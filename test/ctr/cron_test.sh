#!/bin/bash
set -e

DEBUG=${DEBUG:-0}
[[ -n "$DEBUG" && "$DEBUG" == "verbose" ]] && DEBUG=1
[[ -n "$DEBUG" && "$DEBUG" == "debug" ]] && DEBUG=2

[[ "$DEBUG" == "2" ]] && set -x

# where is our functions file? By default, in container so at /functions.sh
# but can override to run independently
FUNCTIONS=${FUNCTIONS:-/functions.sh}

. ${FUNCTIONS}

# list of cron expressions, inputs and results 
declare -a cronnum croninput cronresult cronline nowtime waittime

  set -f
  tests=(
 "* 59 44 44" # 44 is the number in the range * that is >= 44
 "4 59 4 4"  # 4 is the number that is greater than or equal to  "4"
 "5 59 4 5"  # 5 is the next number that matches "5", and is >= 4
 "3-7 59 4 4"  # 4 is the number that fits within 3-7
 "3-7 59 9 3"   # no number in the range 3-7 ever is >= 9, so should cycle back to 3
 "*/2 59 4 4"  # 4 is divisible by 2
 "*/5 59 4 5"  # 5 is the next number in the range * that is divisible by 5, and is >= 4
 "0-20/5 59 4 5" #  5 is the next number in the range 0-20 that is divisible by 5, and is >= 4
 "15-30/5 59 4 15" # 15 is the next number in the range 15-30 that is in increments of 5, and is >= 4
 "18-30/5 59 4 18" # 18 is the next number >=4 in the range 18,23,28
 "15-30/5 59 20 20" # 20 is the next number in the range 15-30 that is in increments of 5, and is >= 20
 "15-30/5 59 35 15"  #  no number in the range 15-30/5 will ever be >=35, so should cycle back to 15
 "*/10 12 11 0" #   the next match after 11 would be 20, but that would be greater than the maximum of 12, so should cycle back to 0
  "1 11 1 1"
  "1,2,3 6 2 2"
  "1-3,5 6 2 2"
  "5-7,6,1 11 4 5"
  "* 11 8 8"
  "5,6,2-3,* 30 7 7"
  )

pass=0
fail=0

for tt in "${tests[@]}"; do
  parts=(${tt})
  expected="${parts[3]}"
  result=$(next_cron_expression ${parts[0]} ${parts[1]} ${parts[2]})
  if [ "$result" = "$expected" ]; then
    ((pass+=1))
  else
    echo "Failed next_cron_expression \"$ex\" \"$in\": received \"$out\" instead of \"$re\"" >&2
    ((fail+=1))
  fi
done

cronline=(
  "1 * * * *"
  "1 * * * *"
  "* 1 * * *"
  "1 * * * *"
  "0 0 * * *"
  "0 0 1 * *"
  #"10 2 10 * *"
)

nowtime=(
  "2018-10-10T10:01:00Z"
  "2018-10-10T10:00:00Z"
  "2018-10-10T10:00:00Z"
  "2018-10-10T10:01:10Z" # this line tests that we use the current minute, and not wait for "-10"
  "2021-11-30T10:00:00Z"
  "2020-12-30T10:00:00Z" # this line tests that we can handle rolling month correctly
)
waittime=(
  "0"
  "60"
  "54000"
  "0"
  "50400"
  "136800"
)

for ((i=0; i< ${#cronline[@]}; i++)); do
        ex="${cronline[$i]}"
        in=$(getdateas "${nowtime[$i]}" "+%s")
        re=${waittime[$i]}
        out=$(wait_for_cron "$ex" "$in" 0)
        if [ "$out" = "$re" ]; then
          ((pass+=1))
        else       
          echo "Failed wait_for_cron \"$ex\" \"$in\": received \"$out\" instead of \"$re\"" >&2
          ((fail+=1))
        fi
done



# report results
echo "Passed: $pass"
echo "Failed: $fail"

if [[ "$fail" != "0" ]]; then
        exit 1
else
        exit 0
fi


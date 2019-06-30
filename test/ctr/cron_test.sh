#!/bin/bash

DEBUG=${DEBUG:-0}
[[ -n "$DEBUG" && "$DEBUG" == "verbose" ]] && DEBUG=1
[[ -n "$DEBUG" && "$DEBUG" == "debug" ]] && DEBUG=2

[[ "$DEBUG" == "2" ]] && set -x

. /functions.sh

# list of cron expressions, inputs and results 
declare -a cronnum croninput cronresult cronline nowtime waittime

cronnum=(
  "1"
  "1,2,3"
  "1-3,5"
  "5-7,6,1"
  "*"
  "5,6,2-3,*"
)

croninput=(
  "1"
  "2"
  "2"
  "4"
  "8"
  "7"
)

cronresult=(
  "1"
  "2"
  "2"
  "5"
  "8"
  "7"
)

pass=0
fail=0

for ((i=0; i< ${#cronnum[@]}; i++)); do
        ex=${cronnum[$i]}
        in=${croninput[$i]}
        re=${cronresult[$i]}
        out=$(next_cron_expression "$ex" "$in")
        if [ "$out" = "$re" ]; then
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
  #"10 2 10 * *"
)
nowtime=(
  "2018.10.10-10:01:00"
  "2018.10.10-10:00:00"
  "2018.10.10-10:00:00"
  "2018.10.10-10:01:10" # this line tests that we use the current minute, and not wait for "-10"
)
waittime=(
  "0"
  "60"
  "54000"
  "0"
)

for ((i=0; i< ${#cronline[@]}; i++)); do
        ex="${cronline[$i]}"
        in=$(date --date="${nowtime[$i]}" +"%s")
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


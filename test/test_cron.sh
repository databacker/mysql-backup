#!/bin/bash

DEBUG=${DEBUG:-0}
[[ -n "$DEBUG" && "$DEBUG" == "verbose" ]] && DEBUG=1
[[ -n "$DEBUG" && "$DEBUG" == "debug" ]] && DEBUG=2

[[ "$DEBUG" == "2" ]] && set -x

source ./_functions.sh

make_test_images
docker run --rm -e DEBUG=${DEBUG} ${BACKUP_TESTER_IMAGE} cron


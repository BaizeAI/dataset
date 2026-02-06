#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# This script holds common bash variables and utility functions.

function util::cmd_exist {
	local CMD=$(command -v ${1})
	if [[ ! -x ${CMD} ]]; then
    	return 1
	fi
	return 0
}

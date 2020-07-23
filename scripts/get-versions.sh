#!/usr/bin/env bash

# This script outputs git tags of Gitlab Sia releases greater than or equal to
# the given version in ascending semantic version order.
#
# Requirements:
#   - curl installed
#   - jq installed
#   - sort supporting -V flag (installed on modern systems by default)
#   - sed installed (installed on modern systems by default)

# Config

# Gitlab Sia repository ID taken from:
# https://gitlab.com/NebulousLabs/Sia > Project Overview > Details
sia_repo_id=7508674

# First released version to include in the builds.
# All newer released versions will be built.
export from_version=v1.3.7

# echo_version_greater_or_equal compares minimum and given semantic version
# strings.
# Inputs:
# - $1: Minimum version to compare
# - $2: Version to compare
# Output:
#   If the version to compare is greater than or equal to minimum version, the
#   function echos the version to compare otherwise it doesn't output anything.
function echo_version_greater_or_equal() {
  if [ "$(printf '%s\n' "$@" | sort -V | head -n 1)" == "$1" ]
  then
    echo $2
  fi
}
export -f echo_version_greater_or_equal

# Get git tags of Gitlab Sia releases greater than or equal to ${from_version}
# curl:             Get released Sia versions through Gitlab API.
# jq:               Parse curl response with releases.
# sed | sort | set: Sort releases in ascending order.
# xargs:            Keep only versions greater or equal to ${from_version}.

curl "https://gitlab.com/api/v4/projects/${sia_repo_id}/releases" | jq -r '.[] | .tag_name' | sed '/-/!{s/$/_/}' | sort -V | sed 's/_$//' | xargs -n 1 -I {} bash -c "echo_version_greater_or_equal ${from_version} "'$@' _ {}

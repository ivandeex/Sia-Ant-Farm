#!/usr/bin/env bash
set -e

# This script generates siad-dev binaries of the versions greater than or equal
# to the given version set in get-versions.sh script (v1.3.7) and stores them
# in target folder.
#
# Requires:
# - Sia repo to be sibling directory of this (`Sia-Ant-Farm`) repo
#   otherwise the 2nd pushd path must be updated.
# - get-versions.sh script with its own requirements

# set working dir to script location
pushd $(dirname "$0") > /dev/null

# set target folder
export target_folder=$(realpath ../upgrade-binaries)

# setup build-time vars
ldflags="-s -w -X 'gitlab.com/NebulousLabs/Sia/build.GitRevision=`git rev-parse --short HEAD`' -X 'gitlab.com/NebulousLabs/Sia/build.BuildTime=`git show -s --format=%ci HEAD`' -X 'gitlab.com/NebulousLabs/Sia/build.ReleaseTag=${rc}'"

# build_version checks out the given version of Sia Gitlab repo, builds
# siad-dev binary and stores the binary in the subfolder of the $target_folder.
# Inputs:
# - $1: Sia version (git tag) to build
function build_version {
  version=$1
  os=linux
  arch=amd64

  echo Building $version...

  # set working dir to Sia repo
  pushd ../../Sia > /dev/null
  
  # checkout the version
  git reset --hard HEAD
  git checkout $version
  
  # create workspace
  folder=$target_folder/Sia-$version-$os-$arch
  rm -rf $folder
  mkdir -p $folder

  # Checkout correct commit in merkletree repository for Sia v1.4.0
  if [[ "$version" == "v1.4.0" ]]
  then
    pushd ../merkletree > /dev/null
    git reset --hard HEAD
    git checkout bc4a11e
    popd > /dev/null
  fi

  # compile siad-dev binaries
  pkg=siad
  bin=$pkg-dev
  GOOS=${os} GOARCH=${arch} go build -a -tags 'dev debug profile netgo' -trimpath -ldflags="$ldflags" -o $folder/$bin ./cmd/$pkg

  # Checkout back to master in merkletree repository after Sia v1.4.0
  if [[ "$version" == "v1.4.0" ]]
  then
    pushd ../merkletree > /dev/null
    git reset --hard HEAD
    git checkout master
    popd > /dev/null
  fi

  popd > /dev/null
}
export -f build_version

# Build dev binaries for Sia releases and latest master.
# Use get-versions.sh to get all releases greater then or equal to the given
# release (v1.3.7).
./get-versions.sh | xargs -n 1 -I {} bash -c 'build_version "$@"' _ {}
build_version master

popd > /dev/null
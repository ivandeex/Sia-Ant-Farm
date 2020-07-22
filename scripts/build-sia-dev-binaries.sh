#!/usr/bin/env bash
set -e

# This script generates siad-dev binaries of the given versions and stores them
# in target folder.
# Requires `Sia` repo to be sibling directory of this (`Sia-Ant-Farm`) repo
# otherwise the 2nd pushd path must be updated.

# set working dir to script location
pushd $(dirname "$0") > /dev/null

# set target folder
target_folder=$(realpath ../upgrade-binaries)

# set working dir to Sia repo
pushd ../../Sia

# setup build-time vars
ldflags="-s -w -X 'gitlab.com/NebulousLabs/Sia/build.GitRevision=`git rev-parse --short HEAD`' -X 'gitlab.com/NebulousLabs/Sia/build.BuildTime=`git show -s --format=%ci HEAD`' -X 'gitlab.com/NebulousLabs/Sia/build.ReleaseTag=${rc}'"

function build {
  version=$1
  os=linux
  arch=amd64

  echo Building $version...
  # checkout the version
  git checkout $version
  # create workspace
  folder=$target_folder/Sia-$version-$os-$arch
  rm -rf $folder
  mkdir -p $folder
  # compile siad-dev binaries
  pkg=siad
  bin=$pkg-dev
  GOOS=${os} GOARCH=${arch} go build -a -tags 'dev' -trimpath -ldflags="$ldflags" -o $folder/$bin ./cmd/$pkg
}

# Build dev binaries.
# TODO: return back: "v1.4.0" after it is tagged to the correct commit
for version in "v1.3.7" "v1.4.1" "v1.4.1.1" "v1.4.1.2" "v1.4.2.0" "v1.4.3" "v1.4.4" "v1.4.5" "v1.4.6" "v1.4.7" "v1.4.8" "v1.4.10" "v1.4.11"
do
  build $version
done

popd > /dev/null
popd > /dev/null
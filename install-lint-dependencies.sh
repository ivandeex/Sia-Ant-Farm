#!/usr/bin/env bash
set -e

if ! [ -x "$(command -v golangci-lint)" ]; then
  echo "Installing golangci-lint..."
  echo "xxx: $GOPATH"
  go env GOPATH
  echo "xxx: $PATH"
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b "$(go env GOPATH)"/bin v1.24.0
fi

if ! [ -x "$(command -v codespell)" ]; then
  echo "Installing codespell..."
  if ! [ -x "$(command -v pip3)" ]; then
    if ! [ -x "$(command -v sudo)" ]; then
      apt-get update
      apt install -y python3-pip
    else
      sudo apt-get update
      sudo apt install -y python3-pip
    fi
  fi
  pip3 install codespell
fi

if ! [ -x "$(command -v analyze)" ]; then
  echo "Installing analyze..."
  go get gitlab.com/NebulousLabs/analyze
fi

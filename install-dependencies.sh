#!/usr/bin/env bash
set -e

if ! [ -x "$(command -v golangci-lint)" ]; then
  echo "Installing golangci-lint..."
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b "$(go env GOPATH)"/bin v1.24.0
fi

if ! [ -x "$(command -v analyze)" ]; then
  echo "Installing analyze..."
  go get gitlab.com/NebulousLabs/analyze
fi

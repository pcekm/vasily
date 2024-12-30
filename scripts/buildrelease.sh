#!/bin/bash

set -o errexit

go build \
    "$@" \
    -ldflags="-X main.Version=$(git describe --tags --dirty)" \
    -o vasily \
    ./cmd

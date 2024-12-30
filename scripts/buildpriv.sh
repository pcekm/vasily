#!/bin/bash

# Builds a setuid root version for testing purposes.

set -o errexit

go build "$@" -o vasily ./cmd
sudo chown 0:0 vasily
sudo chmod u+s vasily

#/bin/bash

# Builds a setuid root version.

set errexit

go build
sudo chown 0:0 graphping
sudo chmod u+s graphping

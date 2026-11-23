#!/bin/bash

set -e

cd ..

if ! test -d bin; then echo "Creating bin folder"; fi
mkdir -p bin

go build -o bin/service ./cmd/service
echo "Finished building service"

go build -o bin/lb ./cmd/lb
echo "Finished building lb"

go build -o bin/agent ./cmd/agent
echo "Finished building agent"

echo "All nodes were built!"
#!/bin/bash

set -e

mkdir -p bin

go build -o bin/service ./cmd/service
echo "Finished building service"

go build -o bin/lb ./cmd/lb
echo "Finished building lb"

go build -o bin/agent ./cmd/agent
echo "Finished building agent"

echo "All nodes were built!"
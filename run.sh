#!/usr/bin/env bash

# Godex Engine Starter Script
# Usage: ./run.sh

# Get the script's directory and cd into it to handle execution from anywhere
DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$DIR"

# Run the Godex CLI
go run cmd/godex/main.go "$@"

#!/usr/bin/env bash
set -euo pipefail
mkdir -p cmd/bin
go build -trimpath -ldflags="-s -w" -o cmd/bin/main .
echo "built: cmd/bin/main"

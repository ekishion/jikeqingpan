#!/usr/bin/env bash
set -euo pipefail
mkdir -p cmd/bin
version=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
go build -trimpath -ldflags="-s -w -X main.version=${version}" -o cmd/bin/main ./cmd/jikeqingpan
echo "built: cmd/bin/main (version: ${version})"

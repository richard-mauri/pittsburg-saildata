#!/usr/bin/env bash
set -Eeuo pipefail
cd "$(dirname "$0")"
[[ $# -eq 0 ]] || { echo 'Usage: ./devbuild.sh' >&2; exit 2; }
go test -count=1 .
go build -o sailing-go .

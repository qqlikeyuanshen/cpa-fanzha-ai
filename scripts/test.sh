#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
export CGO_ENABLED=1
go mod download
go mod verify
unformatted="$(gofmt -l ./*.go)"
[[ -z "$unformatted" ]] || { printf 'Run gofmt on:\n%s\n' "$unformatted" >&2; exit 1; }
go test -mod=readonly -race -count=1 -v ./...
go vet -mod=readonly ./...
node scripts/check-pages.cjs
python3 scripts/scan-secrets.py

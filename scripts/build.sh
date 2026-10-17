#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
if [[ "$(go env GOOS)" != linux ]]; then
  echo 'Build in Linux (or WSL/Docker with a Linux C compiler); a Windows DLL is not a Linux plugin.' >&2
  exit 1
fi
export CGO_ENABLED=1
arch="$(go env GOARCH)"
case "$arch" in amd64|arm64) ;; *) echo "Unsupported target: $arch" >&2; exit 1 ;; esac
command -v "${CC:-gcc}" >/dev/null
version="$(sed -n 's/^const version = "\([^"]*\)"/\1/p' plugin.go)"
[[ -n "$version" ]] || { echo 'Unable to detect plugin version' >&2; exit 1; }
mkdir -p dist
go mod download
go mod verify
go build -mod=readonly -trimpath -buildmode=c-shared -o "dist/fanzha-v${version}-linux-${arch}.so" .
(cd dist && sha256sum "fanzha-v${version}-linux-${arch}.so" > "fanzha-v${version}-linux-${arch}.so.sha256")
printf 'Built %s\n' "dist/fanzha-v${version}-linux-${arch}.so"

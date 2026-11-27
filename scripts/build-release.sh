#!/bin/sh
# Cross-compiles clip-patch and cpatch for the supported platforms into dist/.
# Usage: scripts/build-release.sh 0.1.0
set -eu
VERSION="${1:-0.1.0}"
rm -rf dist && mkdir -p dist
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
  os="${target%/*}"; arch="${target#*/}"
  ext=""; [ "$os" = "windows" ] && ext=".exe"
  for cmd in clip-patch cpatch; do
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o "dist/${cmd}_${VERSION}_${os}_${arch}${ext}" "./cmd/${cmd}"
  done
done
(cd dist && (sha256sum * 2>/dev/null || shasum -a 256 *) > SHA256SUMS.txt)
ls -l dist

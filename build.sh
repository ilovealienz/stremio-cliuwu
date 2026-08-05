#!/usr/bin/env nix-shell
#!nix-shell -i bash -p go
#
# Build stremio-cliuwu without installing anything.
#
# Go is fetched into the nix store for the duration of this script and is
# never added to your profile or system config. `nix-collect-garbage` will
# reclaim it later like any other unreferenced store path.
#
#   ./build.sh              build ./stremio-cliuwu
#   ./build.sh run          build and run
#   ./build.sh static       fully static binary (CGO off)
#   ./build.sh all          linux + windows, amd64 + arm64, into ./dist
#   ./build.sh clean

set -euo pipefail

BINARY="stremio-cliuwu"
VERSION="0.2.0"
LDFLAGS="-s -w -X main.version=${VERSION}"

cd "$(dirname "$0")"

tidy() {
  echo "  ⟳ go mod tidy"
  go mod tidy
}

build() {
  tidy
  echo "  ⟳ building ${BINARY}"
  CGO_ENABLED=0 go build -ldflags="${LDFLAGS}" -o "${BINARY}" .
  echo "  ✓ ./${BINARY}  ($(du -h "${BINARY}" | cut -f1))"
}

case "${1:-build}" in

  build|"")
    build
    ;;

  run)
    build
    echo
    exec "./${BINARY}"
    ;;

  static)
    tidy
    CGO_ENABLED=0 go build -trimpath -ldflags="${LDFLAGS} -extldflags=-static" -o "${BINARY}" .
    echo "  ✓ ./${BINARY}"
    file "${BINARY}" 2>/dev/null || true
    ;;

  all)
    tidy
    mkdir -p dist
    for target in linux/amd64 linux/arm64 windows/amd64 darwin/arm64; do
      os="${target%/*}"
      arch="${target#*/}"
      out="dist/${BINARY}-${os}-${arch}"
      [ "$os" = windows ] && out="${out}.exe"
      echo "  ⟳ ${os}/${arch}"
      CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
        go build -trimpath -ldflags="${LDFLAGS}" -o "$out" .
    done
    echo "  ✓ dist/"
    ls -lh dist/
    ;;

  clean)
    rm -f "${BINARY}"
    rm -rf dist/
    echo "  ✓ cleaned"
    ;;

  *)
    echo "usage: $0 [build|run|static|all|clean]" >&2
    exit 1
    ;;
esac

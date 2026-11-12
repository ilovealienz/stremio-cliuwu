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

cd "$(dirname "$0")"

# Derived from git, not hardcoded. A tagged commit is its tag ("0.6.9.1").
# Anything else is ahead of the newest release ("0.6.9.1-dev-abc1234"): the
# release tags sit on main's squash commits, outside the working branch's
# history, so describe alone would name an old tag. Outside a repo, "dev".
# Override with VERSION=1.2.3 ./build.sh if you need to.
if [ -z "${VERSION:-}" ]; then
  if VERSION="$(git describe --tags --exact-match --dirty 2>/dev/null)"; then :
  elif latest="$(git describe --tags "$(git rev-list --tags --max-count=1 2>/dev/null)" 2>/dev/null)"; then
    VERSION="${latest}-dev-$(git rev-parse --short HEAD)"
    git diff --quiet HEAD 2>/dev/null || VERSION="${VERSION}-dirty"
  else
    VERSION=dev
  fi
fi
VERSION="${VERSION#v}"
LDFLAGS="-s -w -X main.version=${VERSION}"


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

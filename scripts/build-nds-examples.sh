#!/usr/bin/env bash
# Fetches devkitPro nds-examples and builds the subset used by the e2e tests
# into test/roms. Uses a native devkitARM when $DEVKITPRO/devkitARM exists,
# otherwise the devkitpro/devkitarm Docker image.
#
# Usage: scripts/build-nds-examples.sh [--force]
set -euo pipefail

REPO=https://github.com/devkitPro/nds-examples
TAG=v20241110
EXAMPLES="hello_world Graphics/Printing Graphics/Backgrounds Graphics/Sprites input time"

force=0
for arg in "$@"; do
  case "$arg" in
    --force) force=1 ;;
    *) echo "usage: $0 [--force]" >&2; exit 2 ;;
  esac
done

root="$(cd "$(dirname "$0")/.." && pwd)"
out="$root/test/roms"
src="$out/nds-examples"

if [[ $force -eq 0 && -f "$out/hello_world.nds" ]]; then
  echo "test ROMs already built in $out (use --force to rebuild)"
  exit 0
fi

mkdir -p "$out"
if [[ ! -d "$src/.git" ]]; then
  rm -rf "$src"
  git clone --quiet --depth 1 --branch "$TAG" "$REPO" "$src"
fi

# make is retried because the toolchain occasionally segfaults under amd64
# emulation on arm64 hosts; the rebuild is incremental.
build='for d in '"$EXAMPLES"'; do
  (cd "$d" && for i in 1 2 3; do make -k && break; done)
done'

if [[ -n "${DEVKITPRO:-}" && -d "$DEVKITPRO/devkitARM" ]]; then
  echo "building with native devkitPro at $DEVKITPRO"
  (cd "$src" && DEVKITARM="${DEVKITARM:-$DEVKITPRO/devkitARM}" bash -c "$build")
elif command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  echo "building with Docker image devkitpro/devkitarm:latest"
  docker run --rm --platform linux/amd64 -v "$src:/src" -w /src \
    devkitpro/devkitarm:latest bash -c "$build"
else
  echo "error: no devkitARM toolchain found. Install devkitPro and set DEVKITPRO," >&2
  echo "or install and start Docker to use the devkitpro/devkitarm image." >&2
  exit 1
fi

n=0
for d in $EXAMPLES; do
  while IFS= read -r -d '' f; do
    cp -f "$f" "$out/"
    n=$((n + 1))
  done < <(find "$src/$d" -name '*.nds' -print0)
done
if [[ ! -f "$out/hello_world.nds" ]]; then
  echo "error: build did not produce hello_world.nds" >&2
  exit 1
fi
echo "copied $n ROMs into $out"

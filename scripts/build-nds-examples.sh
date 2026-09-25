#!/usr/bin/env bash
# Builds a subset of devkitPro nds-examples into references/nds-examples/bin
# using the devkitpro/devkitarm Docker image.
set -euo pipefail
cd "$(dirname "$0")/../references/nds-examples"
docker run --rm --platform linux/amd64 -v "$PWD:/src" -w /src devkitpro/devkitarm:latest bash -c '
  for d in hello_world Graphics/Printing Graphics/Backgrounds Graphics/Sprites input time; do
    (cd "$d" && make -k)
  done
  mkdir -p bin
  find . -path ./bin -prune -o -name "*.nds" -exec cp -f {} bin \;'
